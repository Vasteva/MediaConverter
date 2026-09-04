# --- Frontend Build Stage ---
FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# --- Go Build Stage ---
FROM golang:1.22-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Copy built frontend from frontend-builder
COPY --from=frontend-builder /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o vastiva ./cmd/server

# --- Runtime Stage ---
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies and MakeMKV
# util-linux provides setpriv, which docker-entrypoint.sh uses to drop root
# before running vastiva (#37) — installed explicitly rather than assumed
# present, even though it's part of Ubuntu's base system.
RUN apt-get update && apt-get install -y \
    software-properties-common \
    && add-apt-repository ppa:heyarje/makemkv-beta \
    && apt-get update \
    && echo "makemkv-bin makemkv-bin/accepted-eula boolean true" | debconf-set-selections \
    && apt-get install -y \
    ffmpeg \
    libva-drm2 libva2 vainfo \
    intel-gpu-tools \
    mesa-va-drivers \
    intel-media-va-driver-non-free \
    i965-va-driver-shaders \
    ca-certificates \
    curl \
    util-linux \
    makemkv-bin makemkv-oss \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /data /storage /output

WORKDIR /app

# Copy single binary from go-builder
COPY --from=go-builder /app/vastiva /app/vastiva
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Set environment
# PORT is unprivileged (>1024): the entrypoint drops root before vastiva
# binds it, and only root/CAP_NET_BIND_SERVICE can bind <1024. Map the host
# port you want in docker-compose.yml's "ports:" instead of changing this.
ENV PORT=8080
ENV SOURCE_DIR=/storage
ENV DEST_DIR=/output
ENV SCANNER_PROCESSED_FILE=/data/processed.json

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/api/health || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/app/vastiva"]
