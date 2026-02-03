# Python Semantic Matcher - Integration Specification

## Overview

Python script that provides semantic tag matching functionality to Go service via subprocess communication. Uses `sentence-transformers` with configurable models to generate embeddings and calculate semantic similarities.

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
3. **Model Loading**: Python loads model, sends ready message to stdout
4. **Ready State**: Python waits for requests on stdin

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
  "existing_tags": ["invoice", "receipt", "tax", "2024"]
}
```

#### Request Fields

| Field           | Type          | Required | Description                                         |
| --------------- | ------------- | -------- | --------------------------------------------------- |
| `text`          | string        | Yes      | Document text (full or reduced) to analyze          |
| `existing_tags` | array[string] | Yes      | All existing tags in Paperless-ngx to match against |

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
    "processing_time_ms": 125.5,
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

| Field                              | Type          | Description                                                   |
| ---------------------------------- | ------------- | ------------------------------------------------------------- |
| `suggested_tags`                   | array[string] | Top N tags meeting similarity threshold, sorted descending    |
| `similarities`                     | array[object] | Top N tags with similarity scores                             |
| `similarities[].tag`               | string        | Tag name                                                      |
| `similarities[].score`             | float         | Cosine similarity score (0.0-1.0)                             |
| `debug_info`                       | object        | Diagnostic information for monitoring/debugging               |
| `debug_info.embedding_dimension`   | integer       | Vector dimension (model-specific)                             |
| `debug_info.processing_time_ms`    | float         | Total processing time in milliseconds (rounded to 2 decimals) |
| `debug_info.total_tags_considered` | integer       | Number of existing tags processed                             |
| `debug_info.tags_above_threshold`  | integer       | Number of tags meeting min_similarity                         |
| `debug_info.model_loaded`          | boolean       | Model successfully loaded                                     |
| `debug_info.model_name`            | string        | Model identifier used                                         |
| `debug_info.text_length_chars`     | integer       | Character count of input text                                 |
| `debug_info.text_estimated_tokens` | integer       | Estimated token count (chars ÷ 4)                             |
| `error`                            | null          | Always null for successful responses                          |

### Error Response Format

```json
{
  "suggested_tags": [],
  "similarities": [],
  "debug_info": {
    "error": "Model failed to load: File not found",
    "model_loaded": false,
    "processing_time_ms": 10.25,
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
| `debug_info.processing_time_ms`  | float   | Time spent before error                      |
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
2. **Ready Signal**: Send `{"status": "ready", "embedding_dim": N}` to stdout
3. **Input Reading**: Read JSON lines from stdin (blocking)
4. **Processing**:
   - Generate embedding for input text
   - Generate embeddings for existing tags in batches of 100
   - Calculate cosine similarities
   - Apply threshold and select top N
5. **Output**: Write JSON response to stdout, flush immediately
6. **Loop**: Continue until stdin closes or EOF received
7. **Error Handling**: Catch all exceptions, return structured error

### Performance Characteristics

| Model                                   | Embedding Time | Memory Usage | Throughput      |
| --------------------------------------- | -------------- | ------------ | --------------- |
| `all-MiniLM-L6-v2`                      | ~50-100ms      | ~90MB        | ~10-20 docs/sec |
| `paraphrase-multilingual-MiniLM-L12-v2` | ~100-200ms     | ~120MB       | ~5-10 docs/sec  |
| `paraphrase-multilingual-mpnet-base-v2` | ~200-400ms     | ~420MB       | ~2-5 docs/sec   |
| `distiluse-base-multilingual-cased-v2`  | ~150-300ms     | ~250MB       | ~3-7 docs/sec   |

**Note**: Throughput assumes batch size of 100 tags and typical document length.

## Go Integration Pattern

### Configuration

```ini
# Environment Variables (Go Side)
SEMANTIC_MODEL_NAME=all-MiniLM-L6-v2  # or multilingual model
SEMANTIC_TOP_N=15
SEMANTIC_MIN_SIMILARITY=0.2
SEMANTIC_TIMEOUT_MS=10000
```

### Process Management

1. **Startup**: Launch Python process when Go service starts
2. **Configuration**: Send config as first message after process start
3. **Ready Wait**: Wait for ready message with embedding dimension
4. **Health Check**: Send test request on startup, restart if fails
5. **Timeout**: Set 10-second timeout per request (configurable)
6. **Restart**: If Python crashes, restart with exponential backoff
7. **Shutdown**: Send EOF, wait for graceful exit

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
3. Monitor similarity scores may differ between models
4. Adjust `min_similarity` threshold if needed

## Testing Protocol

### Model-Specific Test Cases

1. **English Model** (`all-MiniLM-L6-v2`):
   - Test with English documents only
   - Verify performance metrics
   - Check memory usage

2. **Multilingual Models**:
   - Test with documents in different languages
   - Verify cross-language similarity works
   - Check embedding dimensions match spec

3. **Performance Tests**:
   - Measure embedding time per model
   - Monitor memory usage
   - Test with large tag pools (1000+ tags)

### Validation Steps

1. Start Python script manually, send config then test JSON
2. Verify JSON response format matches spec
3. Test with Go integration, check error handling
4. Load test with concurrent requests
5. Verify memory usage doesn't leak

## Directory Structure

```
scripts/
├── semantic_matcher.py          # Main Python script
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

## Versioning

- **API Version**: 1.1.0
- **Backwards Compatibility**: Additive changes only (new optional fields)
- **Breaking Changes**: Increment major version, update Go integration

## Security Considerations

1. **Input Validation**: Python script validates JSON structure
2. **Resource Limits**: Set timeout to prevent hanging
3. **Path Safety**: Use absolute paths for model loading
4. **Error Messages**: Don't expose system details in production
5. **Stderr Output**: Debug info goes to stderr, not included in responses

## Monitoring Metrics

Go service should track:

- Python process uptime
- Average processing time per model
- Success/error rate
- Tags suggested per document
- Model load time on startup
- Memory usage per model
- Embedding dimension per model

## Future Extensions

1. **Batch Processing**: Accept multiple texts in single request
2. **Embedding Cache**: Cache tag embeddings between requests
3. **Model Ensemble**: Combine multiple models for better accuracy
4. **GPU Support**: Optional GPU acceleration flag
5. **Health Endpoint**: HTTP health check for Python process
6. **Dynamic Model Loading**: Switch models without restarting process

---

_Last Updated: 2026-01-28_
_Spec Version: 1.2.0_
_Changes: Updated to match actual implementation, added ready message protocol, corrected defaults_
