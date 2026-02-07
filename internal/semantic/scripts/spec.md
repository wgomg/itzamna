# Python Semantic Matcher - Integration Specification

## Overview

Python script that provides semantic tag matching functionality to Go service via subprocess communication. Uses `sentence-transformers` with configurable models to generate embeddings and calculate semantic similarities with intelligent caching and warm-up optimization.

## Available Models

### Model Selection Guide

| Model Name                                  | Languages     | Embedding Dim | Use Case                  | Performance           | Memory |
| ------------------------------------------- | ------------- | ------------- | ------------------------- | --------------------- | ------ |
| **`all-MiniLM-L6-v2`** (Default)            | English-only  | 384           | English documents only    | Fast, lightweight     | ~90MB  |
| **`paraphrase-multilingual-MiniLM-L12-v2`** | 50+ languages | 384           | Multilingual documents    | Good multilingual     | ~120MB |
| **`paraphrase-multilingual-mpnet-base-v2`** | 50+ languages | 768           | High-quality multilingual | Best accuracy, slower | ~420MB |
| **`distiluse-base-multilingual-cased-v2`**  | 50+ languages | 512           | Balanced multilingual     | Good balance          | ~250MB |

### Key Considerations

1. **Language Support**:
   - `all-MiniLM-L6-v2`: English only (fastest)
   - Multilingual models: Support 50+ languages including English, Spanish, French, German, Chinese, Arabic, etc.

2. **Performance Trade-offs**:
   - **Dimension**: Higher dimensions (768) = better accuracy but slower
   - **Speed**: Lower dimensions (384) = faster inference
   - **Memory**: Larger models require more RAM

3. **Recommendations**:
   - **English-only documents**: Use `all-MiniLM-L6-v2` (default)
   - **Multilingual documents**: Use `paraphrase-multilingual-MiniLM-L12-v2`
   - **Highest accuracy needed**: Use `paraphrase-multilingual-mpnet-base-v2`
   - **Balanced approach**: Use `distiluse-base-multilingual-cased-v2`

## Communication Protocol

### Transport

- **Method**: Subprocess with stdin/stdout pipes
- **Process Lifecycle**: Persistent (model loaded once at startup)
- **Message Delimiter**: Newline (`\n`) between JSON messages
- **Encoding**: UTF-8

### Startup Sequence

1. **Process Start**: Go launches Python script
2. **Configuration**: Go sends config JSON as first line to stdin
3. **Model Loading**: Python loads model, initializes embedding cache, sends ready message to stdout
4. **Ready State**: Python waits for requests on stdin
5. **Cache Warm-up**: Go sequentially sends warm-up requests to pre-load tag embeddings

### Startup Configuration (Go → Python)

**First message sent after process start:**

```json
{
  "model_name": "all-MiniLM-L6-v2",
  "top_n": 15,
  "min_similarity": 0.2,
  "normalize_embeddings": true
}
```

#### Configuration Fields

| Field                  | Type    | Required | Default            | Description                                   |
| ---------------------- | ------- | -------- | ------------------ | --------------------------------------------- |
| `model_name`           | string  | No       | `all-MiniLM-L6-v2` | Model identifier (see Available Models above) |
| `top_n`                | integer | No       | 15                 | Maximum number of tags to return              |
| `min_similarity`       | float   | No       | 0.2                | Minimum cosine similarity threshold (0.0-1.0) |
| `normalize_embeddings` | boolean | No       | true               | L2 normalize embeddings before similarity     |

### Ready Message (Python → Go)

**After loading model successfully:**

```json
{
  "status": "ready",
  "embedding_dim": 384
}
```

#### Ready Message Fields

| Field           | Type    | Description                       |
| --------------- | ------- | --------------------------------- |
| `status`        | string  | Always "ready" on successful load |
| `embedding_dim` | integer | Vector dimension of loaded model  |

### Request Format (Go → Python)

**Subsequent messages for processing:**

```json
{
  "text": "The reduced document content...",
  "new_tags": ["invoice", "receipt", "tax", "2024"]
}
```

#### Request Fields

