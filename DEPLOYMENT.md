# Vastiva Media Converter - Deployment Guide

## Overview
This guide covers deploying Vastiva Media Converter to your production server using GitLab CI/CD and Docker.

## Prerequisites
- GitLab repository with Container Registry enabled
- Production server with Docker installed
- SSH access to production server
- Domain name configured (optional, for HTTPS via Traefik)

## Initial Setup

### 1. Configure GitLab CI/CD Variables
In your GitLab project, go to **Settings > CI/CD > Variables** and add:

| Variable | Description | Example |
|----------|-------------|---------|
| `CI_REGISTRY_USER` | GitLab username | `your-username` |
| `CI_REGISTRY_PASSWORD` | GitLab access token | `glpat-xxxxx` |
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

### Automatic Build
Every push to `main` branch triggers:
1. **Build Stage**: Creates Docker image with embedded frontend
2. **Push to Registry**: Uploads to GitLab Container Registry
3. **Tag**: Creates both SHA-tagged and `latest` versions

### Manual Deployment
After a successful build:
1. Go to **CI/CD > Pipelines** in GitLab
2. Click the play button (▶️) on the `deploy` job
3. Confirm deployment

The pipeline will:
- SSH into your production server
- Pull the latest image from Container Registry
- Stop the old container
- Start the new container
- Clean up old images

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
