# Vastiva Media Converter — Task List

**Generated:** 2026-02-01 | **Last Updated:** 2026-09-04

## 📋 Executive Summary

Seventeen open issues, from a full source review on 2026-09-04 combined with findings
from production testing on 2026-09-03. #35 and #38 closed the same day they were filed.

The media pipeline itself — `internal/media` (ffmpeg, progress, validate) and
`internal/ai/meta` — is in good shape and holds up under review. The open work is
concentrated in two places:

- **Nothing else stops a job from making a file worse.** No codec/bitrate
  filter, an AI CRF suggestion that cannot be switched off, and a resolution
  filter that only applies to scanner jobs (#39–#42). Production testing found
  real files inflated by re-encoding already-efficient HEVC.
- **Shared mutable state is unsynchronised.** `config.Config` has no mutex at all
  while the API mutates it under running workers (#43).

The previous revision of this file claimed all known bugs were resolved. That was
written 2026-02-27 and was not re-verified against the code before this review.

---

## ✅ Closed Today

### 38. No Skip-If-Not-Smaller Guard

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `internal/media/validate.go`; `internal/config/config.go`; `internal/config/precedence.go`; `internal/jobs/manager.go`
- **Details:** `ValidateOutput` rejected outputs that were too *small* but
  nothing rejected one larger than its source. A job could replace a file
  with a bigger file and report success.
- **Fix:**
  - Added `media.MeetsSavingsFloor(srcSize, outSize, floor)` — pure size-ratio
    check, no I/O, so it's unit-testable without ffmpeg.
  - Added `Config.SavingsFloor` (default `0.15`, env `SAVINGS_FLOOR`,
    persisted as `savingsFloor`), following the existing env-vs-file
    precedence pattern (`merger.float`, mirroring `merger.integer`).
  - `runOptimizationFromPath` now checks the gate immediately after
    `ValidateOutput` passes. Below the floor: `discardOutput`, keep the
    original source untouched, set a `StatusDetail` explaining the skip, and
    return `(false, nil)` — the job completes successfully rather than
    failing, and (for the ISO auto-extract path) the returned `false`
    correctly withholds deletion of the original disc image too, the same
    `verified` plumbing #35 fixed.
  - **Not done:** `SavingsFloor` is not yet exposed on `GET`/`POST
    /api/config` or the settings UI. #51 already lists it as one of the
    fields that needs to land there, and its own sequencing note says to do
    that after #43 (the config mutex) — adding another API-writable mutable
    field before that fix just widens the race #43 describes. The field is
    fully live via env var / `config.json` in the meantime.
- **Tests:** `TestMeetsSavingsFloor` (table-driven: under/at/over the floor,
  equal size, bigger output, unknown/negative source size). Full
  `internal/media`, `internal/jobs`, `internal/config` suites re-run with
  `-race -count=1`: all pass. `go vet ./internal/...` clean, `go build
  ./internal/...` clean.

### 35. Source Deletion in `runExtraction` Was Not Gated on Validation

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `internal/jobs/manager.go`; `internal/media/validate.go`; `internal/media/makemkv.go`
- **Details:** Three source-delete sites existed and only one was correctly gated.
  - `:1295` (normal optimize) — ran downstream of `ValidateOutput`. Was already correct.
  - `:617` (ISO auto-extract) — re-derived `verified` inline instead of using the
    result `runOptimizationFromPath` had already computed, assuming true when
    `VerifyOutput` was off.
  - `:911` (`runExtraction`) — deleted the source disc image gated **only** on
    `fi.Size() > 0`. No validation, no duration check. A truncated MakeMKV
    extraction larger than zero bytes destroyed its source.
- **Context:** The nine zero-byte `.h265.mkv` stubs (Dec 2025 / Jan 2026) whose
  originals are gone use a naming scheme that predates the current `_optimized`
  convention, so those specific artifacts are almost certainly pre-fix. The
  extract path would still have produced the same outcome going forward.
- **Fix:**
  - Added `FFmpegWrapper.ValidateExtractedOutput` (`validate.go`) — the same size
    floor and duration-tolerance logic as `ValidateOutput`, but checked against
    the duration MakeMKV itself reported during the disc scan
    (`DiscInfo.TitleDurationSeconds`, new in `makemkv.go`) rather than an ffprobe
    of the source, since ffprobe cannot read a disc/ISO directly.
  - `runExtraction` now runs this validation before deleting the source disc
    image, and discards the extraction output on failure rather than leaving a
    stub that looks like a success.
  - `runOptimizationFromPath` and `runOptimization` now return `(bool, error)` —
    the same `verified` value they already used internally for their own source
    deletion — so the ISO auto-extract path in `processJob` uses that value
    directly for deleting the original disc image, instead of a second,
    independently-derived copy of the same logic (which read `job.Verified`, a
    field only ever set true on an *AI-verified* pass — so with `verifyOutput`
    on but AI verification merely inconclusive, it silently read false too, but
    for the wrong reason, and was one future edit away from the two diverging).
  - Also switched the outer gate at that call site from `m.config.DeleteSource`
    (live config, not what the job was created with) to `job.DeleteSource`, for
    consistency with every other deletion decision in the file.
- **Tests:** `TestValidateExtractedOutputRejectsBrokenFiles` (missing/zero/
  sub-floor, pre-probe — no ffprobe required),
  `TestValidateExtractedOutputCatchesTruncation` (real ffmpeg-generated clip;
  confirms a matching duration passes and a title truncated to a fraction of
  its reported length is rejected), `TestDiscInfoTitleDurationSeconds`. Full
  existing suite for `internal/jobs` and `internal/media` re-run with
  `-race -count=1`: all pass. `go vet` clean. (`go build ./internal/...` could
  not be verified end-to-end in this environment — `internal/scanner` and
  `internal/api` need `fsnotify`/`fiber`/`fasthttp`, which this sandbox's
  network egress blocks; the packages actually touched by this change build,
  vet, and test clean on their own.)

---

## 🔴 Critical

### 36. `time.NewTicker(0)` Panic Takes Down the Process

- **Status:** 🔴 Open
- **File:** `internal/scanner/scanner.go`; `web/src/components/ScannerConfig.tsx`
- **Details:** `ScannerConfig.Validate()` defaults every field except
  `ScanIntervalSec`. The settings UI uses `parseInt(e.target.value)`, so clearing
  the interval field yields `NaN`, serialises as `null`, and arrives as `0`.
  `periodicScan` then calls `time.NewTicker(0)`, which panics in a goroutine and
  kills the server. The `min="60"` attribute is a browser hint with no
  server-side counterpart.
- **Fix:** Default and floor `ScanIntervalSec` in `Validate()`; reject
  out-of-range values in `POST /api/scanner/config`.

### 37. Arbitrary File Write via Unvalidated Paths

- **Status:** 🔴 Open
- **File:** `internal/api/routes.go`; `internal/scanner/scanner.go`; `internal/security/security.go`; `Dockerfile`
- **Details:** Three gaps, compounded by the deployment shape — this instance is
  internet-facing through Traefik and the container runs as root.
  - `POST /api/jobs` validates `sourcePath` (`routes.go:304`) but only
    `filepath.Clean`s `destinationPath` (`:309-323`). Output can be written
    anywhere the container can reach.
  - `POST /api/scanner/queue` → `Scanner.QueueFile` (`scanner.go:960`) takes the
    raw path with no validation at all.
  - `security.ValidatePath` never resolves symlinks — no `EvalSymlinks` anywhere
    in the repo — so a symlink inside the library escapes the sandbox.
- **Note:** `docs/security/audit.md` VAST-001 states this is fixed for source
  *and* destination. It is not. See #52.
- **Fix:** Validate the destination and the queue path; add `EvalSymlinks`; add a
  non-root `USER` to `Dockerfile` and `Dockerfile.nvidia`.

---

## 🟠 High — Output Quality

*All five found during production testing, 2026-09-03.*

### 39. Already-Efficient Sources Are Re-Encoded

- **Status:** 🟠 Open — approach needs sign-off
- **File:** `internal/media/ffmpeg.go`; `internal/scanner/scanner.go`
- **Details:** Nothing checks the source codec before queueing or processing.
  HEVC → HEVC is generational loss with no size benefit. This is what inflated
  the *28 Days / Weeks / Years Later* files, which were already HEVC.
  `MediaInfo.CodecName` is populated at `ffmpeg.go:401` and never consumed for a
  skip decision.
- **Proposal:** Filter on **bitrate density, not codec identity**. Compute
  bits per pixel per frame from the probe (`MediaInfo` already carries `Size`,
  `Duration`, `VideoWidth/Height`; `EncodingSummary` already derives average
  bitrate). Skip HEVC/AV1 sources at or below the density a good HEVC encode
  would produce; let genuinely bloated ones through. Deterministic, testable,
  no model call, no added latency — a hard codec skip would permanently block
  re-encoding a bloated HEVC at a higher CRF.
- **Optional later tier:** sample-encode 60 s and measure VMAF against the source
  to decide. Accurate, but costs a partial encode per candidate.
- **Fix:** Add the gate to `media.CheckSourceSupported`, which already exists for
  exactly this purpose and runs immediately after the probe in
  `runOptimizationFromPath`. Scanner side needs it too so these never queue:
  extend `GetVideoResolution` to return codec and bitrate, then check in
  `createJobForFile` and `Discover` alongside the resolution filter.

### 40. AI CRF Suggestion Cannot Be Switched Off

- **Status:** 🟠 Open
- **File:** `internal/jobs/manager.go`
- **Details:** Adaptive CRF at `:971` is gated only on
  `m.config.IsPremium && m.ai != nil`, with no opt-out. It selected CRF 20 —
  near-transparent — on a REMUX, which guarantees inflation. `ExtractCRF`'s
  accepted band is 14–34, so 20 passes validation.
- **Fix:** Add an explicit config flag (default off) plumbed through `Config`,
  `GET`/`POST /api/config`, and the Settings UI, so a fixed global CRF is
  selectable. Consider also refusing a suggestion lower than the configured CRF
  on an already-efficient source.

### 41. `skipHighResolution` Does Not Apply to Manual Jobs

- **Status:** 🟠 Open
- **File:** `internal/scanner/scanner.go`; `internal/config/config.go`; `internal/api/routes.go`
- **Details:** `SkipHighResolution` and `ResolutionHeightThreshold` live on
  `ScannerConfig` and are checked only in `createJobForFile` and `Discover`.
  `POST /api/jobs` does no resolution check at all — which is how *28 Years Later*
  and *A Bad Moms Christmas* were processed despite the filter being on.
- **Decision:** Move both fields to the system `Config`. This is a policy about
  the library, not about scanning, and both paths should share it.
- **Fix:** Move the fields, check them in `POST /api/jobs`, and migrate persisted
  `scanner_config.json` values on load so existing installs keep their setting.

### 42. `autoUpscale` Defaults On, and Upscaling Ignores SAR

- **Status:** 🟠 Open
- **File:** `internal/scanner/scanner.go`; `internal/media/ffmpeg.go`
- **Details:** Two separate problems behind the *Caged* 856 → 1080 result.
  1. **Default.** `ScannerConfig.AutoUpscale` has no default in `Validate()` or
     `LoadScannerConfig`, so an enabled value came from persisted config or the
     UI's initial state. Upscaling should be opt-in per job.
  2. **Distortion.** There is no SAR handling anywhere in `internal/media` —
     `getUpscaleFilter` emits a bare `scale_vaapi=W:H` / `scale=W:H`, so an
     anamorphic source is stretched to square pixels. Fixing the default hides
     this; anyone who opts in still gets a distorted file.
- **Fix:** Explicit `false` default in both places; add `setsar`/aspect
  preservation to the upscale filter, or refuse to upscale anamorphic sources.

---

## 🟠 High — Concurrency

### 43. Shared Mutable State Is Unsynchronised

- **Status:** 🟠 Open
- **File:** `internal/config/config.go`; `internal/jobs/manager.go`; `internal/scanner/scanner.go`
- **Details:** Four distinct races.
  - **`config.Config` has no mutex at all.** `POST /api/config` mutates `CRF`,
    `IsPremium`, `Schedule`, `DeleteSource` and more from the HTTP goroutine
    while workers read them — 18 read sites in `manager.go`, plus
    `isInScheduleWindow` from `scheduleWatcher`.
  - **`m.ai`** — `UpdateAIProvider` writes under `m.mu`; 14 reads do not take it,
    including `GetAI()` itself.
  - **`s.config`** — `UpdateConfig` writes under `s.mu`; roughly 28 reads in
    `ScanAll`, `scanDirectory`, `createJobForFile`, `handleNewFile` and
    `periodicScan` do not. `GetConfig()` returns the live pointer to the JSON
    encoder.
  - **`Scanner.Stop()` sets `s.stopCh = nil`** while `watchFiles` and
    `periodicScan` are selecting on that field. A goroutine that reads the nil
    blocks forever, deadlocking `Stop`'s own `wg.Wait()`. Reachable from the
    settings UI via `UpdateConfig`. Separately, `delayedProcess` is spawned
    without `wg.Add`, so it outlives `Stop`.
- **Fix:** Snapshotting config per job at dequeue is probably cleaner than a
  mutex. Lock `m.ai` and `s.config`; have `GetConfig` return a copy.
- **Test:** CI already runs `go test -race`, but nothing drives both sides, which
  is why these survived. Add a test that runs a `JobTypeTest` job while hammering
  the config and scanner-config update paths.

### 44. Purged and Retried Jobs Can Run Twice

- **Status:** 🟠 Open
- **File:** `internal/jobs/manager.go`
- **Details:** `PurgeJobs` deletes from `m.jobs` but not from `m.pq`, so a purged
  pending job is still popped and run — the same shape as the cancelled-job bug
  already guarded at `processJob:468`, which purge never received. Separately,
  `RetryJob` pushes onto the heap with no check that the job is already queued,
  so retrying a job the auto-retry path has already re-pushed runs two FFmpeg
  processes against one output file.
- **Fix:** A queued/in-heap marker on `Job` covers both.

### 45. Job State Is Rewritten Several Times a Second

- **Status:** 🟠 Open
- **File:** `internal/jobs/manager.go`
- **Details:** `m.Save()` fires on every progress callback. Each call marshals
  *every* job and rewrites `jobs.json` whole, with an SSE broadcast alongside.
  Cost grows without bound because nothing prunes completed jobs.
- **Fix:** Throttle progress-driven saves to roughly 1/s — state transitions can
  still save immediately — and add a retention cap or age-out for completed and
  failed jobs.

---

## 🟡 Medium

### 46. Progress Parsing Repeats the MakeMKV Scanner Bug

- **Status:** 🟡 Open
- **File:** `internal/media/progress.go`
- **Details:** `parseProgress` uses a default `bufio.Scanner` (64 KB token cap),
  never checks `scanner.Err()`, and splits on `\n` only — while FFmpeg's `-stats`
  output is `\r`-terminated. This is the same defect fixed in `makemkv.go`
  under #30. `stderrMonitor.Write` handles `\r` correctly in the same file.
  Progress can silently stop mid-job.
- **Fix:** Larger buffer, split on `\r` and `\n`, check `Err()`.

### 47. Extract Directory Leak and Reintegration Overwrite

- **Status:** 🟡 Open
- **File:** `internal/jobs/manager.go`; `internal/jobs/reintegrate.go`
- **Details:** Two file-handling holes.
  - `manager.go:598` runs `os.RemoveAll(extractDir)` only under `if err == nil`,
    so a failed optimize after a successful extract strands a full-size MKV in a
    hidden `.extract_<id>` directory permanently.
  - `reintegrate` refuses to overwrite the holding path but not `paths.Final`, so
    replacing `movie.avi` silently clobbers an existing `movie.mkv`.
- **Fix:** `defer` the cleanup; add the same refusal for `Final`.

### 48. Files Are Marked Processed at Job Creation

- **Status:** 🟡 Open
- **File:** `internal/scanner/scanner.go`
- **Details:** `createJobForFile` and `QueueFile` both call `MarkProcessed` the
  moment a job is created, so one transient failure permanently excludes that
  file from future scans. Only `CompleteProcessed` should write the durable
  entry. Also in scope: `MarkProcessed` rewrites the whole DB and hashes 1 MB of
  the file on every call.
- **Fix:** In-flight marker at creation, cleared on failure; durable entry on
  success only.

### 49. No HTTP Client Timeouts

- **Status:** 🟡 Open
- **File:** `internal/ai/*.go`; `internal/subtitles/opensubtitles.go`
- **Details:** Twelve `http.DefaultClient.Do` sites, none with a timeout.
  `AnalyzeEncoding` runs on `job.ctx`, which carries no deadline, so an
  unresponsive Ollama hangs a worker indefinitely.
- **Fix:** Per-provider client with a sane timeout; explicit deadline on AI calls
  made from the job path.

### 50. Session Tokens and Rate Limiting

- **Status:** 🟡 Open
- **File:** `internal/api/auth.go`; `internal/api/ratelimit.go`; `cmd/server/main.go`; `internal/config/config.go`
- **Details:** Tokens are `sha256(adminPassword + YYYY-MM-DD)`: no randomness, no
  server-side session, no revocation, valid up to 48 h, compared
  non-constant-time. A leaked token is an offline brute-force target for the
  password itself. Separately, Fiber is not configured with `ProxyHeader`, so
  behind Traefik `c.IP()` is the proxy for every request — the login limiter is
  one global bucket that provides no brute-force protection and locks out
  everyone at once. The limiter map also never evicts. Finally, `checkInitialized`
  is a bare file-exists check, so a missing `/data/.initialized` makes
  `POST /api/setup/complete` — which sets `AdminPassword` — unauthenticated again.
- **Fix:** Random tokens in a server-side store with expiry; `ProxyHeader`
  configured; limiter eviction; close the setup re-open hole.

---

## 🟢 Low

### 51. Not All Configurable Options Are on the Settings Page

- **Status:** 🟢 Open
- **File:** `internal/api/routes.go`; `web/src/components/Settings.tsx`; `internal/config/config.go`
- **Details:** Several `Config` fields are unreachable from the UI.
  - Absent from both `GET` and `POST /api/config`: `MaxConcurrentJobs` (also
    snapshotted at `NewManager`, so it needs a restart or a resizable worker
    pool), `ReplaceInPlace`, `HoldingDir`, `PUID`, `PGID`.
  - Readable but not settable outside the setup wizard: `SourceDir`, `DestDir`,
    `GPUVendor`.
  - New fields from this review that must land here too: savings floor (#38),
    AI CRF toggle (#40), bitrate-density filter (#39), `skipHighResolution` and
    its threshold once moved (#41).
- **Fix:** For each field decide runtime-settable, restart-required, or
  deliberately env-only, and show that state in the UI rather than silently
  ignoring input.
- **Sequencing:** Land #43 first — every new mutable field widens that race.

### 52. Documentation Contradicts the Code

- **Status:** 🟢 Open
- **File:** `docs/security/audit.md`; `README.md`; `.env.example`
- **Details:**
  - `audit.md` VAST-001 claims path traversal is fixed for source and
    destination; the destination is unvalidated (#37). VAST-005 describes the
    token as "HMAC-SHA256" (it is plain SHA-256) and the login limit as "10
    attempts per minute" (the code uses 5). An overstating security document is
    worse than none.
  - `README.md` advertises Whisper speech-to-text subtitles; the implementation
    is an OpenSubtitles *download*. Its structure tree shows
    `internal/ai/subtitles/`, which is `internal/subtitles/`. Its `PORT` default
    disagrees with `config.go`.
  - `.env.example` sets `SCANNER_CONFIG_FILE=/data/scanner-config.json`; `main.go`
    defaults to `scanner_config.json`, and `docker-compose.yml` does not pass the
    variable at all.
  - `CLAUDE.md` is the most accurate of the three. One nit: it names QSV for
    Intel, but `getHWAccelInputArgs` routes Intel to VAAPI, for reasons its own
    code comment explains.

### 53. Dead Code

- **Status:** 🟢 Open
- **Details:** `/api/browse` is a second directory browser, unused by the UI
  (which calls `/api/fs/list`) and still documented in the README. Also unused:
  `FFmpegWrapper.Transcode`, `SaveWatchDirectories`, `generateID`/`randomString`
  in `routes.go` (whose crypto/rand fallback is deterministic),
  `TranscodeOptions.Container`, `HasHDR10BaseLayer`, `NoOwnership`.

---

## ✅ Previously Resolved

Issues #1–#34, closed between 2026-02-01 and 2026-02-27. Condensed; the reasoning
for the substantial ones now lives in code comments at the relevant sites.

| # | Title | Closed |
|---|-------|--------|
| 1 | Missing `/api/login` endpoint | 2026-02-01 |
| 2 | Frontend lint errors | 2026-02-01 |
| 3 | Missing logo assets | 2026-02-01 |
| 4 | Architecture diagram outdated | 2026-02-01 |
| 5 | Random string generation weakness | 2026-02-01 |
| 6 | Job queue memory persistence | 2026-02-01 |
| 7 | Search component token authentication | 2026-02-01 |
| 8 | CORS configuration too permissive | 2026-02-01 |
| 9 | CI/CD pipeline missing tests | 2026-02-01 |
| 10 | Dockerfile missing NVIDIA support | 2026-02-01 |
| 11 | `ProcessedFile` type mismatch frontend/backend | 2026-02 |
| 12 | `useEffect` dependency warnings | 2026-02 |
| 13 | Roadmap: advanced scheduling, per-job AI logging | 2026-02-27 |
| 14 | MakeMKV not installed in Docker image | 2026-02 |
| 15 | Scanner config persistence | 2026-02 |
| 16 | Data race on `s.watcher` in `UpdateConfig` | 2026-02-25 |
| 17 | TOCTOU on `s.watcher` in `Stop` | 2026-02-25 |
| 18 | `deleteSource` bypassed verification for non-premium | 2026-02-25 |
| 19 | CRF=0 silently ignored by config API | 2026-02-25 |
| 20 | `subtitlePassword` never populated in settings UI | 2026-02-25 |
| 21 | `subtitlePassword` missing from TypeScript interface | 2026-02-25 |
| 22 | `go.mod` missing direct dependency declaration | 2026-02-25 |
| 23 | Silent no-op when watch directories are empty | 2026-02-25 |
| 24 | Corrupted `jobs.json` silently ignored on load | 2026-02-25 |
| 25 | Boolean config values could not persist as `false` | 2026-02-25 |
| 26 | SSE token exposed in URL query parameter | 2026-02-25 |
| 27 | File browser hardcoded `/storage` initial path | 2026-02-25 |
| 28 | Extract job output size read directory inode | 2026-02-27 |
| 29 | 4K upscaling failed on HDR/DV under VAAPI | 2026-02-27 |
| 30 | MakeMKV progress bar stuck at 0% | 2026-02-27 |
| 31 | Scanner re-discovered already-optimized files | 2026-02-27 |
| 32 | Browser served stale JavaScript after deployment | 2026-02-27 |
| 33 | Scanner panel had no sort or filter | 2026-02-27 |
| 34 | AI log panel hidden behind undiscoverable button | 2026-02-27 |

**Note on #18:** the fix recorded there — checking that the output exists and is
non-zero — has since been superseded by the `ValidateOutput` gate in
`internal/media/validate.go`, which is considerably stronger. #35 covers the paths
that gate never reached.

---

## 📊 Priority Summary

| Priority | Open | Resolved |
|----------|------|----------|
| 🔴 Critical | 2 | 5 |
| 🟠 High | 7 | 8 |
| 🟡 Medium | 5 | 7 |
| 🟢 Low | 3 | 17 |
| **Total** | **17** | **37** |

---

## Suggested Order

1. ~~**#35**~~, ~~**#38**~~ — done.
2. **#39, #40, #41, #42** — everything actively making files worse.
3. **#36, #37** — process crash and the arbitrary-write path.
4. **#43** (with its race test), then **#44**, **#45**.
5. **#46, #47, #48, #49**.
6. **#50**, then **#51** (which depends on #43), **#52**, **#53**.

---

*Maintained manually. Every entry above was verified against the source on the
date given — claims here should not be trusted further than the last verification
date in the header.*
