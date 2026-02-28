> **LLM-Assisted Documentation Notice**
>
> This documentation was generated with LLM assistance. Technical details may contain inaccuracies - always verify against source code and test in your environment.

---

# Document Processing Service for Paperless-ngx

A microservice that integrates with Paperless-ngx via webhooks to automatically generate and apply metadata to documents using a hybrid rule-based and LLM-based approach, with semantic tag consistency enforcement.

## Features

- **Automatic Metadata Generation**: Extract titles, tags, authors, and document types from documents
- **Semantic Tag Matching**: Suggest relevant existing tags using sentence-transformers embeddings with intelligent caching
- **Intelligent Text Reduction**: Reduce long documents before LLM processing to save tokens
- **Multi-language Support**: Works with documents in any language using multilingual models
- **Single Process Architecture**: Simplified single Python process for semantic matching
- **Zero Configuration Setup**: Embedded Python scripts with automatic environment setup
- **Manual Processing**: Process untagged documents via API endpoint
- **Request Tracing**: Automatic request ID generation and propagation for easy debugging
- **Performance Monitoring**: Detailed cache statistics and processing metrics
- **Cache Warm-up**: Pre-loads tag embeddings at startup for optimal performance
- **Batch Cache Operations**: Efficient batch processing of cache operations
- **Zero API Overhead**: Eliminates redundant Paperless API calls by using pre-warmed cache

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
5. Start single Python process for semantic matching
6. **Cache Warm-up**: Pre-load all Paperless tags into both Go and Python embedding caches
7. **Ready for Processing**: Service starts with zero API overhead for tag lookups

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
1. Configuration Loading → 2. Client Initialization → 3. Python Process Setup → 4. Cache Warm-up → 5. Server Start
                              ↓                           ↓                      ↓
                      Paperless/LLM clients      Single Python process    Tags pre-loaded into caches
```

### Key Components

1. **Webhook Handler** (`internal/api/`): Receives and validates Paperless-ngx webhooks
2. **Document Fetcher** (`internal/paperless/`): Retrieves document content via REST API
3. **Text Reducer** (`internal/processor/`): Reduces long documents using TF-Graph-Position algorithm
4. **Semantic Matcher** (`internal/semantic/`): Single Python process for tag similarity matching with embedding cache
5. **LLM Client** (`internal/llm/`): Sends prompts and parses structured JSON responses
6. **Document Updater**: Applies validated metadata back to Paperless-ngx
7. **Tags Cache** (`internal/utils/cache.go`): Thread-safe cache with batch operations

### Simplified Architecture

The semantic matcher uses a **single Python process** for all semantic matching:

- **Single process**: Simplified architecture with no worker coordination needed
- **Embedding cache**: Single shared cache for all tag embeddings
- **Task queue**: 100-task buffer for handling concurrent requests
- **Health monitoring**: Built-in health checks
- **Graceful shutdown**: Proper cleanup of Python process

### Cache Architecture

**Dual Cache System**:

1. **Go Tags Cache** (`utils.TagsCache`):
   - Thread-safe with `sync.RWMutex`
   - Batch operations via `AddNewTags()`
   - Statistics tracking for monitoring
   - Pre-loaded at startup with all Paperless tags
   - **Zero API overhead**: Eliminates redundant Paperless API calls during processing

2. **Python Embedding Cache**:
   - Single in-memory cache in Python process
   - Tag → embedding dictionary
   - Warm-up at startup for optimal first-request performance
   - Statistics tracking (processing time, tags considered)

**Cache Warm-up Process**:

```go
// 1. Fetch all tags from Paperless (once at startup)
tags, err := paperlessClient.GetTags(warmReqId)

// 2. Pre-load into Go cache
initialTags := []utils.CacheItem{}
for _, t := range tags {
    initialTags = append(initialTags, utils.NewCacheItem(t.ID, t.Name))
}
tagsCache.AddNewTags(initialTags)

