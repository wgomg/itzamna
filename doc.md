# Developer Documentation

## Architecture Overview

### System Design

The Document Processing Service is built as a Go microservice with an embedded Python component for semantic matching. The architecture follows a pipeline pattern where documents flow through sequential processing stages with intelligent caching and performance optimizations, including a **zero API overhead** design for tag lookups.

### Core Design Principles

1. **Separation of Concerns**: Each component has a single responsibility
2. **Concurrency First**: Designed for parallel processing from the ground up
3. **Resource Efficiency**: Optimized for low-resource environments with intelligent caching
4. **Extensibility**: Interfaces and factories allow for easy component replacement
5. **Manual Recovery**: Support for processing missed documents via API
6. **Observability**: Automatic request tracing and detailed logging
7. **Performance Optimization**: Cache warm-up at startup for optimal first-request performance
8. **Zero API Overhead**: Eliminates redundant Paperless API calls by using pre-warmed cache
9. **Simplified Architecture**: Single Python process for semantic matching, no worker pool complexity

## Component Architecture

### 1. Webhook Handler (`internal/api/`)

**Purpose**: Receives and validates Paperless-ngx webhook events and provides manual processing endpoints

**Key Classes**:

- `Handler`: Main request handler with `Process()` method
- `WebhookPayload`: DTO for incoming webhook data

**Key Methods**:

- `HandleWebhook()`: Processes incoming Paperless-ngx webhooks
- `HandleProcessUntagged()`: Manually processes documents without tags
- `Process()`: Core document processing logic (shared by both endpoints)

**Flow**:

```

HandleWebhook() → extractDocumentID() → GetDocument() → Process() → SuccessResponse()
HandleProcessUntagged() → GetDocumentsWithoutTags() → Process() (for each) → SuccessResponse()

```

**Request Tracing**:

- Automatic UUID generation for each request
- Request ID stored in context and propagated through all components
- All logs include `REQID=` prefix for easy correlation

**Error Handling**:

- HTTP method validation
- JSON payload validation
- Document ID extraction
- Structured error responses via `httputils`
- Batch processing continues on individual document failures

### 2. Configuration Management (`internal/config/`)

**Purpose**: Centralized configuration with environment variable support

**Key Features**:

- Automatic environment detection (development/production)
- Default value management
- Type-safe configuration access

**Configuration Structure**:

```go
type SemanticConfig struct {
    TopN          int
    MinSimilarity float64
    TimeoutMs     int
    Model         string
    TagsThreshold int
    Python        PythonConfig
}

type PythonConfig struct {
    ConfigDir string  // No more worker timing configs
}
```

### 3. Paperless-ngx Client (`internal/paperless/`)

**Purpose**: REST client for Paperless-ngx API interactions

**Key Methods**:

- `GetDocument()`: Fetch document content and metadata
- `GetDocumentsWithoutTags()`: Fetch documents without tags (for manual processing)
- `GetTags()`: Retrieve all tags with pagination support (used only at startup for cache warm-up)
- `CreateTags()`: Bulk tag creation with deduplication
- `UpdateDocument()`: PATCH metadata updates

**Pagination Handling**:

```go
for url != "" {
    // Fetch page
    url = response.Next // Continue to next page
}
```

**Document Filtering**:
The `GetDocumentsWithoutTags()` method uses Paperless-ngx's `?is_tagged=false` filter to efficiently retrieve documents that need processing.

**Zero API Overhead Design**:

- **Tag lookups**: No API calls during document processing (uses pre-warmed cache)
- **Startup only**: Single API call to fetch all tags at service startup
- **Performance**: Eliminates network latency for tag lookups during processing

### 4. Text Reduction Pipeline (`internal/processor/`)

**Purpose**: Reduce long documents before LLM processing

**Algorithm**:

1. **Chunking**: Split text into overlapping segments
2. **Scoring**: TF-IDF + TextRank + Positional scoring
3. **Selection**: Greedy selection with diversity penalty
4. **Reconstruction**: Assemble selected chunks

**Key Data Structures**:

