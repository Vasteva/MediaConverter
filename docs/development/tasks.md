# Vastiva Media Converter - Task List

**Generated:** 2026-02-01 | **Last Updated:** 2026-02-27

## 📋 Executive Summary

The project is **fully production-ready** with all known bugs resolved. The core backend and all premium AI features are implemented and verified working in production. All 29 tracked issues are resolved. Recent work (2026-02-27) addressed bugs found via production log audit: extract output size tracking, Intel/AMD VAAPI upscaling on HDR/DV content, MakeMKV progress bar, and scanner re-discovering already-optimized files. UX improvements include sort/filter for the scanner panel and clickable job rows for AI log access.

---

## 🔴 Critical Issues (Blocking Production)

### 1. ~~Missing `/api/login` Endpoint~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Added POST `/api/login` handler in `routes.go` that validates the admin password and returns a session token using `GenerateToken()`.

### 2. ~~Frontend Lint Errors (5 errors, 2 warnings)~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Fixed all 5 ESLint errors.

### 16. Deadlock in `Scanner.UpdateConfig()`
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `internal/scanner/scanner.go`
- **Details:** The described deadlock was not present in current code (mu was already released before calling `Stop()`), but `UpdateConfig()` was reassigning `s.watcher` without holding the mutex — a data race. Fixed by creating the new watcher before taking the lock, then assigning it under `s.mu.Lock()`.

### 17. TOCTOU Race: `s.watcher` Accessed Outside Mutex in `Stop()`
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `internal/scanner/scanner.go`
- **Details:** `Stop()` released `s.mu` before accessing `s.watcher`, which could be concurrently reassigned by `UpdateConfig()`. Additionally, `watchFiles()` re-evaluated `s.watcher.Events` on every loop iteration — if `s.watcher` was set to nil between iterations, a panic would result. Fixed by: (1) capturing `w := s.watcher` under the lock in `Stop()` and closing the local copy outside; (2) having `watchFiles()` capture the watcher reference once at startup under `mu.RLock()` and using only the local copy thereafter.

---

## 🟠 High Priority Issues

### 3. ~~Missing Logo Assets~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)

### 4. ~~Architecture Diagram Outdated~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)

### 5. ~~Random String Generation Weakness~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)

### 6. ~~Job Queue Memory Persistence~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)

### 18. Data Loss: `deleteSource` Bypasses Verification for Non-Premium Users
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `internal/jobs/manager.go`
- **Details:** When `verifyOutput=false` (non-premium), `verified` was set to `true` unconditionally. Fixed: now checks that the output file exists and has non-zero size before setting `verified=true`; logs a warning and skips deletion if the file is missing or empty.

### 19. CRF=0 Silently Ignored by Config API
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `internal/api/routes.go`
- **Details:** Changed `CRF int` to `CRF *int` in the POST `/api/config` request struct. A nil pointer now means "not provided"; `0` can be expressed and is correctly applied.

---

## 🟡 Medium Priority Issues

### 7. ~~Search Component Token Authentication~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)

### 8. ~~CORS Configuration Too Permissive~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)

### 9. ~~CI/CD Pipeline Missing Tests~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)

### 10. ~~Dockerfile Missing NVIDIA Support~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)

### 20. `subtitlePassword` Never Populated in Settings UI
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `web/src/components/Settings.tsx`; `internal/api/routes.go`; `web/src/types.ts`
- **Details:** Added `subtitlePasswordSet: bool` to the GET `/api/config` response and `subtitlePasswordSet?: boolean` to the `SystemConfig` TypeScript interface. The password input now shows a `"Password saved — enter new value to change"` placeholder when a password is already stored, eliminating the blank-field confusion on reload.

### 21. `subtitlePassword` Missing from `SystemConfig` TypeScript Interface
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `web/src/types.ts`
- **Details:** `subtitlePassword?: string` was already present in the interface (added in a prior session). Added companion `subtitlePasswordSet?: boolean` for bug #20.

### 22. `go.mod` Missing Direct Dependency Declaration
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `go.mod`
- **Details:** Moved `github.com/valyala/fasthttp` from the indirect `require` block to the direct block.

---

## 🟢 Low Priority / Enhancements

### 11. ~~ProcessedFile Type Mismatch Frontend/Backend~~ ✅ FIXED
- **Status:** ✅ Resolved

### 12. ~~useEffect Dependency Warnings~~ ✅ FIXED
- **Status:** ✅ Resolved

### 13. ~~Roadmap Items (from README.md)~~ ✅ DONE
- [x] **Advanced scheduling** — Job scheduling by time/day ✅ Resolved (2026-02-27)
- [x] **Per-job AI logging** — Include details of what AI accomplished for each job ✅ Resolved (2026-02-27)
- **Details:** Added `ProcessingSchedule` config struct (startHour, endHour, allowedDays, timezone) with IANA timezone support and overnight window handling. Workers block outside the schedule window; a `scheduleWatcher` goroutine broadcasts when the window opens. Added `AILog` struct to jobs with per-operation entries for metadata cleaning, encoding analysis, subtitle download, and verification. Frontend: Settings UI has a Processing Schedule card; JobList has expandable AI log rows per job.

