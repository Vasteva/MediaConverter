# Vastiva Media Converter - Architecture

## System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Web UI (React)                          │
│               Dashboard, Jobs, Scanner, Settings                │
└────────────────────────────┬────────────────────────────────────┘
                             │ HTTP/REST
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Fiber Web Server                           │
│                    (cmd/server/main.go)                         │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                      API Routes Layer                           │
│                   (internal/api/routes.go)                      │
│                                                                 │
│  • GET  /api/health         • POST /api/login                  │
│  • GET  /api/jobs           • POST /api/jobs                   │
│  • GET  /api/config         • POST /api/config                 │
│  • GET  /api/scanner/config • POST /api/scanner/config         │
│  • GET  /api/search         • GET  /api/dashboard/stats        │
│  • GET  /api/setup/status   • POST /api/setup/complete         │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Job Manager                                │
│                  (internal/jobs/manager.go)                     │
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │  Worker 1   │  │  Worker 2   │  │  Worker N   │            │
│  │  Goroutine  │  │  Goroutine  │  │  Goroutine  │            │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘            │
│         │                │                │                     │
│         └────────────────┴────────────────┘                     │
│                          │                                      │
│                    Job Queue (chan)                             │
│                          │                                      │
│         ┌────────────────┴────────────────┐                    │
│         ▼                                 ▼                     │
│  ┌─────────────┐                  ┌─────────────┐             │
│  │ Extraction  │                  │Optimization │             │
│  │    Jobs     │                  │    Jobs     │             │
│  └──────┬──────┘                  └──────┬──────┘             │
└─────────┼─────────────────────────────────┼────────────────────┘
          │                                 │
          ▼                                 ▼
┌─────────────────────┐         ┌─────────────────────┐
│   MakeMKV Wrapper   │         │   FFmpeg Wrapper    │
│ (media/makemkv.go)  │         │  (media/ffmpeg.go)  │
│                     │         │                     │
│ • ScanDisc()        │         │ • Transcode()       │
│ • Extract()         │         │ • GetMediaInfo()    │
│ • FindLargestTitle()│         │ • Progress Monitor  │
└──────────┬──────────┘         └──────────┬──────────┘
           │                               │
           ▼                               ▼
┌─────────────────────┐         ┌─────────────────────┐
│   makemkvcon CLI    │         │    ffmpeg CLI       │
│                     │         │                     │
│ • Disc scanning     │         │ • NVIDIA NVENC      │
│ • MKV extraction    │         │ • Intel QSV         │
│ • Title detection   │         │ • AMD VAAPI         │
└─────────────────────┘         │ • CPU libx265       │
                                └─────────────────────┘
```

## Data Flow

### Extraction Job Flow

```
1. User creates extraction job via API
   POST /api/jobs { type: "extract", sourcePath: "/dev/sr0" }
        ↓
2. Job Manager adds to queue
   Job { ID, Type: Extract, Status: Pending }
        ↓
3. Worker picks up job
   Status: Processing
        ↓
4. MakeMKV Wrapper scans disc
   ScanDisc() → DiscInfo { Titles[] }
        ↓
5. Find main title
   FindLargestTitle() → titleIndex
        ↓
6. Extract to MKV
   Extract() → /output/movie_t00.mkv
        ↓
7. Job completes
   Status: Completed, Progress: 100%
```

### Optimization Job Flow

```
1. User creates optimization job via API
   POST /api/jobs { type: "optimize", sourcePath: "/input/movie.mkv" }
        ↓
2. Job Manager adds to queue
   Job { ID, Type: Optimize, Status: Pending }
        ↓
3. Worker picks up job
   Status: Processing
        ↓
4. FFmpeg gets media info
   GetMediaInfo() → MediaInfo { Duration, Size }
        ↓
5. Configure transcoding
   TranscodeOptions { GPU: nvidia, CRF: 23, Preset: medium }
        ↓
6. Start transcoding with progress
   TranscodeWithProgress() + callback
        ↓
7. Real-time updates
   Progress: 25% | FPS: 180 | Speed: 2.5x | ETA: 00:15:30
        ↓
8. Job completes
   Status: Completed, Progress: 100%
   Output: /output/movie_optimized.mkv
```

## Configuration Flow

```
Environment Variables (.env)
        ↓
config.Load() (internal/config/config.go)
        ↓
Config struct {
    GPUVendor: "nvidia"
    QualityPreset: "medium"
    CRF: 23
    MaxConcurrentJobs: 2
    AIProvider: "none"
}
        ↓
Passed to Job Manager
        ↓
Used by Media Wrappers
```

## Progress Tracking Architecture

```
FFmpeg Process (stderr)
        ↓
frame=1234 fps=180.5 bitrate=5000kbits/s time=00:05:30 speed=2.5x
        ↓
parseProgress() - Regex parsing
        ↓
