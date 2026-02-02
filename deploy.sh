#!/bin/bash
set -e

# Vastiva Media Converter - Deployment Script
# This script handles initial setup and updates on the production server

INSTALL_DIR="/opt/vastiva"
COMPOSE_FILE="$INSTALL_DIR/docker-compose.yml"
ENV_FILE="$INSTALL_DIR/.env"

echo "=== Vastiva Media Converter Deployment ==="

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
    echo "Please run as root or with sudo"
    exit 1
fi

# Create installation directory
mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

# Check if .env exists, if not create from example
if [ ! -f "$ENV_FILE" ]; then
    echo "Creating .env file from template..."
    cat > "$ENV_FILE" << 'EOF'
# Vastiva Media Converter Configuration
PORT=80
SOURCE_DIR=/storage
DEST_DIR=/output
GPU_VENDOR=cpu
AI_PROVIDER=none
AI_API_KEY=
AI_ENDPOINT=
AI_MODEL=
ADMIN_PASSWORD=changeme
LICENSE_KEY=
SCANNER_ENABLED=false
SCANNER_MODE=manual
SCANNER_PROCESSED_FILE=/data/processed.json
MEDIA_ROOT=/mnt/media
IMAGE_NAME=ghcr.io/vasteva/mediaconverter
EOF
    echo "⚠️  Please edit $ENV_FILE with your configuration"
    echo "Then run this script again to complete deployment"
    exit 0
fi

# Source environment variables
source "$ENV_FILE"

# Ensure required directories exist
mkdir -p "$SOURCE_DIR"
mkdir -p "$DEST_DIR"

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "Docker not found. Installing Docker..."
    curl -fsSL https://get.docker.com -o get-docker.sh
    sh get-docker.sh
    rm get-docker.sh
fi

# Check if Docker Compose is installed
if ! command -v docker-compose &> /dev/null; then
    echo "Docker Compose not found. Installing..."
    curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
    chmod +x /usr/local/bin/docker-compose
fi

# Login to GitHub Container Registry if credentials are provided
if [ -n "$GITHUB_ACTOR" ] && [ -n "$GITHUB_TOKEN" ]; then
    echo "Logging into GitHub Container Registry..."
    echo "$GITHUB_TOKEN" | docker login -u "$GITHUB_ACTOR" --password-stdin ghcr.io
fi

# Pull latest image
echo "Pulling latest Vastiva image..."
docker pull "$IMAGE_NAME:latest" || echo "Warning: Could not pull from registry, will use local build"

# Stop existing container
if [ "$(docker ps -q -f name=vastiva)" ]; then
    echo "Stopping existing Vastiva container..."
    docker-compose down
fi

# Start new container
echo "Starting Vastiva Media Converter..."
docker-compose up -d

# Show status
echo ""
echo "=== Deployment Complete ==="
docker-compose ps
echo ""
echo "Vastiva is now running!"
echo "Access the web interface at: http://$(hostname -I | awk '{print $1}'):8091"
echo ""
echo "To view logs: docker-compose logs -f"
echo "To stop: docker-compose down"
echo "To restart: docker-compose restart"
