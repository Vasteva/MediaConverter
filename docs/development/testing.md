# Testing Guide - Vastiva Media Converter

## Quick Test Checklist

### 1. Build Test
```bash
# Clone the repository
git clone git@github.com:Vasteva/MediaConverter.git
cd MediaConverter

# Install dependencies
go mod download

# Build the binary
go build -o vastiva ./cmd/server

# Run tests
go test ./internal/media -v
```

### 2. Basic Server Test

```bash
# Create .env file
cp .env.example .env

# Start the server
./vastiva
```

Expected output:
```
[Scanner] Disabled, not starting
Vastiva starting on :8080
```

Test the API:
```bash
# Health check
curl http://localhost:8080/api/health

# Get configuration
curl http://localhost:8080/api/config

# List jobs
curl http://localhost:8080/api/jobs
```

### 3. Scanner Test (Manual Mode)

Edit `.env`:
```bash
SCANNER_ENABLED=true
SCANNER_MODE=manual
SOURCE_DIR=/tmp/test-media
DEST_DIR=/tmp/test-output
```

Create test directories:
```bash
mkdir -p /tmp/test-media /tmp/test-output
```

Start server and check logs - scanner should be disabled in manual mode.

### 4. Scanner Test (Startup Mode)

Create test files:
```bash
# Create a test video file (or copy a real one)
touch /tmp/test-media/test-movie.mkv
# Make it large enough to pass size filter
dd if=/dev/zero of=/tmp/test-media/test-movie.mkv bs=1M count=200
```

Edit `.env`:
```bash
SCANNER_ENABLED=true
SCANNER_MODE=startup
SCANNER_AUTO_CREATE=true
```

Edit `scanner-config.json`:
```json
[
  {
    "path": "/tmp/test-media",
    "recursive": true,
    "includePatterns": ["*.mkv", "*.mp4"],
    "excludePatterns": ["*_optimized.mkv"],
    "minFileSizeMB": 10,
    "minFileAgeMinutes": 0
  }
]
```

Start server:
```bash
./vastiva
```

Expected logs:
```
[Scanner] Starting in startup mode
[Scanner] Starting full scan of all directories
[Scanner] Found 1 files, created 1 jobs
[Scanner] Created optimize job ... for /tmp/test-media/test-movie.mkv
```

Check jobs:
```bash
curl http://localhost:8080/api/jobs | jq
```

### 5. Scanner Test (Watch Mode)

Edit `.env`:
```bash
SCANNER_MODE=watch
```

Start server in one terminal:
```bash
./vastiva
```

In another terminal, add a new file:
```bash
dd if=/dev/zero of=/tmp/test-media/new-movie.mkv bs=1M count=200
```

Expected logs:
```
[Scanner] File watcher started
[Scanner] Watching directory: /tmp/test-media
[Scanner] Created optimize job ... for /tmp/test-media/new-movie.mkv
```

### 6. FFmpeg Integration Test

**Prerequisites**: FFmpeg must be installed

```bash
# Check FFmpeg is available
which ffmpeg
ffmpeg -version
```

Create a real test video or use an existing one:
```bash
# Copy a real video file
cp /path/to/real/video.mkv /tmp/test-media/
```

Create an optimization job manually:
```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "type": "optimize",
    "sourcePath": "/tmp/test-media/video.mkv",
    "destinationPath": "/tmp/test-output/video_optimized.mkv",
    "priority": 5
  }'
```

Monitor job progress:
```bash
# Get job ID from response, then:
curl http://localhost:8080/api/jobs/JOB_ID | jq
```

Watch server logs for progress updates:
```
[Job ...] Progress: 25% | FPS: 180.5 | Speed: 2.5x | ETA: 00:15:30
```

### 7. MakeMKV Integration Test

**Prerequisites**: MakeMKV must be installed

```bash
# Check MakeMKV is available
which makemkvcon
```

If you have an ISO file:
```bash
cp /path/to/disc.iso /tmp/test-media/
```

Create an extraction job:
```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "type": "extract",
    "sourcePath": "/tmp/test-media/disc.iso",
    "destinationPath": "/tmp/test-output/extracted",
    "priority": 10
  }'
```

### 8. Docker Test

