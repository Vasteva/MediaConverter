# Vastiva Media Converter - Task List

**Generated:** 2026-02-01 | **Last Updated:** 2026-02-25

## 📋 Executive Summary

The project is in a **solid foundation state** with the core backend functionality implemented. All original blocking issues have been resolved. A new code review (2026-02-25) of recent changes identified additional bugs documented below.

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

### 13. Roadmap Items (from README.md)
Per the README, these features are planned but not implemented:
- [ ] **Multi-user support** — Currently single admin only
- [ ] **Advanced scheduling** — Job scheduling by time/day
- [ ] **Webhook notifications** — Notify external services on events

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

---

## ✅ Working Features (Confirmed)

| Feature | Status |
|---------|--------|
| Go build | ✅ Passes |
| Go tests | ✅ All pass |
| Frontend build | ✅ Passes |
| FFmpeg integration | ✅ Implemented |
| MakeMKV integration | ✅ Implemented |
| GPU auto-detection | ✅ Working (NVIDIA, Intel, AMD) |
| Job queue system | ✅ Working |
| Scanner (watch/periodic) | ✅ Working |
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
| 🟢 Low | 0 | 9 |

**All known bugs resolved as of 2026-02-25.**

---

*This task list is maintained manually. Last full review: 2026-02-25.*
