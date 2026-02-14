#!/bin/bash
# Build script for cross-platform compilation

set -e

echo "Building Remote Terminal for all platforms..."
echo ""

# Clean old builds
rm -f remote-terminal-*

# Linux
echo "🐧 Building for Linux (amd64)..."
GOOS=linux GOARCH=amd64 go build -o remote-terminal-linux-amd64
echo "✓ remote-terminal-linux-amd64"

# macOS Intel
echo "🍎 Building for macOS Intel (amd64)..."
GOOS=darwin GOARCH=amd64 go build -o remote-terminal-darwin-amd64
echo "✓ remote-terminal-darwin-amd64"

# macOS Apple Silicon
echo "🍎 Building for macOS Apple Silicon (arm64)..."
GOOS=darwin GOARCH=arm64 go build -o remote-terminal-darwin-arm64
echo "✓ remote-terminal-darwin-arm64"

# Windows
echo "🪟 Building for Windows (amd64)..."
GOOS=windows GOARCH=amd64 go build -o remote-terminal-windows-amd64.exe
echo "✓ remote-terminal-windows-amd64.exe"

echo ""
echo "✅ All builds complete!"
echo ""
echo "Files created:"
ls -lh telegram-terminal-* | awk '{print "  " $9 " (" $5 ")"}'
echo ""
echo "Distribute the appropriate file for each platform:"
echo "  • Linux   → remote-terminal-linux-amd64"
echo "  • Mac Intel → remote-terminal-darwin-amd64"
echo "  • Mac M1/M2 → remote-terminal-darwin-arm64"
echo "  • Windows → remote-terminal-windows-amd64.exe"