```go
type Chunk struct {
    Id                   int
    NormalizedPosition   float64
    RawText              string
    Words                []string
    TokenFrequencies     map[string]int
    TFScore              float64
    GraphScore           float64
    FinalScore           float64
}

type Graph struct {
    Nodes     []*Node
    Adjacency [][]float64
}
```

### 5. Semantic Matcher (`internal/semantic/`)

**Purpose**: Find semantically similar tags using sentence-transformers with intelligent caching and warm-up optimization

#### Simplified Single-Process Architecture

**PythonMatcher**:

- Manages a single Python process for semantic matching
- Buffered task queue (100 tasks) for concurrent request handling
- Automatic process lifecycle management
- Health monitoring and recovery
- **Single cache**: One embedding cache shared across all requests
- **Simplified initialization**: No worker coordination needed

**Embedding Cache**:

- **In-memory cache**: Single Python process maintains tag → embedding dictionary
- **Performance**: 10x speedup after initial tag embedding
- **Statistics**: Track processing time and tags considered
- **Persistence**: Cache lives for Python process lifetime
- **Warm-up**: Pre-loaded at startup for optimal first-request performance

**Communication Protocol**:

```
Go → Python: {"model_name": "...", "top_n": 15, "min_similarity": 0.2}\n
Python → Go: {"status": "ready", "embedding_dim": 384}\n
Go → Python: {"text": "...", "new_tags": [...]}\n
Python → Go: {"suggested_tags": [...], "debug_info": {...}, "error": null}\n
```

#### Embedded Python System

**First-Run Setup**:

1. Extract embedded scripts to `~/.config/itzamna/python/`
2. Create Python virtual environment
3. Install dependencies (`sentence-transformers`, `torch`, `numpy`)
4. Start single Python process
5. **Cache Warm-up**: Pre-load all Paperless tags into embedding cache
6. **Zero API Overhead Ready**: Service starts with no additional API calls needed for tag lookups

**Development Mode**:
Place scripts in `scripts/` directory to override embedded versions:

```bash
mkdir -p scripts
cp internal/semantic/scripts/* scripts/
```

### 6. LLM Client (`internal/llm/`)

**Purpose**: Interface with external LLM APIs

**Prompt Engineering**:

```go
prompt := fmt.Sprintf(
    "Analyze the excerpts of a document...\n" +
    "- Document title: ...\n" +
    "- Document type: choose one of '%s'\n" +
    "- Tags: At most five thematic tags...\n" +
    "- Author: ...\n" +
    "- Language: ...\n" +
    "Return ONLY a json string...",
    typesString,
    tagsString,
    pages,
    content,
)
```

**Response Parsing**:

- Structured JSON parsing with validation
- Token usage tracking
- Error handling for malformed responses

### 7. Tags Cache (`internal/utils/cache.go`)

**Purpose**: Thread-safe cache for Paperless tags with batch operations and zero API overhead design

**Key Features**:

- **Thread-safe**: Uses `sync.RWMutex` for concurrent access
- **Batch operations**: `AddNewTags()` for efficient bulk updates
- **Statistics tracking**: Monitor cache reads, updates, and uptime
- **Warm-up support**: Pre-loaded at startup with all Paperless tags
- **Zero API overhead**: Eliminates redundant Paperless API calls during processing
- **Direct cache access**: `GetCachedTags()` provides immediate access without API calls

**Cache Architecture**:

```go
type TagsCache struct {
    mu          sync.RWMutex
    items       map[string]CacheItem
    reads       int
    updates     int
    startupTime time.Time
}

func (c *TagsCache) GetCachedTags() map[string]CacheItem {
    c.mu.RLock()
    defer c.mu.RUnlock()
    c.reads++
    return c.items
}

func (c *TagsCache) AddNewTags(items []CacheItem) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if len(items) > 0 {
        c.updates++
    }

    for _, item := range items {
        c.items[item.value] = item
    }
}
```

**Zero API Overhead Implementation**:

```go
// Before optimization (redundant API calls):
tags, err := h.paperless.GetTags(reqID)  // API call for every document

// After optimization (zero API overhead):
cachedTags := h.tagsCache.GetCachedTags()  // Direct cache access - no API call
```

## Performance Optimizations

### Zero API Overhead Design

