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
