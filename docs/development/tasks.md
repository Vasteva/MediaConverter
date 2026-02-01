# Vastiva Media Converter - Task List

**Generated:** 2026-02-01

## 📋 Executive Summary

The project is in a **solid foundation state** with the core backend functionality implemented. However, there are several critical issues blocking a production-ready release, primarily around authentication and frontend polish.

---

## 🔴 Critical Issues (Blocking Production)

### 1. ~~Missing `/api/login` Endpoint~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Added POST `/api/login` handler in `routes.go` that validates the admin password and returns a session token using `GenerateToken()`.


### 2. ~~Frontend Lint Errors (5 errors, 2 warnings)~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Fixed all 5 ESLint errors:
  - `Dashboard.tsx` — Used `React.CSSProperties` for CSS custom property
  - `Login.tsx` — Removed unused catch variable
  - `ScannerConfig.tsx` — Used `ScannerConfig['mode']` type instead of `any`
  - `Search.tsx` — Used `unknown` type with proper error handling
  - `SetupWizard.tsx` — Created `ProbesState` interface
- **Remaining:** 2 warnings about React hook deps (non-blocking)

## 🟠 High Priority Issues

### 3. ~~Missing Logo Assets~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Generated dark and light theme logos with "VASTIVA MEDIA CONVERTER" branding, saved as PNG files in `web/public/`. Updated `App.tsx` to reference `.png` instead of `.svg`.

### 4. ~~Architecture Diagram Outdated~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Updated `ARCHITECTURE.md`:
  - Removed "(Frontend - WIP)" - frontend is complete
  - Updated API routes list with all current endpoints
  - Added new "AI Integration (Premium Features)" section documenting provider abstraction

### 5. ~~Random String Generation Weakness~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Replaced `time.Now().UnixNano()` with `crypto/rand.Int()` in both `routes.go` and `scanner.go`. Now uses cryptographically secure random number generation for job IDs.

### 6. ~~Job Queue Memory Persistence~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Implemented JSON-based job persistence in `manager.go`:
  - Jobs saved to `jobs.json` on add and status change
  - Jobs loaded automatically on startup
  - Processing jobs reset to pending on restart
  - `RequeuePendingJobs()` method resumes interrupted jobs

---

## 🟡 Medium Priority Issues

### 7. ~~Search Component Token Authentication~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Updated `Search.tsx` to accept `authFetch` prop and use it instead of direct `fetch()`. Updated `App.tsx` to pass the prop.

### 8. ~~CORS Configuration Too Permissive~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Configured CORS with `CORS_ORIGINS` environment variable support. Defaults to localhost for dev, can be set to specific domains for production.

### 9. ~~CI/CD Pipeline Missing Tests~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Added `go test ./... -v` step and `npm run lint` to CI pipeline before Docker build. Tests must pass before deployment.

### 10. ~~Dockerfile Missing NVIDIA Support~~ ✅ FIXED
- **Status:** ✅ Resolved (2026-02-01)
- **Details:** Created `Dockerfile.nvidia` based on NVIDIA CUDA 12.3 runtime, added `docker-compose.nvidia.yml` override, and documented NVIDIA deployment in README.

---

## 🟢 Low Priority / Enhancements

### 11. ProcessedFile Type Mismatch Frontend/Backend
- **Location:** `web/src/types.ts` vs `internal/scanner/scanner.go`  
- **Details:** Frontend `ProcessedFile` type is missing `inputSize`, `outputSize`, `aiSubtitles`, `aiUpscale`, `aiCleaned` fields that exist in backend.
- **Fix:** Sync TypeScript type definitions with Go structs.

### 12. useEffect Dependency Warnings
- **Location:** `App.tsx:159, 168`
- **Details:** React hooks have missing dependencies (`fetchJobs`, `fetchStats`, `fetchConfigs`).
- **Fix:** Add dependencies or use `useCallback` to memoize fetch functions.

### 13. Roadmap Items (from README.md)
Per the README, these features are planned but not implemented:
- [ ] **Multi-user support** — Currently single admin only
- [ ] **Advanced scheduling** — Job scheduling by time/day
- [ ] **Webhook notifications** — Notify external services on events

### 14. MakeMKV Not Installed in Docker Image
- **Status:** Documentation Gap
- **Details:** Dockerfile doesn't install MakeMKV; users expecting disc extraction will need to mount it separately.
- **Fix:** Document this limitation or add optional MakeMKV installation.

### 15. Scanner Config Persistence
- **Status:** Works but Fragile
- **Details:** Scanner config saves to `scanner-config.json` path, but the path is hardcoded. Docker restarts may lose config unless volume mounted.
- **Fix:** Ensure `/data` is documented as required volume mount.

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

| Priority | Count | Effort Estimate |
|----------|-------|-----------------|
| 🔴 Critical | 2 | 1-2 hours |
| 🟠 High | 4 | 4-6 hours |
| 🟡 Medium | 4 | 4-8 hours |
| 🟢 Low | 5 | 8+ hours |

**Recommended Next Steps:**
1. Fix `/api/login` endpoint (Critical - 15 min)
2. Fix lint errors (Critical - 30 min)
3. Add logo assets (High - 15 min)
4. Improve random string generation (High - 15 min)
5. Run tests in CI pipeline (Medium - 30 min)

---

*This task list was generated by analyzing the codebase, running builds and tests, and reviewing documentation.*
