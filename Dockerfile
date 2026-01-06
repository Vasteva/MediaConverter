# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o vastiva ./cmd/server

# ---

# Runtime stage
FROM ubuntu:22.04
ENV DEBIAN_FRONTEND=noninteractive

# Install FFmpeg, MakeMKV dependencies, and VA-API drivers
RUN apt-get update && apt-get install -y \
    ffmpeg \
    libva-drm2 libva2 vainfo \
    mesa-va-drivers \
    intel-media-va-driver-non-free \
    i965-va-driver-shaders \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# TODO: Add MakeMKV installation (see old Dockerfile for reference)

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/vastiva /app/vastiva

# Copy frontend static files (if built separately)
COPY web/dist /app/web/dist

# Environment
ENV PORT=80
ENV SOURCE_DIR=/storage
ENV DEST_DIR=/output

EXPOSE 80

CMD ["/app/vastiva"]
