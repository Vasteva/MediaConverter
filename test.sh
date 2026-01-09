#!/bin/bash
# Quick test script for Vastiva Media Converter

set -e

echo "==================================="
echo "Vastiva Media Converter - Quick Test"
echo "==================================="
echo ""

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Check if .env exists
if [ ! -f .env ]; then
    echo -e "${YELLOW}Creating .env from .env.example...${NC}"
    cp .env.example .env
    echo -e "${GREEN}✓ Created .env${NC}"
else
    echo -e "${GREEN}✓ .env file exists${NC}"
fi

# Build the binary
echo ""
echo -e "${YELLOW}Building binary...${NC}"
if go build -o vastiva ./cmd/server; then
    echo -e "${GREEN}✓ Build successful${NC}"
else
    echo -e "${RED}✗ Build failed${NC}"
    exit 1
fi

# Run tests
echo ""
echo -e "${YELLOW}Running tests...${NC}"
if go test ./internal/media -v; then
    echo -e "${GREEN}✓ Tests passed${NC}"
else
    echo -e "${RED}✗ Tests failed${NC}"
    exit 1
fi

# Check for FFmpeg
echo ""
echo -e "${YELLOW}Checking dependencies...${NC}"
if command -v ffmpeg &> /dev/null; then
    FFMPEG_VERSION=$(ffmpeg -version | head -n1)
    echo -e "${GREEN}✓ FFmpeg found: $FFMPEG_VERSION${NC}"
else
    echo -e "${RED}✗ FFmpeg not found (required for optimization jobs)${NC}"
fi

if command -v makemkvcon &> /dev/null; then
    echo -e "${GREEN}✓ MakeMKV found${NC}"
else
    echo -e "${YELLOW}⚠ MakeMKV not found (optional, needed for extraction jobs)${NC}"
fi

# Create test directories
echo ""
echo -e "${YELLOW}Setting up test directories...${NC}"
mkdir -p /tmp/vastiva-test/{input,output}
echo -e "${GREEN}✓ Test directories created${NC}"

# Update .env for testing
echo ""
echo -e "${YELLOW}Configuring for test mode...${NC}"
cat > .env.test << EOF
PORT=8080
SOURCE_DIR=/tmp/vastiva-test/input
DEST_DIR=/tmp/vastiva-test/output
GPU_VENDOR=cpu
QUALITY_PRESET=medium
CRF=23
MAX_CONCURRENT_JOBS=2
AI_PROVIDER=none
SCANNER_ENABLED=false
SCANNER_MODE=manual
EOF
echo -e "${GREEN}✓ Created .env.test${NC}"

echo ""
echo "==================================="
echo -e "${GREEN}Setup Complete!${NC}"
echo "==================================="
echo ""
echo "To start the server:"
echo "  ./vastiva"
echo ""
echo "Or with test configuration:"
echo "  cp .env.test .env && ./vastiva"
echo ""
echo "Test the API:"
echo "  curl http://localhost:8080/api/health"
echo ""
echo "Create a test job:"
echo "  curl -X POST http://localhost:8080/api/jobs \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"type\":\"optimize\",\"sourcePath\":\"/tmp/vastiva-test/input/test.mkv\",\"destinationPath\":\"/tmp/vastiva-test/output/test_optimized.mkv\"}'"
echo ""
echo "See TESTING.md for comprehensive testing guide"
echo ""
