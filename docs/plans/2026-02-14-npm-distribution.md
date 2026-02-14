# npm Distribution Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable `npm i -g remote-term` to install the Remote Terminal Go binary on Linux, macOS, and Windows via GitHub Actions CI/CD.

**Architecture:** Single npm package with postinstall script that downloads the correct platform binary from GitHub Releases. Version injected at build time via Go ldflags. CI/CD triggered by git tag push (`v*`).

**Tech Stack:** Go (build), Node.js (npm package), GitHub Actions (CI/CD), npm registry (distribution)

**Repo:** `https://github.com/jazztong/jimmy-workspace.git` (telegram-terminal-go subdirectory)

---

### Task 1: Add version variable to Go source

Replace hardcoded version strings with a build-time variable.

**Files:**
- Modify: `main.go:1-15` (add version var)
- Modify: `main.go:95` (use version var)
- Modify: `main.go:209` (use version var)

**Step 1: Add version variable after imports**

Add this between the imports block (line 15) and the Config struct (line 17):

```go
// Version is set at build time via ldflags
var version = "dev"
```

**Step 2: Replace hardcoded version strings**

Replace both occurrences of:
```go
fmt.Println("Remote Terminal v2.0")
```

With:
```go
fmt.Printf("Remote Terminal v%s\n", version)
```

There are exactly 2 occurrences:
- Line 95 in `setupWithApproval()`
- Line 209 in `startListening()`

**Step 3: Add --version flag handling**

In `main()` (after line 23), add version flag check before other arg checks:

```go
if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
    fmt.Printf("remote-term v%s\n", version)
    return
}
```

**Step 4: Verify it builds and runs**

Run: `go build -o remote-term . && ./remote-term --version`
Expected: `remote-term vdev`

Run: `go build -ldflags="-X main.version=0.1.0" -o remote-term . && ./remote-term --version`
Expected: `remote-term v0.1.0`

**Step 5: Run existing tests**

Run: `go test -v`
Expected: All 85+ tests pass (version var doesn't affect tests)

**Step 6: Commit**

```bash
git add main.go
git commit -m "feat: Add build-time version injection via ldflags

Replace hardcoded 'v2.0' with variable set by:
  go build -ldflags=\"-X main.version=0.1.0\"
Add --version / -v flag."
```

---

### Task 2: Update build.sh with version support and ldflags

**Files:**
- Modify: `build.sh`

**Step 1: Rewrite build.sh**

Replace the entire contents of `build.sh` with:

```bash
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
```

**Step 2: Verify build script works**

Run: `chmod +x build.sh && ./build.sh 0.1.0`
Expected: 4 binaries created, each ~5-6MB (smaller due to `-s -w`)

Run: `./remote-term-linux-amd64 --version`
Expected: `remote-term v0.1.0`

**Step 3: Clean up build artifacts**

Run: `rm -f remote-term-linux-amd64 remote-term-darwin-amd64 remote-term-darwin-arm64 remote-term-windows-amd64.exe`

**Step 4: Commit**

```bash
git add build.sh
git commit -m "feat: Update build.sh with version arg and ldflags

Usage: ./build.sh 0.1.0
Injects version via ldflags, strips debug symbols (-s -w)."
```

---

### Task 3: Create npm package structure

**Files:**
- Create: `npm/package.json`
- Create: `npm/install.js`
- Create: `npm/bin/remote-term` (unix stub)
- Create: `npm/bin/remote-term.cmd` (windows stub)
- Create: `npm/README.md`

**Step 1: Create npm directory structure**

Run: `mkdir -p npm/bin`

**Step 2: Create package.json**

Create `npm/package.json`:

```json
{
  "name": "remote-term",
  "version": "0.1.0",
  "description": "Remote terminal access via Telegram or browser WebUI. Full PTY support for interactive programs like Claude Code, Python REPL, vim.",
  "license": "MIT",
  "repository": {
    "type": "git",
    "url": "https://github.com/jazztong/jimmy-workspace.git",
    "directory": "telegram-terminal-go"
  },
  "homepage": "https://github.com/jazztong/jimmy-workspace/tree/master/telegram-terminal-go#readme",
  "keywords": [
    "terminal",
    "remote",
    "telegram",
    "webui",
    "pty",
    "shell",
    "claude"
  ],
  "bin": {
    "remote-term": "bin/remote-term"
  },
  "scripts": {
    "postinstall": "node install.js"
  },
  "os": [
    "linux",
    "darwin",
    "win32"
  ],
  "cpu": [
    "x64",
    "arm64"
  ],
  "files": [
    "bin/",
    "install.js",
    "README.md"
  ]
}
```

**Step 3: Create install.js**

Create `npm/install.js`:

```js
"use strict";

const https = require("https");
const http = require("http");
const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");

const VERSION = require("./package.json").version;
const REPO = "jazztong/jimmy-workspace";

const PLATFORM_MAP = {
  "linux-x64": "remote-term-linux-amd64",
  "darwin-x64": "remote-term-darwin-amd64",
  "darwin-arm64": "remote-term-darwin-arm64",
  "win32-x64": "remote-term-windows-amd64.exe",
};

function getBinaryName() {
  const key = `${process.platform}-${process.arch}`;
  const name = PLATFORM_MAP[key];
  if (!name) {
    console.error(
      `Unsupported platform: ${key}\n` +
        `Supported: ${Object.keys(PLATFORM_MAP).join(", ")}\n` +
        `Please build from source: https://github.com/${REPO}`
    );
    process.exit(1);
  }
  return name;
}

