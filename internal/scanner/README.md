# File Scanner System

The Vastiva Media Converter includes a powerful file scanner that can automatically discover and process media files across multiple directories.

## Features

- **Multiple Scan Modes**: Manual, startup, periodic, watch, or hybrid
- **Multi-Directory Support**: Monitor multiple parent directories simultaneously
- **Recursive Scanning**: Optionally scan subdirectories
- **Pattern Matching**: Include/exclude files based on glob patterns
- **File Filtering**: Filter by size, age, and extension
- **Processed File Tracking**: Avoid reprocessing the same files
- **Automatic Job Creation**: Automatically create extraction or optimization jobs
- **Real-time Watching**: Detect new files as they're added (using fsnotify)

## Scan Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| **manual** | No automatic scanning | API-driven job creation only |
| **startup** | Scan once on server startup | Process existing backlog |
| **periodic** | Scan at regular intervals | Batch processing with polling |
| **watch** | Real-time file system monitoring | Immediate processing of new files |
| **hybrid** | Startup + Watch + Periodic backup | Most comprehensive, recommended |

## Configuration

### Environment Variables

Add these to your `.env` file:

```bash
# Enable the scanner
SCANNER_ENABLED=true

# Choose scan mode
SCANNER_MODE=hybrid

# Scan interval for periodic/hybrid modes (seconds)
SCANNER_INTERVAL_SEC=300

# Automatically create jobs for discovered files
SCANNER_AUTO_CREATE=true

# Path to processed files database
SCANNER_PROCESSED_FILE=/data/processed.json

# Path to watch directories configuration
SCANNER_CONFIG_FILE=./scanner-config.json
```

### Watch Directories Configuration

Create `scanner-config.json` to define which directories to monitor:

```json
[
  {
    "path": "/storage/movies",
    "recursive": true,
    "includePatterns": ["*.mkv", "*.mp4", "*.avi", "*.mov"],
    "excludePatterns": ["*_optimized.mkv", "*_temp*", ".*"],
    "minFileSizeMB": 100,
    "minFileAgeMinutes": 5
  },
  {
    "path": "/storage/isos",
    "recursive": false,
    "includePatterns": ["*.iso"],
    "excludePatterns": [],
    "minFileSizeMB": 500,
    "minFileAgeMinutes": 10
  }
]
```

### Watch Directory Options

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Absolute path to directory to monitor |
| `recursive` | boolean | Scan subdirectories recursively |
| `includePatterns` | string[] | Glob patterns to include (e.g., `["*.mkv"]`) |
| `excludePatterns` | string[] | Glob patterns to exclude (e.g., `["*_temp*"]`) |
| `minFileSizeMB` | integer | Minimum file size in MB (0 = no limit) |
| `minFileAgeMinutes` | integer | Wait time before processing new files (0 = immediate) |

## How It Works

### File Discovery Flow

```
1. Scanner starts based on configured mode
   ↓
2. Scans configured directories
   ↓
3. Matches files against include/exclude patterns
   ↓
4. Filters by size and age requirements
   ↓
5. Checks if file was already processed
   ↓
6. Determines job type based on extension
   ↓
7. Creates job (if auto-create enabled)
   ↓
8. Marks file as processed
```

### Job Type Determination

| File Extension | Job Type | Action |
|----------------|----------|--------|
| `.iso` | Extract | MakeMKV extraction |
| `.mkv`, `.mp4`, `.avi`, `.mov`, etc. | Optimize | FFmpeg transcoding |

### Processed File Tracking

The scanner maintains a JSON database of processed files:

```json
{
  "/storage/movies/example.mkv": {
    "path": "/storage/movies/example.mkv",
    "hash": "abc123...",
    "processedAt": "2026-01-09T10:00:00Z",
    "jobId": "20260109100000-xyz789",
    "jobType": "optimize"
  }
}
```

This prevents:
- Reprocessing the same file multiple times
- Creating duplicate jobs
- Wasting resources on already-optimized files

## Usage Examples

### Example 1: Process Existing Backlog

Scan once on startup to process all existing files:

```bash
SCANNER_ENABLED=true
SCANNER_MODE=startup
SCANNER_AUTO_CREATE=true
```

### Example 2: Real-time Processing

Watch for new files and process immediately:

```bash
SCANNER_ENABLED=true
SCANNER_MODE=watch
SCANNER_AUTO_CREATE=true
```

### Example 3: Periodic Batch Processing

Scan every 5 minutes for new files:

