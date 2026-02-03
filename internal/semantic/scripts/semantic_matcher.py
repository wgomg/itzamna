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


def process_single_request(request, model, embedding_dim, config):
    """Process one request and return response."""
    start_time = time.time()

    text = request.get("text", "")
    existing_tags = request.get("existing_tags", [])

    top_n = config.get("top_n", 15)
    min_similarity = float(config.get("min_similarity", 0.2))
    normalize = config.get("normalize_embeddings", True)

    if not text or not isinstance(text, str):
        return create_error_response("Invalid or empty text", start_time)

    if not isinstance(existing_tags, list):
        return create_error_response("existing_tags must be a list", start_time)

    existing_tags = [str(tag) for tag in existing_tags]

    try:
        doc_embedding = model.encode([text], normalize_embeddings=normalize)[0]

        batch_size = 100
        similarities = []
        for i in range(0, len(existing_tags), batch_size):
            batch_tags = existing_tags[i:i + batch_size]
            if batch_tags:
                tag_embeddings = model.encode(batch_tags, normalize_embeddings=normalize)

                for j, tag in enumerate(batch_tags):
                    similarity = float(np.dot(doc_embedding, tag_embeddings[j]))
                    similarities.append({"tag": tag, "score": similarity})

        similarities.sort(key=lambda x: x["score"], reverse=True)

        filtered = [s for s in similarities if s["score"] >= min_similarity]

        suggested_tags = [s["tag"] for s in filtered[:top_n]]
        top_similarities = filtered[:top_n]

        processing_time = (time.time() - start_time) * 1000

        debug_info = {
            "embedding_dimension": embedding_dim,
            "processing_time_ms": round(processing_time, 2),
            "total_tags_considered": len(existing_tags),
            "tags_above_threshold": len(filtered),
            "model_loaded": True,
            "model_name": str(model),
            "text_length_chars": len(text),
            "text_estimated_tokens": len(text) // 4,
        }

        return {
            "suggested_tags": suggested_tags,
            "similarities": top_similarities,
            "debug_info": debug_info,
            "error": None,
        }

    except Exception as e:
        return create_error_response(
            f"Processing error: {str(e)}", start_time, traceback.format_exc()
        )


def create_error_response(error_msg, start_time, config, traceback_str=None):
    """Create an error response."""
    processing_time = (time.time() - start_time) * 1000

    debug_info = {
        "error": error_msg,
        "model_loaded": False,
        "processing_time_ms": round(processing_time, 2),
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

    ready_msg = {"status": "ready", "embedding_dim": embedding_dim}
    print(json.dumps(ready_msg), flush=True)

    print("Ready. Send JSON requests to stdin.", file=sys.stderr)

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
            response = process_single_request(request, model, embedding_dim, config)

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
