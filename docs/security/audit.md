# Security Audit: Vastiva Media Converter

## Audit Date: 2026-01-09
**Auditor**: Antigravity (AI Engineering Lead)

---

## 1. Findings Summary

| ID | Title | Severity | Status |
|----|-------|----------|--------|
| VAST-001 | Arbitrary File Access / Path Traversal | **CRITICAL** | ✅ Fixed |
| VAST-002 | Sensitive Information Disclosure (API Keys) | **HIGH** | ✅ Mitigated |
| VAST-003 | Command Argument Injection (FFmpeg) | **LOW** | ✅ Mitigated |
| VAST-004 | AI Prompt Injection | **LOW** | ⚠️ Documented |
| VAST-005 | Missing API Authentication | **MEDIUM** | ✅ Fixed |

---

## 2. Detailed Findings & Remediations

### VAST-001: Arbitrary File Access / Path Traversal
- **Problem**: The `/api/jobs` and `/api/scanner/config` endpoints allowed users to provide arbitrary file system paths for source and destination files. A malicious user could read `/etc/passwd` by setting it as a transcode source, or overwrite system binaries by setting them as a transcode target.
- **Remediation**: Implemented a **Path Sandboxing** utility (`internal/security/ValidatePath`). All user-provided paths are now strictly validated against the `SOURCE_DIR` and `DEST_DIR` environment variables. Attempts to access files outside these roots are rejected with `403 Forbidden`.

### VAST-002: Sensitive Information Disclosure
- **Problem**: The `GET /api/config` endpoint returned raw `AI_API_KEY` and `LICENSE_KEY` values to the frontend. This exposed credentials to anyone with access to the dashboard or API.
- **Remediation**: 
  - Implemented `security.MaskKey` to obsfuscate sensitive keys (e.g., `sk-a....5tQ`).
  - Updated `POST /api/config` to ignore masked patterns, preventing accidental overwrites of real keys with masked versions during configuration updates.

### VAST-003: Command Argument Injection
- **Problem**: User-controlled strings (like Quality Presets) were passed as arguments to FFmpeg.
- **Remediation**: The system uses Go's `exec.Command` which avoids shell execution and treats each value as a distinct argument. This prevents traditional shell injection. 

### VAST-004: AI Prompt Injection
- **Problem**: Natural Language Search queries are directly injected into LLM prompts. A user could potentially "jailbreak" the search assistant to perform unintended tasks.
- **Status**: Documented. For the current scope (internal media tool), the risk is minimal.
- **Future Recommendation**: Implement query sanitization and fixed output formatting (JSON schemas).

### VAST-005: Missing API Authentication
- **Problem**: The REST API did not require authentication. Anyone on the network could create jobs or change settings.
- **Remediation**: Implemented token-based authentication (`internal/api/auth.go`). All `/api/*` routes are protected by `AuthMiddleware`, which validates a session token against a server-side `SessionStore` (`internal/api/sessions.go`). Login is rate-limited to 5 attempts per minute per IP. The `/api/setup/*` and SSE endpoints are exempt while setup is incomplete — "incomplete" now also requires `AdminPassword` to be unset, not just a missing `.initialized` marker file.
- **Update (2026-09-08, #50)**: The token itself was originally `sha256(adminPassword + today's date)` — deterministic, derivable by anyone who knew the password without logging in, and a leaked token was itself an offline brute-force oracle for the password. It has been replaced with a random 256-bit token the server issues and tracks itself: sessions last 24h, the short-lived SSE token (passed in a URL query string, since `EventSource` cannot set an `Authorization` header) lasts 2 minutes, and `POST /api/logout` revokes a token immediately — a capability the old scheme had no way to offer, since nothing recorded which tokens were live. Separately, the rate limiter's per-IP tracking only works as described behind a reverse proxy if `TRUSTED_PROXY_CIDRS` is configured (see `.env.example`); without it, every client behind the proxy previously shared one bucket.

---

## 3. Post-Audit Security Posture
With the implementation of **Path Sandboxing** and **Credential Masking**, the application is now significantly more resilient against common web-to-system attacks. The risk of host system compromise via the media converter has been reduced from **Critical** to **Low**.
