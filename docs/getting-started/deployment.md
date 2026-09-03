# Vastiva Media Converter - Deployment Guide


## Deployment settings (GitHub repository variables)

The deploy workflow reads three repository variables (Settings → Secrets and
variables → Actions → Variables). All are optional.

| Variable | Default | Purpose |
|---|---|---|
| `DEPLOY_PATH` | `/opt/vastiva` | Directory holding the compose stack on the self-hosted runner. Set this when Vastiva runs inside an existing stack rather than a standalone install. |
| `DEPLOY_SERVICE` | `vastiva` | Compose service name. Every deploy command is scoped to it, so other services in the same file are never stopped. |
| `AUTO_DEPLOY` | unset (off) | Set to `true` to deploy automatically on every merge to `main`. |

### Why automatic deployment is off by default

Deploying restarts the container, which kills any transcode in flight. A 2160p
REMUX can be hours of work, and on restart the job returns to `pending` and
begins again from zero — so a merge landing mid-transcode silently costs an
evening of encoding.

Push to `main` builds and publishes the image either way. To ship it, run the
**Deploy to Production** workflow manually when the queue is idle:

```bash
gh workflow run "Deploy to Production"
```

Set `AUTO_DEPLOY=true` only if unattended restarts are acceptable for your queue.

### Running alongside other services

If the compose file also runs Jellyfin or anything else, set `DEPLOY_PATH` to
that directory and leave `DEPLOY_SERVICE` as `vastiva`. The workflow uses
`docker compose pull vastiva` and `docker compose up -d vastiva`, never
`docker compose down`, so nothing else in the stack is touched.


## Overview
This guide covers deploying Vastiva Media Converter to your production server using GitHub Actions and Docker.

## Prerequisites
- GitHub repository with GitHub Packages enabled
- Production server with Docker installed
- SSH access to production server
- Domain name configured (optional, for HTTPS via Traefik)

## Initial Setup

### 1. Configure GitHub Secrets
In your GitHub repository, go to **Settings > Secrets and variables > Actions** and add:

| Secret | Description | Example |
|--------|-------------|---------|
| `DEPLOY_HOST` | Production server hostname | `server.vasteva.net` |
| `DEPLOY_USER` | SSH user for deployment | `root` |
| `SSH_PRIVATE_KEY` | SSH private key for server access | `-----BEGIN RSA PRIVATE KEY-----...` |

### 2. Prepare Production Server

Copy the deployment script to your server:
```bash
scp deploy.sh root@server.vasteva.net:/tmp/
ssh root@server.vasteva.net
chmod +x /tmp/deploy.sh
sudo /tmp/deploy.sh
```

This will:
- Create `/opt/vastiva` directory
- Generate `.env` configuration file
- Install Docker and Docker Compose (if needed)
- Set up the application structure

### 3. Configure Environment Variables

Edit `/opt/vastiva/.env` on your production server:
```bash
nano /opt/vastiva/.env
```

Key settings to configure:
```env
# AI Provider (openai, claude, gemini, ollama, none)
AI_PROVIDER=openai
AI_API_KEY=sk-your-api-key-here
AI_MODEL=gpt-4

# License Key for Premium Features
LICENSE_KEY=your-license-key

# Media Paths
MEDIA_ROOT=/mnt/media

# Scanner Settings
SCANNER_ENABLED=true
SCANNER_MODE=watch
```

### 4. Copy docker-compose.yml

Copy the production docker-compose.yml to your server:
```bash
scp docker-compose.yml root@server.vasteva.net:/opt/vastiva/
```

## CI/CD Pipeline
### Automatic Build & Deploy
Every push to `main` branch triggers the GitHub Action workflow:

1. **Build Stage**: Creates Docker image
2. **Push to Registry**: Uploads to GitHub Container Registry (ghcr.io)
3. **Deploy**:
   - SSH into your production server
   - Pull the latest image
   - Restart the application
   - Clean up old images

