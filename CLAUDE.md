# Claude Code Instructions
## Remote Terminal

**Last Updated:** 2026-02-15
**Project Version:** 0.1.x

---

## Project Overview

Cross-platform terminal bridge — remote shell access via **Telegram** and **browser WebUI**. Full PTY support for interactive programs (Claude Code, Python REPL, vim).

**Tech Stack:** Go 1.20+, Telegram Bot API, WebSockets, creack/pty
**Distribution:** `npm i -g remote-term` / GitHub Releases / source build
**Repository:** https://github.com/jazztong/remote-terminal
**npm:** https://www.npmjs.com/package/remote-term

→ Architecture details: [ARCHITECTURE.md](./ARCHITECTURE.md)
→ Security analysis: [SECURITY.md](./SECURITY.md)

### File Structure

```
├── main.go              - Entry point, config, ANSI cleaning, version
├── telegram.go          - Telegram bot, session management
├── webui.go             - WebSocket server + embedded UI + auth
├── terminal.go          - PTY management, streaming
├── markdown.go          - Markdown-to-Telegram-HTML converter
├── screenreader.go      - VTE-based terminal screen reader
├── standalone.go        - CLI testing mode
├── npm/                 - npm package (install.js, bin stubs)
├── .github/workflows/   - CI/CD (release.yml)
└── *_test.go            - Test suite (85+ tests)
```

---

## Before Making Changes

**Always:**
1. Read the relevant test file first (`*_test.go`)
2. Run existing tests: `go test -v`
3. Check for race conditions: `go test -race -v`

