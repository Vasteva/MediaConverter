# Media Processing Package

This package provides comprehensive wrappers for FFmpeg and MakeMKV to handle media transcoding and disc extraction.

## Features

### FFmpeg Wrapper (`ffmpeg.go`)

- **Multi-GPU Support**: NVIDIA NVENC, Intel QSV, AMD VAAPI, and CPU fallback
- **Hardware Acceleration**: Automatic configuration for each GPU vendor
- **H.265/HEVC Encoding**: High-quality 10-bit encoding with configurable CRF
- **Real-time Progress Tracking**: Frame count, FPS, bitrate, speed, and ETA
- **Flexible Audio Handling**: Copy, AAC, or AC3 encoding options

#### Supported GPU Vendors

| Vendor | Encoder | Hardware Accel | Notes |
|--------|---------|----------------|-------|
| NVIDIA | `hevc_nvenc` | CUDA | Presets: p4 (fast), p5 (medium), p7 (slow) |
| Intel | `hevc_qsv` | QSV | Requires Intel GPU with Quick Sync |
| AMD | `hevc_vaapi` | VAAPI | Uses `/dev/dri/renderD128` |
| CPU | `libx265` | None | Software encoding, slower but universal |

#### Example Usage

```go
// Initialize wrapper
ffmpeg, err := media.NewFFmpegWrapper()
if err != nil {
    log.Fatal(err)
}

// Configure transcoding
opts := media.TranscodeOptions{
    InputPath:  "/input/movie.mkv",
    OutputPath: "/output/movie_optimized.mkv",
    GPUVendor:  media.GPUVendorNvidia,
    Preset:     media.PresetMedium,
    CRF:        23,
    AudioCodec: "copy",
    Container:  "mkv",
}

// Transcode with progress monitoring
err = ffmpeg.TranscodeWithProgress(ctx, opts, func(progress media.TranscodeProgress) {
    fmt.Printf("Progress: %d%% | FPS: %.1f | Speed: %s | ETA: %s\n",
        progress.Percentage, progress.FPS, progress.Speed, progress.Time)
})
```

### MakeMKV Wrapper (`makemkv.go`)

- **Disc Scanning**: Detect and analyze DVD/Blu-ray discs and ISO files
- **Title Selection**: Automatic detection of main title or manual selection
- **Metadata Extraction**: Duration, chapter count, and title information
- **Filename Sanitization**: Safe filename generation from disc metadata

#### Example Usage

```go
// Initialize wrapper
makemkv, err := media.NewMakeMKVWrapper()
if err != nil {
    log.Fatal(err)
}

// Scan disc
discInfo, err := makemkv.ScanDisc(ctx, "/dev/sr0")
if err != nil {
    log.Fatal(err)
}

// Find main title (longest duration)
titleIndex := discInfo.FindLargestTitle()

// Extract title
opts := media.ExtractOptions{
    SourcePath: "/dev/sr0",
    OutputDir:  "/output",
    TitleIndex: titleIndex,
    MinLength:  300, // 5 minutes minimum
}

err = makemkv.Extract(ctx, opts)
```

### Progress Tracking (`progress.go`)

Real-time monitoring of FFmpeg transcoding with detailed metrics:

- **Frame Count**: Current frame being processed
- **FPS**: Encoding speed in frames per second
- **Bitrate**: Current output bitrate
- **Speed**: Processing speed multiplier (e.g., "2.5x")
- **ETA**: Estimated time remaining
- **Percentage**: Completion percentage (requires duration)

## Integration with Job Manager

The media wrappers are integrated into the job manager (`internal/jobs/manager.go`):

### Extraction Jobs

1. Scan disc to detect titles
2. Select main title (longest duration)
3. Extract to MKV format
4. Update job progress

### Optimization Jobs

1. Verify source file exists
2. Get media info for progress calculation
3. Configure GPU-accelerated transcoding
4. Monitor progress in real-time
5. Generate optimized output file

## Configuration

Environment variables (via `internal/config`):

| Variable | Description | Default | Options |
|----------|-------------|---------|---------|
| `GPU_VENDOR` | Hardware acceleration | `cpu` | `nvidia`, `intel`, `amd`, `cpu` |
| `QUALITY_PRESET` | Encoding speed/quality | `medium` | `fast`, `medium`, `slow` |
| `CRF` | Quality level (lower = better) | `23` | `18-28` recommended |

## Testing

Run the test suite:

```bash
go test ./internal/media -v
```

Tests include:
- FFmpeg argument building for all GPU vendors
- Progress percentage calculation
- ETA estimation
- Filename sanitization
- Progress callback mechanism

## Performance Notes

### GPU Acceleration

- **NVIDIA**: Fastest, best quality-to-speed ratio with NVENC
- **Intel**: Good performance with Quick Sync Video
- **AMD**: VAAPI support, quality varies by GPU generation
- **CPU**: Slowest but highest quality control with libx265

### Recommended Settings

| Use Case | GPU Vendor | Preset | CRF |
|----------|-----------|--------|-----|
| Fast archival | NVIDIA | Fast (p4) | 23 |
| Balanced quality | NVIDIA/Intel | Medium (p5) | 21 |
| Maximum quality | CPU | Slow | 18 |
| Space-constrained | Any | Medium | 25 |

## Error Handling

All wrappers return descriptive errors:

- **FFmpeg not found**: Install FFmpeg with hardware acceleration support
- **MakeMKV not found**: MakeMKV is optional, extraction jobs will fail gracefully
- **Invalid source**: File/device doesn't exist or is not accessible
- **Transcoding failed**: Check FFmpeg output for codec/format issues

## Future Enhancements

- [ ] JSON parsing for ffprobe output (detailed stream info)
- [ ] Multi-pass encoding support
- [ ] Custom FFmpeg filter chains
- [ ] Automatic GPU detection
- [ ] Subtitle extraction and conversion
- [ ] Audio track selection and normalization
