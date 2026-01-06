# Vastiva - Intelligent Media Converter

A high-performance, AI-powered media transcoding platform built with Go.

## Features

- **Job Queue**: Concurrent processing with goroutine worker pool
- **Hardware Acceleration**: NVIDIA NVENC, Intel QSV, AMD AMF, VAAPI
- **AI Integration**: Gemini, OpenAI, Claude, Ollama support
- **Web UI**: React SPA (to be added)

## Quick Start

### Prerequisites

- Go 1.22+
- Docker (for containerized deployment)
- FFmpeg (for transcoding)

### Development

```bash
# Install dependencies
go mod download

# Run locally
go run ./cmd/server

# Build binary
go build -o vastiva ./cmd/server
```

### Docker Deployment

```bash
# Create .env file
cp .env.example .env
# Edit .env with your settings

# Build and run
docker compose up -d --build
```

## Project Structure

```
vastiva-go/
├── cmd/
│   └── server/
│       └── main.go         # Entry point
├── internal/
│   ├── api/
│   │   └── routes.go       # REST API handlers
│   ├── config/
│   │   └── config.go       # Environment config
│   ├── jobs/
│   │   └── manager.go      # Job queue with workers
│   └── media/
│       └── (ffmpeg/makemkv wrappers)
├── web/
│   └── dist/               # Frontend static files
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `8080` |
| `SOURCE_DIR` | Media source directory | `/storage` |
| `DEST_DIR` | Output directory | `/output` |
| `GPU_VENDOR` | GPU type (nvidia/intel/amd/cpu) | `cpu` |
| `MAX_CONCURRENT_JOBS` | Worker count | `2` |
| `AI_PROVIDER` | AI backend (gemini/openai/claude/ollama/none) | `none` |
| `AI_API_KEY` | API key for AI provider | |
| `ADMIN_PASSWORD` | Web UI password | |

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/health` | Health check |
| `GET` | `/api/jobs` | List all jobs |
| `POST` | `/api/jobs` | Create new job |
| `GET` | `/api/jobs/:id` | Get job by ID |
| `DELETE` | `/api/jobs/:id` | Cancel job |
| `GET` | `/api/config` | Get configuration |

## License

Proprietary - Vastiva Pro
