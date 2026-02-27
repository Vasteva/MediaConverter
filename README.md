# Vastiva - AI-Powered Media Converter

A production-ready, AI-enhanced media transcoding platform with intelligent optimization and natural language search.

## ✨ Features

### Core Capabilities
- **Job Queue System**: Concurrent processing with goroutine worker pool
- **Hardware Acceleration**: NVIDIA NVENC, Intel QSV, AMD VAAPI (Auto-detected)
- **Multi-Format Support**: H.265/HEVC encoding with 10-bit color depth
- **Real-time Monitoring**: Live progress tracking, FPS, and ETA calculation
- **Automated Scanner**: Watch directories for new media with multiple scan modes

### 🤖 AI-Powered Features (Premium)
- **Adaptive Encoding**: AI analyzes media to select optimal CRF values
- **Smart Metadata**: Automatic filename cleaning and Title/Year extraction
- **Whisper Subtitles**: AI-generated speech-to-text transcription (OpenAI)
- **AI Upscaling**: Enhance videos to 1080p or 4K with intelligent scaling
- **Natural Language Search**: Find media using semantic queries
- **AI-Enhanced Dashboard**: Storage savings analytics and efficiency scoring

### 🎨 Modern Web Interface
- **Premium Dashboard**: Glassmorphism design with real-time insights
- **Job Management**: Create, monitor, and cancel transcoding jobs
- **Scanner Configuration**: Visual setup for automated media discovery
- **Settings Panel**: AI provider configuration and license management
- **Dark/Light Themes**: Polished UI with smooth transitions

## 🚀 Quick Start

### Docker Deployment (Recommended)

```bash
# Clone repository
git clone https://github.com/Vasteva/MediaConverter.git
cd MediaConverter

# Create environment file
cp .env.example .env
nano .env  # Configure your settings

# Deploy with Docker Compose
docker-compose up -d

# Access web interface
open http://localhost:8091
```

### NVIDIA GPU Support

For NVIDIA GPU acceleration (NVENC encoding):

```bash
# Prerequisites: nvidia-container-toolkit installed
# See: https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html

# Deploy with NVIDIA GPU support
docker compose -f docker-compose.yml -f docker-compose.nvidia.yml up -d
```

### Development Setup

```bash
# Install dependencies
go mod download
cd web && npm install

# Run development server
go run ./cmd/server
```

## 📦 Production Deployment

See [Deployment Guide](docs/getting-started/deployment.md) for comprehensive deployment guide including:
- GitHub Actions CI/CD pipeline setup
- Automated builds and deployments
- Traefik integration for HTTPS
- Backup and monitoring procedures

Quick deployment to production server:
```bash
# Copy deployment script
scp deploy.sh root@your-server:/tmp/

# Run deployment
ssh root@your-server
sudo /tmp/deploy.sh
```

## 🏗️ Project Structure

```
vastiva/
├── cmd/server/           # Application entry point
├── internal/
│   ├── api/             # REST API routes
│   ├── ai/              # AI provider integrations
│   │   ├── meta/        # Smart metadata cleaning
│   │   ├── search/      # Natural language search
│   │   └── whisper/     # Subtitle generation
│   ├── config/          # Configuration management
│   ├── jobs/            # Job queue and workers
│   ├── media/           # FFmpeg/MakeMKV wrappers
│   ├── scanner/         # Automated file discovery
│   ├── security/        # Path validation & masking
│   └── system/          # System monitoring
├── web/                 # React frontend
├── Dockerfile           # Multi-stage build (CPU/Intel/AMD)
├── Dockerfile.nvidia    # NVIDIA CUDA build
├── docker-compose.yml   # Production orchestration
├── docker-compose.nvidia.yml  # NVIDIA GPU override
└── .github/workflows/   # CI/CD pipeline
```

## 🔧 Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `80` |
| `SOURCE_DIR` | Media source directory | `/storage` |
| `DEST_DIR` | Output directory | `/output` |
| `GPU_VENDOR` | GPU type (nvidia/intel/amd/cpu) | `cpu` |
| `AI_PROVIDER` | AI backend (openai/claude/gemini/ollama) | `none` |
| `AI_API_KEY` | API key for AI provider | - |
| `AI_MODEL` | AI model to use | - |
| `LICENSE_KEY` | Vastiva Pro license key | - |
| `SCANNER_ENABLED` | Enable automatic scanning | `false` |
| `SCANNER_MODE` | Scan mode (watch/periodic/hybrid) | `manual` |

