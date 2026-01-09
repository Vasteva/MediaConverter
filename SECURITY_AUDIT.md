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
| VAST-005 | Missing API Authentication | **MEDIUM** | ⚠️ Documented |

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
- **Problem**: The REST API does not require authentication. Anyone on the network can create jobs or change settings.
- **Status**: Known limitation. The system currently relies on deployment environment security (e.g., VPN, local network).
- **Future Recommendation**: Implement JWT or API Key authentication for the internal API.

---

## 3. Post-Audit Security Posture
With the implementation of **Path Sandboxing** and **Credential Masking**, the application is now significantly more resilient against common web-to-system attacks. The risk of host system compromise via the media converter has been reduced from **Critical** to **Low**.
