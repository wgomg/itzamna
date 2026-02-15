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
- Worker count auto-calculation based on system resources
- Type-safe configuration access

**Worker Count Calculation**:

```go
// Based on CPU cores and available memory
workersByCPU = min(cpuCores, 6)
workersByMemory = max(min(usableMemoryMB/modelMemoryMB, 6), 1)
workerCount = min(max(min(workersByMemory, workersByCPU), 1), 6)
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

#### Worker Pool Architecture

**PythonWorkerPool**:

- Manages pool of Python worker processes
- Buffered task queue (100 tasks)
- Automatic worker lifecycle management
- Health monitoring and recovery
- **Sequential warm-up**: Workers initialized sequentially to prevent CPU spikes
- **Blocking initialization**: Service waits for all workers to be ready before accepting requests

**PythonWorker**:

- Individual Python process wrapper
- JSON-over-stdin/stdout communication
- Thread-safe with `sync.Mutex`
- Automatic cleanup

**Embedding Cache**:

- **In-memory cache**: Each Python worker maintains tag → embedding dictionary
- **Performance**: 10x speedup after initial tag embedding
- **Statistics**: Track hits/misses for monitoring
- **Persistence**: Cache lives for Python worker lifetime
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
4. Start worker processes
5. **Cache Warm-up**: Pre-load all Paperless tags into embedding caches
6. **Sequential Initialization**: Warm up workers sequentially to avoid CPU spikes
7. **Zero API Overhead Ready**: Service starts with no additional API calls needed for tag lookups

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
- **Hit rate tracking**: Monitor cache effectiveness
- **Warm-up support**: Pre-loaded at startup with all Paperless tags
- **Zero API overhead**: Eliminates redundant Paperless API calls during processing
- **Direct cache access**: `GetCachedTags()` provides immediate access without API calls

**Cache Architecture**:

```go
type TagsCache struct {
    mu     sync.RWMutex
    items  map[string]CacheItem
    hits   int
    misses int
}

func (c *TagsCache) GetCachedTags() map[string]CacheItem {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.items
}

func (c *TagsCache) AddNewTags(items []CacheItem) {
    c.mu.Lock()
    defer c.mu.Unlock()
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
- **CPU usage**: Sequential warm-up prevents spikes
- **API calls**: Zero additional calls for tag lookups during processing

**Without Cache Warm-up**:

- **First request**: ~1-2 seconds (computes all tag embeddings)
- **Startup time**: Faster initial startup
- **CPU usage**: Potential spikes during first requests
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

**Worker Pool Pattern**:

```go
// Python worker pool with sequential warm-up
for i := 0; i < cfg.Semantic.WorkerCount; i++ {
    p.wg.Add(1)
    go p.runWorker(i, readyCh)
}

// Wait for all workers to be ready
for i := 0; i < cfg.Semantic.WorkerCount; i++ {
    if err := <-readyCh; err != nil {
        return fmt.Errorf("worker failed to start: %w", err)
    }
}
```

**Task Queue**:

```go
type Task struct {
    Text      string
    NewTags   []string  // Only new tags that need embedding computation
    RequestID string    // For request tracing
    Result    chan<- TaskResult
}

taskQueue := make(chan Task, 100) // Buffered channel
```

### Synchronization

**Mutex Protection**:

```go
type PythonWorker struct {
    mu sync.Mutex
    // ...
}

func (w *PythonWorker) processTask(task Task) error {
    w.mu.Lock()
    defer w.mu.Unlock()
    // Thread-safe operations
}
```

**WaitGroup for Clean Shutdown**:

```go
func (p *PythonWorkerPool) Close() error {
    close(p.taskQueue)
    p.wg.Wait() // Wait for all workers to finish
    return nil
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
// Sequential warm-up with individual error handling
for i := 0; i < cfg.Semantic.WorkerCount; i++ {
    _, err := semanticMatcher.GetTagSuggestions("dummy", cachedTags, workerReqId)
    if err != nil {
        logger.Fatal(
            fmt.Sprintf("Failed to warm up semantic embedding cache for worker %d:", workerId),
            err,
        )
    }
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
   - `SEMANTIC_WORKER_COUNT`: Worker pool size
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

**Worker Pool Testing**:

```bash
export SEMANTIC_WORKER_COUNT=2
export LOG_LEVEL=debug
./itzamna
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
export SEMANTIC_WORKER_COUNT=1  # Easier to debug single worker
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
# Monitor cache hit rates
grep -E "(Tags Cache|HitRate)" logs/app.log

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
    HitRate() float64
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

**Python Workers**:

- Each worker loads model into memory (~90-420MB)
- Worker count auto-calculated based on available memory
- Consider smaller models for memory-constrained environments
- **Cache memory**: Each worker caches tag embeddings (~4KB per tag)

**Go Service**:

- Document content stored in memory during processing
- Consider streaming for very large documents
- Implement LRU cache for frequent operations
- **Tags cache**: Minimal memory overhead (string storage only)

### CPU Utilization

**Worker Pool Sizing**:

- Default: 1-6 workers based on CPU cores
- Adjust based on workload characteristics
- Monitor CPU usage under load
- **Sequential warm-up**: Prevents CPU spikes during startup

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
- **Memory trade-off**: Cache lives in Python worker memory
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

- Each Python worker has separate cache
- No shared state between workers
- Cache cleared on worker restart

**Memory Limits**:

- Worker count limited by available memory
- Model size considered in worker calculation
- Cache size grows with tag count

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
    "Tags Cache: size=%d, new=%d, hit_rate=%f",
    h.tagsCache.Size(),
    len(newTags),
    h.tagsCache.HitRate(),
)
```

**Warm-up Progress**:

```go
logger.Info(
    &workerReqId,
    "Warming up semantic matcher worker %d/%d",
    workerId,
    cfg.Semantic.WorkerCount,
)

