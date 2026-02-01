.PHONY: build run test clean docker

# Build the binary
build:
	go build -ldflags="-s -w" -o vastiva ./cmd/server

# Run locally
run:
	go run ./cmd/server

# Run tests
test:
	go test -v ./...

# Clean build artifacts
clean:
	rm -f vastiva

# Build Docker image
docker:
	docker build -t vastiva:latest .

# Run with Docker Compose
up:
	docker compose up -d --build

# Stop containers
down:
	docker compose down

# View logs
logs:
	docker compose logs -f

# Create a local test environment
test-setup:
	@echo "Setting up test environment..."
	@mkdir -p /tmp/vastiva-test/{input,output}
	@if [ ! -f .env.test ]; then \
		printf "PORT=8080\nSOURCE_DIR=/tmp/vastiva-test/input\nDEST_DIR=/tmp/vastiva-test/output\nGPU_VENDOR=cpu\nQUALITY_PRESET=medium\nCRF=23\nMAX_CONCURRENT_JOBS=2\nAI_PROVIDER=none\nSCANNER_ENABLED=false\nSCANNER_MODE=manual\n" > .env.test; \
		echo "Created .env.test"; \
	fi
	@ffmpeg -version > /dev/null 2>&1 || echo "Warning: ffmpeg not found"
	@makemkvcon --version > /dev/null 2>&1 || echo "Warning: makemkvcon not found"

# Run tests with the test environment
test-full: test-setup
	@cp .env.test .env
	go test -v ./...