The service implements a **zero API overhead** design that eliminates redundant Paperless API calls:

**Key Improvements**:

1. **Eliminated Redundant API Calls**: No more `GetTags()` calls during document processing
2. **Direct Cache Access**: Uses pre-warmed cache instead of API calls
3. **Reduced Latency**: Eliminates network round-trip for tag lookups
4. **Lower Load on Paperless**: Significantly reduces API calls, especially during batch processing

**Impact on Performance**:

- **Webhook Processing**: Each document saves 1 API call to Paperless
- **Batch Processing**: For N untagged documents, saves N API calls
- **Network Efficiency**: Reduces overall network traffic
- **Service Reliability**: Less dependent on Paperless API availability during processing

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

### Automatic Processing

- `POST /webhook`: Processes individual documents from Paperless-ngx webhooks

### Manual Processing

- `POST /process/untagged`: Processes all documents without tags in batch

### Health & Monitoring

- `GET /health`: Service health check

## Concurrency Model

### Goroutine-Based Processing

**Main Processing Flow**:

```go
// Each webhook request processed in its own goroutine
go handler.Process(documentID)
```

**Batch Processing**:

```go
// Manual processing handles documents sequentially
for _, document := range documents {
    if err := h.Process(&document); err != nil {
        // Log error but continue with other documents
        continue
    }
}
```

**Task Queue Pattern**:

```go
type Task struct {
    Text      string
    NewTags   []string  // Only new tags that need embedding computation
    RequestID string    // For request tracing
    Result    chan<- TaskResult
}

taskQueue := make(chan Task, 100) // Buffered channel for concurrent requests
```

### Synchronization

**Mutex Protection**:

```go
type PythonMatcher struct {
    mu        sync.Mutex
    // ...
}

func (p *PythonMatcher) processTask(task Task) {
    p.mu.Lock()
    defer p.mu.Unlock()
    // Thread-safe operations
}
```

**Cache Synchronization**:

```go
type TagsCache struct {
    mu     sync.RWMutex
    items  map[string]CacheItem
    // ...
}

func (c *TagsCache) GetCachedTags() map[string]CacheItem {
    c.mu.RLock()
    defer c.mu.RUnlock()
    // Thread-safe read-only access
}
```

## Error Handling Strategy

### Layered Error Handling

1. **HTTP Layer** (`httputils`):
   - Method validation
   - JSON parsing
   - Content-Type checking

2. **Business Logic Layer**:
   - Document fetching errors
   - LLM API errors
   - Semantic matching errors

3. **Infrastructure Layer**:
   - Python process failures
   - Network timeouts
   - Resource exhaustion
   - Cache warm-up failures

### Batch Processing Error Handling

**Individual Document Failures**:

```go
for _, document := range documents {
    if err := h.Process(&document); err != nil {
        // Log error, track failed IDs, but continue processing
        failed++
        failedIDs = append(failedIDs, document.ID)
        continue
    }
    processed++
}
```

**Response with Statistics**:

```go
response := map[string]interface{}{
    "status":    "completed",
    "total":     len(documents),
    "processed": processed,
    "failed":    failed,
}
if failed > 0 {
    response["failed_document_ids"] = failedIDs
}
```

### Cache Warm-up Error Handling

**Startup Failures**:

```go
// Single process warm-up with error handling
_, err = semanticMatcher.GetTagSuggestions("dummy", cachedTags, warmReqId)
if err != nil {
    logger.Fatal("Failed to warm up semantic embedding cache", err)
}
```

### Error Types

```go
// HTTP errors
type HTTPError struct {
    Code    int
    Message string
}

// API errors
type APIError struct {
    StatusCode int
    Message    string
    Body       string
}

// Python errors
type PythonResponse struct {
    SuggestedTags []string `json:"suggested_tags"`
    Error         *string  `json:"error,omitempty"`
}
```

## Configuration System

### Environment Variable Hierarchy

1. **Required Variables**:
   - `PAPERLESS_URL`, `PAPERLESS_TOKEN`
   - `LLM_URL`, `LLM_TOKEN`

