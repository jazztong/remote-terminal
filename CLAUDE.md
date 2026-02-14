# Claude Code Instructions
## Telegram Terminal Bridge Project

**Last Updated:** 2026-02-15
**Project Version:** 0.1.x

---

## Project Overview

This is a **cross-platform terminal bridge** that enables remote shell access via Telegram and local WebUI. The core innovation is **pseudo-terminal (PTY) support** allowing interactive programs (Claude AI, Python REPL, vim) to work seamlessly over messaging interfaces.

**Tech Stack:** Go 1.20+, Telegram Bot API, WebSockets, creack/pty

**Code Size:** ~960 lines production, ~1200 lines tests

**Current Status:** Production (Telegram + WebUI modes)

**Distribution:** npm (`npm i -g remote-term`), GitHub Releases, source

**Repository:** https://github.com/jazztong/remote-terminal

---

## Architecture Quick Reference

### Core Design Pattern

**Interface-Driven Architecture** via `OutputSink`:

```go
type OutputSink interface {
    SendOutput(output string)
    SendStatus(status string)
}
```

This enables 95% code reuse between Telegram and WebUI modes.

### File Structure

```
├── main.go              - Entry point, config, ANSI cleaning, version
├── telegram.go          - Telegram bot, session management
├── webui.go             - WebSocket server + embedded UI + auth
├── terminal.go          - PTY management, streaming
├── markdown.go          - Markdown-to-Telegram-HTML converter
├── screenreader.go      - VTE-based terminal screen reader
├── standalone.go        - CLI testing mode
├── build.sh             - Cross-platform build script (ldflags)
├── npm/                 - npm package (install.js, bin stubs)
├── .github/workflows/   - CI/CD (release.yml)
└── *_test.go            - Comprehensive test suite (85+ tests)
```

### Key Components

1. **Terminal Manager** (`terminal.go`) - Creates PTY, spawns shells, streams output
2. **Telegram Bridge** (`telegram.go`) - Routes commands, manages sessions
3. **WebUI Server** (`webui.go`) - WebSocket + embedded HTML/CSS/JS
4. **Config Manager** (`main.go`) - Setup flow, file I/O, utilities

---

## Working with This Codebase

### Before Making Changes

**Always:**
1. ✅ Read the relevant test file first (`*_test.go`)
2. ✅ Check `ARCHITECTURE.md` for design patterns
3. ✅ Review `SECURITY.md` for security implications
4. ✅ Run existing tests: `go test -v`

**Never:**
- ❌ Add features without updating tests
- ❌ Change `OutputSink` interface without reviewing all implementations
- ❌ Modify PTY logic without testing on Linux/macOS/Windows
- ❌ Introduce new dependencies without justification

### Testing Strategy

**Test Coverage Requirements:**
- Utilities: 100% (already achieved)
- Core logic: 80%+ (streaming, session management)
- Integration: E2E tests for critical flows

**Run tests:**
```bash
# All tests
go test -v

# Specific package
go test -v -run TestCleanANSI

# With race detection (IMPORTANT)
go test -race -v

# Coverage report
go test -cover
go test -coverprofile=coverage.out
go tool cover -html=coverage.out
```

**Test Philosophy:**
- Use `MockSink` for testing without Telegram
- E2E tests should use real PTY (not mocked)
- Timing-dependent tests should use event-based waiting, not `time.Sleep`

---

## Critical Code Areas

### 🔴 CRITICAL: Session Map Race Condition

**File:** `telegram.go`
**Issue:** Session map accessed without mutex protection

```go
// CURRENT (UNSAFE):
type TelegramBridge struct {
    sessions map[int64]*Session  // ← NO MUTEX!
}

func (tb *TelegramBridge) handleCommand(...) {
    session := tb.sessions[chatID]  // RACE!
}
```