// 3. Warm up Python embedding cache
cachedTags := tagsCache.GetCachedTagsValues()
_, err = semanticMatcher.GetTagSuggestions("dummy", cachedTags, warmReqId)
```

## Performance Optimizations

### Zero API Overhead Design

The service implements a **zero API overhead** design for tag lookups:

**Before Optimization**:

```go
// Each document processing required an API call
tags, err := h.paperless.GetTags(reqID)  // API call for every document
```

**After Optimization**:

```go
// Direct cache access - no API calls
cachedTags := h.tagsCache.GetCachedTags()  // Zero API overhead
```

### Impact on Performance

1. **Webhook Processing**: Each document saves 1 API call to Paperless
2. **Batch Processing**: For N untagged documents, saves N API calls
3. **Reduced Latency**: Eliminates network round-trip for tag lookups
4. **Lower Load on Paperless**: Significantly reduces API calls, especially during batch processing

### Cache Warm-up Benefits

**With Cache Warm-up**:

- **First request**: ~20-50ms (embeddings already cached)
- **Startup time**: Additional 1-2 seconds for warm-up
- **API calls**: Zero additional calls for tag lookups during processing

**Without Cache Warm-up**:

- **First request**: ~1-2 seconds (computes all tag embeddings)
- **Startup time**: Faster initial startup
- **API calls**: Still zero for tag lookups (uses cold cache)

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
[DEBUG] REQID=ea63fdf8-883d-45d2-a0b1-7144e29f1671 Semantic matcher stats: process_ms=23, total_tags=1561
```

This makes debugging production issues significantly easier.

## Performance

### Resource Requirements

- **CPU**: 1-2 cores recommended
- **RAM**: 1-2GB (Go) + 400-800MB (Python process)
- **Storage**: ~500MB-1GB for Python dependencies

### Throughput

- **Documents/second**: 2-10 (varies by model and document length)
- **Concurrent processing**: Go goroutines handle requests in parallel
- **Cache performance**: All tag embeddings cached after warm-up
- **API efficiency**: Zero additional API calls for tag lookups during processing

### Startup Performance

**With Cache Warm-up**:

- **First request**: ~20-50ms (embeddings already cached)
- **Startup time**: Additional 1-2 seconds for warm-up
- **API calls**: 1 call to Paperless for tags (at startup only)

**Without Cache Warm-up**:

- **First request**: ~1-2 seconds (computes all tag embeddings)
- **Startup time**: Faster initial startup
- **API calls**: 1 call to Paperless for tags (at startup only)

### Embedding Cache

The semantic matcher includes an intelligent embedding cache:

- **Warm-up at startup**: All tag embeddings pre-computed during initialization
- **Batch operations**: Efficient `AddNewTags()` for bulk cache updates
- **Cache persistence**: Cache lives for Python process lifetime
- **Cache statistics**: Logged per request for monitoring
- **Thread-safe operations**: Proper locking for concurrent access
- **Zero API overhead**: No Paperless API calls for tag lookups during processing

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
│   ├── semantic/              # Semantic matching with single Python process
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

**Python process fails to start:**

- Ensure Python 3.8+ is installed
- Check write permissions to `~/.config/itzamna/`
- Verify internet connectivity for model downloads

**Low similarity scores:**

- Scores typically range 0.2 and above for meaningful relationships
- Consider lowering `SEMANTIC_MIN_SIMILARITY` to 0.15
- Use multilingual models for non-English documents

**Memory issues:**

- Use smaller models (`all-MiniLM-L6-v2` uses ~90MB)
- Monitor Python process memory usage

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

- **Cache size**: Number of tags cached
- **Processing time**: Time spent in semantic matching
- **New tags cached**: Number of new embeddings computed per request
- **Warm-up progress**: Cache initialization status during startup
- **API calls saved**: Number of redundant Paperless API calls eliminated

Example log output:

```
[INFO] Warming up semantic matcher
[INFO] Semantic matcher embeddings warmed up successfully
[INFO] Tags Cache: size=1561, reads=1, updates=1, uptime=2s
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

_Implementation Version: 1.6.0_

_Changes: Simplified architecture to single Python process, removed worker pool complexity, maintained zero API overhead design, updated documentation to reflect simplified architecture_