2. **Optional Variables**:
   - `SEMANTIC_MODEL_NAME`: Model selection
   - `SEMANTIC_MIN_SIMILARITY`: Similarity threshold
   - `REDUCTION_THRESHOLD_TOKENS`: Text reduction threshold

3. **Development Variables**:
   - `LOG_LEVEL`: debug/info/error
   - `RAW_BODY_LOG`: Request/response logging
   - `APP_ENV`: development/production

### Configuration Loading

```go
func Load() (*Config, error) {
    _ = godotenv.Load() // Load .env file if present

    return &Config{
        App: AppConfig{
            Env:        parseEnvironment(getEnv("APP_ENV", "development")),
            LogLevel:   getLogLevel(env),
            ServerPort: getEnv("APP_SERVER_PORT", "8080"),
        },
        // ... other config sections
    }, nil
}
```

## Testing Strategy

### Unit Testing

**Component Tests**:

```bash
# Test individual packages
go test ./internal/processor/...
go test ./internal/config/...
go test ./internal/utils/...  # Cache tests
```

**Integration Tests**:

```bash
# Test with mock services
go test -tags=integration ./...
```

### Manual Testing

**Webhook Testing**:

```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"document_url": "https://paperless/api/documents/123/"}'
```

**Manual Processing Testing**:

```bash
# Process untagged documents
curl -X POST http://localhost:8080/process/untagged
```

**Cache Warm-up Testing**:

```bash
# Monitor startup logs for cache warm-up
export LOG_LEVEL=info
./itzamna 2>&1 | grep -E "(Warming up|warmed up|Cache)"
```

**Zero API Overhead Testing**:

```bash
# Monitor API calls during processing
export LOG_LEVEL=debug
./itzamna 2>&1 | grep -E "(Fetching tags|GetTags)"  # Should only appear at startup
```

## Development Workflow

### Setting Up Development Environment

1. **Clone and Build**:

```bash
git clone <repository>
cd itzamna
go build -o itzamna ./cmd
```

2. **Development Configuration**:

```bash
# Create .env file
cat > .env << EOF
PAPERLESS_URL=http://localhost:8000
PAPERLESS_TOKEN=test-token
LLM_URL=http://localhost:8081
LLM_TOKEN=test-llm-token
LOG_LEVEL=debug
APP_ENV=development
EOF
```

3. **Python Script Development**:

```bash
# Use local scripts instead of embedded ones
mkdir -p scripts
cp internal/semantic/scripts/* scripts/
```

### Debugging Tips

**Enable Detailed Logging**:

```bash
export LOG_LEVEL=debug
export RAW_BODY_LOG=true
```

**Python Process Debugging**:

- Check `~/.config/itzamna/` for extracted scripts
- Monitor Python process stderr output
- Check model download progress in logs

**Request Tracing**:

- All logs include `REQID=` prefix
- Filter logs by request ID: `grep "REQID=abc123" logs/app.log`
- Trace complete request flow across components

**Cache Performance Monitoring**:

```bash
# Monitor cache statistics
grep -E "(Tags Cache|reads=|updates=)" logs/app.log

# Monitor warm-up progress
grep -E "(Warming up|warmed up)" logs/app.log

# Monitor zero API overhead
grep -E "(Fetching tags|GetTags)" logs/app.log  # Should only appear at startup
```

**Performance Profiling**:

```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=.
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=.
go tool pprof mem.prof
```

## Extension Points

### Adding New Models

1. **Update Python Script** (`semantic_matcher.py`):

```python
# Add model to available models
SUPPORTED_MODELS = {
    "all-MiniLM-L6-v2": "English-only, fast",
    "new-model-name": "Description of new model",
}
```

2. **Update Configuration**:

```go
// Add model validation if needed
func validateModel(modelName string) error {
    supportedModels := []string{
        "all-MiniLM-L6-v2",
        "paraphrase-multilingual-MiniLM-L12-v2",
        "new-model-name",
    }
    // ... validation logic
}
```

### Custom Text Reduction

**Implement New Reducer**:

```go
type Reducer interface {
    Reduce(content string, cfg *ReductionConfig) string
}

func NewCustomReducer() Reducer {
    return &customReducer{}
}
```

**Update Factory**:

