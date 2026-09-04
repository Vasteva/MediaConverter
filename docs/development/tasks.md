# Vastiva Media Converter — Task List

**Generated:** 2026-02-01 | **Last Updated:** 2026-09-04

## 📋 Executive Summary

Eleven open issues, from a full source review on 2026-09-04 combined with findings
from production testing on 2026-09-03. #35, #36–#42, and #54 closed the same day
they were filed — every Critical item and everything found in production testing,
including one (#54) found only by actually deploying and using today's fixes.
#37's Docker/entrypoint half is now deploy-verified on the homelab host: the
process genuinely drops to PUID/PGID, not just in theory. VAAPI itself is still
unconfirmed — see #37's entry for where that attempt got interrupted.

The media pipeline itself — `internal/media` (ffmpeg, progress, validate) and
`internal/ai/meta` — is in good shape and holds up under review. The remaining open
work is concentrated in one place:

- **Shared mutable state is unsynchronised.** `config.Config` has no mutex at all
  while the API mutates it under running workers (#43) — and this is no longer
  theoretical: a #36 regression test tripped `s.config`'s race under `-race` on
  the first try.

The previous revision of this file claimed all known bugs were resolved. That was
written 2026-02-27 and was not re-verified against the code before this review.

---

## ✅ Closed Today

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
    **Confirmed, not just theorized:** a #36 regression test that started
    and stopped a live scanner across repeated `POST /api/scanner/config`
    calls tripped this under `-race` immediately — `periodicScan` at
    `scanner.go:886` racing a concurrent `UpdateConfig` write. Removed from
    that test rather than fixed there, since it's this ticket's bug, not
    #36's — see #36's own entry above for the reproduction shape.
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
    bitrate-density filter (#39). The AI CRF toggle (#40) and
    `skipHighResolution`/threshold (#41) already landed here as part of their
    own fixes rather than waiting on this ticket.
- **Fix:** For each field decide runtime-settable, restart-required, or
  deliberately env-only, and show that state in the UI rather than silently
  ignoring input.
- **Sequencing:** Land #43 first — every new mutable field widens that race.

### 52. Documentation Contradicts the Code

- **Status:** 🟢 Open
- **File:** `docs/security/audit.md`; `README.md`; `.env.example`
- **Details:**
  - `audit.md` VAST-001 claims path traversal is fixed for source and
    destination — as of #37 (2026-09-04) that claim is now actually true,
    but the document should still be checked against the rest of what it
    asserts rather than trusted on the strength of one now-correct line.
    VAST-005 describes the token as "HMAC-SHA256" (it is plain SHA-256) and
    the login limit as "10 attempts per minute" (the code uses 5). An
    overstating security document is worse than none.
  - `README.md` advertises Whisper speech-to-text subtitles; the implementation
    is an OpenSubtitles *download*. Its structure tree shows
    `internal/ai/subtitles/`, which is `internal/subtitles/`. Its `PORT`
    default (`80`) disagreed with `config.go`'s (`8080`) even before #37;
    now the Docker image's default has moved to match `config.go`, so
    README is further out of date, not closer.
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
| 🔴 Critical | 0 | 7 |
| 🟠 High | 3 | 12 |
| 🟡 Medium | 5 | 8 |
| 🟢 Low | 3 | 17 |
| **Total** | **11** | **44** |

---

## Suggested Order

1. ~~**#35**~~, ~~**#36**~~, ~~**#37**~~, ~~**#38**~~, ~~**#39**~~, ~~**#40**~~,
   ~~**#41**~~, ~~**#42**~~, ~~**#54**~~ — done. #37's entrypoint is now
   deploy-verified; VAAPI itself still isn't confirmed — see its entry above.
2. **#43** (with its race test — now with a confirmed reproduction, see #43), then **#44**, **#45**.
3. **#46, #47, #48, #49**.
4. **#50**, then **#51** (which depends on #43), **#52**, **#53**.

---

*Maintained manually. Every entry above was verified against the source on the
date given — claims here should not be trusted further than the last verification
date in the header.*