function getDownloadUrl(binaryName) {
  return `https://github.com/${REPO}/releases/download/v${VERSION}/${binaryName}`;
}

function download(url) {
  return new Promise((resolve, reject) => {
    const get = url.startsWith("https") ? https.get : http.get;
    get(url, (res) => {
      // Follow redirects (GitHub releases redirect to S3)
      if (res.statusCode === 301 || res.statusCode === 302) {
        download(res.headers.location).then(resolve).catch(reject);
        return;
      }

      if (res.statusCode !== 200) {
        reject(new Error(`Download failed: HTTP ${res.statusCode} from ${url}`));
        return;
      }

      const chunks = [];
      res.on("data", (chunk) => chunks.push(chunk));
      res.on("end", () => resolve(Buffer.concat(chunks)));
      res.on("error", reject);
    }).on("error", reject);
  });
}

async function main() {
  const binaryName = getBinaryName();
  const url = getDownloadUrl(binaryName);
  const binDir = path.join(__dirname, "bin");
  const isWindows = process.platform === "win32";
  const outputName = isWindows ? "remote-term.exe" : "remote-term";
  const outputPath = path.join(binDir, outputName);

  console.log(`Downloading remote-term v${VERSION} for ${process.platform}-${process.arch}...`);

  try {
    const data = await download(url);
    fs.mkdirSync(binDir, { recursive: true });
    fs.writeFileSync(outputPath, data);

    if (!isWindows) {
      fs.chmodSync(outputPath, 0o755);
    }

    console.log(`Installed remote-term v${VERSION} to ${outputPath}`);
  } catch (err) {
    console.error(`Failed to download remote-term: ${err.message}`);
    console.error(`\nManual install:`);
    console.error(`  Download: ${url}`);
    console.error(`  Place in: ${binDir}`);
    process.exit(1);
  }
}

main();
```

**Step 4: Create Unix bin stub**

Create `npm/bin/remote-term`:

```bash
#!/bin/sh
basedir=$(dirname "$(echo "$0" | sed -e 's,\\,/,g')")

case `uname` in
    *CYGWIN*|*MINGW*|*MSYS*)
        if [ -x "$basedir/remote-term.exe" ]; then
            exec "$basedir/remote-term.exe" "$@"
        fi
        ;;
esac

if [ -x "$basedir/remote-term" ] && [ ! "$0" -ef "$basedir/remote-term" ]; then
    exec "$basedir/remote-term" "$@"
fi

