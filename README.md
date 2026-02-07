# Document Processing Service for Paperless-ngx

A microservice that integrates with Paperless-ngx via webhooks to automatically generate and apply metadata to documents using a hybrid rule-based and LLM-based approach, with semantic tag consistency enforcement.

## Features

- **Automatic Metadata Generation**: Extract titles, tags, authors, and document types from documents
- **Semantic Tag Matching**: Suggest relevant existing tags using sentence-transformers embeddings with intelligent caching
- **Intelligent Text Reduction**: Reduce long documents before LLM processing to save tokens
- **Multi-language Support**: Works with documents in any language using multilingual models
- **Worker Pool Architecture**: Concurrent processing with auto-scaled Python workers
- **Zero Configuration Setup**: Embedded Python scripts with automatic environment setup
- **Manual Processing**: Process untagged documents via API endpoint
- **Request Tracing**: Automatic request ID generation and propagation for easy debugging
- **Performance Monitoring**: Detailed cache statistics and processing metrics
- **Cache Warm-up**: Pre-loads tag embeddings at startup for optimal performance
- **Batch Cache Operations**: Efficient batch processing of cache operations

## Quick Start

### Prerequisites

- Go 1.24+
- Python 3.8+ (for Python environment setup)
- Paperless-ngx instance with API access
- LLM API access (OpenAI-compatible)

### Installation

```bash
# Clone and build
git clone <repository-url>
cd itzamna
go build -o itzamna ./cmd

# Configure environment
export PAPERLESS_URL="https://your-paperless-instance"
export PAPERLESS_TOKEN="your-api-token"
export LLM_URL="https://your-llm-provider"
export LLM_TOKEN="your-llm-api-key"

# Run
./itzamna
```

### First Run

On first execution, the service will:

1. Create configuration directory at `~/.config/itzamna/`
2. Extract embedded Python scripts
3. Create Python virtual environment
4. Install dependencies (`sentence-transformers`, `torch`, `numpy`)
5. Start worker processes with auto-calculated worker count
6. **Cache Warm-up**: Pre-load all Paperless tags into both Go and Python embedding caches
7. **Sequential Initialization**: Warm up workers sequentially to avoid CPU spikes

## Configuration

### Essential Environment Variables

```bash
# Paperless-ngx
PAPERLESS_URL=https://your-paperless-instance
PAPERLESS_TOKEN=your-api-token

# LLM
LLM_URL=https://your-llm-provider
LLM_TOKEN=your-llm-api-key

# Service
APP_SERVER_PORT=8080
LOG_LEVEL=info
```

### Optional Configuration

```bash
# Semantic matching
SEMANTIC_MODEL_NAME=all-MiniLM-L6-v2  # or multilingual model
SEMANTIC_MIN_SIMILARITY=0.2
SEMANTIC_WORKER_COUNT=2  # do not include for automatic

# Text reduction
REDUCTION_THRESHOLD_TOKENS=2000

# Logging
RAW_BODY_LOG=false  # set to true for debugging request/response bodies
```

### Available Models

| Model                                   | Languages     | Embedding Dim | Use Case               |
| --------------------------------------- | ------------- | ------------- | ---------------------- |
| `all-MiniLM-L6-v2` (default)            | English-only  | 384           | English documents      |
| `paraphrase-multilingual-MiniLM-L12-v2` | 50+ languages | 384           | Multilingual documents |
| `paraphrase-multilingual-mpnet-base-v2` | 50+ languages | 768           | Highest accuracy       |
| `distiluse-base-multilingual-cased-v2`  | 50+ languages | 512           | Balanced approach      |

## Architecture

### Processing Pipeline

```
Paperless-ngx Webhook → Document Fetch → Length Check → Semantic Tag Matching → LLM Analysis → Paperless Update
                         ↓ (if long)     ↓ (if many tags)
                    Text Reduction    Tag Suggestions (with caching)
```

### Startup Sequence

```
1. Configuration Loading → 2. Client Initialization → 3. Worker Pool Setup → 4. Cache Warm-up → 5. Server Start
                              ↓                           ↓                      ↓
                      Paperless/LLM clients      Python workers start    Tags pre-loaded into caches
```

