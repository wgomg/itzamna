#!/usr/bin/env python3
"""
Simple Semantic Tag Matcher for Paperless-ngx

Python script that provides semantic tag matching functionality to Go service
via stdin/stdout JSON communication. Uses sentence-transformers with
all-MiniLM-L6-v2 model.

Communication: JSON over stdin/stdout, one object per line
"""

import json
import sys
import time
import traceback

import numpy as np

try:
    from sentence_transformers import SentenceTransformer

    HAS_DEPENDENCIES = True
except ImportError:
    HAS_DEPENDENCIES = False
    print(
        "ERROR: Missing sentence-transformers. Install with: pip install sentence-transformers",
        file=sys.stderr,
    )
    sys.exit(1)


class EmbeddingCache:
    def __init__(self, model, cfg):
        self.model = model
        self.cache = {}
        self.hits = 0
        self.misses = 0
        self.cfg = cfg

    def get_embeddings(self, tags):
        """Get embeddings for tags, computes missing ones."""
        cached_embeddings = []
        new_tags = []
        request_hits = 0
        request_misses = 0

        for tag in tags:
            if tag in self.cache:
                cached_embeddings.append(self.cache[tag])
                request_hits += 1
                self.hits += 1
            else:
                new_tags.append(tag)
                request_misses += 1
                self.misses += 1

        if new_tags:
            new_embeddings = self.model.encode(
                new_tags, normalize_embeddings=self.cfg.get("normalize_embeddings", True)
            )
            for tag, embedding in zip(new_tags, new_embeddings):
                self.cache[tag] = embedding
        else:
            new_embeddings = np.array([])

        if cached_embeddings and new_embeddings.size > 0:
            all_embeddings = np.vstack([cached_embeddings, new_embeddings])
        elif cached_embeddings:
            all_embeddings = np.array(cached_embeddings)
        else:
            all_embeddings = new_embeddings

        stats = {
            "cache_size": len(self.cache),
            "total_hits": self.hits,
            "total_misses": self.misses,
            "total_hit_rate": (
                self.hits / (self.hits + self.misses)
                if (self.hits + self.misses) > 0
                else 0
            ),
            "request_hits": request_hits,
            "request_misses": request_misses,
            "request_hit_rate": (
                request_hits / (request_hits + request_misses)
                if (request_hits + request_misses) > 0
                else 0
            ),
        }

        return all_embeddings, new_tags, stats


def load_model(model_name):
    """Load the sentence transformer model."""
    print(f"Loading model: {model_name}", file=sys.stderr)
    try:
        model = SentenceTransformer(model_name)
        test_embed = model.encode(["test"], normalize_embeddings=True)
        embedding_dim = test_embed.shape[1]
        print(f"Model loaded. Embedding dimension: {embedding_dim}", file=sys.stderr)
        return model, embedding_dim
    except Exception as e:
        print(f"ERROR: Failed to load model: {e}", file=sys.stderr)
        return None, 0


def process_single_request(request, model, embedding_cache, config):
    """Process one request and return response."""
    start_time = time.time()

    text = request.get("text", "")
    existing_tags = request.get("existing_tags", [])

    if not text or not isinstance(text, str):
        return create_error_response("Invalid or empty text", start_time)

    if not isinstance(existing_tags, list):
        return create_error_response("existing_tags must be a list", start_time)

    top_n = config.get("top_n", 15)
    min_similarity = float(config.get("min_similarity", 0.2))
    normalize = config.get("normalize_embeddings", True)

    all_embeddings, newly_cached_tags, stats = embedding_cache.get_embeddings(
        existing_tags
    )
    doc_embedding = model.encode([text], normalize_embeddings=normalize)[0]

    similarities = []
    for i, tag in enumerate(existing_tags):
        similarity = float(np.dot(doc_embedding, all_embeddings[i]))
        similarities.append({"tag": tag, "score": similarity})

    similarities.sort(key=lambda x: x["score"], reverse=True)
    filtered = [s for s in similarities if s["score"] >= min_similarity]
    suggested_tags = [s["tag"] for s in filtered[:top_n]]
    top_similarities = filtered[:top_n]

    processing_time = (time.time() - start_time) * 1000

    debug_info = {
        "embedding_dimension": doc_embedding.shape[0],
        "processing_time_ms": round(processing_time),
        "total_tags_considered": len(existing_tags),
        "tags_above_threshold": len(filtered),
        "model_loaded": True,
        "model_name": str(model),
        "text_length_chars": len(text),
        "text_estimated_tokens": len(text) // 4,
        "cache_stats": stats,
        "newly_cached_tags": len(newly_cached_tags),
    }

    return {
        "suggested_tags": suggested_tags,
        "similarities": top_similarities,
        "debug_info": debug_info,
        "error": None,
    }


def create_error_response(error_msg, start_time, config, traceback_str=None):
    """Create an error response."""
    processing_time = (time.time() - start_time) * 1000

    debug_info = {
        "error": error_msg,
        "model_loaded": False,
        "processing_time_ms": round(processing_time),
        "model_name": config.get("model_name", "unknown"),
        "embedding_dimension": 0,
    }

    if traceback_str:
        debug_info["traceback"] = traceback_str

    return {
        "suggested_tags": [],
        "similarities": [],
        "debug_info": debug_info,
        "error": error_msg,
    }


def main():
    """Main function - simple stdin/stdout loop."""
    print("Semantic Tag Matcher starting...", file=sys.stderr)

    config_line = sys.stdin.readline()
    if not config_line:
        print("ERROR: No config received", file=sys.stderr)
        sys.exit(1)

    try:
        config = json.loads(config_line.strip())
    except json.JSONDecodeError as e:
        print(f"ERROR: Invalid config JSON: {e}", file=sys.stderr)
        sys.exit(1)

    model_name = config.get("model_name", "all-MiniLM-L6-v2")
    model, embedding_dim = load_model(model_name)
    if not model:
        sys.exit(1)

    embedding_cache = EmbeddingCache(model, config)

    ready_msg = {"status": "ready", "embedding_dim": embedding_dim}
    print(json.dumps(ready_msg), flush=True)

    print("Cache initialized. Ready for requests.", file=sys.stderr)

    request_count = 0
    while True:
        try:
            line = sys.stdin.readline()
            if not line:  # EOF
                print(f"EOF. Processed {request_count} requests.", file=sys.stderr)
                break

            line = line.strip()
            if not line:
                continue

            try:
                request = json.loads(line)
            except json.JSONDecodeError as e:
                error_resp = create_error_response(f"Invalid JSON: {str(e)}", time.time())
                print(json.dumps(error_resp), flush=True)
                continue

            request_count += 1
            response = process_single_request(request, model, embedding_cache, config)

            print(json.dumps(response), flush=True)

        except KeyboardInterrupt:
            print("\nInterrupted. Shutting down.", file=sys.stderr)
            break
        except Exception as e:
            error_resp = create_error_response(
                f"Unexpected error: {str(e)}", time.time(), config, traceback.format_exc()
            )
            print(json.dumps(error_resp), flush=True)
            print(f"ERROR in main loop: {e}", file=sys.stderr)


if __name__ == "__main__":
    main()