# Fallback: maybe the binary wasn't downloaded yet
echo "Error: remote-term binary not found. Try reinstalling: npm i -g remote-term" >&2
exit 1
```

Wait — actually the postinstall downloads the binary to the same `bin/` directory as the stub, overwriting it. The stub approach doesn't work cleanly because npm expects `bin/remote-term` to be the executable. The postinstall downloads the actual Go binary _as_ `bin/remote-term`, replacing the stub. This is cleaner:

**Revised Step 4: Create a placeholder bin stub (overwritten by install.js)**

Create `npm/bin/remote-term` with just a shebang to satisfy `npm pack`:

```bash
#!/bin/sh
echo "Error: remote-term binary not installed. Run: npm rebuild remote-term" >&2
exit 1
```

Make it executable: `chmod +x npm/bin/remote-term`

**Step 5: Create Windows cmd stub**

Create `npm/bin/remote-term.cmd`:

```cmd
@echo off
"%~dp0\remote-term.exe" %*
```

**Step 6: Create npm README.md**

Create `npm/README.md`:

```markdown
# remote-term

Remote terminal access via **Telegram** or **browser WebUI**. Full PTY support for interactive programs like Claude Code, Python REPL, vim, and more.

## Install

```bash
npm i -g remote-term
```

## Usage

```bash
# Telegram bot mode
remote-term

# WebUI mode (browser-based terminal)
remote-term --web 8080

# Check version
remote-term --version
```

## Supported Platforms

- Linux x64
- macOS x64 (Intel)
- macOS arm64 (Apple Silicon)
- Windows x64

## Documentation

See the full docs at [GitHub](https://github.com/jazztong/jimmy-workspace/tree/master/telegram-terminal-go).
```

**Step 7: Verify npm package structure**

Run: `cd npm && npm pack --dry-run && cd ..`
Expected: Lists files that would be included in the package (package.json, install.js, bin/, README.md)

**Step 8: Commit**

```bash
git add npm/
git commit -m "feat: Add npm package for binary distribution

Package name: remote-term
postinstall downloads correct platform binary from GitHub Releases.
Supports: linux-x64, darwin-x64, darwin-arm64, win32-x64."
```

---

### Task 4: Create GitHub Actions release workflow

**Files:**
- Create: `.github/workflows/release.yml`

**Step 1: Create workflow directory**

Run: `mkdir -p .github/workflows`

**Step 2: Create release.yml**

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  test:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: telegram-terminal-go
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - run: go test -race -v

  build:
    needs: test
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: telegram-terminal-go
    strategy:
      matrix:
        include:
          - goos: linux
            goarch: amd64
            suffix: linux-amd64
          - goos: darwin
            goarch: amd64
            suffix: darwin-amd64
          - goos: darwin
            goarch: arm64
            suffix: darwin-arm64
          - goos: windows
            goarch: amd64
            suffix: windows-amd64
            ext: .exe
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - name: Build
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
        run: |
          VERSION="${GITHUB_REF_NAME#v}"
          go build -ldflags="-s -w -X main.version=${VERSION}" \
            -o "remote-term-${{ matrix.suffix }}${{ matrix.ext }}" .
      - uses: actions/upload-artifact@v4
        with:
          name: remote-term-${{ matrix.suffix }}
          path: telegram-terminal-go/remote-term-${{ matrix.suffix }}${{ matrix.ext }}

  release:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4
        with:
          path: artifacts
          merge-multiple: true
      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          files: artifacts/*
          generate_release_notes: true

  publish-npm:
    needs: release
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: telegram-terminal-go/npm
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          registry-url: 'https://registry.npmjs.org'
      - name: Set version from tag
        run: |
          VERSION="${GITHUB_REF_NAME#v}"
          npm version "$VERSION" --no-git-tag-version --allow-same-version
      - name: Publish to npm
        run: npm publish --access public
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
```

**Step 3: Verify YAML is valid**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))" 2>&1 || echo "Install: pip install pyyaml"`

If pyyaml isn't available, visually inspect the indentation — YAML is whitespace-sensitive.

