# Document Processing Service for Paperless-ngx

A microservice that integrates with Paperless-ngx via webhooks to automatically generate and apply metadata to documents using a hybrid rule-based and LLM-based approach, with semantic tag consistency enforcement.

## Features

- **Automatic Metadata Generation**: Extract titles, tags, authors, and document types from documents
- **Semantic Tag Matching**: Suggest relevant existing tags using sentence-transformers embeddings
- **Intelligent Text Reduction**: Reduce long documents before LLM processing to save tokens
- **Multi-language Support**: Works with documents in any language using multilingual models
- **Worker Pool Architecture**: Concurrent processing with auto-scaled Python workers
- **Zero Configuration Setup**: Embedded Python scripts with automatic environment setup
- **Manual Processing**: Process untagged documents via API endpoint

## Quick Start

### Prerequisites

- Go 1.21+
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
SEMANTIC_WORKER_COUNT=auto  # auto-calculated based on system resources

# Text reduction
REDUCTION_THRESHOLD_TOKENS=2000
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
                    Text Reduction    Tag Suggestions
```

### Key Components

1. **Webhook Handler** (`internal/api/`): Receives and validates Paperless-ngx webhooks
2. **Document Fetcher** (`internal/paperless/`): Retrieves document content via REST API
3. **Text Reducer** (`internal/processor/`): Reduces long documents using TF-Graph-Position algorithm
4. **Semantic Matcher** (`internal/semantic/`): Python worker pool for tag similarity matching
5. **LLM Client** (`internal/llm/`): Sends prompts and parses structured JSON responses
6. **Document Updater**: Applies validated metadata back to Paperless-ngx

### Worker Pool Architecture

The semantic matcher uses a worker pool for concurrent processing:

- **Auto-scaled workers**: Based on CPU cores and available memory
- **Task queue**: 100-task buffer for handling bursts
- **Health monitoring**: Built-in health checks with automatic recovery
- **Graceful shutdown**: Proper cleanup of Python processes

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

## Performance

### Resource Requirements

- **CPU**: 2-4 cores recommended
- **RAM**: 1-2GB (Go) + 400-800MB (Python workers)
- **Storage**: ~500MB-1GB for Python dependencies

### Throughput

- **Documents/second**: 2-10 (varies by model and document length)
- **Concurrent processing**: Multiple workers handle requests in parallel
- **Auto-scaling**: Worker count adjusts based on system resources

## Development

### Project Structure

```
itzamna/
├── cmd/main.go                 # Entry point
├── internal/
│   ├── api/                   # Webhook handlers
│   ├── config/                # Configuration
│   ├── llm/                   # LLM client
│   ├── paperless/             # Paperless API client
│   ├── processor/             # Text reduction
│   ├── semantic/              # Semantic matching
│   └── utils/                 # Utilities
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

## Troubleshooting

### Common Issues

**Python workers fail to start:**

- Ensure Python 3.8+ is installed
- Check write permissions to `~/.config/itzamna/`
- Verify internet connectivity for model downloads

**Low similarity scores:**

- Scores typically range 0.1-0.35 for meaningful relationships
- Consider lowering `SEMANTIC_MIN_SIMILARITY` to 0.15
- Use multilingual models for non-English documents

**Memory issues:**

- Reduce `SEMANTIC_WORKER_COUNT` for constrained environments
- Use smaller models (`all-MiniLM-L6-v2` uses ~90MB per worker)

**Untagged documents not being processed:**

- Check Paperless-ngx API supports `?is_tagged=false` filter
- Verify document has no tags in Paperless-ngx
- Check service logs for API errors

**Detailed logging:**

```bash
export LOG_LEVEL=debug
export RAW_BODY_LOG=true
./itzamna
```

## License

This project uses:

- `sentence-transformers` models (Apache 2.0)
- Paperless-ngx REST API
- Go standard library and community packages

All models are open-source and freely available for commercial use.

---

_Last Updated: 2026-01-28_
_Implementation Version: 1.3.0_
_Changes: Added `/process/untagged` endpoint for manual document processing_