```go
func NewReducer(cfg *ReductionConfig) Reducer {
    if cfg.UseCustomAlgorithm {
        return NewCustomReducer()
    }
    return NewDefaultReducer()
}
```

### Alternative LLM Providers

**Implement New Client**:

```go
type LLMClient interface {
    AnalyzeContent(content string, pages int,
                   documentTypes []paperless.DocumentType,
                   tags []string, reqID string) (*AnalysisResult, error)
}

func NewCustomLLMClient(cfg *Config, logger *utils.Logger) (LLMClient, error) {
    // Custom implementation
}
```

### Adding New Processing Endpoints

**Example: Process documents without correspondent**:

```go
func (h *Handler) HandleProcessUnauthored(w http.ResponseWriter, r *http.Request) {
    documents, err := h.paperless.GetDocumentsWithoutCorrespondent()
    // ... similar pattern to HandleProcessUntagged
}
```

### Cache Customization

**Implement Custom Cache Strategy**:

```go
type CacheStrategy interface {
    GetCachedTags() map[string]CacheItem
    AddNewTags(items []CacheItem)
    Size() int
    Stats() map[string]interface{}
    ResetStats()
}

func NewLRUCache(maxSize int) CacheStrategy {
    return &lruCache{
        maxSize: maxSize,
        items:   make(map[string]CacheItem),
    }
}
```

## Performance Considerations

### Memory Management

**Python Process**:

- Single process loads model into memory (~90-420MB depending on model)
- Consider smaller models for memory-constrained environments
- **Cache memory**: Single cache for all tag embeddings (~4KB per tag)

**Go Service**:

- Document content stored in memory during processing
- Consider streaming for very large documents
- **Tags cache**: Minimal memory overhead (string storage only)

### CPU Utilization

**Simplified Architecture**:

- No worker pool coordination overhead
- Single Python process handles all semantic matching
- Go goroutines handle concurrent requests efficiently

**Batch Processing**:

- Consider batching similar operations
- Implement request coalescing for identical documents
- Use connection pooling for HTTP clients
- **Cache operations**: Direct cache access eliminates lock contention

### I/O Optimization

**HTTP Client Configuration**:

```go
httpClient := &http.Client{
    Timeout: time.Duration(cfg.App.HttpTimeoutSeconds) * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        IdleConnTimeout:     90 * time.Second,
        TLSHandshakeTimeout: 10 * time.Second,
    },
}
```

**File System Operations**:

- Cache extracted Python scripts
- Reuse virtual environment across restarts
- Implement retry logic for network operations

### Cache Performance

**Warm-up Benefits**:

- **First request**: ~20-50ms (embeddings already cached)
- **Without warm-up**: ~1-2 seconds (computes all tag embeddings)
- **Memory trade-off**: Cache lives in Python process memory
- **Startup time**: Additional 1-2 seconds for warm-up

**Zero API Overhead Benefits**:

- **Network efficiency**: Eliminates API calls for tag lookups
- **Reduced latency**: No network round-trip during processing
- **Improved reliability**: Less dependent on Paperless API availability
- **Lower load**: Reduces API calls on Paperless server

## Security Considerations

### API Security

**Token Management**:

- Environment variables for sensitive data
- No hardcoded credentials
- Regular token rotation recommended

**Input Validation**:

- Validate all incoming JSON
- Sanitize document content
- Limit request sizes

### Process Isolation

**Python Sandboxing**:

- Virtual environment isolation
- Limited filesystem access
- Process resource limits

**Network Security**:

- HTTPS for all external communications
- Certificate validation
- Firewall rules for service ports

### Cache Security

**Data Isolation**:

- Single Python process cache
- No shared state between instances
- Cache cleared on process restart

**Memory Limits**:

- Model size considered in resource planning
- Cache size grows with tag count
- Monitor Python process memory usage

## Monitoring and Observability

### Logging Strategy

**Structured Logging with Request Tracing**:

```go
logger.Info(&reqID, "Processing document ID: %d", documentID)
logger.Debug(&reqID, "Path decision: estimated_tokens=%d, should_reduce=%v",
    estimatedTokens, shouldReduce)
logger.Error(&reqID, "Failed to fetch document %d: %v", documentID, err)
```