**Step 4: Commit**

```bash
git add .github/
git commit -m "ci: Add GitHub Actions release workflow

Triggered by v* tag push. Pipeline:
1. Run tests (go test -race)
2. Cross-compile 4 platform binaries
3. Create GitHub Release with binaries
4. Publish npm package (remote-term)

Requires NPM_TOKEN secret in GitHub repo settings."
```

---

### Task 5: Set up npm account and GitHub secret

This task is **manual** — the user must do these steps themselves.

**Step 1: Create npmjs.com account (if needed)**

Go to https://www.npmjs.com/signup and create an account.

**Step 2: Generate npm access token**

1. Go to https://www.npmjs.com → Profile → Access Tokens
2. Click "Generate New Token" → "Classic Token"
3. Select type: **Automation** (for CI/CD, no 2FA prompt)
4. Copy the token (starts with `npm_`)

**Step 3: Add NPM_TOKEN to GitHub repo secrets**

1. Go to https://github.com/jazztong/jimmy-workspace/settings/secrets/actions
2. Click "New repository secret"
3. Name: `NPM_TOKEN`
4. Value: paste the token from step 2
5. Click "Add secret"

**Step 4: Verify**

The secret should appear in the secrets list. It will be available to the release workflow as `${{ secrets.NPM_TOKEN }}`.

---

### Task 6: Test the full release pipeline

**Step 1: Push all changes**

Run: `git push origin master`

**Step 2: Create and push the first version tag**

Run: `git tag v0.1.0 && git push origin v0.1.0`

**Step 3: Monitor GitHub Actions**

Go to: https://github.com/jazztong/jimmy-workspace/actions

Watch the "Release" workflow. It should:
1. Run tests (green)
2. Build 4 binaries (green)
3. Create GitHub Release (green)
4. Publish to npm (green — only if NPM_TOKEN secret is set)

**Step 4: Verify GitHub Release**

Go to: https://github.com/jazztong/jimmy-workspace/releases

Should see "v0.1.0" with 4 binary attachments:
- `remote-term-linux-amd64`
- `remote-term-darwin-amd64`
- `remote-term-darwin-arm64`
- `remote-term-windows-amd64.exe`

**Step 5: Verify npm install works**

Run: `npm i -g remote-term && remote-term --version`
Expected: `remote-term v0.1.0`

**Step 6: Clean up**

Run: `npm uninstall -g remote-term` (if you want to remove the test install)

---

### Task 7: Update .gitignore for npm artifacts

**Files:**
- Modify: `.gitignore`

**Step 1: Add npm ignores**

Add to the project `.gitignore`:

```
# npm build artifacts (downloaded binaries)
npm/bin/remote-term
npm/bin/remote-term.exe
!npm/bin/remote-term.cmd
node_modules/
```

Note: `npm/bin/remote-term` the stub script IS tracked. But if someone runs `npm install` locally, the real binary would replace it — that binary should be ignored. Use `git checkout npm/bin/remote-term` to restore the stub if needed.

Actually, the stub needs to be tracked but the downloaded binary shouldn't. Since they share the same path, this is tricky. Simpler approach: just track everything in npm/ and add only `node_modules/` to .gitignore.

Add to `.gitignore`:

```
node_modules/
```

**Step 2: Commit**

```bash
git add .gitignore
git commit -m "chore: Add node_modules to gitignore"
```

---

## Summary

| Task | Description | Time |
|------|-------------|------|
| 1 | Version variable in Go source | 3 min |
| 2 | Update build.sh | 2 min |
| 3 | Create npm package structure | 5 min |
| 4 | GitHub Actions workflow | 3 min |
| 5 | npm account + GitHub secret (manual) | 5 min |
| 6 | Test release pipeline | 5 min |
| 7 | Gitignore cleanup | 1 min |

**Total:** ~25 minutes

## Future Version Release

After this is set up, releasing a new version is just:

```bash
git tag v0.2.0
git push origin v0.2.0
# Done. GitHub Actions handles the rest.
```