Build the Docker image:
```bash
docker compose build
```

Run with Docker:
```bash
# Edit docker-compose.yml to mount test directories
docker compose up -d

# Check logs
docker compose logs -f

# Test API
curl http://localhost:8080/api/health
```

### 9. GPU Acceleration Test

Edit `.env`:
```bash
GPU_VENDOR=nvidia  # or intel, amd
```

Create an optimization job and check logs for GPU usage:
```
[Job ...] Starting transcoding with nvidia acceleration...
```

Verify GPU is being used:
```bash
# For NVIDIA
nvidia-smi

# For Intel
intel_gpu_top

# For AMD
radeontop
```

### 10. Multi-Directory Scanner Test

Create multiple test directories:
```bash
mkdir -p /tmp/test-movies /tmp/test-isos /tmp/test-tv
```

Edit `scanner-config.json`:
```json
[
  {
    "path": "/tmp/test-movies",
    "recursive": true,
    "includePatterns": ["*.mkv", "*.mp4"],
    "minFileSizeMB": 100
  },
  {
    "path": "/tmp/test-isos",
    "recursive": false,
    "includePatterns": ["*.iso"],
    "minFileSizeMB": 500
  },
  {
    "path": "/tmp/test-tv",
    "recursive": true,
    "includePatterns": ["*.mkv"],
    "minFileSizeMB": 50
  }
]
```

Add test files to each directory:
```bash
dd if=/dev/zero of=/tmp/test-movies/movie.mkv bs=1M count=200
dd if=/dev/zero of=/tmp/test-isos/disc.iso bs=1M count=1000
dd if=/dev/zero of=/tmp/test-tv/episode.mkv bs=1M count=100
```

Start in hybrid mode:
```bash
SCANNER_MODE=hybrid ./vastiva
```

Expected logs:
```
[Scanner] Starting in hybrid mode
[Scanner] Watching directory: /tmp/test-movies
[Scanner] Watching directory: /tmp/test-isos
[Scanner] Watching directory: /tmp/test-tv
[Scanner] Found 3 files, created 3 jobs
```

## Expected Results Summary

| Test | Expected Result |
|------|----------------|
| Build | Binary created successfully |
| API Health | `{"status":"ok","time":"..."}` |
| Scanner Startup | Jobs created for existing files |
| Scanner Watch | Jobs created for new files |
| FFmpeg Job | Progress updates with FPS/ETA |
| MakeMKV Job | Extraction completes successfully |
| Multi-Directory | All directories monitored |
| GPU Acceleration | GPU utilization visible |

## Troubleshooting

### Scanner Not Finding Files
- Check directory paths are absolute
- Verify file sizes meet minimum requirements
- Check file age requirements
- Review include/exclude patterns

### FFmpeg Not Found
```bash
# Install FFmpeg
sudo apt install ffmpeg  # Ubuntu/Debian
brew install ffmpeg      # macOS
```

### MakeMKV Not Found
```bash
# Install MakeMKV
# Follow instructions at https://www.makemkv.com/download/
```

### Jobs Not Processing
- Check `MAX_CONCURRENT_JOBS` setting
- Verify job manager started: look for "Job manager started" in logs
- Check job status via API

### Permission Errors
```bash
# Ensure directories are writable
chmod 755 /tmp/test-media /tmp/test-output
```

## Cleanup

```bash
# Stop server
Ctrl+C

# Remove test files
rm -rf /tmp/test-media /tmp/test-output /tmp/test-movies /tmp/test-isos /tmp/test-tv

# Remove processed files database
rm /data/processed.json
```

## Production Deployment Checklist

- [ ] Set appropriate `GPU_VENDOR`
- [ ] Configure real media directories
- [ ] Set `SCANNER_MODE=hybrid` for comprehensive monitoring
- [ ] Adjust `MAX_CONCURRENT_JOBS` based on hardware
- [ ] Configure `scanner-config.json` for your directory structure
- [ ] Set appropriate file size/age filters
- [ ] Configure exclude patterns for unwanted files
- [ ] Set up log rotation
- [ ] Configure backup for processed files database
- [ ] Test with real media files
- [ ] Monitor resource usage (CPU, GPU, disk I/O)
- [ ] Set up monitoring/alerting