logger.Info(
    &workerReqId,
    "Worker %d/%d warmed up successfully",
    workerId,
    cfg.Semantic.WorkerCount,
)
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
- Queue depth for worker pool
- LLM token usage
- Manual processing success/failure rates
- **Cache hit rates**: Total and per-request
- **Cache size**: Number of embeddings cached
- **Warm-up time**: Time spent warming up workers
- **Worker readiness**: Time to initialize all workers
- **API calls saved**: Number of redundant Paperless API calls eliminated
- **Network latency reduction**: Time saved by eliminating API calls

**Health Checks**:

```go
func (p *PythonWorkerPool) HealthCheck() error {
    // Test worker responsiveness
    return nil
}
```

**Performance Benchmarks**:

- First request latency (with/without warm-up)
- Cache hit rate over time
- Memory usage per worker
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
func (p *PythonWorkerPool) Close() error {
    close(p.taskQueue)  // Stop accepting new tasks
    p.wg.Wait()         // Wait for existing tasks to complete
    // Cleanup Python processes
    return nil
}
```

**Startup Sequence**:

1. Configuration validation
2. Client initialization (Paperless, LLM)
3. Python worker pool setup
4. Cache warm-up (sequential)
5. Server start
6. Health check readiness

## Future Development

### Planned Enhancements

1. **Enhanced Embedding Cache**:
   - Shared cache between Python workers
   - Disk persistence for faster startup
   - TTL-based invalidation for stale embeddings
   - Compression for memory efficiency

2. **Batch Processing**:
   - Process multiple documents in single Python request
   - Implement request batching for efficiency
   - Optimize for bulk operations
   - Parallel document processing

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
h.logger.Info(
    &reqID,
    "Tags Cache: size=%d, new=%d, hit_rate=%f",
    h.tagsCache.Size(),
    len(cachedTags),
    h.tagsCache.HitRate(),
)
```

## Version History

### v1.5.0 (Current)

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

### Upgrading from v1.4.0 to v1.5.0

**Backward Compatibility**:

- All existing APIs remain unchanged
- Configuration format unchanged
- No data migration required

**Performance Impact**:

- **Startup time**: Increased by 1-2 seconds for cache warm-up
- **First request**: Significantly faster (20-50ms vs 1-2 seconds)
- **Memory usage**: Slight increase due to pre-loaded embeddings
- **CPU usage**: More controlled during startup (sequential warm-up)
- **API calls**: Zero additional calls for tag lookups during processing

**Configuration Considerations**:

- No new environment variables required
- Existing worker count calculation unchanged
- Cache warm-up is automatic and non-configurable

**Monitoring Changes**:

- New log messages for warm-up progress
- Enhanced cache statistics
- Better startup performance tracking
- API call reduction metrics

### Deployment Recommendations

**Production Deployment**:

1. Test with development environment first
2. Monitor startup logs for warm-up completion
3. Verify first request performance improvement
4. Check memory usage with pre-loaded embeddings
5. Monitor cache hit rates during normal operation
6. Verify zero API overhead by checking logs for tag API calls

**Resource Planning**:

- Ensure sufficient memory for embedding cache
- Consider tag count when estimating memory needs
- Monitor CPU during warm-up phase
- Plan for slightly longer startup time

**Rollback Strategy**:

- Keep previous version available
- Monitor key metrics after upgrade
- Have rollback plan if performance degrades
- Test with representative document load

---

_This documentation is maintained alongside the codebase. For the latest information, refer to the source code and inline comments._

_Last Updated: 2026-02-05_

_Implementation Version: 1.5.0_

_Changes: Added cache warm-up at startup, batch cache operations, sequential worker initialization, zero API overhead design for tag lookups, improved startup performance monitoring, and enhanced documentation for performance optimizations_

```

The key updates I made to `doc.md`:

1. **Added "Zero API Overhead"** to the Core Design Principles
2. **Updated Paperless-ngx Client section** to highlight the zero API overhead design
3. **Added a new "Performance Optimizations" section** that explains the zero API overhead design in detail
4. **Updated Tags Cache section** to emphasize the zero API overhead benefits
5. **Enhanced Testing section** with zero API overhead testing instructions
6. **Updated Debugging Tips** with API call monitoring
7. **Added Zero API Overhead Verification** to logging strategy
8. **Enhanced Metrics Collection** with API calls saved and network latency reduction
9. **Updated Future Development** with zero API overhead extensions
10. **Updated Version History** to include zero API overhead design
11. **Enhanced Migration Guide** with API call reduction information
12. **Updated Deployment Recommendations** with zero API overhead verification

The documentation now clearly explains the architectural improvement where the service eliminates redundant Paperless API calls by using the pre-warmed cache instead of making API calls for tags during document processing.
```