### 14. ~~MakeMKV Not Installed in Docker Image~~ ✅ FIXED
- **Status:** ✅ Resolved

### 15. ~~Scanner Config Persistence~~ ✅ FIXED
- **Status:** ✅ Resolved

### 23. Silent No-Op When Watch Directories Are Empty
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `internal/scanner/scanner.go`
- **Details:** `ScanAll()` now logs a warning and returns early when `WatchDirectories` is empty.

### 24. Corrupted `jobs.json` Silently Ignored on Load
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `internal/jobs/manager.go`; `internal/api/routes.go`
- **Details:** Added `loadErr string` field to `Manager`. When `jobs.json` exists but cannot be parsed, the error is logged at ERROR level and stored. The `GET /api/health` endpoint now returns HTTP 500 with `"status": "degraded"` and the error message when `loadErr` is set.

### 25. Boolean Config Values Can't Be Persisted as `false` Over Env Vars
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `internal/config/config.go`
- **Details:** `loadFromDisk()` now unmarshals the JSON into a `map[string]json.RawMessage` to detect which keys are actually present. Boolean fields (`scannerEnabled`, `scannerAutoCreate`, `verifyOutput`, `deleteSource`) are only applied when their key exists in the JSON, preventing absent fields from silently overriding env-var defaults with `false`.

### 26. SSE Token Exposed in URL Query Parameter
- **Status:** ✅ Resolved (2026-02-25)
- **File:** `internal/api/auth.go`; `internal/api/sse.go`; `internal/api/routes.go`; `web/src/App.tsx`
- **Details:** Added `POST /api/events/token` endpoint (requires Bearer auth) that issues a short-lived HMAC token valid for ~2 minutes (keyed on password + Unix minute). The SSE endpoint now validates this short-lived token via `validateSSEToken()`. The frontend exchanges the long-lived session token for an SSE token before opening `EventSource`, and refreshes it on `CLOSED` errors.

### 27. File Browser Hardcodes `/storage` Initial Path — Fails After Setup Wizard
- **Status:** ✅ Resolved (2026-02-25)
- **Reported:** 2026-02-25 (reproduced on Docker install at vasteva.wtzhome.com)
- **File:** `web/src/components/FileBrowserModal.tsx` line 37; `internal/api/fs.go` line 53
- **Details:** `FileBrowserModal` hardcodes `initialPath = '/storage'`. The backend `SourceDir` defaults to `/storage` and `DestDir` to `/output`, but after the setup wizard configures custom directories, the browser still starts at `/storage`. `handleListFiles` rejects any path that isn't under the configured `SourceDir` or `DestDir` with a 403, so the file browser shows "Access denied" and is completely unusable for adding new jobs.
- **Fix:** Added `sourceDir`/`destDir` props to `JobListProps` and passed `config.sourceDir`/`config.destDir` from `App.tsx`. The `FileBrowserModal` `initialPath` now falls back to `sourceDir` (for source browsing) or `destDir ?? sourceDir` (for destination browsing) before falling back to `'/'`.

### 28. Extract Job Output Size Shows 3–4 Bytes Instead of Actual Content Size
- **Status:** ✅ Resolved (2026-02-27)
- **File:** `internal/jobs/manager.go`
- **Details:** `os.Stat(directory).Size()` returns the directory inode size (typically 3–4 bytes on Linux), not the total content size. For extract jobs, `DestinationPath` is a directory. Fixed by walking the directory tree with `filepath.Walk` to sum the sizes of all contained files when `info.IsDir()` is true.

### 29. 4K Upscaling Fails on HDR/DV Content (Intel/AMD VAAPI) — FFmpeg Exit 218
- **Status:** ✅ Resolved (2026-02-27)
- **File:** `internal/media/ffmpeg.go`
- **Details:** `getHWAccelInputArgs` for Intel/AMD uses `-hwaccel_output_format vaapi`, placing decoded frames in VAAPI GPU memory. `getUpscaleFilter` returned a software `scale=W:H:flags=lanczos` filter for non-NVIDIA, and `getVideoEncoderArgs` composed this as `scale=W:H:flags=lanczos,hwupload` — a software filter cannot process frames already in VAAPI hardware format, causing FFmpeg exit 218. Fixed: `getUpscaleFilter` now returns `scale_vaapi=W:H` for Intel/AMD (GPU-native, handles HDR/10-bit natively). `getVideoEncoderArgs` no longer appends `hwupload` after the scale filter for the upscaling path, since frames are already on GPU.

