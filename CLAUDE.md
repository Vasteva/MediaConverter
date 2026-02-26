# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

Vastiva Media Converter is an AI-powered media transcoding platform. A Go/Fiber backend serves a React frontend, managing a job queue that runs FFmpeg and MakeMKV via subprocess. Optional AI integrations (OpenAI, Claude, Gemini, Ollama) provide metadata cleaning, subtitle generation, media verification, and natural language search. Hardware GPU acceleration is supported for NVIDIA (NVENC), Intel (QSV), and AMD (VAAPI).

## Commands

### Backend (Go)
```bash
make build          # Build binary with size optimization flags
make run            # go run ./cmd/server (uses .env if present)
make test           # go test -v ./...
make test-full      # Create test environment at /tmp/vastiva-test then run tests
go test ./internal/jobs/...    # Run tests for a specific package
go test -run TestJobManager ./internal/jobs/    # Run a single test
```

### Frontend (React/Vite)
```bash
cd web
npm install
npm run dev         # Vite dev server (proxies /api to backend on :8080)
npm run build       # TypeScript compile + Vite build (output embedded into binary)
npm run lint        # ESLint
```

### Docker
```bash
make docker         # docker build
make up             # docker compose up -d --build
make down           # docker compose down
make logs           # docker compose logs -f
# NVIDIA GPU:
docker compose -f docker-compose.yml -f docker-compose.nvidia.yml up -d
```

## Architecture

### Request Flow
```
React (web/) → GET/POST /api/* → Fiber routes (internal/api/routes.go)
                                       ↓
                              jobs.Manager (internal/jobs/manager.go)
                                       ↓
                         media.FFmpegWrapper / media.MakeMKVWrapper
                                       ↓
                         Progress via SSE (internal/api/sse.go)
```

### Key Packages

**`internal/api/`** — Fiber HTTP routes and middleware. All routes are prefixed `/api` and require Bearer token auth except `/api/login`, `/api/setup/*`. SSE broadcasting for real-time job progress is in `sse.go`.

**`internal/jobs/`** — Priority job queue with a goroutine-based worker pool. Jobs are persisted to `/data/jobs.json` for restart recovery. `manager.go` is the central coordinator: it holds references to the AI provider, media wrappers, and subtitle module.

**`internal/config/`** — Loads from environment variables with `/data/config.json` as persistent override. The config struct is the single source of truth for all runtime settings. Many fields have env var fallbacks so Docker deployments work without a config file.

**`internal/media/`** — Thin wrappers around `ffmpeg`/`ffprobe` and `makemkvcon` CLI binaries via `exec.Command`. Progress is parsed from stderr using regex. GPU encoder selection (nvenc/qsv/vaapi/libx264) is determined from `config.GPUVendor`.

**`internal/scanner/`** — Watches directories for new media files and auto-creates jobs. Supports modes: `manual`, `startup`, `periodic`, `watch`, `hybrid`. Tracks processed files in `/data/processed.json`.

**`internal/ai/`** — Pluggable provider interface (`Provider` in `provider.go`) implemented by openai, claude, gemini, and ollama adapters. `VerifyMedia` is implemented separately per provider in `*_verify.go` files. `ai/meta/` cleans metadata; `ai/search/` handles natural language queries.

**`internal/subtitles/`** — Subtitle generation module (recently refactored from `internal/ai/whisper/`). Integrates with the jobs manager for SRT output.

**`internal/security/`** — `ValidatePath()` sandboxes all file operations to configured source/destination directories. API responses mask credential values as `key[:4]....key[-4:]`.

**`web/src/`** — React 19 single-page app. `App.tsx` owns all state and passes props down to view components. Real-time job updates come from SSE. All API calls use Bearer token from `localStorage`.

### Configuration
Environment variables are the primary config mechanism (see `.env.example`). Key ones: `PORT`, `SOURCE_DIR`, `DEST_DIR`, `GPU_VENDOR` (auto/nvidia/intel/amd/cpu), `AI_PROVIDER` (none/openai/claude/gemini/ollama), `AI_API_KEY`, `AI_MODEL`, `ADMIN_PASSWORD`, `LICENSE_KEY` (format: `VASTIVA-PRO-{USERID}-{CHECKSUM}`), `MAX_CONCURRENT_JOBS`, `SCANNER_MODE`.

### Premium Features (License-gated)
AI metadata cleaning, adaptive encoding, subtitle generation, `VerifyMedia`, AI upscaling, natural language search, and dashboard analytics all require a valid `VASTIVA-PRO-*` license key. License validation is in `internal/license/license.go`. A keygen utility lives at `cmd/keygen/`.

### Frontend Build Embedding
`frontend.go` at the repo root contains an `//go:embed` directive that bakes the built `web/dist/` output into the Go binary. Always run `cd web && npm run build` before `make build` if frontend changes need to be included.

### Data Persistence (Docker volumes)
- `/data/config.json` — persistent settings
- `/data/jobs.json` — job queue state
- `/data/scanner_config.json` — watch directories
- `/data/processed.json` — scanner dedup tracking