**Never:**
- Add features without updating tests
- Change `OutputSink` interface without reviewing all implementations (→ [ARCHITECTURE.md](./ARCHITECTURE.md#interface-contracts))
- Modify PTY logic without testing on Linux/macOS
- Introduce new dependencies without justification

---

## Testing

```bash
go test -v                        # All tests
go test -v -run TestCleanANSI     # Specific test
go test -race -v                  # Race detection (IMPORTANT)
go test -cover                    # Coverage report
go test -bench=. -benchmem        # Benchmarks
```

**Test Philosophy:**
- Use `MockSink` for testing without Telegram API
- E2E tests use real PTY (not mocked)
- Use event-based waiting, not `time.Sleep`

---

## Known Open Issues

### 🔴 Race Condition in Session Map

**File:** `telegram.go` — `TelegramBridge.sessions` map has NO mutex protection.

→ Full analysis: [ARCHITECTURE.md — Race Condition Analysis](./ARCHITECTURE.md#race-condition-analysis)
→ Security impact: [SECURITY.md — CVE-INTERNAL-003](./SECURITY.md#cve-internal-003-race-condition-in-session-map)

**When fixing:** Add `sync.RWMutex`, protect ALL map access (read AND write), test with `go test -race`.

### 🟡 Weak Approval Code PRNG

**File:** `main.go:97-99` — uses `math/rand` instead of `crypto/rand`.

→ Details: [SECURITY.md — CVE-INTERNAL-002](./SECURITY.md#cve-internal-002-weak-approval-code-generation)

### 🟡 Windows Process Group Kill

Child processes not killed on Windows. Linux/macOS only for now.

### 🟡 Large Output Truncation

Telegram messages >4000 chars split awkwardly. Use WebUI for large outputs.

---

## Common Tasks

### Adding a New Output Sink (e.g., Discord, Slack)

Implement the `OutputSink` interface — no changes needed to `terminal.go`:

```go
type OutputSink interface {
    SendOutput(output string)
    SendStatus(status string)
}
```

→ Full interface contract and existing implementations: [ARCHITECTURE.md — Interface Contracts](./ARCHITECTURE.md#interface-contracts)

### Adding an Interactive Command

**File:** `telegram.go` — add to `interactiveCommands` slice:

```go
var interactiveCommands = []string{
    "mycli",  // ← Add here
    "claude", "python3", "node", ...
}
```

Test: start bot → send command in Telegram → verify session starts → send `/exit`.

### Modifying the WebUI

**File:** `webui.go` — HTML/CSS/JS is embedded as string literal.

- Edit the HTML string directly in `webui.go`
- Auth pages: `setupPasswordHTML`, `loginHTML`
- Terminal page: `htmlContent`
- Test in browser: `http://localhost:8080`

### Adding Configuration Options

**File:** `main.go` — extend the Config struct:

```go
type Config struct {
    BotToken          string  `json:"bot_token"`
    AllowedUsers      []int64 `json:"allowed_users"`
    WebUIPasswordHash string  `json:"webui_password_hash,omitempty"`
    // Add new field with omitempty for backwards compatibility
}
```

Config stored at `~/.telegram-terminal/config.json` (0600 permissions).

---

## Code Style & Conventions

### Naming

- **Exported:** PascalCase (`TelegramBridge`, `OutputSink`)
- **Unexported:** camelCase (`sessionTimeout`, `handleCommand`)
- **Interfaces:** Noun or Adjective (`OutputSink`, `Runnable`)

### Error Handling

```go
// ✅ Wrap with context
return fmt.Errorf("failed to create terminal: %w", err)

// ✅ User-facing: actionable
return fmt.Errorf("config file not found at %s. Run setup first.", configPath)
```

### Concurrency

Always protect shared state with mutex. Use `RLock()` for reads, `Lock()` for writes.

→ Concurrency patterns and goroutine model: [ARCHITECTURE.md — Concurrency Model](./ARCHITECTURE.md#concurrency-model)

---

## Debugging

### PTY Issues
```bash
strace -e read,write -p $(pidof remote-term)
stty -a < /dev/pts/X
```

### Telegram API
```go
bot.Debug = true  // Logs all API calls
```

### WebSocket
```javascript
ws = new WebSocket('ws://localhost:8080/ws');
ws.onmessage = (e) => console.log('Received:', JSON.parse(e.data));
ws.send(JSON.stringify({type: 'command', content: 'ls'}));
```

### Goroutine Leaks
```bash
go test -run TestYourTest &
PID=$!; sleep 1; kill -SIGQUIT $PID  # Dumps goroutine stacks
```

### Test Failures
```bash
go test -v -run TestFailingTest       # Isolate
go test -race -run TestFailingTest    # Check races
```

---

## Building & Deployment

```bash
# Development
go build -o remote-term .

# Production (smaller binary, with version)
go build -ldflags="-s -w -X main.version=0.1.5" -o remote-term .

# Cross-compile all platforms
./build.sh 0.1.5
```

### Running

```bash
remote-term                 # Telegram mode
remote-term --web 8080      # WebUI mode
remote-term --version       # Check version
```

### Releasing

Releases are automated via GitHub Actions (`.github/workflows/release.yml`):

```bash
git tag v0.1.5 && git push origin v0.1.5
```

Pipeline: test → build (linux/darwin matrix) → GitHub Release → npm publish (OIDC trusted publishing)

### npm Package

The `npm/` directory contains the npm distribution:
- `install.js` — postinstall script that downloads platform-specific Go binary
- `bin/remote-term` — Node.js stub that exec's the Go binary
- `bin/remote-term.cmd` — Windows batch stub
- `package.json` — version is overridden by CI at publish time

---

## Contributing

### Before Submitting

```bash
go test -v          # Tests pass
go test -race       # No races
go vet ./...        # Linter
go fmt ./...        # Format
```

### Commit Messages

```
<type>: <description>

Types: feat, fix, docs, test, refactor, perf, security
```

---

## Emergency: Production Crash

```bash
pkill remote-term                    # Stop service
ps aux | grep defunct                # Check zombies
tail -100 bot.log                    # Check logs
```

→ Full incident response: [SECURITY.md — Incident Response](./SECURITY.md#incident-response)

---

## Quick Reference

```bash
# Build & Test
go build -o remote-term .
go test -v && go test -race

# Run
remote-term                     # Telegram mode
remote-term --web 8080          # WebUI mode

# Release
git tag v0.1.5 && git push origin v0.1.5

# Config
~/.telegram-terminal/config.json  # 0600 permissions
```

### Reference Documents

| Document | Contains |
|----------|----------|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Design patterns, data flow, component design, concurrency model, process management, interface contracts, performance benchmarks, technical debt |
| [SECURITY.md](./SECURITY.md) | Threat model, authentication layers, vulnerability list (CVEs), OWASP compliance, incident response, security roadmap |
| [README.md](./README.md) | User-facing setup guide, commands, configuration |

---

## Notes for Claude

- This is a **security-sensitive** project (full shell access)
- **Race condition exists** in `telegram.go` session map — needs mutex
- **Interface design is key** — don't break `OutputSink` contract
- **Tests are comprehensive** — always run them before and after changes
- Read ARCHITECTURE.md before structural changes
- Read SECURITY.md before auth/access-related changes

---

**Last Updated:** 2026-02-15
