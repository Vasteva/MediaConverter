# Vastiva - Intelligent Media Converter

A high-performance, AI-powered media transcoding platform built with Go.

## Features

- **Job Queue**: Concurrent processing with goroutine worker pool
- **Hardware Acceleration**: NVIDIA NVENC, Intel QSV, AMD AMF, VAAPI (Auto-detected)
- **AI Integration**: Gemini, OpenAI, Claude, Ollama support
- **Vastiva Pro Features**:
    - **Adaptive Encoding**: AI-powered CRF selection for optimal quality/size.
    - **Smart Metadata**: Automatic filename cleaning and Title/Year extraction.
    - **Whisper Subtitles**: (Coming Soon) AI-generated speech-to-text.
- **Web UI**: Modern React Dashboard with real-time monitoring.

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
│       └── main.go              # Entry point
├── internal/
│   ├── api/
│   │   └── routes.go            # REST API handlers
│   ├── config/
│   │   └── config.go            # Environment config
│   ├── jobs/
│   │   └── manager.go           # Job queue with workers ✅
│   └── media/
│       ├── ffmpeg.go            # FFmpeg wrapper ✅
│       ├── makemkv.go           # MakeMKV wrapper ✅
│       ├── progress.go          # Progress tracking ✅
│       ├── media_test.go        # Test suite ✅
│       └── README.md            # Detailed documentation
├── web/
│   └── dist/                    # Frontend static files (WIP)
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

**Implementation Status:**
- ✅ FFmpeg wrapper with multi-GPU support (NVIDIA, Intel, AMD, CPU)
- ✅ MakeMKV wrapper for disc extraction
- ✅ Real-time progress tracking with ETA calculation
- ✅ Job manager integration
- ✅ **File scanner with multiple modes** (manual, startup, periodic, watch, hybrid)
- ✅ **Multi-directory monitoring** with recursive scanning
- 🚧 Frontend UI (React/Vite scaffold in place)

See [`internal/media/README.md`](internal/media/README.md) for detailed media processing documentation.  
See [`internal/scanner/README.md`](internal/scanner/README.md) for file scanner configuration and usage.

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
| `AI_MODEL` | AI model to use | |
| `LICENSE_KEY` | Vastiva Pro license key | |
| `ADMIN_PASSWORD` | Web UI password | |
| `SCANNER_ENABLED` | Enable automatic file scanning | `false` |
| `SCANNER_MODE` | Scan mode (manual/startup/periodic/watch/hybrid) | `manual` |
| `SCANNER_INTERVAL_SEC` | Scan interval for periodic mode | `300` |
| `SCANNER_AUTO_CREATE` | Auto-create jobs for discovered files | `true` |

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