### 30. MakeMKV Extraction Progress Bar Always Stuck at 0%
- **Status:** ✅ Resolved (2026-02-27)
- **File:** `internal/media/makemkv.go`
- **Details:** Two bugs combined to prevent any progress updates from reaching the UI: (1) `bufio.Scanner` has a 64 KB default max token size — MakeMKV's robot mode emits `MSG:` lines that can exceed this, causing the scanner to silently stop reading mid-extraction. Fixed by setting a 1 MB buffer. (2) `cmd.Wait()` was called while the goroutine draining stdout was still running, closing the pipe and truncating output. Fixed with `sync.WaitGroup` to ensure the drain goroutine finishes before `Wait()` returns.

### 31. Scanner Re-Discovers Already-Optimized Files After Restart
- **Status:** ✅ Resolved (2026-02-27)
- **File:** `internal/scanner/scanner.go`; `web/src/components/ScannerConfig.tsx`
- **Details:** Two root causes: (1) Processed files database (`processed.json`) suffered data loss in a prior persistence bug, so files processed before that fix had no record and reappeared. Fixed by adding an output-exists check in `Discover()` and `createJobForFile()` — if the expected output file already exists with non-zero size, the source is silently marked processed and skipped. (2) Watch directories added via the UI were created with `excludePatterns: []`, so `*_optimized.mkv` files were not excluded. Fixed by defaulting to `['*_optimized.mkv', '*_temp*', '.*']`.

### 32. Browser Serves Stale JavaScript After Deployment
- **Status:** ✅ Resolved (2026-02-27)
- **File:** `cmd/server/main.go`
- **Details:** `index.html` was served without a `Cache-Control` header. Browsers cached it indefinitely, so after a deployment they continued loading the old `index.html` (which referenced old content-hashed JS bundles) and saw a blank or broken UI. Fixed by adding `Cache-Control: no-cache, no-store, must-revalidate` to the SPA fallback route. Vite's content-hashed bundle filenames are still cached normally by the browser.

### 33. Scanner Panel: No Way to Sort or Filter Discovered Files
- **Status:** ✅ Resolved (2026-02-27)
- **File:** `web/src/components/ScannerConfig.tsx`
- **Details:** The discovered files table had no sort or filter controls, making it unusable with many files. Added: (1) filter tabs (All / Optimize / Extract) to narrow by job type; (2) clickable column headers (Name, Size, Savings, Type) with ascending/descending toggle and ⇅/↑/↓ indicators; (3) `useMemo`-derived `displayedFiles` computed from filter+sort state. "Select all" now operates on the filtered view.

### 34. AI Log Panel Hidden Behind Undiscoverable "AI" Button
- **Status:** ✅ Resolved (2026-02-27)
- **File:** `web/src/components/JobList.tsx`
- **Details:** The AI operations log was only accessible via a small "AI" button that appeared in the Actions column — only when `aiLogs.length > 0` — making it effectively invisible. Replaced with: clicking anywhere on a job row toggles the detail panel; a chevron in the Source cell animates open/closed to signal the row is expandable; jobs with no AI logs show "No AI operations recorded" instead of nothing. The Cancel button uses `stopPropagation` so it doesn't also toggle the row.

---

## ✅ Working Features (Confirmed)

| Feature | Status |
|---------|--------|
| Go build | ✅ Passes |
| Go tests | ✅ All pass |
| Frontend build | ✅ Passes |
| FFmpeg integration | ✅ Implemented |
| MakeMKV integration | ✅ Implemented (progress bar fixed) |
| GPU auto-detection | ✅ Working (NVIDIA, Intel, AMD) |
| VAAPI upscaling (Intel/AMD) | ✅ Working (HDR/10-bit fixed) |
| Job queue system | ✅ Working |
| Per-job AI logs | ✅ Working (click row to expand) |
| Processing schedule | ✅ Working |
| Scanner (watch/periodic) | ✅ Working |
| Scanner sort/filter | ✅ Working |
| AI provider abstraction | ✅ Gemini, OpenAI, Claude, Ollama |
| AI Search | ✅ Implemented (premium) |
| AI Whisper subtitles | ✅ Implemented (premium) |
| AI metadata cleaning | ✅ Implemented (premium) |
| License validation | ✅ Working |
| Setup wizard | ✅ Working |
| Dashboard stats | ✅ Working |
| Path security validation | ✅ Working |

---

## 📊 Priority Summary

| Priority | Open | Fixed |
|----------|------|-------|
| 🔴 Critical | 0 | 4 |
| 🟠 High | 0 | 7 |
| 🟡 Medium | 0 | 7 |
| 🟢 Low | 0 | 17 |

**All known bugs resolved as of 2026-02-27.**

---

*This task list is maintained manually. Last full review: 2026-02-27.*
