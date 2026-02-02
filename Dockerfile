# --- Frontend Build Stage ---
FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm install
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

# Install FFmpeg and hardware drivers
RUN apt-get update && apt-get install -y \
    ffmpeg \
    libva-drm2 libva2 vainfo \
    intel-gpu-tools \
    mesa-va-drivers \
    intel-media-va-driver-non-free \
    i965-va-driver-shaders \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy single binary from go-builder
COPY --from=go-builder /app/vastiva /app/vastiva

# Set environment
ENV PORT=80
ENV SOURCE_DIR=/storage
ENV DEST_DIR=/output
ENV SCANNER_PROCESSED_FILE=/data/processed.json

EXPOSE 80

CMD ["/app/vastiva"]