### Key Components

1. **Webhook Handler** (`internal/api/`): Receives and validates Paperless-ngx webhooks
2. **Document Fetcher** (`internal/paperless/`): Retrieves document content via REST API
3. **Text Reducer** (`internal/processor/`): Reduces long documents using TF-Graph-Position algorithm
4. **Semantic Matcher** (`internal/semantic/`): Python worker pool for tag similarity matching with embedding cache
5. **LLM Client** (`internal/llm/`): Sends prompts and parses structured JSON responses
6. **Document Updater**: Applies validated metadata back to Paperless-ngx
7. **Tags Cache** (`internal/utils/cache.go`): Thread-safe cache with batch operations

### Worker Pool Architecture

The semantic matcher uses a worker pool for concurrent processing:

- **Auto-scaled workers**: Based on CPU cores and available memory
- **Task queue**: 100-task buffer for handling bursts
- **Embedding cache**: Each Python worker caches tag embeddings for 10x performance
- **Health monitoring**: Built-in health checks with automatic recovery
- **Graceful shutdown**: Proper cleanup of Python processes
- **Sequential warm-up**: Workers initialized sequentially to prevent CPU spikes
- **Blocking initialization**: Service waits for all workers to be ready before accepting requests

### Cache Architecture

**Dual Cache System**:

1. **Go Tags Cache** (`utils.TagsCache`):
   - Thread-safe with `sync.RWMutex`
   - Batch operations via `GetMissingAndAdd()`
   - Hit rate tracking for monitoring
   - Pre-loaded at startup with all Paperless tags

2. **Python Embedding Cache**:
   - Per-worker in-memory cache
   - Tag → embedding dictionary
   - Warm-up at startup for optimal first-request performance
   - Statistics tracking (hits, misses, hit rates)

**Cache Warm-up Process**:

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

## API Endpoints

### `GET /health`

Returns service status.

### `POST /webhook`

Processes Paperless-ngx document added events.

**Request:**

```json
{
  "document_url": "https://paperless/api/documents/123/"
}
```

**Response:**

```json
{
  "status": "success",
  "message": "Webhook processed successfully"
}
```

### `POST /process/untagged`

Manually processes documents that have no tags. Useful for catching up on missed documents or reprocessing.

**Request:**

```bash
curl -X POST http://localhost:8080/process/untagged
```

**Response:**

```json
{
  "status": "success",
  "message": "Untagged documents processing completed",
  "data": {
    "status": "completed",
    "total": 15,
    "processed": 14,
    "failed": 1,
    "failed_document_ids": [123]
  }
}
```

## Request Tracing

Every request automatically receives a unique request ID that propagates through the entire system:

```
[INFO] POST /webhook REQID=ea63fdf8-883d-45d2-a0b1-7144e29f1671
[DEBUG] REQID=ea63fdf8-883d-45d2-a0b1-7144e29f1671 Fetching document 123
[DEBUG] REQID=ea63fdf8-883d-45d2-a0b1-7144e29f1671 Semantic matcher stats: cache_hit_rate=100%
```

This makes debugging production issues significantly easier.

## Performance

### Resource Requirements

- **CPU**: 2-4 cores recommended
- **RAM**: 1-2GB (Go) + 400-800MB (Python workers)
- **Storage**: ~500MB-1GB for Python dependencies

### Throughput

- **Documents/second**: 2-10 (varies by model and document length)
- **Concurrent processing**: Multiple workers handle requests in parallel
- **Auto-scaling**: Worker count adjusts based on system resources
- **Cache performance**: 90%+ cache hit rate after initial tag embedding

### Startup Performance

**With Cache Warm-up**:

- **First request**: ~20-50ms (embeddings already cached)
- **Startup time**: Additional 1-2 seconds for warm-up
- **CPU usage**: Sequential warm-up prevents spikes

**Without Cache Warm-up**:

- **First request**: ~1-2 seconds (computes all tag embeddings)
- **Startup time**: Faster initial startup
- **CPU usage**: Potential spikes during first requests

### Embedding Cache

The semantic matcher includes an intelligent embedding cache:

