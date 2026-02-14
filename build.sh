#!/bin/bash
# Build script for cross-platform compilation
set -e

VERSION="${1:-dev}"
LDFLAGS="-s -w -X main.version=${VERSION}"
OUTPUT_PREFIX="remote-term"

echo "Building Remote Terminal v${VERSION} for all platforms..."
echo ""

platforms=(
    "linux/amd64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
)

for platform in "${platforms[@]}"; do
    GOOS="${platform%/*}"
    GOARCH="${platform#*/}"
    output="${OUTPUT_PREFIX}-${GOOS}-${GOARCH}"
    if [ "$GOOS" = "windows" ]; then
        output="${output}.exe"
    fi

    echo "Building ${output}..."
    GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="${LDFLAGS}" -o "${output}" .
    echo "  done ($(du -h "${output}" | cut -f1))"
done

echo ""
echo "All builds complete!"
