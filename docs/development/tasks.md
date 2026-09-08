# Vastiva Media Converter — Task List

**Generated:** 2026-02-01 | **Last Updated:** 2026-09-08

## 📋 Executive Summary

Zero open issues. Every item from the full source review on 2026-09-04, plus
everything found in production testing on 2026-09-03 (including #54, found
only by actually deploying and using the fixes), is now resolved: #35 through
#54, closed within days of being filed.
#37's Docker/entrypoint half is now deploy-verified on the homelab host: the
process genuinely drops to PUID/PGID, not just in theory. VAAPI itself is still
unconfirmed — see #37's entry for where that attempt got interrupted.

The media pipeline itself — `internal/media` (ffmpeg, progress, validate) and
`internal/ai/meta` — is in good shape and holds up under review. Every
concurrency issue (#43–#45), #46's silent progress stall, #47's leaked
extract directory and reintegration overwrite, #48's permanently-excluded
failed-job files, #49's unbounded HTTP calls, #50's password-derived session
tokens, and #51's unreachable config fields are fixed and covered by
regression tests that reproduce the original bugs (extractDir's own #47 fix,
#49's `AnalyzeEncoding` deadline, and #50's `ProxyHeader`/`TrustedProxies`
wiring are the exceptions — each verified by review and full-suite regression
rather than a dedicated test, since exercising any of them needs
infrastructure disproportionate to the size of the fix). The last two,
housekeeping items, are closed too: #52 brought `docs/security/audit.md`,
`README.md`, `.env.example`, `CLAUDE.md`, and `docs/architecture/overview.md`
back in line with the code (the token/rate-limit claims, the Whisper-vs-
OpenSubtitles subtitle description in two separate documents, a stale port
default, a stale env var default, and an Intel QSV-vs-VAAPI nit), and #53
removed seven confirmed-dead symbols, including the unused `/api/browse`
endpoint.
  `ReplaceInPlace`, `HoldingDir`, `PUID`/`PGID` are absent from both `GET` and
  `POST /api/config` entirely, and `SourceDir`/`DestDir`/`GPUVendor` are
  readable but not settable outside the setup wizard (#51).

The previous revision of this file claimed all known bugs were resolved. That was
written 2026-02-27 and was not re-verified against the code before this review.

---

## ✅ Closed Today

### 53. Dead Code

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `internal/api/routes.go`; `internal/media/ffmpeg.go`;
  `internal/scanner/config.go`; `internal/util/ownership.go`;
  `internal/util/filename_test.go`; `README.md`
- **Details:** Confirmed each symbol genuinely had zero call sites (beyond
  its own definition and, for `NoOwnership`, its own test) before removing
  anything: `/api/browse` (`RegisterFileBrowserRoute`) — a second directory
  browser, unused by the UI, which calls `/api/fs/list` instead;
  `FFmpegWrapper.Transcode` — superseded by `TranscodeWithProgress`, which
  is what every caller actually uses; `SaveWatchDirectories`;
  `generateID`/`randomString` in `routes.go` (job IDs actually come from
  `util.GenerateID`, a different function in a different package);
  `TranscodeOptions.Container`; `HasHDR10BaseLayer`; `NoOwnership`.
- **Fix:** Deleted all of the above, including `RegisterFileBrowserRoute`'s
  call site and its README documentation row, and the now-unused
  `crypto/rand`/`math/big` imports in `routes.go` that only `randomString`
  needed. `NoOwnership()` was a thin `FileOwnership{UID: -1, GID: -1}`
  convenience constructor with no production callers — its three
  `filename_test.go` call sites became the literal directly, preserving
  those tests' actual coverage of `Enabled`/`Apply`/`String` (which remain
  very much alive) rather than deleting them along with the dead
  constructor they happened to use.
- **Tests:** No new tests — this is subtraction, not new behavior. Existing
  coverage for everything that stayed (`FileOwnership`'s other methods,
  `buildFFmpegArgs`, etc.) continues to pass. `gofmt -l .`, `go vet ./...`,
  `go build ./...`, `go test -race -count=1 ./...`, and `npm run
  build`/`lint` all clean.

### 52. Documentation Contradicts the Code

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `docs/security/audit.md`; `README.md`; `.env.example`;
  `CLAUDE.md`; `docs/architecture/overview.md`
- **Details:** Verified each claim in `audit.md` against the current code
  rather than trusting it on the strength of VAST-001 alone. Also found, in
  the course of checking, that `docs/architecture/overview.md` carried the
  exact same subtitle-implementation error as `README.md`, pointing at a
  path (`internal/ai/whisper/generator.go`) that hasn't existed since the
  refactor `CLAUDE.md` itself already documents.
- **Fix:**
  - `audit.md` VAST-005 no longer claims the token is "HMAC-SHA256" (it was
    plain SHA-256, and is now — per #50 — a random server-side token with no
    relation to the password at all) or that the login limit is "10 attempts
    per minute" (the code has always used 5). Added a dated note describing
    the #50 token redesign and the `TRUSTED_PROXY_CIDRS` caveat for the rate
    limiter behind a reverse proxy, rather than silently rewriting the
    original finding.
  - `README.md`: "Whisper Subtitles" → "Subtitle Downloads" (both in the
    features list and the roadmap), matching the real OpenSubtitles-based
    implementation; the structure tree's `internal/ai/subtitles/` corrected
    to the real top-level `internal/subtitles/`; `PORT` default `80` → `8080`;
    removed the `/api/browse` row (#53) and added the previously-undocumented
    `/api/logout` (#50); "Intel QSV" → "Intel/AMD VAAPI" in the features list,
    matching the actual `getHWAccelInputArgs` routing.
  - `docs/architecture/overview.md`'s AI-features diagram: replaced the
    "Whisper Subtitles → internal/ai/whisper/generator.go" bullet with the
    real subtitle-download path and description.
  - `.env.example`: `SCANNER_CONFIG_FILE=/data/scanner-config.json` →
    `/data/scanner_config.json` (underscore), matching `main.go`'s actual
    default — the hyphenated version never matched anything the code looked
    for, whether or not `docker-compose.yml` happens to pass the variable
    through.
  - `CLAUDE.md`: "Intel (QSV)" → "Intel/AMD (VAAPI — more reliable than QSV
    in containers, per the comment in `getHWAccelInputArgs`)."
- **Tests:** Documentation-only; no test coverage applies. Verified each
  corrected claim by reading the referenced code directly (`getHWAccelInputArgs`'s
  VAAPI routing, `main.go`'s `SCANNER_CONFIG_FILE`/`PORT` defaults,
  `sessions.go`'s token design, `ratelimit.go`'s attempt cap) rather than
  taking the old document's word for any of it.

### 51. Not All Configurable Options Are on the Settings Page

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `internal/api/routes.go`; `internal/config/config.go`;
  `web/src/components/Settings.tsx`; `web/src/types.ts`; `web/src/App.tsx`;
  `internal/api/config_route_test.go` (new)
- **Details:** Several `Config` fields were unreachable from the UI: absent
  from both `GET`/`POST /api/config` entirely (`MaxConcurrentJobs`,
  `ReplaceInPlace`, `HoldingDir`, `PUID`, `PGID`, `SavingsFloor` (#38),
  `DensityFloor` (#39)), or readable but not settable anywhere
  (`GPUVendor` — the Settings dropdown existed but was hardcoded
  `disabled`). `SourceDir`/`DestDir` turned out not to be settable via any
  UI flow either, including the setup wizard, despite the original ticket
  text assuming otherwise.
- **Fix:** Classified each field per the ticket's own framework
  (runtime-settable / restart-required / deliberately env-only) rather than
  exposing everything as a plain editable input:
  - **Runtime-settable, added to `GET`/`POST /api/config` and the Settings
    UI:** `GPUVendor` (validated against the same four values the dropdown
    already offered), `ReplaceInPlace`, `HoldingDir`, `PUID`/`PGID`
    (validated `>= -1`, matching the existing "-1 leaves ownership
    untouched" convention), `SavingsFloor` (validated `0-1`), `DensityFloor`
    (validated `>= 0`). All are read fresh per job via `Snapshot()` already,
    so none of these needed anything beyond wiring.
  - **Restart-required:** `MaxConcurrentJobs` is snapshotted once into
    `Manager.maxConcurrent` at construction and never re-read — a
    resizable worker pool is a real feature, not a config-exposure fix, so
    out of scope here. Exposed as read-write regardless (validated `>= 1`),
    with `POST /api/config`'s response carrying a new `restartRequired`
    flag set only when this field was part of the request, so the UI can
    say so explicitly instead of implying an effect that hasn't happened
    yet. Settings.tsx shows a banner on that flag.
  - **Deliberately env-only:** `SourceDir`/`DestDir` are container
    bind-mount paths (`docker-compose.yml`'s `volumes:`) that the app has
    no ability to remount at runtime — accepting new values for them would
    update `cfg.SourceDir`/`DestDir` (used by `security.ValidatePath`,
    scanner defaults, job creation) while the actual filesystem content
    stayed wherever the old mount pointed, a worse outcome than leaving
    them unreachable. Added to the Settings UI as disabled, read-only
    fields with an explanatory note, rather than left invisible — the
    ticket's own ask ("show that state in the UI rather than silently
    ignoring input") applies to *why not*, not just *how*.
- **Tests:** `TestPostConfigExposesPreviouslyUnreachableFields` round-trips
  every newly-writable field through `POST` then `GET`.
  `TestPostConfigRestartRequiredOnlyForMaxConcurrentJobs` confirms the flag
  doesn't fire for an unrelated field. `TestPostConfigValidatesNewFields`
  table-tests each new validation rule. `GPUVendor`/`MaxConcurrentJobs`/
  `HoldingDir`/`PUID` were additionally verified by hand end-to-end: ran the
  real server and Vite dev server together, logged in through the actual
  UI, changed each field, confirmed the value round-tripped through a page
  reload by reading `GET /api/config` directly, and confirmed the restart
  banner appears exactly when `maxConcurrentJobs` is saved and not
  otherwise. `npm run build`/`lint` clean. `gofmt -l .`, `go vet ./...`,
  `go build ./...`, and `go test -race -count=1 ./...` all clean.

### 50. Session Tokens and Rate Limiting

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `internal/api/sessions.go` (new); `internal/api/auth.go`;
  `internal/api/routes.go`; `internal/api/sse.go`; `internal/api/ratelimit.go`;
  `internal/config/config.go`; `cmd/server/main.go`; `.env.example`;
  `web/src/App.tsx`; plus new/updated tests in `internal/api` and
  `internal/config`
- **Details:** Four distinct problems bundled under one ticket.
  - Tokens were `sha256(adminPassword + YYYY-MM-DD)`: deterministic (anyone
    who knew the password could compute a valid token without ever logging
    in), impossible to revoke individually (nothing recorded which tokens
    had been issued), valid up to 48h, and — worst of all — a leaked token
    was itself an offline brute-force oracle for the password, since
    checking a candidate password against a stolen token never had to touch
    the rate-limited login endpoint at all. The SSE token variant had the
    identical defect on a 2-minute window.
  - Fiber wasn't configured with `ProxyHeader`, so behind Traefik `c.IP()`
    returned the proxy's own address for every request — the login limiter
    saw one client no matter how many real ones there were.
  - `RateLimiter`'s IP map never evicted, growing by one entry per
    attacker-controlled IP forever on a public, unauthenticated endpoint.
  - `checkInitialized` was a bare `.initialized`-file existence check. Since
    `AuthMiddleware` unlocks every setup route (including `POST
    /api/setup/complete`, which sets `AdminPassword`) whenever
    `IsInitialized` is false, a missing marker file — a lost volume, a wiped
    `/data`, a restore that didn't carry it over — reopened unauthenticated
    admin-password-setting even with a password already configured.
- **Fix:**
  - Added `SessionStore` (`sessions.go`): random 256-bit tokens in a
    server-side map with expiry, replacing every password-derived token.
    Login sessions get `SessionTTL` (24h), SSE tokens get the much shorter
    `SSETokenTTL` (2 minutes, since that one travels in a URL query string
    rather than a header). `AuthMiddleware` and the SSE route now validate
    against the store instead of recomputing a hash; a new `POST
    /api/logout` (inside the authenticated group) calls `Revoke` — a
    capability that could not have existed under the old scheme, since
    nothing recorded which tokens were live. The store self-evicts expired
    entries at most once per `evictionInterval`, mirroring the same fix
    applied to `RateLimiter` below. The login handler's password comparison
    also moved to `subtle.ConstantTimeCompare`, the same class of concern
    the token redesign addresses for tokens themselves.
  - `cmd/server/main.go`'s `fiber.New` now sets `ProxyHeader:
    fiber.HeaderXForwardedFor`, gated behind a new `TRUSTED_PROXY_CIDRS` env
    var (`EnableTrustedProxyCheck` + `TrustedProxies`, both empty/off by
    default — an unconditional `ProxyHeader` would let the container's
    directly-published port, see `docker-compose.yml`, spoof
    `X-Forwarded-For` to bypass the limiter entirely). Documented in
    `.env.example`; left unset, behavior is unchanged from before this fix.
  - `RateLimiter` gained the same eviction pattern as `SessionStore`:
    entries inactive longer than `staleAfter` (10 minutes — well past the
    1-minute reset window) are swept at most once per
    `rateLimiterEvictionInterval`.
  - `checkInitialized` now takes `adminPassword` and returns `true`
    immediately if it's non-empty, independent of the marker file's
    presence — a password already configured is by itself sufficient
    evidence setup ran before.
  - `web/src/App.tsx`'s `handleLogout` now calls `POST /api/logout`
    (fire-and-forget, best-effort) before forgetting the token locally —
    without this the new revocation capability would exist server-side but
    never actually be used by the one client that has it.
- **Tests:** `internal/api`: `TestSessionStoreIssueAndValid`,
  `TestSessionStoreIssueIsRandom`, `TestSessionStoreExpiry`,
  `TestSessionStoreRevoke`, `TestSessionStoreEvictionBoundsMapGrowth`,
  `TestRateLimiterBlocksAfterFiveAttempts`,
  `TestRateLimiterEvictionBoundsMapGrowth`, `TestLogoutRevokesToken` (through
  the real app: a request succeeds before logout and is rejected with the
  same token after), `TestLoginIssuesFreshTokenEachTime`,
  `TestInvalidTokenRejected`. `internal/config`:
  `TestCheckInitializedTreatsExistingPasswordAsInitialized` (confirmed this
  fails against the pre-fix code with the exact reopened-hole symptom) and
  `TestCheckInitializedFalseWithNeitherPasswordNorMarker` (the negative
  case). `ProxyHeader`/`TrustedProxies` has no dedicated test — meaningfully
  simulating a request that arrives from a specific trusted-vs-untrusted
  peer address isn't practical through Fiber's `app.Test()` harness — and is
  covered by review and full-suite regression instead. `npm run
  build`/`lint` clean on the frontend change. `gofmt -l .`, `go vet ./...`,
  `go build ./...`, and `go test -race -count=1 ./...` all clean.

### 49. No HTTP Client Timeouts

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `internal/ai/provider.go`; `internal/ai/openai.go`;
  `internal/ai/openai_verify.go`; `internal/ai/claude.go`;
  `internal/ai/claude_verify.go`; `internal/ai/gemini.go`;
  `internal/ai/ollama.go`; `internal/ai/ollama_verify.go`;
  `internal/ai/timeout_test.go` (new); `internal/subtitles/opensubtitles.go`;
  `internal/subtitles/opensubtitles_test.go` (new); `internal/jobs/manager.go`
- **Details:** Thirteen `http.DefaultClient.Do` sites (`internal/ai`'s six
  providers/verifiers plus `internal/subtitles/opensubtitles.go`'s four),
  none with a timeout — `http.DefaultClient` has none by default. Every one
  of those requests is already built with `NewRequestWithContext(ctx, ...)`,
  but on the job path `ctx` is `job.ctx`, a plain `context.WithCancel` with
  no deadline of its own. An unresponsive endpoint — most concretely a
  hung local Ollama instance — blocked its caller forever, and since the
  job path calls these synchronously from a worker goroutine, that meant a
  permanently stuck worker, not just a stuck request.
- **Fix:**
  - Added a package-level `httpClient` (`internal/ai/provider.go`, 120s
    timeout) and replaced every `http.DefaultClient.Do` in the six
    provider/verifier files with it — one client shared by every AI call in
    the package, since `Timeout` bounds the whole round trip regardless of
    what context the caller passes.
  - Added the equivalent in `internal/subtitles/opensubtitles.go` (30s — a
    much shorter bound for small JSON API calls and one small subtitle
    file), replacing its four call sites the same way.
  - `AnalyzeEncoding`'s call site in `manager.go` — the ticket's named
    example of "the job path" — now wraps `job.ctx` in its own
    `context.WithTimeout(job.ctx, 30*time.Second)` rather than relying on
    the shared client's more generous general-purpose bound: the CRF
    suggestion is explicitly optional (the existing fallback is the
    configured CRF, already a good answer), so it shouldn't hold up a job
    for the full 120s just to fail gracefully anyway.
- **Tests:** `TestHTTPClientEnforcesTimeout` in both `internal/ai` (via
  `OpenAIProvider.Analyze` against an `httptest.Server` that never responds)
  and `internal/subtitles` (via `fetchContent`, the one call that takes its
  URL directly rather than through the fixed `osBaseURL` constant) shrink
  the shared client's `Timeout` to 100ms for the test and confirm the call
  returns an error well within a 5s outer bound rather than hanging.
  Confirmed both fail against a reverted single call site (temporarily
  restoring `http.DefaultClient.Do` in `openai.go` and in
  `opensubtitles.go`'s `fetchContent`) with "the client timeout was not
  enforced", and pass with the fix. `AnalyzeEncoding`'s explicit deadline
  has no dedicated test — exercising it needs a full job pipeline with a
  real AI provider and a slow endpoint, disproportionate to a two-line
  `context.WithTimeout` wrap — and is covered by review and full-suite
  regression instead. `gofmt -l .`, `go vet ./...`, `go build ./...`, and
  `go test -race -count=1 ./...` all clean.

### 48. Files Are Marked Processed at Job Creation

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `internal/scanner/scanner.go`; `internal/scanner/scanner_test.go`
- **Details:** `createJobForFile` and `QueueFile` both called `MarkProcessed`
  — the durable, permanent entry — the moment a job was merely *created*,
  before it had done anything. `shouldProcessFile` (the gate every scan
  checks before creating a job) treats any tracked entry as "skip this
  file", so a job that later failed left the source permanently excluded
  from every future scan, with no way back short of manually editing
  `processed.json`.
- **Fix:** Added `ProcessedFile.InFlight` and two `ProcessedDB` methods:
  `MarkInFlight` (writes a lightweight non-durable entry — no hash, no
  `ProcessedAt` — when a job is created) and `ClearInFlight` (removes it,
  but only if it's still marked in-flight, so it never clobbers a durable
  entry that raced in). `createJobForFile` and `QueueFile` now call
  `MarkInFlight` instead of `MarkProcessed`. `CompleteProcessed` — the
  `jobs.Manager.OnJobComplete` hook, which fires on both success and
  failure — now checks `job.GetStatus()` first: `StatusCompleted` writes
  the durable entry exactly as before, anything else calls `ClearInFlight`
  instead, making the file visible to scans again. The three other
  `MarkProcessed` calls in `createJobForFile`/`Discover` (skip-high-res,
  skip-already-efficient, skip-output-exists) are unchanged — those are
  genuine "this file will never be touched" policy decisions made without
  creating a job at all, not the bug this ticket describes.
- **Tests:** `TestFailedJobDoesNotPermanentlyExcludeFileFromScans` drives
  `createJobForFile` then `CompleteProcessed` with a failed job, and checks
  `shouldProcessFile` directly (not `QueueFile`, which — being the manual
  "process this file now" bypass — never checked `IsProcessed` to begin
  with and would have passed regardless of whether the fix worked).
  Confirmed this reproduces the original bug: a throwaway copy of the test
  built against the pre-fix `scanner.go` (via `git stash`) failed with "a
  failed job left the file permanently excluded from future scans", and
  passes with the fix. `TestCompletedJobPromotesInFlightEntryToDurable`
  covers the success path, confirming the entry ends up durable
  (`InFlight: false`, `ProcessedAt` set) rather than still in-flight.
  `gofmt -l .`, `go vet ./...`, `go build ./...`, and
  `go test -race -count=1 ./...` all clean.

### 47. Extract Directory Leak and Reintegration Overwrite

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `internal/jobs/manager.go`; `internal/jobs/reintegrate.go`;
  `internal/jobs/reintegrate_test.go`
- **Details:** Two unrelated file-handling holes.
  - The ISO auto-extract path's `os.RemoveAll(extractDir)` ran only inside
    `if err == nil` after the optimize step, so a failed optimize following
    a successful extraction left a full-size intermediate MKV behind in a
    hidden `.extract_<id>` directory permanently — and on a retry, the stale
    file could still be sitting there for `filepath.Glob(extractDir,
    "*.mkv")` to pick up alongside the freshly re-extracted one.
  - `reintegrate` refused to overwrite an existing holding-path file but had
    no equivalent guard on `paths.Final`. `os.Rename` replaces its
    destination atomically with no warning, so replacing `movie.avi` (whose
    transcode promotes to `movie.mkv`) silently destroyed an unrelated
    `movie.mkv` that happened to already sit next to it in the library.
- **Fix:**
  - Changed the extract-dir cleanup to `defer os.RemoveAll(extractDir)`
    right after it's created, so it runs on every exit from `processJob` —
    success, failure, or an in-progress auto-retry — not just the success
    path. Safe to call again on a subsequent retry: `MkdirAll` recreates the
    directory, and `RemoveAll` on an already-gone path is a no-op.
  - Added a `paths.Final != paths.Source` guard before `reintegrate` touches
    anything: if something already exists at `Final` and it isn't the file
    about to be moved to holding, refuse before the source is touched at
    all. Skipped when `Final == Source` (the common same-container
    replace-in-place case, e.g. `movie.mkv` → `movie.mkv`), where `Final`
    legitimately "already exists" as the very file about to be relocated.
- **Tests:** `TestReintegrateRefusesToOverwriteUnrelatedFinalFile` (an
  avi→mkv replacement with an unrelated pre-existing `.mkv` at the output
  path — confirmed this fails against the pre-fix code, silently clobbering
  the unrelated file, and passes with the fix) and
  `TestReintegrateAllowsSameContainerSelfReplacement` (the ordinary
  mkv→mkv case must still succeed). The `extractDir` leak fix has no
  dedicated test — exercising it needs a real or mocked `makemkvcon`, which
  neither this sandbox nor the existing test suite has infrastructure for —
  and is instead a small, self-evidently-correct `defer` change verified by
  full-suite regression. `gofmt -l .`, `go vet ./...`, `go build ./...`, and
  `go test -race -count=1 ./...` all clean.

### 46. Progress Parsing Repeats the MakeMKV Scanner Bug

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `internal/media/progress.go`; `internal/media/media_test.go`
- **Details:** FFmpeg writes two things to the same stderr stream this code
  scans: `-progress pipe:2`'s `key=value\n` lines, and the human-readable
  `-stats` line (also explicitly passed in `buildFFmpegArgs`), which is
  rewritten in place with `\r` and never terminated by `\n`. `parseProgress`
  used a default `bufio.Scanner` — 64 KB token cap, splits on `\n` only, and
  never checked `scanner.Err()` — so a long enough run of un-terminated
  `-stats` output was read as one giant token, hit `bufio.ErrTooLong`, and
  silently ended the scan. The transcode itself kept running to completion;
  only the UI's progress bar and ETA froze for the rest of the job. This is
  the identical defect already fixed once in `makemkv.go` under #30 —
  `stderrMonitor.Write` in the same file already handles `\r` correctly for
  its own copy of this stream, just not `parseProgress`'s.
- **Fix:** Added `scanLinesOrCR`, a custom `bufio.SplitFunc` that splits on
  the first of `\r` or `\n` (mirroring `stderrMonitor.Write`'s
  `bytes.IndexAny` approach), and set it via `scanner.Split()`. Grew the
  scanner buffer to 1 MB, matching `makemkv.go`'s #30 fix. Added a
  `scanner.Err()` check after the scan loop that logs rather than silently
  dropping any remaining error.
- **Tests:** `TestParseProgressSurvivesLongUnterminatedStatsLine` feeds
  `parseProgress` over 64 KB of `\r`-only-terminated synthetic `-stats`
  output followed by one real `\n`-terminated progress line, and confirms
  the callback still fires and reports the final frame. Confirmed this
  fails against the pre-fix code (`git stash` on just `progress.go`) with
  "scanning stopped silently", and passes with the fix. `gofmt -l .`,
  `go vet ./...`, `go build ./...`, and `go test -race -count=1 ./...` all
  clean.

### 45. Job State Is Rewritten Several Times a Second

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `internal/jobs/manager.go`; `internal/jobs/jobs_test.go`
- **Details:** `updateJob`'s `m.Save()` fired on every progress callback —
  FFmpeg/MakeMKV's `-stats`-equivalent output several times a second — and
  each call marshaled *every* tracked job and rewrote `jobs.json` whole.
  Separately, nothing pruned completed or failed jobs, so the file (and the
  cost of that rewrite) grew without bound on a long-running install, since
  the scanner auto-creates one job per source file it finds.
- **Fix:**
  - Added `updateJobProgress`, used only at the four progress-tick call
    sites (FFmpeg transcode, both MakeMKV extraction progress callbacks, and
    `runTest`'s ticker). It still calls `OnJobUpdate` on every tick, so SSE
    progress bars stay live, but throttles the `Save()` call to at most once
    per `progressSaveInterval` (1s) via a manager-wide last-saved timestamp.
    Every other `updateJob` call site — actual state transitions — is
    unchanged and still saves immediately.
  - Added `pruneOldJobs`, called once at the end of `processJob` after a job
    reaches `StatusCompleted` or `StatusFailed`. It keeps at most
    `maxRetainedTerminalJobs` (500) terminal jobs, dropping the oldest by
    `CompletedAt` and re-saving; pending/processing jobs are never touched.
- **Tests:** `TestUpdateJobProgressThrottlesSaves` drives `updateJobProgress`
  directly and reads `jobs.json` back after each call to prove a tick within
  the throttle window doesn't hit disk, while one after the window does
  (`progressSaveInterval` is a `var` so the test shrinks it rather than
  waiting out a real second). `TestPruneOldJobsCapsTerminalJobCount` seeds
  more completed jobs than a (similarly shrunk) `maxRetainedTerminalJobs`
  and confirms only the most recent survive. `gofmt -l .`, `go vet ./...`,
  `go build ./...`, and `go test -race -count=1 ./...` all clean.

### 44. Purged and Retried Jobs Can Run Twice

- **Status:** ✅ Resolved (2026-09-08)
- **File:** `internal/jobs/manager.go`; `internal/jobs/jobs_test.go`
- **Details:** Two distinct ways the same job could end up running twice.
  - `PurgeJobs` deleted a job from `m.jobs` but never touched `m.pq`. A
    pending job purged while still sitting in the heap was popped and run by
    a worker anyway — the same shape as the cancelled-job bug already
    guarded at `processJob`'s "cancelled before it started" check, which
    `PurgeJobs` never received.
  - `RetryJob` pushed onto the heap with no check that the job was already
    there. The real trigger: a job fails, the auto-retry backoff path resets
    it to `StatusPending` and sleeps before its own re-push; a manual retry
    landing during that sleep sees an ordinary pending job, resets it again,
    and pushes it — so when the sleep ends, the backoff path pushes the same
    `*Job` a second time. Two workers pop the same pointer and run two
    FFmpeg processes against one output file.
- **Fix:**
  - Added an unexported `queued bool` on `Job` (guarded by its existing
    `mu`), with `markQueued()` (sets it, reports whether it was already set)
    and `clearQueued()` helpers. Every push site (`AddJob`, `RetryJob`, the
    auto-retry backoff re-push, `RequeuePendingJobs`) now goes through
    `markQueued()` and skips its push if the job was already queued; the
    worker's `heap.Pop` clears it immediately so a later retry is free to
    queue the job again.
  - `RetryJob` now rejects a job that's already queued with the same
    "already processing"-style error, instead of resetting and pushing it.
  - `PurgeJobs` now gives a purged job the same treatment `CancelJob` gives
    an active one: cancel its context (a no-op if it never started) and set
    `Status = StatusCancelled`, so `processJob`'s existing guard skips it if
    a worker ever pops it after the fact.
- **Tests:** `TestPurgeJobsStopsQueuedPendingJobFromRunning` (purges a
  pending, still-queued job, then starts workers and confirms it never
  transitions out of `StatusCancelled`); `TestJobMarkQueuedAndClearQueued`
  (unit-tests the queued-flag primitive directly); `TestRetryJobRejectsAlreadyQueuedJob`
  (an already-queued pending job — standing in for the auto-retry-backoff
  window — is rejected rather than pushed twice). `gofmt -l .`, `go vet
  ./...`, `go build ./...`, and `go test -race -count=1 ./...` all clean.

### 43. Shared Mutable State Is Unsynchronised

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `internal/config/config.go`; `internal/config/config_test.go`;
  `internal/jobs/manager.go`; `internal/jobs/reintegrate.go`;
  `internal/api/auth.go`; `internal/api/fs.go`; `internal/api/sse.go`;
  `internal/api/routes.go`; `internal/scanner/scanner.go`;
  `internal/scanner/scanner_test.go`
- **Details:** Four distinct races, all now fixed.
  - **`config.Config` had no mutex at all.** `POST /api/config` mutated
    `CRF`, `IsPremium`, `Schedule`, `DeleteSource` and more from the HTTP
    goroutine while workers read them with no synchronization.
  - **`m.ai`** — `UpdateAIProvider` wrote under `m.mu`; 14 reads did not,
    including `GetAI()` itself.
  - **`s.config`** — `UpdateConfig` wrote under `s.mu`; roughly 28 reads in
    `Start`, `ScanAll`, `scanDirectory`, `createJobForFile`,
    `generateOutputPath`, `setupWatchers`, `handleNewFile`, `periodicScan`,
    and `Discover` did not. `GetConfig()` returned the live pointer straight
    to the JSON encoder.
  - **`Scanner.Stop()` set `s.stopCh = nil`** while `watchFiles`,
    `periodicScan`, and `delayedProcess` were selecting on that field — a
    goroutine that read the nil after the close but before (or in place of)
    seeing it fire blocked on that case forever, and for `periodicScan`
    specifically that meant `Stop()`'s own `wg.Wait()` could hang
    indefinitely. Separately, `delayedProcess` was spawned without
    `wg.Add`, so it outlived `Stop` entirely.
- **Fix:**
  - Added a nil-tolerant `*sync.RWMutex` to `config.Config` with a
    `WithLock(fn func())` write helper (deliberately not named
    `Lock`/`Unlock` — that shape satisfies `sync.Locker` and makes `go vet`'s
    copylocks check flag every copy of `Config`, including `Snapshot`'s own
    return) and a `Snapshot() Config` method returning a point-in-time copy.
    Every HTTP handler and worker-side read now goes through `Snapshot()`;
    every write goes through `WithLock`. CRF validation in `POST /api/config`
    was deliberately moved before the `WithLock` call — returning from
    inside that closure would only exit the closure, not the HTTP handler.
  - `m.ai` reads now go through `GetAI()` (which takes `m.mu.RLock()`);
    `processJob`/`runOptimizationFromPath` take one `aiProvider` snapshot per
    job run rather than re-reading the field at each of the ~15 use sites.
  - `s.config` reads in every listed function now capture `cfg :=
    s.GetConfig()` once under `s.mu.RLock()` at the top of the function,
    mirroring the pattern `QueueFile` already used. No deep copy is needed —
    `UpdateConfig` always swaps the whole pointer rather than mutating
    fields in place.
  - `watchFiles`, `periodicScan`, and `delayedProcess` now capture `stopCh
    := s.stopCh` once (under `s.mu.RLock()`) before entering their `select`
    loop, instead of re-reading the field each iteration — closing a channel
    is visible on an already-captured reference regardless of what the
    field is later set to. `delayedProcess`'s spawn site in `handleNewFile`
    now calls `s.wg.Add(1)` to match every other long-lived scanner
    goroutine.
- **Tests:** `TestSnapshotIsRaceFreeUnderConcurrentWrites`
  (`internal/config/config_test.go`) hammers `Config.WithLock`/`Snapshot`
  from 4 writer and 8 reader goroutines. `TestConcurrentJobRunWithConfigAndScannerConfigChurn`
  (`internal/scanner/scanner_test.go`) runs a real `JobTypeTest` job through
  the worker pool while one goroutine hammers system config the way `POST
  /api/config` does and another repeatedly calls `Scanner.UpdateConfig` —
  which itself calls `Stop()` then `Start()`, exactly where the `stopCh`
  race lived — the way `POST /api/scanner/config` does. `gofmt -l .`, `go
  vet ./...`, `go build ./...`, and `go test -race -count=1 ./...` are all
  clean across every package.

### 54. Job Creation Failures Are Silently Swallowed by the UI

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `web/src/App.tsx`; `web/src/components/JobList.tsx`
- **Details:** Found live, not by review — deploying #37/#41 to production and
  testing a real conversion surfaced it immediately. `createJob` in `App.tsx`
  returned a bare `boolean`; on any non-2xx response it returned `false` with
  no message, and `JobList.tsx`'s create-job modal did nothing with a `false`
  beyond re-enabling the submit button — the modal just sat there. This made
  every rejection the backend already explains clearly (#41's resolution
  filter, #37's path validation, a bad CRF, license checks, ...) look
  identical to the UI being broken. The specific trigger: a `POST /api/jobs`
  for a 2160p source correctly rejected by `skipHighResolution`, which
  produced a clear 400 with a `{error}` body that the UI threw away.
- **Fix:** `createJob` now returns `{ ok, error? }`, reading the backend's
  JSON `{error}` body on a non-2xx response (falling back to a generic
  `Request failed (status)` message if the body isn't JSON, and a distinct
  network-error message if the fetch itself throws). `JobList.tsx` renders
  that message in the create-job modal (existing `alert alert-error` style,
  matching Settings' own save-error display) and keeps the modal open
  instead of doing nothing; the modal now also clears any stale error when
  opened or closed, via `openCreateModal`/`closeCreateModal` helpers
  replacing the scattered direct `setShowCreateModal` calls.
- **Verified:** manually, against a local server — a source path outside
  `SourceDir` (`/etc/passwd`) now shows "access denied: path /etc/passwd is
  outside allowed directories" inline and leaves the modal open; a valid
  path still creates the job and closes the modal normally. `npm run
  build`/`lint` clean.

### 37. Arbitrary File Write via Unvalidated Paths

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `internal/security/security.go`; `internal/api/routes.go`;
  `internal/scanner/scanner.go`; `internal/util/ownership.go`;
  `Dockerfile`; `Dockerfile.nvidia`; `docker-entrypoint.sh` (new);
  `docker-compose.yml`; `.env.example`; `deploy.sh`
- **Details:** Three validation gaps, compounded by the deployment shape —
  internet-facing through Traefik, container running as root.
  - `POST /api/jobs` validated `sourcePath` but only `filepath.Clean`d
    `destinationPath`. Output could be written anywhere the container could
    reach.
  - `POST /api/scanner/queue` → `Scanner.QueueFile` took the raw path with
    no validation at all.
  - `security.ValidatePath` never resolved symlinks, so a symlink inside an
    allowed directory could point anywhere and still read as contained —
    `filepath.Clean` is purely lexical, it never touches the filesystem.
- **Fix:**
  - `ValidatePath` now resolves both the target and each candidate base
    through a new `resolveSymlinks` helper before the containment check.
    `filepath.EvalSymlinks` requires every path component to exist, which a
    destination file being created for the first time never does;
    `resolveSymlinks` walks up to the nearest existing ancestor, resolves
    that, and rejoins the not-yet-existing tail, so a brand-new output path
    is still checked against where its parent directory actually points.
  - `POST /api/jobs`'s `destinationPath` is now run through `ValidatePath`
    against `{SourceDir, DestDir}` — both, since the no-destination-given
    default already writes back beside the source.
  - `Scanner.QueueFile` validates its path against `SourceDir` before doing
    anything else with it, closing the gap for both its direct callers and
    `POST /api/scanner/queue`.
  - **Root, the fourth and largest piece:** added `docker-entrypoint.sh`,
    run as root only long enough to `chown` `/data` (including files
    already there from before this change — critical for the upgrade path)
    and read the GPU render node's group (host-dependent, not knowable at
    build time), then `exec setpriv` into the `vastiva` binary as
    PUID:PGID directly. Chosen over the alternative (stay some fixed
    identity, chown output after the fact) because it also resolves the
    tension with the existing PUID/PGID feature almost for free: a process
    that *runs as* PUID:PGID owns everything it writes from the moment of
    creation, with no `CAP_CHOWN` grant needed, rather than a process that
    writes as one identity and needs elevated privilege to hand files to
    another. `internal/util/ownership.go`'s `Apply()` is kept rather than
    removed — it's a same-owner no-op in the now-common case, but still the
    only path to correct ownership when PUID/PGID aren't set (fixed
    1000:1000 fallback) or don't match a particular write.
    `PORT` moved to 8080 (root is required to bind <1024, and the whole
    point is not being root); `docker-compose.yml`'s external `8091` and
    the Traefik label are updated to match internally, so this is invisible
    from outside the container.
- **Deploy-verified on the homelab host (2026-09-04, post-merge):** the
  container builds, starts, and stays healthy on the new port; the actual
  `vastiva` process (PID 51, child of `docker-init`/tini — `init: true`
  makes tini PID 1, which is root by design and not what to check) runs as
  `ubuntu`/uid 1000, matching `PUID`/`PGID`, not root. Getting here surfaced
  two operational gotchas worth remembering, neither a code defect: the
  deploy workflow only builds and pushes a new image on push to `main`, it
  does **not** auto-deploy it (`docker compose pull && up -d` on the host is
  a separate, manual step — see the `deploy` job's own comment on why); and
  `:latest` resolved to a stale digest even after an explicit pull, cause
  still unconfirmed, worked around by pinning the compose file to the exact
  digest CI produced. VAAPI itself is still unconfirmed — the first real
  conversion attempt hit #54 (a 2160p source correctly rejected by
  `skipHighResolution`, but the UI gave no indication why) before actually
  reaching the encode step.
- **Tests:** `TestValidatePathResolvesSymlinks` (an existing symlink
  escaping the sandbox is rejected; one pointing back inside is accepted;
  a not-yet-existing path behind an escaping symlink is still rejected; the
  allowed base itself being a symlink still resolves correctly) plus the
  full existing `TestValidatePath` table, unchanged and still passing.
  `TestPostJobsValidatesDestinationPath`, `TestPostScannerQueueValidatesPath`
  (first tests of these two routes), `TestQueueFileValidatesPath`. Full
  `-race` suite, `go vet ./...`, `go build ./...`, `gofmt -l .` clean.

### 36. `time.NewTicker(0)` Panic Takes Down the Process

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `internal/scanner/scanner.go`; `internal/api/routes.go`;
  `web/src/components/ScannerConfig.tsx`
- **Details:** `ScannerConfig.Validate()` defaulted every field except
  `ScanIntervalSec`. The settings UI used `parseInt(e.target.value)`, so
  clearing the interval field yielded `NaN`, serialised as JSON `null`, and
  arrived at the backend as `0`. `periodicScan` then called
  `time.NewTicker(0)`, which panics — in a goroutine, so it took the whole
  process down, not just the request. The `min="60"` attribute was a
  browser hint with no server-side counterpart.
- **Fix:** All three layers named in the ticket:
  - `Validate()` now floors `ScanIntervalSec` to `DefaultScanIntervalSec`
    (300s) whenever it's below `MinScanIntervalSec` (60s, exported so
    `routes.go` can reject at the same number rather than duplicating it).
    Applies regardless of scan mode — irrelevant when not periodic/hybrid,
    but guarantees no code path can ever hand `periodicScan` a bad value.
  - `POST /api/scanner/config` rejects (400) a `ScanIntervalSec` that's
    present but under the floor (1-59, or negative) — so a deliberately bad
    value is surfaced to the caller instead of silently repaired. `0` is
    deliberately *not* rejected here and left to `Validate()`'s silent
    default instead: this endpoint parses directly into a value
    `ScannerConfig` struct with no partial-update contract (unlike `POST
    /api/config`'s pointer fields), so an omitted field already arrives as
    `0` the same way an explicit `0` would — treating plain `0` as
    "not provided," consistent with how every other field on this struct
    already behaves, while still catching an unambiguous bad input.
  - `ScannerConfig.tsx`'s interval input now falls back to `300` when
    `parseInt` returns `NaN` (`parseInt(e.target.value) || 300`, matching
    the same defensive pattern already used for `resolutionHeightThreshold`
    in Settings.tsx from #41) — so a cleared field never leaves local state
    at a value that would trip the backend floor in the first place.
- **Tests:** `TestScannerConfigValidateFloorsScanInterval` (0, negative, and
  below-floor all repaired to 300; the floor itself and values above it
  pass through unchanged; asserts the result is never `<= 0`) and
  `TestPostScannerConfigRejectsLowScanInterval` — the first route-level
  test in `internal/api`, wired against a real `jobs.Manager` and
  `scanner.Scanner` rather than mocks. Deliberately posts with the scanner
  disabled: repeatedly starting/stopping a *live* scanner across subtests
  turned out to trip the scanner's own pre-existing, already-tracked
  config-access race (#43) under `-race` — confirmed but not this ticket's
  to fix, so the crash path itself is proven separately and
  deterministically by the `Validate()` test instead. Full `-race` suite,
  `go vet ./...`, `go build ./...`, `gofmt -l .` clean; frontend `npm run
  build`/`lint` clean. Manually verified end-to-end against a running
  server: cleared the Scan Interval field in the browser (confirmed it
  snaps to 300 rather than going blank), saved without the process
  crashing, and confirmed a direct `POST /api/scanner/config` with
  `scanIntervalSec: 10` returns 400 with a clear message.

### 42. `autoUpscale` Defaults On, and Upscaling Ignores SAR

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `internal/media/ffmpeg.go`; `internal/scanner/scanner.go`;
  `internal/scanner/config.go`
- **Details:** Two separate problems behind the *Caged* 856 → 1080 result.
  1. **Default.** `ScannerConfig.AutoUpscale` had no explicit default in
     `Validate()` or `LoadScannerConfig`.
  2. **Distortion.** There was no SAR handling anywhere in `internal/media` —
     `getUpscaleFilter` emitted a bare `scale_vaapi=W:H` / `scale=W:H`, so an
     anamorphic source was stretched to square pixels.
- **Fix:**
  - **Distortion (the real bug):** Added `MediaInfo.SARNum`/`SARDen`
    (parsed from ffprobe's `sample_aspect_ratio`) and
    `upscaleTargetDimensions`, a pure function that computes an upscale
    target from the source's *display* aspect ratio — stored width × SAR ÷
    height — rather than its stored pixel dimensions. `getUpscaleFilter`
    now uses it before scaling and appends `setsar=1` after: `setsar` alone
    only re-tags the output as square-pixel, it doesn't undo a stretch that
    already happened during scaling, which is why both pieces were needed.
    This also fixes the same distortion for any non-16:9 square-pixel
    source (4:3, cinemascope, …), not only anamorphic ones — the bug was
    never really "no SAR support," it was "no aspect-ratio handling at
    all," and SAR was just the specific case that got named. No padding:
    a source that isn't 16:9 comes out at a smaller, correctly-proportioned
    resolution rather than letterboxed, which keeps the fix to plain
    `scale=W:H` on every backend (CPU, `scale_cuda`, `scale_vaapi`) instead
    of needing a hardware-aware pad filter too.
  - **Default:** Investigated first rather than assumed — every code path
    that builds a `ScannerConfig` (the `LoadScannerConfig` defaults, a
    freshly-unmarshaled `scanner_config.json`, `BodyParser` on `POST
    /api/scanner/config`) already leaves an unset `AutoUpscale` at Go's
    zero value, `false`; no path was found that silently turns it on. Added
    an explicit `AutoUpscale: false` in `LoadScannerConfig`'s literal and a
    comment in `Validate()` recording that finding, so the field reads as a
    deliberate decision rather than an oversight — but did not invent
    defaulting logic for a bug that isn't there.
- **Tests:** `TestParseRatio`, `TestUpscaleTargetDimensions` (anamorphic
  16:9 DVD → exact 1080p; anamorphic 4:3 DVD → fits within the box instead
  of being stretched to fill it; square-pixel cinemascope → same fix
  applies; unknown dimensions → falls back to the target box; odd
  computed dimensions → rounded to even for 4:2:0 chroma subsampling) and
  `TestUpscaleFilterPreservesAnamorphicAspectRatio` (end-to-end through
  `buildFFmpegArgs` for CPU/NVIDIA/Intel-AMD, asserting the anamorphic 4:3
  case does *not* produce `1920:1080`). Existing upscale tests
  (`TestVAAPIUpscaleReplacesHwupload` and friends) re-run unchanged and
  still pass. Full `-race` suite, `go vet ./...`, `go build ./...`, `gofmt
  -l .` clean. No `ffmpeg`/`ffprobe` binary in this sandbox, so — same
  caveat as #41 — this is verified at the argument-construction level, not
  against a real encode.

### 41. `skipHighResolution` Does Not Apply to Manual Jobs

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `internal/config/config.go`; `internal/scanner/scanner.go`;
  `internal/scanner/config.go`; `internal/api/routes.go`;
  `web/src/types.ts`; `web/src/components/ScannerConfig.tsx`;
  `web/src/components/Settings.tsx`
- **Details:** `SkipHighResolution`/`ResolutionHeightThreshold` lived only on
  `ScannerConfig` and were checked only in `createJobForFile` and
  `Discover`. `POST /api/jobs` did no resolution check at all — how *28
  Years Later* and *A Bad Moms Christmas* got processed despite the filter
  being on.
- **Fix:** Moved both fields to the system `Config`, per the decision that
  this is library policy, not scanning policy.
  - `POST /api/jobs` now probes and rejects an optimize job at or above the
    configured threshold with a 400, reusing `Manager.GetVideoResolution`
    (already extended for #39) rather than a new probe path. A probe
    failure fails open — same as the scanner's own filter — so a transient
    ffprobe error never blocks a manual job.
  - Existing installs: `LoadScannerConfig` now checks the raw
    `scanner_config.json` bytes for the legacy keys (the struct no longer
    has the fields, so a plain `Unmarshal` would silently drop them),
    migrates them into the system config, `cfg.Save()`s it, and — only on
    success — rewrites `scanner_config.json` without the legacy keys. That
    last step is what makes the migration one-time: without it, a restart
    would re-read the untouched legacy file and clobber any value someone
    had since changed through the new Settings control. If `cfg.Save()`
    fails, the legacy file is left alone so the setting isn't lost and
    migration retries on the next start.
  - Frontend: the field moved from `ScannerConfig`/`ScannerConfig.tsx` to
    `SystemConfig`/`Settings.tsx` (Encoding Settings card, next to CRF).
    `ScannerConfig.tsx` keeps a short note pointing at the new location
    instead of removing the control silently.
- **Tests:** `TestLoadScannerConfigMigratesResolutionFilter` (legacy keys
  present → migrated into system config, persisted, and stripped from the
  scanner file so a second load doesn't re-migrate and clobber a
  since-changed Settings value) and
  `TestLoadScannerConfigNoMigrationWhenNoLegacyKeys` (no legacy keys → no
  write at all). Full `internal/config`, `internal/scanner`,
  `internal/jobs`, `internal/api` suites re-run with `-race -count=1`: all
  pass. `go vet ./...`, `go build ./...`, `gofmt -l .` clean. Frontend:
  `npm run build`/`npm run lint` clean. Manually verified end-to-end
  against a running server: toggled the new Settings control and the
  threshold field, confirmed both persisted via `GET /api/config`, and
  confirmed the Scanner page no longer has a duplicate control. Could not
  verify the actual `POST /api/jobs` *rejection* path against a real
  high-resolution file — no `ffmpeg`/`ffprobe` binary in this sandbox — but
  did confirm the fail-open path: with the filter on and a threshold of
  2160, a job request against a source ffprobe can't read still returns
  201, not a false rejection.

### 40. AI CRF Suggestion Cannot Be Switched Off

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `internal/config/config.go`; `internal/media/validate.go`;
  `internal/jobs/manager.go`; `internal/api/routes.go`;
  `web/src/types.ts`; `web/src/components/Settings.tsx`
- **Details:** Adaptive CRF was gated only on `m.config.IsPremium && m.ai !=
  nil`, with no opt-out. It selected CRF 20 — near-transparent — on a REMUX,
  which guarantees inflation.
- **Fix:**
  - Added `Config.OverrideAICRF` (default `false`, env `OVERRIDE_AI_CRF`),
    plumbed through `GET`/`POST /api/config` — unlike #38/#39's floor
    fields, this one's own fix explicitly called for full API/UI exposure
    now rather than deferring to #51, so it got it.
  - The Settings page already had an "Adaptive Encoding" dropdown with the
    right two options, but it was dead: no `onChange` handler, and its
    `value` was derived from `isPremium` rather than any real config field,
    so selecting an option never did anything for anyone, premium or not.
    Wired it to `overrideAICRF` instead of adding a second control.
  - `runOptimizationFromPath` now skips the AI suggestion step entirely
    when `OverrideAICRF` is set, logging why, before any AI call is made.
  - Also implemented the "consider" in the original fix note: added
    `media.ShouldRefuseCRFSuggestion(codec, suggested, default)` — refuses
    an AI suggestion more indulgent (lower CRF) than the configured default
    on a source already in HEVC/AV1, the exact shape of the reported REMUX
    failure, while still trusting a suggestion to go more aggressive. Kept
    as a pure, unit-tested function alongside #39's `IsAlreadyEfficient`,
    sharing its codec-identity check (`isHEVCOrAV1`).
- **Tests:** `TestShouldRefuseCRFSuggestion` (media package). Full
  `internal/config`, `internal/media`, `internal/jobs`, `internal/api`
  suites re-run with `-race -count=1`: all pass. `go vet ./...` and `go
  build ./...` clean. Frontend: `npm run build` (tsc + vite) and `npm run
  lint` both clean. Manually verified end-to-end against a running
  `go run ./cmd/server` + the built frontend: toggled the Settings dropdown
  both directions, confirmed `POST /api/config` persisted `overrideAICRF`
  and a reload of `GET /api/config` read it back correctly into the UI.

### 39. Already-Efficient Sources Are Re-Encoded

- **Status:** ✅ Resolved (2026-09-04)
- **File:** `internal/media/ffmpeg.go`; `internal/media/validate.go`;
  `internal/config/config.go`; `internal/config/precedence.go`;
  `internal/jobs/manager.go`; `internal/scanner/scanner.go`
- **Details:** Nothing checked the source codec before queueing or
  processing. HEVC → HEVC is generational loss with no size benefit. This is
  what inflated the *28 Days / Weeks / Years Later* files, which were already
  HEVC.
- **Fix:** Bitrate-density filter, as proposed — codec identity alone wasn't
  enough since a hard codec skip would permanently block re-encoding a
  bloated HEVC at a higher CRF.
  - Added `MediaInfo.FrameRate` (parsed from ffprobe's `avg_frame_rate`,
    falling back to `r_frame_rate`) and `MediaInfo.BitsPerPixel()` — average
    bitrate divided by pixel count and frame rate, the standard density
    measure. Zero on any missing input, which never satisfies the "at or
    below the floor" check, so an unprobeable source is never skipped by
    mistake.
  - Added `media.IsAlreadyEfficient(codec, bitsPerPixel, floor)` — true only
    for HEVC/AV1 sources at or below the floor. An H.264 source at the same
    density still benefits from moving to HEVC, so codec identity gates it
    as well as density.
  - Added `Config.DensityFloor` (default `0.06`, env `DENSITY_FLOOR`),
    default sourced from the new `media.DefaultDensityFloor` constant so the
    number lives in one place. Same settings-page/API exposure deferral as
    `SavingsFloor` (#38) — see #51 and #43.
  - `media.CheckSourceSupported` now also runs this check and, gated on
    codec/density, returns a new `*media.SkipEncodeError` — distinct from
    its existing plain-error case (DV profile 5) so the caller can tell "this
    pipeline can't handle it" (real failure) apart from "handling it
    wouldn't help" (skip). `runOptimizationFromPath` in `manager.go` type-
    switches on it: a skip sets `StatusDetail` and completes the job
    successfully, before any transcode work starts — zero added latency for
    the common case, matching the proposal.
  - Scanner side: `Manager.GetVideoResolution` now also returns codec and
    density from the same probe it already ran — no second ffprobe per file.
    `createJobForFile` and `Discover` both apply the filter alongside the
    existing high-resolution one, so these never occupy a queue slot in the
    first place; the job-path check above is what actually protects manually
    queued and `QueueFile`-created jobs.
- **Not done:** the optional sample-encode/VMAF tier from the proposal —
  correctly scoped out as a later, costlier accuracy improvement, not part
  of this fix.
- **Tests:** `TestParseFrameRate`, `TestBitsPerPixel`, `TestIsAlreadyEfficient`
  (media package, pure functions), `TestCheckSourceSupportedSkipsEfficientSources`
  (confirms an efficient HEVC source is skipped via `*SkipEncodeError`, the
  same density on H.264 is not, and a bloated HEVC source is not). Full
  `internal/media`, `internal/config`, `internal/jobs`, `internal/scanner`
  suites re-run with `-race -count=1`: all pass. `go vet ./...` and `go
  build ./...` clean across the whole module.

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

*#36 and #37 — every Critical item found in this review — closed 2026-09-04.*

---

*#43, #44, and #45 — every High/Concurrency item found in this review — closed
2026-09-04 through 2026-09-08.*

---

*#46 through #50 — every Medium item found in this review — closed
2026-09-08.*

---

*#52 and #53 — every Low item found in this review — closed 2026-09-08.
Every issue from the 2026-09-04 review is now resolved.*

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
| 🔴 Critical | 0 | 7 |
| 🟠 High | 0 | 15 |
| 🟡 Medium | 0 | 13 |
| 🟢 Low | 0 | 20 |
| **Total** | **0** | **55** |

---

## Suggested Order

Every tracked issue from the 2026-09-04 review, plus #54 found in production
testing, is closed as of 2026-09-08: ~~**#35**~~ through ~~**#54**~~. #37's
entrypoint is now deploy-verified on the homelab host; VAAPI itself still
isn't confirmed — see its entry above for where that attempt got
interrupted. Nothing left to order.

---

*Maintained manually. Every entry above was verified against the source on the
date given — claims here should not be trusted further than the last verification
date in the header.*
