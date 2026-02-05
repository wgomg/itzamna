# Developer Documentation

## Architecture Overview

### System Design

The Document Processing Service is built as a Go microservice with an embedded Python component for semantic matching. The architecture follows a pipeline pattern where documents flow through sequential processing stages.

### Core Design Principles

1. **Separation of Concerns**: Each component has a single responsibility
2. **Concurrency First**: Designed for parallel processing from the ground up
3. **Resource Efficiency**: Optimized for low-resource environments
4. **Extensibility**: Interfaces and factories allow for easy component replacement
5. **Manual Recovery**: Support for processing missed documents via API
6. **Observability**: Automatic request tracing and detailed logging

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
- `GetTags()`: Retrieve all tags with pagination support
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

**Purpose**: Find semantically similar tags using sentence-transformers with intelligent caching

#### Worker Pool Architecture

**PythonWorkerPool**:

- Manages pool of Python worker processes
- Buffered task queue (100 tasks)
- Automatic worker lifecycle management
- Health monitoring and recovery

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

**Communication Protocol**:

```
Go → Python: {"model_name": "...", "top_n": 15, "min_similarity": 0.2}\n
Python → Go: {"status": "ready", "embedding_dim": 384}\n
Go → Python: {"text": "...", "existing_tags": [...]}\n
Python → Go: {"suggested_tags": [...], "cache_stats": {...}, "error": null}\n
```

#### Embedded Python System

**First-Run Setup**:

1. Extract embedded scripts to `~/.config/itzamna/python/`
2. Create Python virtual environment
3. Install dependencies (`sentence-transformers`, `torch`, `numpy`)
4. Start worker processes

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
// Python worker pool
for i := 0; i < cfg.WorkerCount; i++ {
    p.wg.Add(1)
    go p.runWorker(i)
}
```

**Task Queue**:

```go
type Task struct {
    Text         string
    ExistingTags []string
    RequestID    string  // For request tracing
    Result       chan<- TaskResult
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
    Error         string   `json:"error,omitempty"`
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

## Performance Considerations

### Memory Management

**Python Workers**:

- Each worker loads model into memory (~90-420MB)
- Worker count auto-calculated based on available memory
- Consider smaller models for memory-constrained environments

**Go Service**:

- Document content stored in memory during processing
- Consider streaming for very large documents
- Implement LRU cache for frequent operations

### CPU Utilization

**Worker Pool Sizing**:

- Default: 1-6 workers based on CPU cores
- Adjust based on workload characteristics
- Monitor CPU usage under load

**Batch Processing**:

- Consider batching similar operations
- Implement request coalescing for identical documents
- Use connection pooling for HTTP clients

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
logger.Info(&reqID, "Semantic matcher stats: process_ms=%d, new_tags=%d, cache_size=%d, total_cache_hit_rate=%f",
    processingTimeMS, newlyCachedTags, cacheSize, totalHitRate)
```

**Log Levels**:

- `debug`: Detailed processing information, cache statistics
- `info`: Normal operation events, request tracing
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

**Health Checks**:

```go
func (p *PythonWorkerPool) HealthCheck() error {
    // Test worker responsiveness
    return nil
}
```

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

**Graceful Shutdown**:

```go
func (p *PythonWorkerPool) Close() error {
    close(p.taskQueue)  // Stop accepting new tasks
    p.wg.Wait()         // Wait for existing tasks to complete
    // Cleanup Python processes
    return nil
}
```

## Future Development

### Planned Enhancements

1. **Enhanced Embedding Cache**:
   - Shared cache between Python workers
   - Disk persistence for faster startup
   - TTL-based invalidation for stale embeddings

2. **Batch Processing**:
   - Process multiple documents in single Python request
   - Implement request batching for efficiency
   - Optimize for bulk operations

3. **Model Management**:
   - Dynamic model loading/unloading
   - Model versioning
   - A/B testing for model selection

4. **Native Go Implementation**:
   - Replace Python with ONNX runtime
   - Pure Go tensor operations
   - Reduced memory footprint

5. **Enhanced Manual Processing**:
   - More filter options (by date, document type, etc.)
   - Progress tracking for long-running batch jobs
   - Scheduled automatic cleanup of missed documents

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

## Version History

### v1.4.0 (Current)

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

---

_This documentation is maintained alongside the codebase. For the latest information, refer to the source code and inline comments._

_Last Updated: 2026-02-05_
_Implementation Version: 1.4.0_
