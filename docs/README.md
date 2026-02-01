# Vastiva Media Converter - Documentation

Welcome to the Vastiva Media Converter documentation. This documentation provides comprehensive guides for deploying, using, and developing the application.

## 📚 Documentation Index

### Getting Started
- **[Deployment Guide](getting-started/deployment.md)** - Production deployment with GitLab CI/CD and Docker

### Architecture
- **[System Overview](architecture/overview.md)** - High-level architecture, data flows, and technology stack
- **Implementation Details:**
  - [Backend Implementation](architecture/implementation/backend.md) - FFmpeg wrapper and media processing
  - [Frontend Implementation](architecture/implementation/frontend.md) - React UI and design system
  - [Scanner Implementation](architecture/implementation/scanner.md) - File scanner and auto-discovery

### Development
- **[Testing Guide](development/testing.md)** - Test procedures, checklists, and troubleshooting
- **[Task List](development/tasks.md)** - Current development status and remaining tasks

### Security
- **[Security Audit](security/audit.md)** - Security findings and remediation status

### Package Documentation
The following README files live alongside their respective code packages:
- `internal/media/README.md` - Media processing package (FFmpeg/MakeMKV wrappers)
- `internal/scanner/README.md` - File scanner package (auto-discovery system)

---

## Quick Links

| I want to... | Go to |
|--------------|-------|
| Deploy to production | [Deployment Guide](getting-started/deployment.md) |
| Understand the architecture | [System Overview](architecture/overview.md) |
| Run tests | [Testing Guide](development/testing.md) |
| Check security status | [Security Audit](security/audit.md) |
| See what's been done | [Task List](development/tasks.md) |

---

*For the main project README, see [../README.md](../README.md)*