**Fix Required:**
```go
type TelegramBridge struct {
    mu       sync.RWMutex
    sessions map[int64]*Session
}

func (tb *TelegramBridge) handleCommand(...) {
    tb.mu.RLock()
    session, hasSession := tb.sessions[chatID]
    tb.mu.RUnlock()
    // ...
}
```

**When fixing:**
- Add mutex to struct
- Protect ALL map access (read AND write)
- Use `RLock()` for reads, `Lock()` for writes
- Test with `go test -race`

---

### ✅ RESOLVED: WebUI Authentication

**File:** `webui.go`
**Status:** Implemented in v2.1 (2026-02-14)

**Solution:** Password-based authentication with server-side sessions:
- First access → "Create Password" page → bcrypt hash stored in config
- Subsequent access → "Login" page → validate bcrypt → set session cookie
- WebSocket endpoint rejects unauthenticated connections (401)
- Sessions: `crypto/rand` 32-byte tokens, HttpOnly/SameSite=Strict cookies, 24h expiry
- Origin-checking WebSocket upgrader (same-origin only)

**Key routes:** `/` (root), `/setup-password`, `/login`, `/logout`, `/ws`

**Tests:** 12 auth tests covering setup, login, logout, session expiry, WebSocket gate

---

### 🟡 IMPORTANT: Process Cleanup

**File:** `terminal.go:204-238`
**Logic:** Graduated kill sequence (SIGHUP → SIGTERM → SIGKILL)

**Critical Points:**
- Process group kill uses negative PID: `kill(-pid, SIGHUP)`
- Always call `cmd.Wait()` to prevent zombies
- PTY must be closed: `t.ptmx.Close()`
- Done channel must be closed: `close(t.done)`

**When modifying:**
- Test zombie process prevention: `ps aux | grep defunct`
- Verify all children killed: `pstree -p $(pidof telegram-terminal)`
- Test on Linux AND macOS (Windows behavior differs)

---

### 🟡 IMPORTANT: ANSI Cleaning

**File:** `main.go:215-285`
**Purpose:** Strip terminal escape codes for Telegram display

**Performance:** ~50ns per operation (benchmarked)

**Handles:**
- CSI sequences: `\x1b[...m` (colors, cursor)
- OSC sequences: `\x1b]...` (window title)
- Control characters: backspace, tabs