**Batch Processing Logs**:

```go
logger.Info(&reqID, "Found %d untagged documents to process", len(documents))
logger.Info(&reqID, "Successfully processed untagged document ID=%d", document.ID)
logger.Error(&reqID, "Error processing untagged document ID=%d: %v", document.ID, err)
```

**Cache Statistics**:

```go
logger.Info(
    &reqID,
    "Tags Cache: size=%d, reads=%d, updates=%d, uptime=%.0fs",
    h.tagsCache.Size(),
    stats["total_reads"],
    stats["total_updates"],
    stats["uptime_seconds"],
)
```

**Warm-up Progress**:

```go
logger.Info(&warmReqId, "Warming up semantic matcher and internal cache")
logger.Info(&warmReqId, "Semantic matcher embeddings warmed up successfully")
```

**Zero API Overhead Verification**:

```go
// Log when tags are fetched (should only happen at startup)
logger.Debug(&reqID, "Fetching tags from Paperless (startup only)")
```

**Log Levels**:

- `debug`: Detailed processing information, cache statistics, API call tracking
- `info`: Normal operation events, request tracing, warm-up progress
- `error`: Error conditions

### Metrics Collection

**Key Metrics**:

- Documents processed per second
- Average processing time
- Error rates by component
- LLM token usage
- Manual processing success/failure rates
- **Cache size**: Number of tags cached
- **Cache reads/updates**: Cache activity statistics
- **Warm-up time**: Time spent warming up cache
- **API calls saved**: Number of redundant Paperless API calls eliminated
- **Network latency reduction**: Time saved by eliminating API calls

**Health Checks**:

```go
func (p *PythonMatcher) HealthCheck() error {
    // Test Python process responsiveness
    return nil
}
```

**Performance Benchmarks**:

- First request latency (with/without warm-up)
- Cache effectiveness over time
- Memory usage of Python process
- Startup time with cache warm-up
- API call reduction metrics

## Deployment Considerations

### Containerization

**Dockerfile**:

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o itzamna ./cmd

FROM alpine:latest
RUN apk --no-cache add python3 py3-pip
WORKDIR /app
COPY --from=builder /app/itzamna .
CMD ["./itzamna"]
```

**Resource Limits**:

```yaml
# Kubernetes resource limits
resources:
  limits:
    memory: '2Gi'
    cpu: '2'
  requests:
    memory: '1Gi'
    cpu: '1'