| Field      | Type          | Required | Description                                          |
| ---------- | ------------- | -------- | ---------------------------------------------------- |
| `text`     | string        | Yes      | Document text (full or reduced) to analyze           |
| `new_tags` | array[string] | Yes      | New tags to compute embeddings for (batch operation) |

**Note**: The field is named `new_tags` (not `existing_tags`) to reflect the batch cache operation pattern where only missing tags are sent.

### Response Format (Python → Go)

```json
{
  "suggested_tags": ["invoice", "tax"],
  "similarities": [
    { "tag": "invoice", "score": 0.87 },
    { "tag": "tax", "score": 0.72 },
    { "tag": "receipt", "score": 0.45 }
  ],
  "debug_info": {
    "embedding_dimension": 384,
    "processing_time_ms": 125,
    "total_tags_considered": 42,
    "tags_above_threshold": 2,
    "model_loaded": true,
    "model_name": "all-MiniLM-L6-v2",
    "text_length_chars": 1250,
    "text_estimated_tokens": 312
  },
  "error": null
}
```

#### Success Response Fields

| Field                              | Type          | Description                                                |
| ---------------------------------- | ------------- | ---------------------------------------------------------- |
| `suggested_tags`                   | array[string] | Top N tags meeting similarity threshold, sorted descending |
| `similarities`                     | array[object] | Top N tags with similarity scores                          |
| `similarities[].tag`               | string        | Tag name                                                   |
| `similarities[].score`             | float         | Cosine similarity score (0.0-1.0)                          |
| `debug_info`                       | object        | Diagnostic information for monitoring/debugging            |
| `debug_info.embedding_dimension`   | integer       | Vector dimension (model-specific)                          |
| `debug_info.processing_time_ms`    | integer       | Total processing time in milliseconds (rounded)            |
| `debug_info.total_tags_considered` | integer       | Number of tags processed (cached + newly computed)         |
| `debug_info.tags_above_threshold`  | integer       | Number of tags meeting min_similarity                      |
| `debug_info.model_loaded`          | boolean       | Model successfully loaded                                  |
| `debug_info.model_name`            | string        | Model identifier used                                      |
| `debug_info.text_length_chars`     | integer       | Character count of input text                              |
| `debug_info.text_estimated_tokens` | integer       | Estimated token count (chars ÷ 4)                          |
| `error`                            | null          | Always null for successful responses                       |

### Error Response Format

```json
{
  "suggested_tags": [],
  "similarities": [],
  "debug_info": {
    "error": "Model failed to load: File not found",
    "model_loaded": false,
    "processing_time_ms": 10,
    "model_name": "all-MiniLM-L6-v2",
    "embedding_dimension": 0
  },
  "error": "Model failed to load: File not found"
}
```

#### Error Response Fields

| Field                            | Type    | Description                                  |
| -------------------------------- | ------- | -------------------------------------------- |
| `suggested_tags`                 | array   | Empty array                                  |
| `similarities`                   | array   | Empty array                                  |
| `debug_info`                     | object  | Error details                                |
| `debug_info.error`               | string  | Human-readable error message                 |
| `debug_info.model_loaded`        | boolean | Model state at error time                    |
| `debug_info.processing_time_ms`  | integer | Time spent before error                      |
| `debug_info.model_name`          | string  | Model name that failed to load               |
| `debug_info.embedding_dimension` | integer | 0 on error                                   |
| `error`                          | string  | Same as debug_info.error (convenience field) |

**Note**: Traceback is only printed to stderr, not included in JSON response.

## Python Script Requirements

### Dependencies

```txt
sentence-transformers>=2.2.2
torch>=2.0.0
numpy>=1.21.0
```

### Script Behavior

1. **Startup**: Read configuration from first stdin message, load model once
2. **Cache Initialization**: Create embedding cache for tag embeddings
3. **Ready Signal**: Send `{"status": "ready", "embedding_dim": N}` to stdout
4. **Input Reading**: Read JSON lines from stdin (blocking)
5. **Processing**:
   - Generate embedding for input text
   - Get embeddings for new tags (cached or compute new)
   - Calculate cosine similarities
   - Apply threshold and select top N
6. **Output**: Write JSON response to stdout, flush immediately
7. **Loop**: Continue until stdin closes or EOF received
8. **Error Handling**: Catch all exceptions, return structured error