**When modifying:**
- Run benchmarks: `go test -bench=BenchmarkCleanANSI`
- Test with actual program output (Claude, Python)
- Preserve readability (don't over-strip)

---

## Common Tasks

### Adding a New Output Sink (e.g., Discord, Slack)

1. **Create sink struct:**
```go
type DiscordSink struct {
    channelID string
    client    *discord.Client
}

func (ds *DiscordSink) SendOutput(output string) {
    ds.client.SendMessage(ds.channelID, output)
}

func (ds *DiscordSink) SendStatus(status string) {
    ds.client.SendMessage(ds.channelID, "**"+status+"**")
}
```

2. **Test it:**
```go
func TestDiscordSink(t *testing.T) {
    // Mock Discord client
    sink := &DiscordSink{...}
    sink.SendOutput("test")
    // Assert message sent
}
```

3. **Integrate:**
```go
func runDiscordMode(token string) {
    client := discord.New(token)
    sink := &DiscordSink{client: client}
    terminal, _ := NewTerminal()
    streamSessionOutput(session, sink)
}
```

**No changes needed to `terminal.go`!** That's the power of the interface design.

---

### Adding an Interactive Command

**File:** `telegram.go:156-189`

```go
var interactiveCommands = []string{
    // Add your command here:
    "mycli",

    // Existing...
    "claude", "python3", "node", ...
}
```

**Testing:**
```bash
# 1. Start bot
remote-term

# 2. Send command in Telegram
> mycli

# 3. Verify session starts (should see "🟢 Session started")
# 4. Send input, verify routed to session
# 5. Send /exit, verify session ends
```

---

### Modifying the WebUI

**File:** `webui.go`
**Lines:** 343-604 (embedded HTML/CSS/JS)

**Current Approach:** String literal (❌ hard to maintain)

**Better Approach (TODO):**
```go
//go:embed templates/*.html
var templateFS embed.FS

func (s *WebUIServer) serveHTML(w http.ResponseWriter, r *http.Request) {
    tmpl, _ := template.ParseFS(templateFS, "templates/index.html")
    tmpl.Execute(w, nil)
}
```

**For now (v2.0):**
- Edit the HTML string directly
- Test in browser: http://localhost:8080
- Check browser console for errors

---

### Adding Configuration Options

**File:** `main.go`

```go
type Config struct {
    BotToken          string  `json:"bot_token"`
    AllowedUsers      []int64 `json:"allowed_users"`
    WebUIPasswordHash string  `json:"webui_password_hash,omitempty"`

    // Add new field:
    SessionTimeout int `json:"session_timeout_minutes,omitempty"`
}
```

**Default Values:**
```go
func loadConfig() (*Config, error) {
    config := &Config{
        SessionTimeout: 30, // default
    }
    // Load from file...
    return config, nil
}
```

**Usage:**
```go
timeout := time.Duration(config.SessionTimeout) * time.Minute
```

---

## Code Style & Conventions

### Naming

- **Exported:** PascalCase (`TelegramBridge`, `OutputSink`)
- **Unexported:** camelCase (`sessionTimeout`, `handleCommand`)
- **Constants:** PascalCase (`DefaultPort`, `MaxBufferSize`)
- **Interfaces:** Noun or Adjective (`OutputSink`, `Runnable`)

### Error Handling

**Prefer wrapping:**
```go
// ✅ Good
if err != nil {
    return fmt.Errorf("failed to create terminal: %w", err)
}

// ❌ Bad
if err != nil {
    return err
}
```

**User-facing errors:**
```go
// ✅ Good - Actionable
return fmt.Errorf("config file not found at %s. Run setup first.", configPath)

// ❌ Bad - Vague
return fmt.Errorf("error loading config")
```

### Concurrency

**Always protect shared state:**
```go
type Server struct {
    mu       sync.RWMutex
    sessions map[string]*Session
}

func (s *Server) getSession(id string) *Session {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.sessions[id]
}
```

**Channel cleanup:**
```go
// Close channels to signal goroutines
close(done)

// Wait for goroutine acknowledgment
<-done
```

**Goroutine lifecycle:**
```go
go func() {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Goroutine panic: %v", r)
        }
    }()

    // Work...
}()
```

---

## Debugging Tips

### Enable Verbose Logging

```go
// Add to main.go
var debug = flag.Bool("debug", false, "Enable debug logging")

func main() {
    flag.Parse()
    if *debug {
        log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
    }
}
```

### Debug PTY Issues

```bash
# See raw PTY output
strace -e read,write -p $(pidof remote-term)

# Check terminal settings
stty -a < /dev/pts/X  # X = PTY number
```

### Debug Telegram API

```go
bot.Debug = true  // Logs all API calls
```

### Debug WebSocket

```javascript
// In browser console
ws = new WebSocket('ws://localhost:8080/ws');
ws.onmessage = (e) => console.log('Received:', JSON.parse(e.data));
ws.send(JSON.stringify({type: 'command', content: 'ls'}));
```

### Debug Goroutine Leaks

```bash
# Before
go test -run TestYourTest &
PID=$!
sleep 1
kill -SIGQUIT $PID  # Dumps goroutine stacks
```

---

## Security Guidelines

### Input Validation

**Commands:** No validation (by design - it's a shell bridge)

**Config:** Validate structure
```go
if config.BotToken == "" {
    return fmt.Errorf("bot_token required")
}
if len(config.AllowedUsers) == 0 {
    return fmt.Errorf("at least one allowed_user required")
}
```

### Authentication

**Telegram:**
- ✅ User ID whitelist (immutable)
- ✅ Approval code on first use
- ⚠️ Weak PRNG (TODO: use crypto/rand)

**WebUI:**
- ✅ Password auth + bcrypt hashing + server-side sessions (v2.1)

### Secrets Management

**Bot Token:**
- ✅ Stored in config file (0600 permissions)
- ❌ Plaintext (encryption at rest = v3.0)
- ⚠️ Visible in process args during setup

**Best Practice:**
```bash
# Set permissions
chmod 600 ~/.telegram-terminal/config.json

# Verify
ls -l ~/.telegram-terminal/config.json
# -rw------- 1 user user ...
```

---

## Performance Optimization

### Benchmarking

```bash
# Run benchmarks
go test -bench=. -benchmem

# CPU profiling
go test -cpuprofile=cpu.prof -bench=.
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=.
go tool pprof mem.prof
```

### Known Bottlenecks

1. **ANSI Cleaning** - ~50ns/op (acceptable)
2. **Channel Buffering** - 100 items (tune if needed)
3. **Telegram API Rate Limit** - 30 msg/sec (external)
4. **PTY I/O** - System-limited

### Optimization Guidelines

- ✅ Buffer output before sending (current: 500ms)
- ✅ Use channels for async I/O
- ❌ Don't optimize prematurely
- ❌ Don't sacrifice readability for micro-optimizations

---

## Deployment

### Building

```bash
# Development
go build -o remote-term .

# Production (smaller binary, with version)
go build -ldflags="-s -w -X main.version=0.1.3" -o remote-term .

# Cross-compile all platforms
./build.sh 0.1.3
```

### Running

```bash
# Telegram mode
remote-term

# WebUI mode
remote-term --web 8080

# Check version
remote-term --version
```

### CI/CD

Releases are automated via GitHub Actions (`.github/workflows/release.yml`):

```bash
# Tag a release → triggers build + npm publish
git tag v0.1.4
git push origin v0.1.4
```

Pipeline: test → build (matrix) → GitHub Release → npm publish (trusted publishing/OIDC)

---

## Known Issues & Workarounds

### Issue #1: Race Condition in Session Map

**Symptom:** Occasional crash with concurrent messages
**Workaround:** Don't send rapid messages (< 100ms apart)
**Fix:** Add mutex (see Critical Code Areas)
**Status:** Open

### Issue #2: Windows Process Group Kill

**Symptom:** Child processes not killed on Windows
**Workaround:** Use Linux/macOS for production
**Fix:** Platform-specific code for Windows
**Status:** Open

### Issue #3: Large Output Truncation

**Symptom:** Telegram messages >4000 chars split awkwardly
**Workaround:** Use WebUI for large outputs
**Fix:** Word-aware splitting
**Status:** Open

---

## Contributing Guidelines

### Before Submitting Changes

1. **Run tests:**
   ```bash
   go test -v
   go test -race  # Must pass!
   ```

2. **Run linter:**
   ```bash
   go vet ./...
   staticcheck ./...  # If installed
   ```

3. **Format code:**
   ```bash
   go fmt ./...
   ```

4. **Update documentation:**
   - Add/update tests
   - Update relevant .md files
   - Add comments for exported functions

### Commit Messages

**Format:**
```
<type>: <description>

<optional body>

<optional footer>
```

**Types:**
- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation only
- `test:` Test additions/changes
- `refactor:` Code refactoring
- `perf:` Performance improvement
- `security:` Security fix

**Examples:**
```
fix: Add mutex to protect session map in TelegramBridge

Concurrent message handling was causing race conditions. Added
sync.RWMutex to protect all session map access.

Fixes: CVE-INTERNAL-003
```

```
feat: Add HTTP Basic Auth to WebUI

Implements authentication for WebSocket endpoint. Credentials
stored in config file with bcrypt hashing.

Closes: #42
```

---

## Emergency Procedures

### If Tests Fail

1. **Don't panic** - Tests are there to catch issues
2. **Read the error** - Go test output is descriptive
3. **Isolate the failure:**
   ```bash
   go test -v -run TestFailingTest
   ```
4. **Check for races:**
   ```bash
   go test -race -run TestFailingTest
   ```
5. **Add debug logging:**
   ```go
   t.Logf("Debug: value=%v", value)
   ```

### If Production Crashes

1. **Stop the service:**
   ```bash
   pkill remote-term
   ```

2. **Check logs:**
   ```bash
   tail -100 bot.log
   dmesg | tail  # Kernel logs
   ```

3. **Check for zombies:**
   ```bash
   ps aux | grep defunct
   ```

4. **Report issue** at https://github.com/jazztong/remote-terminal/issues

---

## Quick Reference

### Useful Commands

```bash
# Build
go build -o remote-term .

# Test
go test -v
go test -race
go test -cover

# Run
remote-term                 # Telegram mode
remote-term --web 8080      # WebUI mode
remote-term --version       # Check version

# Release (triggers CI/CD)
git tag v0.1.4 && git push origin v0.1.4
```

### File Permissions

```bash
# Config file
chmod 600 ~/.telegram-terminal/config.json
```

### Git Workflow

```bash
# Create feature branch
git checkout -b feat/add-authentication

# Make changes, test
go test -v

# Commit
git add .
git commit -m "feat: Add WebUI authentication"

# Push
git push origin feat/add-authentication
```

---

## Learning Resources

### Go-Specific

- [Effective Go](https://golang.org/doc/effective_go.html)
- [Go Concurrency Patterns](https://go.dev/blog/pipelines)
- [Go Testing](https://go.dev/doc/tutorial/add-a-test)

### Project-Specific

- [Telegram Bot API](https://core.telegram.org/bots/api)
- [creack/pty Documentation](https://pkg.go.dev/github.com/creack/pty)
- [WebSocket RFC](https://datatracker.ietf.org/doc/html/rfc6455)

### Architecture

- [ARCHITECTURE.md](./ARCHITECTURE.md) - Technical details
- [SECURITY.md](./SECURITY.md) - Security considerations

---

## Contact & Support

**Project Owner:** Jazz Tong
**Repository:** https://github.com/jazztong/remote-terminal
**Issues:** https://github.com/jazztong/remote-terminal/issues
**npm:** https://www.npmjs.com/package/remote-term
**Security:** See SECURITY.md

---

## Changelog

| Version | Date | Changes |
|---------|------|---------|
| 0.1.3 | 2026-02-15 | npm distribution fix (Node.js bin stub) |
| 0.1.0 | 2026-02-14 | npm package, CI/CD, versioning, WebUI auth |

---

**Last Updated:** 2026-02-15

---

## Special Notes for Claude

### When Asked to Add Features

1. **Check PRD.md first** - Feature may already be planned
2. **Review security implications** - Consult SECURITY.md
3. **Consider architecture** - Read ARCHITECTURE.md
4. **Write tests first** - TDD approach preferred
5. **Update documentation** - Keep docs in sync

### When Debugging

1. **Read test failures carefully** - Go tests are descriptive
2. **Check for race conditions** - Run with `-race`
3. **Look at existing patterns** - Follow established code style
4. **Don't break OutputSink contract** - Central to architecture

### When Refactoring

1. **Tests must pass** - Before and after
2. **No behavior changes** - Unless intentional
3. **Document why** - Explain rationale in commit
4. **Incremental changes** - Small, reviewable commits

### Remember

- This is a **security-sensitive** project (shell access)
- **WebUI auth implemented** - bcrypt password + session cookies
- **Race condition exists** - Session map needs mutex
- **Tests are comprehensive** - Use them!
- **Interface design is key** - Don't break OutputSink

---

**Happy coding! 🚀**