You can monitor the progress in the **Actions** tab of your repository.

## Manual Deployment

If you prefer manual deployment:

```bash
# On your production server
cd /opt/vastiva

# Pull latest image
docker pull ghcr.io/vasteva/mediaconverter:latest

# Restart services
docker-compose down
docker-compose up -d

# View logs
docker-compose logs -f
```

## Disc Extraction (MakeMKV)

MakeMKV is included in both Docker images. To use disc extraction jobs you must also pass your optical drive device into the container.

### 1. Find your optical drive device

```bash
ls -la /dev/sr* /dev/cdrom 2>/dev/null
```

Typical result: `/dev/sr0` (first drive), `/dev/sr1` (second drive).

### 2. Enable the device in docker-compose.yml

Uncomment the `devices` entry in `docker-compose.yml`:

```yaml
devices:
  - /dev/dri:/dev/dri
  - /dev/sr0:/dev/sr0   # adjust path if needed
```

For the NVIDIA compose override, uncomment the matching block in `docker-compose.nvidia.yml`.

### 3. Restart the container

```bash
docker-compose up -d
```

### Troubleshooting disc extraction

**"makemkvcon not found"** — the binary is missing from the image. Rebuild the image:
```bash
docker-compose build --no-cache
```

**"permission denied" on /dev/sr0** — add the container user to the `cdrom` group on the host, or run with `privileged: true` (not recommended in production):
```bash
# Check device group
ls -la /dev/sr0
# → crw-rw---- 1 root cdrom ...
```

**Disc scan returns no titles** — ensure the disc is inserted and the drive is not mounted by the host OS:
```bash
umount /dev/sr0
```

---

## Traefik Integration (Optional)

If using Traefik for automatic HTTPS:

1. Ensure Traefik network exists:
```bash
docker network create traefik
```

2. The docker-compose.yml already includes Traefik labels:
```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.vastiva.rule=Host(`media.vasteva.net`)"
  - "traefik.http.routers.vastiva.entrypoints=websecure"
  - "traefik.http.routers.vastiva.tls.certresolver=letsencrypt"
```

3. Access via: `https://media.vasteva.net`

## Monitoring

### View Logs
```bash
docker-compose logs -f vastiva
```

### Check Status
```bash
docker-compose ps
```

### Restart Service
```bash
docker-compose restart vastiva
```

### Update Configuration
```bash
nano /opt/vastiva/.env
docker-compose up -d  # Recreates container with new env vars
```

## Backup & Restore

### Backup Processed Files Database
```bash
docker cp vastiva:/data/processed.json /backup/processed-$(date +%Y%m%d).json
```

### Restore Database
```bash
docker cp /backup/processed-20260109.json vastiva:/data/processed.json
docker-compose restart
```

## Troubleshooting

### Container won't start
```bash
# Check logs
docker-compose logs vastiva

# Verify environment variables
docker-compose config
```

### GPU not detected
```bash
# Verify GPU device is accessible
ls -la /dev/dri

# Check GPU vendor setting
grep GPU_VENDOR /opt/vastiva/.env
```

### Permission issues
```bash
# Fix ownership of data volume
docker-compose down
docker volume rm vastiva_vastiva-data
docker-compose up -d
```

## Rollback

To rollback to a previous version:

```bash
# Find the SHA of the previous working build
docker pull ghcr.io/vasteva/mediaconverter:<previous-sha>

# Update docker-compose.yml to use specific tag
# Then restart
docker-compose up -d
```

## Security Notes

- Always use strong `ADMIN_PASSWORD`
- Keep `AI_API_KEY` and `LICENSE_KEY` secure
- Use SSH key authentication for deployments
- Regularly update base images for security patches
- Enable firewall rules to restrict access to port 8091

## Support

For issues or questions:
- Check logs: `docker-compose logs -f`
- Review configuration: `docker-compose config`
- Verify network: `docker network inspect traefik`