TranscodeProgress {
    Frame: 1234
    FPS: 180.5
    Bitrate: "5000kbits/s"
    Time: "00:05:30"
    Speed: "2.5x"
}
        ↓
Progress Callback
        ↓
Job Update {
    Progress: 45%
    FPS: 180.5
    ETA: "00:06:45"
}
        ↓
API Response (GET /api/jobs/:id)
        ↓
Frontend UI Update
```

## GPU Acceleration Decision Tree

```
GPU_VENDOR environment variable
        │
        ├─ "nvidia" → hevc_nvenc
        │             ├─ -hwaccel cuda
        │             ├─ -preset p4/p5/p7
        │             └─ -cq [CRF]
        │
        ├─ "intel"  → hevc_qsv
        │             ├─ -hwaccel qsv
        │             ├─ -preset fast/medium/slow
        │             └─ -global_quality [CRF]
        │
        ├─ "amd"    → hevc_vaapi
        │             ├─ -hwaccel vaapi
        │             ├─ -hwaccel_device /dev/dri/renderD128
        │             └─ -qp [CRF]
        │
        └─ "cpu"    → libx265
                      ├─ No hardware accel
                      ├─ -preset fast/medium/slow
                      └─ -crf [CRF]
```

## AI Integration (Premium Features)

```
┌─────────────────────────────────────────────────────────────────┐
│                      AI Provider Interface                       │
│                    (internal/ai/provider.go)                     │
│                                                                  │
│  Provider.Analyze(prompt) → string                              │
│  Provider.Transcribe(audioPath) → SRT text                      │
│  Provider.GetName() → string                                    │
└────────────────────────────┬─────────────────────────────────────┘
                             │
         ┌───────────────────┼───────────────────┐
         ▼                   ▼                   ▼
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   OpenAI    │    │   Gemini    │    │   Ollama    │
│ (GPT-4, etc)│    │   (Google)  │    │   (Local)   │
├─────────────┤    ├─────────────┤    ├─────────────┤
│ ✓ Analyze   │    │ ✓ Analyze   │    │ ✓ Analyze   │
│ ✓ Transcribe│    │ ✗ Transcribe│    │ ✗ Transcribe│
└─────────────┘    └─────────────┘    └─────────────┘

AI Features (Premium):
├─ Metadata Cleaning  → internal/ai/meta/cleaner.go
│  └─ Cleans messy filenames: "Movie.2024.1080p.BluRay" → "Movie (2024)"
│
├─ Adaptive Encoding  → internal/ai/meta/cleaner.go
│  └─ AI suggests optimal CRF based on content analysis
│
├─ Whisper Subtitles  → internal/ai/whisper/generator.go
│  └─ Generates SRT files from audio using OpenAI Whisper
│
└─ Natural Language Search → internal/ai/search/searcher.go
   └─ Find media with queries like "action movies in space"
```

## Concurrency Model

```
Main Goroutine
    │
    ├─ HTTP Server (Fiber)
    │   └─ Request Handlers (concurrent)
    │
    └─ Job Manager
        │
        ├─ Worker 1 Goroutine
        │   └─ Process Job → FFmpeg/MakeMKV
        │
        ├─ Worker 2 Goroutine
        │   └─ Process Job → FFmpeg/MakeMKV
        │
        └─ Worker N Goroutine
            └─ Process Job → FFmpeg/MakeMKV

Job Queue: Buffered channel (1000 capacity)
Job Storage: Map with RWMutex for thread-safe access
```

## Error Handling Strategy

```
Media Wrapper Error
        ↓
Return error to Job Manager
        ↓
Job.Status = Failed
Job.Error = error.Error()
        ↓
API returns job with error
        ↓
Frontend displays error to user
```

## File System Layout

```
/storage/               ← SOURCE_DIR (input files)
    ├── movie1.mkv
    ├── movie2.iso
    └── disc/

/output/                ← DEST_DIR (output files)
    ├── movie1_optimized.mkv
    ├── movie2_t00.mkv
    └── extracted/

/tmp/                   ← Temporary processing
    └── ffmpeg_temp_*
```

## Technology Stack

```
┌─────────────────────────────────────┐
│         Frontend Layer              │
│  React + TypeScript + Vite          │
└─────────────────────────────────────┘
                 │
                 │ HTTP/JSON
                 ▼
┌─────────────────────────────────────┐
│         Backend Layer               │
│  Go 1.22+ + Fiber Framework         │
└─────────────────────────────────────┘
                 │
                 │ Process Execution
                 ▼
┌─────────────────────────────────────┐
│       Media Processing Layer        │
│  FFmpeg + MakeMKV (CLI tools)       │
└─────────────────────────────────────┘
                 │
                 │ Hardware Access
                 ▼
┌─────────────────────────────────────┐
│         Hardware Layer              │
│  GPU (CUDA/QSV/VAAPI) or CPU        │
└─────────────────────────────────────┘
```