### AI Provider Setup

**OpenAI (Recommended for all features)**
```env
AI_PROVIDER=openai
AI_API_KEY=sk-your-key-here
AI_MODEL=gpt-4
```

**Ollama (Local, no transcription)**
```env
AI_PROVIDER=ollama
AI_ENDPOINT=http://localhost:11434
AI_MODEL=llama2
```

**Claude (No transcription support)**
```env
AI_PROVIDER=claude
AI_API_KEY=sk-ant-your-key
AI_MODEL=claude-3-opus-20240229
```

## 📡 API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/health` | Health check |
| `GET` | `/api/stats` | System statistics |
| `GET` | `/api/dashboard/stats` | AI insights and analytics |
| `GET` | `/api/jobs` | List all jobs |
| `POST` | `/api/jobs` | Create new job |
| `DELETE` | `/api/jobs/:id` | Cancel job |
| `GET` | `/api/config` | Get system configuration |
| `POST` | `/api/config` | Update configuration |
| `GET` | `/api/scanner/config` | Get scanner settings |
| `POST` | `/api/scanner/config` | Update scanner |
| `GET` | `/api/search?q=query` | Natural language search |

## 🔒 Security

- **Path Sandboxing**: All file operations restricted to configured directories
- **Credential Masking**: API keys and licenses masked in responses
- **Input Validation**: Strict validation on all user inputs
- **HTTPS Support**: Traefik integration for automatic SSL certificates

See [Security Audit](docs/security/audit.md) for detailed security analysis.

## 📊 Monitoring

### View Logs
```bash
docker-compose logs -f vastiva
```

### Check Container Status
```bash
docker-compose ps
```

### System Resources
Access the dashboard at `/` for real-time CPU, memory, GPU, and disk metrics.

## 🛠️ Troubleshooting

### GPU Not Detected
```bash
# Verify GPU device
ls -la /dev/dri

# Check GPU vendor setting
docker-compose exec vastiva env | grep GPU_VENDOR
```

### AI Features Not Working
```bash
# Verify AI configuration
docker-compose exec vastiva env | grep AI_

# Check license status
curl http://localhost:8091/api/config | jq '.isPremium'
```

### Scanner Not Running
```bash
# Check scanner configuration
curl http://localhost:8091/api/scanner/config | jq

# Verify watch directories exist and are accessible
```

## 📚 Documentation

All documentation is organized in the [`docs/`](docs/) folder:

- **Getting Started**
  - [User Guide](docs/user-guide.md) - Installation, configuration, and feature walkthrough
  - [Deployment Guide](docs/getting-started/deployment.md) - Production deployment and CI/CD
- **Architecture**
  - [System Overview](docs/architecture/overview.md) - Architecture and data flows
  - [Backend Implementation](docs/architecture/implementation/backend.md) - FFmpeg wrapper details
  - [Frontend Implementation](docs/architecture/implementation/frontend.md) - React UI design
  - [Scanner Implementation](docs/architecture/implementation/scanner.md) - Auto-discovery system
- **Development**
  - [Testing Guide](docs/development/testing.md) - Test procedures and checklists
  - [Task List](docs/development/tasks.md) - Current status and remaining work
- **Security**
  - [Security Audit](docs/security/audit.md) - Security analysis and hardening
- **Package Docs** (inline with code)
  - [Media Processing](internal/media/README.md) - FFmpeg integration details
  - [File Scanner](internal/scanner/README.md) - Automated discovery system

## 🎯 Roadmap

- [x] Core transcoding engine
- [x] Hardware acceleration
- [x] Job queue system
- [x] Web interface
- [x] AI metadata cleaning
- [x] AI adaptive encoding
- [x] Whisper subtitles
- [x] AI upscaling
- [x] Natural language search
- [x] AI-enhanced dashboard
- [x] CI/CD pipeline
- [x] **Advanced scheduling**
- [x] **Per-job AI logging**

## 📄 License

GPL v3

---

**Built with ❤️ using Go, React, and AI**
