# npm Distribution Design

**Date:** 2026-02-14
**Version:** v0.1.0 (initial public release)
**Status:** Approved

---

## Goal

Enable `npm i -g remote-term` to install the Remote Terminal binary on any platform. Automate the build-release-publish pipeline with GitHub Actions.

## Decisions

- **Package name:** `remote-term` (available on npmjs.com)
- **Starting version:** 0.1.0
- **Distribution pattern:** Single npm package with postinstall binary download (Approach 2 — same pattern as Prisma, Playwright)
- **CI/CD:** GitHub Actions, triggered by git tag push
- **Versioning:** Semantic versioning via git tags, injected at build time via Go ldflags

## Architecture

```
User runs: npm i -g remote-term
  → npm installs package from registry
  → postinstall hook runs install.js
  → install.js detects platform (linux/darwin/win32) + arch (x64/arm64)
  → Downloads correct binary from GitHub Releases
  → Places binary in npm/bin/, makes executable
  → npm links "remote-term" command globally

User runs: remote-term --web 8080
  → npm/bin/remote-term stub script executes the downloaded Go binary
```

### Platform Mapping

| `process.platform` | `process.arch` | Binary name |
|---------------------|----------------|-------------|
| linux | x64 | remote-term-linux-amd64 |
| darwin | x64 | remote-term-darwin-amd64 |
| darwin | arm64 | remote-term-darwin-arm64 |
| win32 | x64 | remote-term-windows-amd64.exe |

## Versioning Strategy

Version is injected at build time — no hardcoded strings in source:

```go
var version = "dev"
// Built with: go build -ldflags="-s -w -X main.version=0.1.0"
```

Release flow:
1. Developer pushes git tag: `git tag v0.1.0 && git push origin v0.1.0`
2. GitHub Actions triggers on `v*` tag
3. Pipeline: test → build (4 platforms) → GitHub Release → npm publish
4. No manual steps after tagging

## Files to Create/Modify

### New files

| File | Purpose |
|------|---------|
| `npm/package.json` | npm package metadata, bin entry, postinstall script |
| `npm/install.js` | Platform detection + binary download from GitHub Releases |
| `npm/bin/remote-term` | Shell stub that proxies to downloaded binary |
| `npm/bin/remote-term.cmd` | Windows cmd stub |
| `.github/workflows/release.yml` | Build + GitHub Release + npm publish |

### Modified files

| File | Change |
|------|--------|
| `main.go` | Replace hardcoded `"Remote Terminal v2.0"` with ldflags `version` variable |
| `build.sh` | Accept version arg, pass `-ldflags="-s -w -X main.version=$VERSION"` |

## CI/CD Pipeline (GitHub Actions)

**Trigger:** Push tag matching `v*`

```
jobs:
  test:
    - go test -race -v

  build:
    needs: test
    matrix: [linux-amd64, darwin-amd64, darwin-arm64, windows-amd64]
    - GOOS/GOARCH cross-compile
    - Upload binary as artifact

  release:
    needs: build
    - Create GitHub Release
    - Attach all 4 binaries

  publish-npm:
    needs: release
    - Update npm/package.json version from tag
    - npm publish
```

**Required secret:** `NPM_TOKEN` (stored in GitHub repo settings)

## npm Package Details

```json
{
  "name": "remote-term",
  "version": "0.1.0",
  "description": "Remote terminal access via Telegram or browser WebUI",
  "bin": { "remote-term": "bin/remote-term" },
  "scripts": { "postinstall": "node install.js" },
  "os": ["linux", "darwin", "win32"],
  "cpu": ["x64", "arm64"]
}
```

## install.js Behavior

1. Detect `process.platform` + `process.arch`
2. Map to binary filename
3. Construct download URL: `https://github.com/jazztong/jimmy-workspace/releases/download/v${version}/${binaryName}`
4. Download with Node.js https module (no external dependencies)
5. Write to `bin/remote-term` (or `bin/remote-term.exe` on Windows)
6. `chmod +x` on unix
7. On failure: print clear error message with manual install instructions

## Error Handling

- Unsupported platform → clear error message listing supported platforms
- Network failure → retry once, then print manual download URL
- Permission error → suggest `sudo npm i -g remote-term`

## Testing

- Unit: `go test -race -v` (existing 85+ tests)
- Build: verify all 4 platform binaries compile
- npm: local `npm pack` + `npm install` test before first publish
- CI: pipeline must pass tests before release