### Embedding Cache Implementation

**Cache Structure**:

- In-memory dictionary: `tag_name → embedding_vector`
- Per-worker cache (not shared between workers)
- Automatic warm-up at startup via sequential requests

**Cache Benefits**:

- **First request after warm-up**: ~20-50ms (embeddings already cached)
- **Without warm-up**: ~1-2 seconds (computes all tag embeddings)
- **Performance**: 10x speedup after initial tag embedding
- **Memory**: Cache lives for Python worker lifetime

**Cache Warm-up Process**:

1. Go service fetches all Paperless tags at startup
2. Sequentially sends warm-up requests to each Python worker
3. Each worker computes and caches embeddings for all tags
4. Workers ready for optimal first-request performance

## Go Integration Pattern

### Configuration

```ini
# Environment Variables (Go Side)
SEMANTIC_MODEL_NAME=all-MiniLM-L6-v2  # or multilingual model
SEMANTIC_TOP_N=15
SEMANTIC_MIN_SIMILARITY=0.2
SEMANTIC_WORKER_COUNT=2  # auto-calculated if not specified
```

### Process Management

1. **Startup**: Launch Python processes when Go service starts
2. **Configuration**: Send config as first message after process start
3. **Ready Wait**: Wait for ready message with embedding dimension
4. **Cache Warm-up**: Sequentially send warm-up requests to pre-load embeddings
5. **Health Check**: Send test request on startup, restart if fails
6. **Timeout**: Set 10-second timeout per request (configurable)
7. **Restart**: If Python crashes, restart with exponential backoff
8. **Shutdown**: Send EOF, wait for graceful exit

### Worker Pool Architecture

**Key Features**:

- **Auto-scaled workers**: Based on CPU cores and available memory
- **Task queue**: 100-task buffer for handling bursts
- **Thread-safe workers**: Each worker has mutex protection
- **Sequential warm-up**: Workers warmed up sequentially to prevent CPU spikes
- **Blocking initialization**: Service waits for all workers to be ready before accepting requests

**Worker Count Calculation**:

```go
workersByCPU = min(cpuCores, 6)
workersByMemory = max(min(usableMemoryMB/modelMemoryMB, 6), 1)
workerCount = min(max(min(workersByMemory, workersByCPU), 1), 6)
```

**Cache Warm-up Implementation**:

```go
// 1. Fetch all tags from Paperless
tags, err := paperlessClient.GetTags(warmReqId)

// 2. Pre-load into Go cache
cachedTags := tagsCache.GetMissingAndAdd(initialTags)

// 3. Sequentially warm up each Python worker
for i := 0; i < cfg.Semantic.WorkerCount; i++ {
    _, err := semanticMatcher.GetTagSuggestions("dummy", cachedTags, workerReqId)
}
```

## Performance Characteristics

| Model                                   | Embedding Time | Memory Usage | Throughput      | Cache Performance |
| --------------------------------------- | -------------- | ------------ | --------------- | ----------------- |
| `all-MiniLM-L6-v2`                      | ~50-100ms      | ~90MB        | ~10-20 docs/sec | 90%+ hit rate     |
| `paraphrase-multilingual-MiniLM-L12-v2` | ~100-200ms     | ~120MB       | ~5-10 docs/sec  | 90%+ hit rate     |
| `paraphrase-multilingual-mpnet-base-v2` | ~200-400ms     | ~420MB       | ~2-5 docs/sec   | 90%+ hit rate     |
| `distiluse-base-multilingual-cased-v2`  | ~150-300ms     | ~250MB       | ~3-7 docs/sec   | 90%+ hit rate     |

**Startup Performance**:

- **With Cache Warm-up**: Additional 1-2 seconds startup time, first request ~20-50ms
- **Without Cache Warm-up**: Faster startup, first request ~1-2 seconds
- **CPU Usage**: Sequential warm-up prevents spikes during initialization

**Note**: Throughput assumes batch size of 100 tags and typical document length. Cache hit rates assume stable tag pool after warm-up.

## Model Switching Considerations

### When to Switch Models

1. **From English to Multilingual**:
   - When processing non-English documents
   - When tag pool contains non-English terms
   - For international deployments