- **Warm-up at startup**: All tag embeddings pre-computed during initialization
- **Batch operations**: Efficient `GetMissingAndAdd()` for bulk cache updates
- **Cache persistence**: Cache lives for Python worker lifetime
- **Cache statistics**: Logged per request for monitoring
- **Thread-safe operations**: Proper locking for concurrent access

## Development

### Project Structure

```
itzamna/
├── cmd/main.go                 # Entry point with cache warm-up
├── internal/
│   ├── api/                   # Webhook handlers with request tracing
│   ├── config/                # Configuration with validation
│   ├── llm/                   # LLM client
│   ├── paperless/             # Paperless API client
│   ├── processor/             # Text reduction
│   ├── semantic/              # Semantic matching with Python workers
│   └── utils/                 # Utilities (logging, HTTP helpers, cache)
└── README.md
```

### Building and Testing

```bash
# Build
go build -o itzamna ./cmd

# Test webhook
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"document_url": "https://paperless/api/documents/123/"}'

# Test untagged processing
curl -X POST http://localhost:8080/process/untagged

# Test with different model
export SEMANTIC_MODEL_NAME="paraphrase-multilingual-MiniLM-L12-v2"
./itzamna
```

### Development Notes

- Python scripts are embedded using `go:embed`
- For development, place scripts in `scripts/` directory to override embedded versions
- Enable debug logging: `export LOG_LEVEL=debug`
- Request IDs are automatically generated and logged
- Cache warm-up can be monitored in startup logs

## Troubleshooting

### Common Issues

**Python workers fail to start:**

- Ensure Python 3.8+ is installed
- Check write permissions to `~/.config/itzamna/`
- Verify internet connectivity for model downloads

**Low similarity scores:**

- Scores typically range 0.2 and above for meaningful relationships
- Consider lowering `SEMANTIC_MIN_SIMILARITY` to 0.15
- Use multilingual models for non-English documents

**Memory issues:**

- Reduce `SEMANTIC_WORKER_COUNT` for constrained environments
- Use smaller models (`all-MiniLM-L6-v2` uses ~90MB per worker)

**Untagged documents not being processed:**

- Check Paperless-ngx API supports `?is_tagged=false` filter
- Verify document has no tags in Paperless-ngx
- Check service logs for API errors

**Cache warm-up failures:**

- Check Paperless API connectivity during startup
- Verify sufficient memory for embedding computation
- Monitor startup logs for warm-up progress

**Debugging with request IDs:**

```bash
# Filter logs by request ID
grep "REQID=ea63fdf8-883d-45d2-a0b1-7144e29f1671" logs/app.log

# See complete request flow
grep -A5 -B5 "REQID=ea63fdf8-883d-45d2-a0b1-7144e29f1671" logs/app.log
```

**Detailed logging:**

```bash
export LOG_LEVEL=debug
export RAW_BODY_LOG=true
./itzamna
```

## Performance Monitoring

Key metrics available in logs:

- **Cache hit rate**: Percentage of tag embeddings served from cache
- **Processing time**: Time spent in semantic matching
- **New tags cached**: Number of new embeddings computed per request
- **Total cache size**: Number of tag embeddings cached
- **Warm-up progress**: Worker initialization status during startup

Example log output:

```
[INFO] Warming up semantic matcher worker 1/3
[INFO] Worker 1/3 warmed up successfully
[INFO] Warming up semantic matcher worker 2/3
[INFO] Worker 2/3 warmed up successfully
[INFO] Warming up semantic matcher worker 3/3
[INFO] Worker 3/3 warmed up successfully
[INFO] Tags Cache: size=1561, new=0, hit_rate=0.50
[DEBUG] Semantic matcher stats: process_ms=23, total_tags_considered=1561, tags_above_threshold=15
```

## License

This project uses:

- `sentence-transformers` models (Apache 2.0)
- Paperless-ngx REST API
- Go standard library and community packages

All models are open-source and freely available for commercial use.

---

_Last Updated: 2026-02-05_

_Implementation Version: 1.5.0_

_Changes: Added cache warm-up at startup, batch cache operations, sequential worker initialization, and improved startup performance monitoring_
