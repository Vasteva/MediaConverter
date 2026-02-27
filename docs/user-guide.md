# Vastiva Media Converter — User Guide

**Version:** 2026-02 | **Audience:** End users and self-hosters

---

## Table of Contents

1. [Overview](#1-overview)
2. [Requirements](#2-requirements)
3. [Installation](#3-installation)
4. [First-Time Setup](#4-first-time-setup)
5. [The Dashboard](#5-the-dashboard)
6. [Creating and Managing Jobs](#6-creating-and-managing-jobs)
7. [Scanner — Automatic File Discovery](#7-scanner--automatic-file-discovery)
8. [AI Features (Premium)](#8-ai-features-premium)
9. [Settings Reference](#9-settings-reference)
10. [Processing Schedule](#10-processing-schedule)
11. [Subtitles](#11-subtitles)
12. [Maintenance](#12-maintenance)
13. [Troubleshooting](#13-troubleshooting)

---

## 1. Overview

Vastiva Media Converter is a self-hosted transcoding platform that converts and optimises video files using FFmpeg. It can:

- Re-encode video files to H.265/HEVC to reduce file size
- Extract disc images (ISO/IMG) to MKV using MakeMKV
- Use GPU acceleration (NVIDIA, Intel, AMD) when available
- Automatically watch directories for new files and queue jobs
- Optionally use AI to clean filenames, generate subtitles, and choose optimal encoding settings

The web interface runs in a browser. All processing happens on the server — no files are uploaded to any external service unless you configure an AI provider.

---

## 2. Requirements

| Component | Minimum |
|-----------|---------|
| OS | Any Linux host (or Windows/macOS with Docker Desktop) |
| Docker | 24.0+ with Docker Compose v2 |
| RAM | 2 GB (4 GB recommended for concurrent jobs) |
| Disk | Enough space for source files and output |

**Optional:**
- NVIDIA GPU — requires [nvidia-container-toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html) on the host
- Intel GPU (QSV) / AMD GPU (VAAPI) — `/dev/dri` device passthrough (included by default in `docker-compose.yml`)
- Optical drive — for disc extraction jobs (MakeMKV)

---

## 3. Installation

### Standard Installation (CPU / Intel / AMD GPU)

**1. Create a directory for the application:**

```bash
mkdir -p /opt/vastiva
cd /opt/vastiva
```

**2. Download the compose file and create your `.env`:**

```bash
# Download from the repository
curl -O https://raw.githubusercontent.com/Vasteva/MediaConverter/main/docker-compose.yml
curl -O https://raw.githubusercontent.com/Vasteva/MediaConverter/main/.env.example
cp .env.example .env
```

**3. Edit `.env` with your settings:**

```bash
nano .env
```

At a minimum, set:

```env
ADMIN_PASSWORD=your-secure-password
MEDIA_ROOT=/path/to/your/media     # host directory with your media files
GPU_VENDOR=cpu                      # or: intel, amd
```

**4. Start the container:**

```bash
docker compose up -d
```

**5. Open the web interface:**

Navigate to `http://your-server-ip:8091` in a browser.

---

### NVIDIA GPU Installation

In addition to the steps above, install [nvidia-container-toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html), then start with the NVIDIA override:

```bash
docker compose -f docker-compose.yml -f docker-compose.nvidia.yml up -d
```

Set `GPU_VENDOR=nvidia` in `.env`. Vastiva will use NVENC for hardware-accelerated H.265 encoding.

---

### Optical Drive / Disc Extraction (MakeMKV)

To extract Blu-ray or DVD discs, pass your optical drive into the container.

**1. Find the device path:**

```bash
ls /dev/sr* /dev/cdrom 2>/dev/null
```

Typically `/dev/sr0` for the first drive.

**2. Uncomment the device line in `docker-compose.yml`:**

```yaml
devices:
  - /dev/dri:/dev/dri
  - /dev/sr0:/dev/sr0   # add this line
```

**3. Restart the container:**

```bash
docker compose up -d
```

---

### Environment Variables Reference

| Variable | Description | Default |
|----------|-------------|---------|
| `ADMIN_PASSWORD` | Web interface login password | *(required)* |
| `MEDIA_ROOT` | Host path mounted as `/storage` | `/mnt/media` |
| `GPU_VENDOR` | `cpu`, `nvidia`, `intel`, `amd` | `cpu` |
| `AI_PROVIDER` | `none`, `openai`, `claude`, `gemini`, `ollama` | `none` |
| `AI_API_KEY` | API key for your AI provider | — |
| `AI_MODEL` | Model name (e.g. `gpt-4o`, `claude-opus-4-5`) | — |
| `AI_ENDPOINT` | Base URL for Ollama or custom endpoints | — |
| `LICENSE_KEY` | Vastiva Pro key (`VASTIVA-PRO-…`) | — |
| `SCANNER_ENABLED` | Enable the automatic scanner on startup | `false` |
| `SCANNER_MODE` | `manual`, `startup`, `periodic`, `watch`, `hybrid` | `manual` |
| `MAX_CONCURRENT_JOBS` | Worker threads running simultaneously | `2` |
| `CORS_ORIGINS` | Allowed CORS origins for the API | localhost dev origins |

---

## 4. First-Time Setup

On first launch, Vastiva shows a **Setup Wizard**. This wizard runs once and configures the most important settings.

### Step 1 — System Detection

The wizard probes the server and reports:

- Whether FFmpeg is installed (always true in Docker)
- Whether MakeMKV is available
- Which GPU was detected

### Step 2 — Admin Password

Set the password you will use to log in. There is only one account. Store it securely — it cannot be recovered without editing the server configuration.

### Step 3 — AI Provider (Optional)

If you want AI features (metadata cleaning, subtitle generation, adaptive encoding, media verification), configure an AI provider here. You can skip this and configure it later in **Settings**.

| Provider | Best for | Notes |
|----------|----------|-------|
| OpenAI (GPT-4o) | All features including subtitles | Requires API key, billed per use |
| Claude (Anthropic) | Metadata and encoding analysis | No subtitle transcription |
| Gemini (Google) | Metadata and encoding analysis | No subtitle transcription |
| Ollama | Local, private, free | Requires Ollama server; no transcription |
| None | No AI features | All AI features disabled |

### Step 4 — License Key

Enter your `VASTIVA-PRO-…` license key to unlock premium features. You can skip this and enter it later in **Settings**.

### Step 5 — Encoding Defaults

Set your default quality level and CRF value. These become the defaults for all new jobs; you can override them per-job.

| Quality Preset | CRF | Result |
|----------------|-----|--------|
| High | 18 | Larger files, better quality |
| Medium (default) | 23 | Good balance |
| Low | 28 | Smallest files |

After completing the wizard, you are redirected to the login page.

---

## 5. The Dashboard

After logging in, the Dashboard shows a live overview of your system.

### Stats Cards

- **Jobs Queued** — pending jobs waiting to start
- **Processing** — jobs actively running right now
- **Completed** — total jobs finished (this session and restored from disk)
- **Storage Saved** — total bytes reclaimed by completed optimize jobs

### System Resources

Live graphs for CPU, memory, and GPU utilisation update every few seconds via Server-Sent Events.

### AI Insights (Premium)

When a license key and AI provider are configured, the dashboard shows:
- Efficiency scores for recent transcodes
- Estimated savings from adaptive encoding decisions
- A breakdown of AI operations run across jobs

---

## 6. Creating and Managing Jobs

### Creating a Job Manually

1. Click **New Job** (or the **+** button).
2. Choose the **job type**:
   - **Optimize** — re-encode a video file to H.265/HEVC
   - **Extract** — extract a disc image (ISO/IMG) to MKV using MakeMKV
3. Select the **source file** using the file browser.
4. Optionally choose a **destination** directory. Defaults to the configured output directory.
5. Set **priority** (1–10, higher = processed first).
6. Configure **Premium options** if you have a license:
   - Download subtitles
   - AI upscaling (480p/720p → 1080p or 4K)
   - Verify output integrity after encoding
   - Delete source file after successful encode
7. Click **Create Job**.

### Job States

| Status | Meaning |
|--------|---------|
| Pending | Waiting in the queue |
| Processing | Actively encoding |
| Completed | Finished successfully |
| Failed | Encountered an error |
| Cancelled | Manually cancelled |

### Monitoring Progress

The Job List updates in real-time. For each active job you can see:
- **Progress bar** — percentage of encoding complete
- **FPS** — current encoding speed
- **ETA** — estimated time remaining

### AI Log (Premium)

Jobs processed with an AI provider show an expandable **AI** button in the Actions column. Click it to see a log of every AI operation run on that job:

- Metadata cleaning (filename normalisation)
- Encoding analysis (adaptive CRF selection)
- Subtitle download
- Output verification

### Cancelling a Job

Click the **×** button next to a pending or processing job. Running jobs are stopped gracefully; FFmpeg is sent a termination signal and the partial output file is cleaned up.

### Retries

Set **Max Retries** to a number greater than 0 when creating a job. If encoding fails, Vastiva will automatically re-queue the job up to that many times before marking it as permanently failed.

---

## 7. Scanner — Automatic File Discovery

The Scanner watches your media directories and can automatically find and queue new files.

### Enabling the Scanner

1. Go to **Scanner** in the navigation.
2. Check **Enabled** in the General Settings card.
3. Add at least one **Watch Directory**.
4. Click **Save Configuration**.

### Scan Modes

| Mode | Behaviour |
|------|-----------|
| Manual | Scanner never runs automatically; only triggered via the API or the **Scan Now** button |
| Startup | Scans once when the container starts, then stops |
| Periodic | Scans on a fixed interval (configurable, minimum 60 seconds) |
| Watch | Uses filesystem events (inotify) to detect new files the moment they appear |
| Hybrid | Watch mode for instant detection + periodic backup scan to catch missed files |

**Recommended for most users:** `hybrid` mode.

### Watch Directories

Each watch directory has:

- **Path** — the directory to monitor (must be under `/storage` or `/output`)
- **Recursive** — scan subdirectories automatically
- **Include patterns** — glob patterns for which files to consider (e.g. `*.mkv`, `*.iso`)
- **Exclude patterns** — patterns to skip (e.g. `*_optimized.mkv` to avoid re-encoding already-processed files)
- **Min file size** — ignore files smaller than this (MB)
- **Min file age** — wait this many minutes after a file appears before processing it (prevents picking up partially-copied files)

Use the **Browse** button to navigate the server's filesystem and pick a directory.

### Auto-Create Jobs

When **Auto-Create Jobs** is enabled and the scanner finds a new file, it immediately creates and queues a job without any manual action. When disabled, files appear in the Discovered Files panel for you to review and queue selectively.

### Discover Files

Click **Discover Files** to run a one-off scan and show every file that would be queued. The results table shows:

- File name and path
- Type (Optimize or Extract)
- Current file size
- Estimated storage savings after encoding (% and bytes)

You can **filter** the results by job type (All / Optimize / Extract) and **sort** by any column (file name, size, or estimated savings) by clicking the column header. Click again to reverse the sort order.

Select individual files or use **Select All**, then click **Queue Selected** to add them to the job queue in one batch.

### Storage Savings Tracking

Every completed optimize job records the input and output file sizes. The **Storage Saved** total on the Dashboard accumulates across restarts — this data is persisted in `/data/processed.json`.

---

## 8. AI Features (Premium)

All AI features require:
1. A valid `VASTIVA-PRO-…` license key
2. A configured AI provider (see [Step 3 of Setup](#step-3--ai-provider-optional))

### Metadata Cleaning

When a job starts, Vastiva sends the source filename to the AI and receives a cleaned version with proper Title and Year formatting. For example:

```
The.Dark.Knight.2008.1080p.BluRay.x264-GROUP.mkv
→ The Dark Knight (2008).mkv
```

The rename only happens if the AI returns a meaningfully different name. The original name is always preserved in the AI log.

### Adaptive Encoding (Encoding Analysis)

Instead of using the fixed global CRF value, the AI analyses the media (codec, resolution, bitrate, content type) and selects an optimal CRF for that specific file. A 4K animation may need a lower CRF than a 1080p live-action film. The chosen value is logged in the job's AI log.

### Subtitle Generation / Download

Enable **Download Subtitles** on a job (or enable it globally in Scanner settings). Vastiva will search OpenSubtitles for a matching SRT file and embed it in the output. Requires an [OpenSubtitles](https://www.opensubtitles.com) account configured in Settings.

### AI Upscaling

Enable **Upscaling** on individual jobs or via the Scanner's auto-upscale setting. Vastiva uses FFmpeg's super-resolution filters to upscale to 1080p or 4K. Best results on content originally recorded at 480p or 720p.

### Output Verification

After encoding completes, the AI runs an integrity check on the output file to detect corruption before the source file is deleted. If verification fails, the source is retained and the job is marked as failed.

---

## 9. Settings Reference

Access via the **Settings** gear icon in the navigation.

### General

| Setting | Description |
|---------|-------------|
| Source Directory | Where Vastiva looks for input files |
| Output Directory | Where encoded files are saved |
| Max Concurrent Jobs | How many encodes run simultaneously |

### Encoding

| Setting | Description |
|---------|-------------|
| GPU Vendor | `cpu`, `nvidia`, `intel`, or `amd` |
| Quality Preset | `high`, `medium`, or `low` |
| CRF | Constant Rate Factor (lower = better quality, larger file) |
| Verify Output | Run integrity check after every encode |
| Delete Source | Remove original file after successful encode |

### AI Provider

Configure which AI backend to use and supply the API key. Changes take effect immediately — no restart needed.

### Subtitles

| Setting | Description |
|---------|-------------|
| Subtitle Mode | `always` (every job), `selective` (only when requested), `never` |
| Language | ISO 639-1 code, e.g. `en`, `fr`, `de` |
| OpenSubtitles API Key | Application key from opensubtitles.com |
| OpenSubtitles Username / Password | Account credentials |

### License Key

Enter or update your `VASTIVA-PRO-…` key. Premium status is shown next to the field.

---

## 10. Processing Schedule

The **Processing Schedule** (in Settings) restricts when workers are allowed to pick up jobs. Jobs can be queued at any time; they only start executing during the configured window.

This is useful to limit CPU/GPU use during the day and batch process overnight.

### Configuration

| Field | Description |
|-------|-------------|
| Enabled | Toggle scheduling on or off |
| Allowed Days | Days of the week when processing is permitted |
| Start Hour | Hour (0–23) when the window opens |
| End Hour | Hour (0–23) when the window closes |
| Timezone | IANA timezone name, e.g. `America/New_York`, `Europe/London`, `UTC` |

Overnight windows are supported — setting Start Hour to `22` and End Hour to `6` means jobs run from 10 PM to 6 AM.

A summary preview below the fields shows the human-readable schedule before you save.

---

## 11. Subtitles

### OpenSubtitles Setup

1. Create a free account at [opensubtitles.com](https://www.opensubtitles.com)
2. Generate an **API key** in your account settings (App API key, not the REST API key)
3. Enter your credentials in Vastiva's **Settings → Subtitles**:
   - API Key
   - Username
   - Password

### Per-Job vs Global

- **Per job:** Check **Download Subtitles** when creating a job manually
- **Via Scanner:** Enable **Download Subtitles** in Scanner → General Settings. All auto-created jobs will attempt subtitle download

### Language

Set your preferred language in Settings. Vastiva will request subtitles in that language and fall back gracefully if none are available.

---

## 12. Maintenance

### Viewing Logs

```bash
docker compose logs -f vastiva
```

Logs rotate automatically (10 MB per file, 3 files max).

### Updating Vastiva

```bash
cd /opt/vastiva
docker compose pull
docker compose up -d
docker image prune -f
```

### Backup

All persistent data lives in the `vastiva-data` Docker volume, mounted at `/data` inside the container:

| File | Contents |
|------|----------|
| `/data/config.json` | All settings saved via the UI |
| `/data/jobs.json` | Job queue state (survives restarts) |
| `/data/scanner_config.json` | Scanner watch directories |
| `/data/processed.json` | Record of processed files and storage savings |

**Backup all data:**

```bash
docker compose stop vastiva
docker run --rm -v vastiva_vastiva-data:/data -v $(pwd):/backup \
  alpine tar czf /backup/vastiva-backup-$(date +%Y%m%d).tar.gz /data
docker compose start vastiva
```

**Restore:**

```bash
docker compose stop vastiva
docker run --rm -v vastiva_vastiva-data:/data -v $(pwd):/backup \
  alpine tar xzf /backup/vastiva-backup-20260101.tar.gz -C /
docker compose start vastiva
```

### Resetting the Setup Wizard

Delete `config.json` from the data volume:

```bash
docker exec vastiva rm /data/config.json
docker compose restart vastiva
```

---

## 13. Troubleshooting

### "Access denied" in the file browser

The file browser restricts navigation to your configured Source and Destination directories. If you see an access denied error, check that your watch directory path is under the configured `SOURCE_DIR` (default `/storage`).

### GPU not being used

1. Check the detected GPU in Settings — it shows the active encoder
2. Verify the device is passed into the container:
   ```bash
   docker exec vastiva ls /dev/dri
   ```
3. For NVIDIA: confirm the runtime:
   ```bash
   docker exec vastiva nvidia-smi
   ```
4. If auto-detection selected `cpu` incorrectly, set `GPU_VENDOR` explicitly in `.env` and restart

### Jobs stay "Pending" and never start

This usually means either:
- **All workers are busy** — wait for an active job to complete, or increase `MAX_CONCURRENT_JOBS`
- **Processing Schedule is active** — check Settings → Processing Schedule. Jobs will start when the window opens

### Storage Saved not persisting across restarts

This was a known bug fixed in February 2026. If you are running an older version, update the container:

```bash
docker compose pull && docker compose up -d
```

### Subtitles not downloading

1. Verify your OpenSubtitles credentials in **Settings → Subtitles**
2. Check that `SUBTITLE_MODE` is not set to `never`
3. Review the job's AI log (expand the AI button in Job List) for the subtitle download entry and its error field

### "No new files found" in Discover

- Confirm the watch directory exists and has readable files
- Check that the files match the configured **Include patterns** (default includes `.mkv`, `.mp4`, `.avi`, `.iso`)
- Ensure the files exceed the **Min File Size** and **Min File Age** thresholds
- Files that were previously processed are tracked in `processed.json` and will not appear again unless you clear that file

### Container restarts repeatedly

```bash
docker compose logs vastiva | tail -50
```

Common causes: `ADMIN_PASSWORD` not set in `.env`, or port 8091 already in use on the host.

### Forgot admin password

Edit the password directly in the container's config:

```bash
docker exec -it vastiva sh
# Edit /data/config.json and change "adminPassword"
# OR delete config.json to re-run the setup wizard
```

Then restart: `docker compose restart vastiva`