```bash
SCANNER_ENABLED=true
SCANNER_MODE=periodic
SCANNER_INTERVAL_SEC=300
SCANNER_AUTO_CREATE=true
```

### Example 4: Comprehensive Monitoring (Recommended)

Combine all modes for maximum coverage:

```bash
SCANNER_ENABLED=true
SCANNER_MODE=hybrid
SCANNER_INTERVAL_SEC=600
SCANNER_AUTO_CREATE=true
```

This will:
1. Scan all directories on startup
2. Watch for new files in real-time
3. Re-scan every 10 minutes as a backup

## Advanced Configuration

### Multiple Parent Directories

Monitor completely separate directory trees:

```json
[
  {
    "path": "/mnt/nas/movies",
    "recursive": true,
    "includePatterns": ["*.mkv", "*.mp4"],
    "minFileSizeMB": 100
  },
  {
    "path": "/mnt/usb/imports",
    "recursive": false,
    "includePatterns": ["*.iso"],
    "minFileSizeMB": 500
  },
  {
    "path": "/home/user/downloads",
    "recursive": true,
    "includePatterns": ["*.mkv"],
    "excludePatterns": ["*sample*", "*trailer*"],
    "minFileSizeMB": 50,
    "minFileAgeMinutes": 15
  }
]
```

### Exclude Patterns

Prevent processing of temporary or unwanted files:

```json
{
  "excludePatterns": [
    "*_optimized.mkv",    // Already processed files
    "*_temp*",            // Temporary files
    ".*",                 // Hidden files
    "*sample*",           // Sample videos
    "*trailer*",          // Trailers
    "*.part",             // Incomplete downloads
    "*.tmp"               // Temporary files
  ]
}
```

### File Age Filtering

Wait for files to stabilize before processing (useful for active downloads):

```json
{
  "minFileAgeMinutes": 10
}
```

This ensures:
- Files are completely downloaded
- No processing of actively-writing files
- Reduced errors from incomplete files

## Performance Considerations

### Watch Mode Performance

- **Pros**: Instant processing of new files
- **Cons**: Uses inotify watches (limited on Linux)
- **Recommendation**: Use for < 100 directories

### Periodic Mode Performance

- **Pros**: No watch limit, simple
- **Cons**: Delayed processing (up to scan interval)
- **Recommendation**: Use for > 100 directories or network mounts

### Hybrid Mode Performance

- **Pros**: Best of both worlds
- **Cons**: Slightly higher resource usage
- **Recommendation**: Best for most use cases

## Troubleshooting

### Scanner Not Finding Files

1. Check directory paths are absolute
2. Verify include patterns match your files
3. Check file size/age requirements
4. Review exclude patterns

### Files Being Reprocessed

1. Check `SCANNER_PROCESSED_FILE` path is writable
2. Verify processed.json is being saved
3. Ensure file paths haven't changed

### Too Many Jobs Created

1. Increase `minFileAgeMinutes`
2. Add more specific include patterns
3. Use exclude patterns for unwanted files
4. Set higher `minFileSizeMB`

### Watch Mode Not Working

1. Check inotify limits: `cat /proc/sys/fs/inotify/max_user_watches`
2. Increase if needed: `sudo sysctl fs.inotify.max_user_watches=524288`
3. Consider using periodic mode for network mounts

## API Integration

The scanner can be controlled via API (future enhancement):

```bash
# Trigger manual scan
POST /api/scanner/scan

# Get scanner status
GET /api/scanner/status

# View processed files
GET /api/scanner/processed

# Reset processed files database
DELETE /api/scanner/processed
```

## Logging

Scanner operations are logged with `[Scanner]` prefix:

```
[Scanner] Starting in hybrid mode
[Scanner] Watching directory: /storage/movies
[Scanner] Found 15 files, created 3 jobs
[Scanner] Created optimize job 20260109100000-abc123 for /storage/movies/example.mkv
[Scanner] Skipping /storage/movies/small.mp4: too small (45 MB < 100 MB)
[Scanner] Running periodic scan...
```

## Best Practices

1. **Start with startup mode** to process existing files
2. **Use hybrid mode** for ongoing monitoring
3. **Set appropriate file age** to avoid processing incomplete files
4. **Use exclude patterns** to filter unwanted files
5. **Monitor processed.json** to ensure it's being saved
6. **Set realistic scan intervals** (5-10 minutes for periodic)
7. **Use recursive: false** for large directory trees to improve performance
8. **Test patterns** with a small directory first

## Security Considerations

- Scanner runs with server permissions
- Ensure watch directories are trusted
- Processed file database contains file paths
- Consider file permissions when setting output directories
