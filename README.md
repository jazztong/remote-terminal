# Remote Terminal

A cross-platform terminal bridge that gives you remote shell access via **Telegram** or a **browser-based WebUI**. Supports interactive programs like Claude Code, Python REPL, vim, and more — all through a single Go binary.

## Features

- **Telegram + WebUI** — two interfaces, same core. Use whichever fits your workflow.
- **Interactive programs** — full PTY support means Claude Code, Python REPL, node, vim, etc. all work.
- **Markdown rendering** — Claude's markdown output renders as rich HTML in Telegram (bold, code blocks, headers, links).
- **Secure** — approval code + user whitelist for Telegram. Password auth for WebUI.
- **Single binary** — no runtime dependencies. Build once, run anywhere.
- **Cross-platform** — Linux, macOS (Intel + Apple Silicon).

## Install

### npm (recommended)

```bash
npm i -g remote-term
```

### From source

Requires Go 1.20+:

```bash
git clone https://github.com/jazztong/remote-terminal.git
cd remote-terminal
go build -o remote-term .
```

### GitHub Releases

Download pre-built binaries from [Releases](https://github.com/jazztong/remote-terminal/releases).

## Setup

### 1. Create a Telegram Bot

1. Open [@BotFather](https://t.me/botfather) in Telegram
2. Send `/newbot` and follow the prompts
3. Copy the bot token (e.g. `1234567890:ABCdefGHI...`)

### 2. Configure Remote Terminal

```bash
remote-term
```

On first run, you'll see:

```
Remote Terminal v0.1.x

Run: /setup <bot-token>
```

Paste your bot token:

```
/setup 1234567890:ABCdefGHI...
```

The terminal will display a 5-digit approval code:

```
Bot connected: @YourBotName

Then send this approval code:
    --> 48291

Waiting for approval...
```

### 3. Approve in Telegram

Open your bot in Telegram and send the approval code (`48291`). Your Telegram user ID is now whitelisted.

```
✅ User approved!
```

The bot is ready. Send any command from Telegram.

### 4. Set a Default Workspace (Optional)

By default, commands run from wherever `remote-term` was started. To always start in a specific directory:

```bash
cd /path/to/your/workspace && remote-term
```

Or create an alias:

```bash
# Add to ~/.bashrc or ~/.zshrc
alias remote-term='cd ~/projects && remote-term'
```

## Usage

### Telegram Commands

| Command | Description |
|---------|-------------|
| `/start` | Show help and available commands |
| `/status` | Show active session info |
| `/exit` or `/stop` | End the current interactive session |
| Any text | Runs as shell command or routes to active session |

### One-Shot Commands

Send any shell command from Telegram — output is returned directly:

```
ls -la
git status
docker ps
df -h
```

### Interactive Sessions

Interactive programs are auto-detected and given a persistent PTY session:

```
claude          # Claude Code session
python3         # Python REPL
node            # Node.js REPL
vim file.txt    # Vim editor
ssh user@host   # SSH session
```

While in a session, all messages are routed to the running program. Send `/exit` to end the session.

### WebUI Mode

```bash
remote-term --web 8080
```

Open `http://localhost:8080` in your browser. On first access you'll be prompted to create a password. After that, login is required. Full terminal emulation via WebSocket.

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

## Adding Users

Only the first user (who sends the approval code) is whitelisted automatically. To add more users:

1. Get the user's Telegram ID — they can find it via [@userinfobot](https://t.me/userinfobot)
2. Edit the config file:

```bash
nano ~/.telegram-terminal/config.json
```

3. Add their ID to `allowed_users`:

```json
{
  "bot_token": "...",
  "allowed_users": [400702758, 123456789]
}
```

4. Restart `remote-term`

## Reset

### Reset Telegram Setup (new bot token or re-approve)

```bash
rm ~/.telegram-terminal/config.json
remote-term
```

This restarts the setup flow — you'll need to enter a new bot token and approve again.

### Reset WebUI Password

Edit the config and remove the password hash:

```bash
nano ~/.telegram-terminal/config.json
```

Delete the `"webui_password_hash"` line, save, and restart. The next WebUI access will prompt you to create a new password.

### Full Reset (remove everything)

```bash
rm -rf ~/.telegram-terminal
remote-term
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

| Field | Description |
|-------|-------------|
| `bot_token` | Telegram bot token from [@BotFather](https://t.me/botfather) |
| `allowed_users` | Telegram user IDs authorized to send commands |
| `webui_password_hash` | bcrypt hash of WebUI password (set automatically on first WebUI access) |

File permissions are set to `0600` (owner read/write only).

## Security

1. **Approval code** — random 5-digit code required on first Telegram connection
2. **User whitelist** — only approved Telegram user IDs can execute commands
3. **WebUI authentication** — bcrypt password hashing, server-side sessions with 24h expiry, HttpOnly/SameSite cookies
4. **Config permissions** — `0600` on config file containing the bot token
5. **URL sanitization** — markdown links only allow `http://`, `https://`, and `tg://` protocols
6. **Origin validation** — WebSocket upgrades only accepted from same-origin requests

> **Warning:** This tool provides full shell access to your machine. Only authorize trusted users.

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

Both Telegram and WebUI implement the `OutputSink` interface, sharing 95% of the terminal management code.

## Testing

```bash
go test -v          # Run all tests
go test -race -v    # With race detection
go test -cover      # Coverage report
```

85+ tests covering terminal management, markdown conversion, WebUI authentication, and end-to-end flows.

## Credits

- [go-telegram-bot-api](https://github.com/go-telegram-bot-api/telegram-bot-api) — Telegram Bot API
- [creack/pty](https://github.com/creack/pty) — cross-platform PTY
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket for WebUI
- [charmbracelet/x/vt](https://github.com/charmbracelet/x) — virtual terminal emulation

## License

MIT
