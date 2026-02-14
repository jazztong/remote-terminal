# Remote Terminal

A cross-platform terminal bridge that gives you remote shell access via **Telegram** or a **browser-based WebUI**. Supports interactive programs like Claude Code, Python REPL, vim, and more — all through a single Go binary.

## Features

- **Telegram + WebUI** — two interfaces, same core. Use whichever fits your workflow.
- **Interactive programs** — full PTY support means Claude Code, Python REPL, node, vim, etc. all work.
- **Markdown rendering** — Claude's markdown output renders as rich HTML in Telegram (bold, code blocks, headers, links).
- **Secure** — approval code + user whitelist for Telegram. Only authorized users can execute commands.
- **Single binary** — no runtime dependencies. Build once, run anywhere.
- **Cross-platform** — Linux, macOS (Intel + Apple Silicon), Windows.

## Quick Start

### Prerequisites

- Go 1.20+ (to build from source)
- A Telegram bot token (from [@BotFather](https://t.me/botfather))

### Build

```bash
go build -o remote-terminal .
```

Or cross-compile for all platforms:

```bash
./build.sh
```

### Run — Telegram Mode

```bash
./remote-terminal
```

On first run, you'll be prompted to set up your bot:

```
Remote Terminal v2.0

Run: /setup <bot-token>
```

1. Paste your bot token: `/setup <YOUR_BOT_TOKEN>`
2. The terminal displays an approval code
3. Open your bot in Telegram and send that code
4. Done — you're connected. Send commands from Telegram.

### Run — WebUI Mode

```bash
./remote-terminal --web 8080
```

Open `http://localhost:8080` in your browser. On first access you'll be prompted to create a password. After that, login is required to access the terminal. Full terminal emulation via xterm.js.

## Usage

### Simple Commands

Send any shell command from Telegram:

```
ls -la
git status
docker ps
df -h
```

Plain command output is sent as-is — no unnecessary formatting.

### Interactive Sessions

Interactive programs are auto-detected and given a persistent PTY session:

```
claude          # Claude Code session
python3         # Python REPL
node            # Node.js REPL
vim file.txt    # Vim editor
```

While in a session, all messages are routed to the running program. Use `/exit` to end the session.

### Telegram Formatting

When running Claude Code, markdown responses are rendered as rich HTML in Telegram:

| Markdown | Renders as |
|----------|-----------|
| `**bold**` | **bold** |
| `` `code` `` | `code` |
| ` ```go ... ``` ` | Syntax-highlighted code block |
| `# Header` | **Header** |
| `[link](url)` | Clickable link |
| `- item` | Bullet list |

Long responses use Telegram's expandable blockquote. Plain command output (ls, pwd, etc.) is sent without HTML wrapping.

## Architecture

```
┌──────────────┐     ┌────────────────┐     ┌─────────────┐
│  Telegram     │────▶│                │────▶│             │
│  Bot API      │◀────│  Terminal      │◀────│  PTY        │
└──────────────┘     │  Manager       │     │  (shell)    │
                     │                │     │             │
┌──────────────┐     │  OutputSink    │     └─────────────┘
│  WebUI        │────▶│  interface     │
│  (WebSocket)  │◀────│                │
└──────────────┘     └────────────────┘
```

Both Telegram and WebUI implement the `OutputSink` interface, sharing 95% of the terminal management code:

```go
type OutputSink interface {
    SendOutput(output string)
    SendStatus(status string)
}
```

### File Structure

```
main.go           Entry point, config, ANSI cleaning
telegram.go       Telegram bot, session management, markdown output
terminal.go       PTY management, command execution, output streaming
webui.go          WebSocket server + embedded HTML/JS terminal
standalone.go     CLI testing mode
markdown.go       Markdown-to-Telegram-HTML converter
screenreader.go   VTE-based terminal screen reader
```

## Configuration

Config is stored at `~/.telegram-terminal/config.json` (created automatically during setup):

```json
{
  "bot_token": "<YOUR_BOT_TOKEN>",
  "allowed_users": [123456789],
  "webui_password_hash": "$2a$10$..."
}
```

- **bot_token** — from [@BotFather](https://t.me/botfather)
- **allowed_users** — Telegram user IDs authorized to send commands. Get yours from [@userinfobot](https://t.me/userinfobot).
- **webui_password_hash** — bcrypt hash of WebUI password (set automatically on first WebUI access)

File permissions are set to `0600` (owner read/write only).

## Security

1. **Approval code** — random 5-digit code required on first Telegram connection
2. **User whitelist** — only approved Telegram user IDs can execute commands
3. **WebUI authentication** — bcrypt password hashing, server-side sessions with 24h expiry, HttpOnly/SameSite cookies
4. **Config permissions** — `0600` on config file containing the bot token
5. **URL sanitization** — markdown links only allow `http://`, `https://`, and `tg://` protocols
6. **Origin validation** — WebSocket upgrades only accepted from same-origin requests

> **Warning:** This tool provides full shell access to your machine. Only authorize trusted users.

## Testing

```bash
# Run all tests
go test -v

# With race detection
go test -race -v

# Coverage report
go test -cover

# Benchmark markdown converter
go test -bench=BenchmarkFormatMarkdown -benchmem
```

85+ tests covering terminal management, markdown conversion, WebUI authentication, and end-to-end flows.

## Building for All Platforms

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o remote-terminal-linux-amd64

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -o remote-terminal-darwin-amd64

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -o remote-terminal-darwin-arm64

# Windows
GOOS=windows GOARCH=amd64 go build -o remote-terminal-windows-amd64.exe
```

Or use the build script: `./build.sh`

## Credits

- [go-telegram-bot-api](https://github.com/go-telegram-bot-api/telegram-bot-api) — Telegram Bot API
- [creack/pty](https://github.com/creack/pty) — cross-platform PTY
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket for WebUI
- [charmbracelet/x/vt](https://github.com/charmbracelet/x) — virtual terminal emulation

## License

MIT