```

### High Availability

**Multiple Instances**:

- Stateless design allows horizontal scaling
- Load balancer for webhook distribution
- Shared-nothing architecture
- **Cache independence**: Each instance warms up its own cache
- **Zero API overhead**: Each instance maintains its own cache, no shared state needed

**Graceful Shutdown**:

```go
func (p *PythonMatcher) Close() {
    p.mu.Lock()
    defer p.mu.Unlock()

    p.closed = true
    close(p.taskQueue)

    if p.stdin != nil {
        p.stdin.Close()
    }
    if p.process != nil {
        p.process.Process.Kill()
    }
    if p.stdout != nil {
        p.stdout.Close()
    }
}
```

**Startup Sequence**:

1. Configuration validation
2. Client initialization (Paperless, LLM)
3. Python process setup
4. Cache warm-up
5. Server start
6. Health check readiness

## Future Development

### Planned Enhancements

1. **Enhanced Embedding Cache**:
   - Disk persistence for faster startup
   - TTL-based invalidation for stale embeddings
   - Compression for memory efficiency

2. **Batch Processing**:
   - Process multiple documents in single Python request
   - Implement request batching for efficiency
   - Optimize for bulk operations

3. **Model Management**:
   - Dynamic model loading/unloading
   - Model versioning
   - A/B testing for model selection
   - Model performance monitoring

4. **Native Go Implementation**:
   - Replace Python with ONNX runtime
   - Pure Go tensor operations
   - Reduced memory footprint
   - Faster startup time

5. **Enhanced Manual Processing**:
   - More filter options (by date, document type, etc.)
   - Progress tracking for long-running batch jobs
   - Scheduled automatic cleanup of missed documents
   - Priority-based processing queues

6. **Cache Optimization**:
   - Adaptive warm-up based on tag count
   - Background cache refresh
   - Cache sharing between instances
   - Predictive pre-warming

7. **Zero API Overhead Extensions**:
   - Cache document types and correspondents
   - Intelligent cache invalidation
   - Background cache synchronization
   - Cache statistics API endpoint

### Research Areas

1. **Embedding Quality**:
   - Model comparison for different document types
   - Fine-tuning for domain-specific documents
   - Multi-modal embeddings (text + metadata)

2. **Performance Optimization**:
   - GPU acceleration for embedding generation
   - Quantized models for reduced memory
   - Streaming document processing
   - Edge deployment optimizations

3. **Intelligent Processing**:
   - Learning from user corrections
   - Adaptive threshold tuning
   - Document clustering for batch processing
   - Predictive tagging based on document history

## API Reference Updates

### New Endpoint: `POST /process/untagged`

**Purpose**: Process all documents in Paperless-ngx that have no tags

**Request**:

```http
POST /process/untagged HTTP/1.1
Host: localhost:8080
Content-Type: application/json
```

**Response**:

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

**Behavior**:

1. Fetches all documents from Paperless-ngx with `?is_tagged=false` filter
2. Processes each document sequentially
3. Continues processing even if individual documents fail
4. Returns statistics about processed/failed documents

**Error Handling**:

- HTTP 400 if Paperless-ngx API returns error
- Individual document failures don't stop batch processing
- Failed document IDs are returned in response

### Cache Warm-up Endpoint (Future)

**Potential Endpoint**: `POST /cache/warmup`

**Purpose**: Manually trigger cache warm-up without restarting service

**Use Cases**:

- After adding many new tags
- When switching models
- Performance tuning

## Code Organization Patterns

### Shared Processing Logic

The `Process()` method in `handler.go` is now shared between:

- `HandleWebhook()`: Single document from webhook
- `HandleProcessUntagged()`: Batch of untagged documents

This pattern ensures consistency and reduces code duplication.

### Paperless-ngx Client Extensions

New method added to support manual processing:

```go
func (c *Client) GetDocumentsWithoutTags(reqID string) ([]Document, error)
```

This follows the existing pattern of Paperless-ngx API wrapper methods.

### Request Tracing Pattern

**Middleware Approach**:

```go
func requestMiddleware(handler *Handler, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        reqID := uuid.New().String()
        ctx := context.WithValue(r.Context(), "reqid", reqID)

        handler.logger.Info(nil, "%s %s REQID=%s", r.Method, r.URL.Path, reqID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

**Propagation Through Components**:

- Request ID passed as parameter to all methods
- Automatically included in all log messages
- Enables end-to-end request tracing

### Cache Integration Pattern

**Dependency Injection**:

```go
func NewHandler(
    logger *utils.Logger,
    paperless *paperless.Client,
    llm *llm.Client,
    semanticMatcher semantic.Matcher,
    cfg *config.Config,
    tagsCache *utils.TagsCache,  // Cache injected as dependency
) *Handler {
    return &Handler{
        logger:          logger,
        paperless:       paperless,
        llm:             llm,
        semanticMatcher: semanticMatcher,
        cfg:             cfg,
        tagsCache:       tagsCache,
    }
}
```

**Zero API Overhead Implementation**:

```go
// Direct cache access instead of API calls
cachedTags := h.tagsCache.GetCachedTags()

// Log cache statistics
stats := h.tagsCache.Stats()
h.logger.Info(
    &reqID,
    "Tags Cache: size=%d, reads=%d, updates=%d, uptime=%.0fs",
    stats["size"],
    stats["total_reads"],
    stats["total_updates"],
    stats["uptime_seconds"],
)
```

## Version History

### v1.6.0 (Current)

- **Simplified Architecture**: Replaced worker pool with single Python process
- **Zero API Overhead Design**: Eliminates redundant Paperless API calls by using pre-warmed cache
- **Cache Warm-up**: Pre-loads tag embeddings at startup for optimal first-request performance
- **Batch Cache Operations**: Efficient `AddNewTags()` method for bulk cache updates
- **Improved Startup Performance**: Detailed warm-up progress logging and monitoring
- **Enhanced Cache Statistics**: Better tracking of cache effectiveness and activity
- **Removed Worker Pool Complexity**: Simplified configuration and error handling

### v1.5.0

- **Cache Warm-up**: Pre-loads tag embeddings at startup for optimal first-request performance
- **Batch Cache Operations**: Efficient `AddNewTags()` method for bulk cache updates
- **Sequential Worker Initialization**: Workers warmed up sequentially to prevent CPU spikes
- **Blocking Initialization**: Service waits for all workers to be ready before accepting requests
- **Improved Startup Performance**: Detailed warm-up progress logging and monitoring
- **Enhanced Cache Statistics**: Better tracking of cache effectiveness and hit rates
- **Zero API Overhead Design**: Eliminates redundant Paperless API calls by using pre-warmed cache

### v1.4.0

- **Request Tracing**: Automatic UUID generation and propagation for all requests
- **Embedding Cache**: Intelligent in-memory cache for tag embeddings with 10x performance improvement
- **Enhanced Logging**: Detailed cache statistics and performance metrics
- **Improved Observability**: Request IDs in all logs for easy debugging

### v1.3.0

- **Manual Processing**: Added `/process/untagged` endpoint for batch processing
- **Error Resilience**: Batch processing continues on individual failures
- **Enhanced Logging**: Detailed statistics for manual processing
- **Shared Logic**: Refactored `Process()` method for reuse

### v1.2.0

- **Worker Pool Architecture**: Implemented PythonWorkerPool for concurrent semantic matching
- **Auto Worker Calculation**: Dynamic worker count based on system resources
- **Enhanced Error Handling**: Structured error responses with debug information

### v1.1.0

- **Multilingual Support**: Added configurable sentence-transformers models
- **Model Selection**: Support for different embedding models via configuration

### v1.0.0

- **Initial Release**: Basic webhook processing pipeline
- **Text Reduction**: Intrinsic reduction algorithm for long documents
- **Semantic Matching**: Embedded Python with all-MiniLM-L6-v2 model
- **LLM Integration**: Structured metadata extraction via LLM API

## Migration Guide

### Upgrading from v1.5.0 to v1.6.0

**Backward Compatibility**:

- All existing APIs remain unchanged
- Configuration format simplified (removed worker timing configs)
- No data migration required

**Performance Impact**:

- **Startup time**: Similar warm-up time (1-2 seconds)
- **First request**: Same performance (~20-50ms with warm-up)
- **Memory usage**: Reduced overhead (single Python process vs multiple workers)
- **CPU usage**: Simplified process management
- **API calls**: Zero additional calls for tag lookups during processing

**Configuration Changes**:

- **Removed environment variables**:
  - `SEMANTIC_PYTHON_PROCESS_STARTUP_DELAY`
  - `SEMANTIC_PYTHON_PROCESS_SHUTDOWN_TIMEOUT`
  - `SEMANTIC_PYTHON_PROCESS_KILL_TIMEOUT`
- **Simplified configuration**: No more worker pool timing settings

**Monitoring Changes**:

- Simplified log messages (no worker-specific logs)
- Same cache statistics and performance metrics
- Better startup performance tracking
- API call reduction metrics maintained

### Deployment Recommendations

**Production Deployment**:

1. Test with development environment first
2. Monitor startup logs for warm-up completion
3. Verify first request performance improvement
4. Check memory usage with single Python process
5. Monitor cache statistics during normal operation
6. Verify zero API overhead by checking logs for tag API calls

**Resource Planning**:

- Ensure sufficient memory for embedding cache
- Consider tag count when estimating memory needs
- Monitor Python process memory usage
- Plan for warm-up time during startup

**Rollback Strategy**:

- Keep previous version available
- Monitor key metrics after upgrade
- Have rollback plan if performance degrades
- Test with representative document load

---

_This documentation is maintained alongside the codebase. For the latest information, refer to the source code and inline comments._

_Last Updated: 2026-02-05_

_Implementation Version: 1.6.0_

_Changes: Simplified architecture to single Python process, removed worker pool complexity, maintained zero API overhead design, updated documentation to reflect simplified architecture_