2. **Performance Optimization**:
   - Use smaller models for faster processing
   - Use larger models for higher accuracy
   - Consider document volume and latency requirements

### Migration Steps

1. Update Go configuration with new model name
2. Restart service (Python process reloads with new model)
3. Cache will be rebuilt with new model embeddings during warm-up
4. Monitor similarity scores may differ between models
5. Adjust `min_similarity` threshold if needed

## Testing Protocol

### Model-Specific Test Cases

1. **English Model** (`all-MiniLM-L6-v2`):
   - Test with English documents only
   - Verify performance metrics
   - Check memory usage
   - Validate cache warm-up process

2. **Multilingual Models**:
   - Test with documents in different languages
   - Verify cross-language similarity works
   - Check embedding dimensions match spec
   - Test cache performance with multilingual tags

3. **Performance Tests**:
   - Measure embedding time per model
   - Monitor memory usage
   - Test with large tag pools (1000+ tags)
   - Validate cache hit rates improve after warm-up

### Validation Steps

1. Start Python script manually, send config then test JSON
2. Verify JSON response format matches spec
3. Test with Go integration, check error handling
4. Load test with concurrent requests
5. Verify memory usage doesn't leak
6. Monitor cache performance over multiple requests
7. Test warm-up sequence and first-request performance

## Directory Structure

```
scripts/
├── semantic_matcher.py          # Main Python script with caching
├── requirements.txt             # Python dependencies
├── spec.md                      # This specification
└── models/                      # Model cache (auto-created)
    ├── all-MiniLM-L6-v2/        # Downloaded by sentence-transformers
    ├── paraphrase-multilingual-MiniLM-L12-v2/
    └── paraphrase-multilingual-mpnet-base-v2/
```

**Note**: Models are automatically downloaded to `~/.cache/torch/sentence_transformers/` on first use.

## Error Recovery Scenarios

| Scenario               | Action                                    |
| ---------------------- | ----------------------------------------- |
| Python process crashes | Go restarts process, retries request      |
| Model download fails   | Return error, suggest manual download     |
| Out of memory          | Return error, suggest reducing batch size |
| Timeout exceeded       | Kill process, restart, return error       |
| Invalid input JSON     | Return error with parsing details         |
| Empty text input       | Return error "Invalid or empty text"      |
| Cache warm-up failure  | Continue with cold cache, log warning     |

## Versioning

- **API Version**: 1.3.0
- **Backwards Compatibility**: Additive changes only (new optional fields)
- **Breaking Changes**: Increment major version, update Go integration

## Security Considerations

1. **Input Validation**: Python script validates JSON structure
2. **Resource Limits**: Set timeout to prevent hanging
3. **Path Safety**: Use absolute paths for model loading
4. **Error Messages**: Don't expose system details in production
5. **Stderr Output**: Debug info goes to stderr, not included in responses
6. **Cache Isolation**: Each worker has separate cache, no shared state

## Monitoring Metrics

Go service should track:

- Python process uptime
- Average processing time per model
- Success/error rate
- Tags suggested per document
- Model load time on startup
- Memory usage per model
- Embedding dimension per model
- **Cache warm-up time**: Time spent warming up workers
- **First request latency**: With and without cache warm-up
- **Worker readiness**: Time to initialize all workers

## Future Extensions

1. **Shared Cache**: Cache shared between Python workers
2. **Disk Persistence**: Save cache to disk for faster startup
3. **Batch Processing**: Accept multiple texts in single request
4. **Model Ensemble**: Combine multiple models for better accuracy
5. **GPU Support**: Optional GPU acceleration flag
6. **Health Endpoint**: HTTP health check for Python process
7. **Dynamic Model Loading**: Switch models without restarting process
8. Cache TTL\*\*: Time-based invalidation for stale embeddings
9. **Adaptive Warm-up**: Smart warm-up based on tag count and usage patterns

---

_Last Updated: 2026-02-05_

_Spec Version: 1.4.0_

_Changes: Added cache warm-up specification, updated request format to new_tags for batch operations, added performance characteristics with warm-up benefits, updated worker pool architecture with sequential warm-up_
