package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramSink sends output to Telegram
type TelegramSink struct {
	bot    *tgbotapi.BotAPI
	chatID int64
}

func (t *TelegramSink) SendOutput(output string) {
	// Trim whitespace and check if empty
	output = strings.TrimSpace(output)
	if output == "" {
		// Skip empty messages - Telegram rejects them
		return
	}

	// Choose format based on content:
	// - ASCII art → <pre> (monospace, preserves alignment)
	// - Markdown content → HTML formatting in <blockquote>
	// - Plain text → send as-is, no wrapping
	maxLen := 4000

	if needsMonospace(output) {
		// ASCII art: wrap in <pre>
		formatted := "<pre>" + html.EscapeString(output) + "</pre>"
		t.sendHTML(formatted, "pre", maxLen)
	} else if hasMarkdown(output) {
		// Markdown: convert and wrap in blockquote
		formatted := formatMarkdownToTelegramHTML(output)
		if len(output) > 500 {
			formatted = "<blockquote expandable>" + formatted + "</blockquote>"
			t.sendHTML(formatted, "blockquote expandable", maxLen)
		} else {
			formatted = "<blockquote>" + formatted + "</blockquote>"
			t.sendHTML(formatted, "blockquote", maxLen)
		}
	} else {
		// Plain text: send without formatting
		t.sendPlain(output, maxLen)
	}
}

// sendPlain sends a plain text message (no HTML parsing).
// Splits into chunks if the message exceeds maxLen.
func (t *TelegramSink) sendPlain(text string, maxLen int) {
	if len(text) <= maxLen {
		msg := tgbotapi.NewMessage(t.chatID, text)
		_, err := t.bot.Send(msg)
		if err != nil {
			log.Printf("❌ Failed to send message: %v\n", err)
		}
		return
	}
	// Split on newlines
	for i := 0; i < len(text); i += maxLen {
		end := i + maxLen
		if end > len(text) {
			end = len(text)
		}
		chunk := strings.TrimSpace(text[i:end])
		if chunk == "" {
			continue
		}
		msg := tgbotapi.NewMessage(t.chatID, chunk)
		_, err := t.bot.Send(msg)
		if err != nil {
			log.Printf("❌ Failed to send chunk: %v\n", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// sendHTML sends an HTML-formatted message. Splits into chunks wrapped
// in the given tag if the message exceeds maxLen. The openTag parameter
// is the full opening tag (e.g., "blockquote expandable") to preserve
// attributes like expandable across chunks.
func (t *TelegramSink) sendHTML(formatted string, openTag string, maxLen int) {
	if len(formatted) <= maxLen {
		msg := tgbotapi.NewMessage(t.chatID, formatted)
		msg.ParseMode = "HTML"
		_, err := t.bot.Send(msg)
		if err != nil {
			log.Printf("❌ Failed to send message: %v\n", err)
		}
		return
	}

	// Derive the closing tag name (first word of openTag)
	closeTag := strings.Fields(openTag)[0]
	tagOverhead := len("<>") + len(openTag) + len("</>") + len(closeTag) + 100
	rawMaxLen := maxLen - tagOverhead

	var chunks []string
	if closeTag == "pre" {
		// Strip outer <pre>...</pre>, split with entity-safe boundaries
		inner := strings.TrimPrefix(formatted, "<pre>")
		inner = strings.TrimSuffix(inner, "</pre>")
		chunks = splitAtSafeBoundary(inner, rawMaxLen)
	} else {
		// Strip outer blockquote to get inner HTML
		inner := formatted
		inner = strings.TrimPrefix(inner, "<blockquote expandable>")
		inner = strings.TrimPrefix(inner, "<blockquote>")
		inner = strings.TrimSuffix(inner, "</blockquote>")
		chunks = splitFormattedMessage(inner, rawMaxLen)
	}

	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		chunkFormatted := "<" + openTag + ">" + chunk + "</" + closeTag + ">"
		msg := tgbotapi.NewMessage(t.chatID, chunkFormatted)
		msg.ParseMode = "HTML"
		_, err := t.bot.Send(msg)
		if err != nil {
			log.Printf("❌ Failed to send chunk: %v\n", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// needsMonospace returns true if content contains ASCII art or alignment
// that requires monospace <pre> formatting to display correctly.
func needsMonospace(s string) bool {
	for _, r := range s {
		switch r {
		case '▐', '▛', '█', '▜', '▌', '▝', '▘', '░', '▒', '▓':
			return true // Block drawing / ASCII art
		}
	}
	return false
}

// Session represents a persistent terminal session
type Session struct {
	Terminal   *Terminal
	Sink       OutputSink
	Active     bool
	Command    string
	StartedAt  time.Time
	done       chan struct{} // Signal to stop streaming goroutine
	doneClosed bool         // Tracks whether done channel has been closed
	closeMu    sync.Mutex   // Protects doneClosed and close(done)
}

// safeCloseDone closes the done channel exactly once, preventing double-close panics.
func (s *Session) safeCloseDone() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if !s.doneClosed {
		close(s.done)
		s.doneClosed = true
	}
}

// TelegramBridge manages Telegram bot and terminal
type TelegramBridge struct {
	bot      *tgbotapi.BotAPI
	config   *Config
	mu       sync.RWMutex
	sessions map[int64]*Session // chatID -> active session
}

func NewTelegramBridge(bot *tgbotapi.BotAPI, config *Config) (*TelegramBridge, error) {
	return &TelegramBridge{
		bot:      bot,
		config:   config,
		sessions: make(map[int64]*Session),
	}, nil
}

func (tb *TelegramBridge) Listen() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := tb.bot.GetUpdatesChan(u)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		<-sigChan
		log.Println("\n🛑 Shutting down gracefully...")
		tb.CleanupAllSessions()
		os.Exit(0)
	}()

	for update := range updates {
		if update.Message == nil {
			continue
		}

		userID := update.Message.From.ID
		username := update.Message.From.UserName
		text := update.Message.Text
		chatID := update.Message.Chat.ID

		// Check whitelist
		allowed := false
		for _, allowedID := range tb.config.AllowedUsers {
			if userID == allowedID {
				allowed = true
				break
			}
		}

		if !allowed {
			log.Printf("⚠️  Unauthorized: @%s (ID: %d)\n", username, userID)
			msg := tgbotapi.NewMessage(chatID, "❌ Unauthorized")
			tb.bot.Send(msg)
			continue
		}

		// Handle /start
		if text == "/start" {
			msg := tgbotapi.NewMessage(chatID,
				"✅ Connected!\n\n"+
					"Just send commands:\n"+
					"• ls, pwd, cat → one-shot commands\n"+
					"• claude, python3, node → auto-starts interactive session\n"+
					"• /exit or /stop → end interactive session\n"+
					"• /status → show session info")
			tb.bot.Send(msg)
			continue
		}

		// Handle exit/stop - end session
		if text == "/exit" || text == "/stop" {
			tb.stopSession(chatID, username)
			continue
		}

		// Handle status
		if text == "/status" {
			tb.showStatus(chatID)
			continue
		}

		// Handle all other commands intelligently
		tb.handleCommand(chatID, username, text)
	}
}

// isInteractiveCommand checks if a command needs a persistent session
func isInteractiveCommand(cmd string) bool {
	// Get first word of command
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	
	firstWord := parts[0]
	
	// Interactive REPLs and tools
	interactive := []string{
		"claude", "claude-code", "aider", // AI assistants
		"python", "python3", "ipython",
		"node", "deno", "bun",
		"irb", "ruby",
		"ghci", "stack",
		"lua",
		"psql", "mysql", "redis-cli",
		"vim", "nvim", "emacs", "nano",
		"less", "more",
		"top", "htop", "btop",
		"watch",
		"ssh", "telnet",
	}
	
	for _, cmd := range interactive {
		if firstWord == cmd {
			return true
		}
	}
	
	return false
}

// handleCommand intelligently routes commands to session or one-shot
func (tb *TelegramBridge) handleCommand(chatID int64, username, text string) {
	// Check if session exists
	tb.mu.RLock()
	session, hasSession := tb.sessions[chatID]
	tb.mu.RUnlock()

	// If session exists and active, send to it
	if hasSession && session.Active {
		fmt.Printf("📱 @%s → [session] %s\n\n", username, text)
		// Show "typing..." while waiting for response
		typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
		tb.bot.Send(typing)
		session.Terminal.SendCommand(text)
		return
	}

	// No session - decide if we need one
	if isInteractiveCommand(text) {
		// Start persistent session
		tb.startSession(chatID, username, text)
	} else {
		// One-shot command
		tb.executeCommand(chatID, username, text)
	}
}

// startSession starts a persistent interactive session
func (tb *TelegramBridge) startSession(chatID int64, username, command string) {
	fmt.Printf("📱 @%s → [new session] %s\n\n", username, command)

	// Create persistent terminal
	sink := &TelegramSink{
		bot:    tb.bot,
		chatID: chatID,
	}

	terminal, err := NewTerminal(sink)
	if err != nil {
		log.Printf("Error creating terminal: %v\n", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Error creating session")
		tb.bot.Send(msg)
		return
	}

	session := &Session{
		Terminal:  terminal,
		Sink:      sink,
		Active:    true,
		Command:   command,
		StartedAt: time.Now(),
		done:      make(chan struct{}),
	}
	tb.mu.Lock()
	tb.sessions[chatID] = session
	tb.mu.Unlock()

	// Show "typing..." while session starts up
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	tb.bot.Send(typing)

	// Send initial command
	terminal.SendCommand(command)

	// Stream output in background
	go tb.streamSessionOutput(chatID)

	// Don't send "session started" message - just let output flow
}

// stopSession ends the active session
func (tb *TelegramBridge) stopSession(chatID int64, username string) {
	tb.mu.Lock()
	session, exists := tb.sessions[chatID]
	if !exists || !session.Active {
		tb.mu.Unlock()
		msg := tgbotapi.NewMessage(chatID, "⚠️ No active session")
		tb.bot.Send(msg)
		return
	}
	session.Active = false
	delete(tb.sessions, chatID)
	tb.mu.Unlock()

	fmt.Printf("📱 @%s → [stop session]\n\n", username)
	session.safeCloseDone() // Signal goroutine to stop
	session.Terminal.Close()

	msg := tgbotapi.NewMessage(chatID, "✅ Session ended")
	tb.bot.Send(msg)
}

// showStatus shows current session info
func (tb *TelegramBridge) showStatus(chatID int64) {
	tb.mu.RLock()
	session, exists := tb.sessions[chatID]
	tb.mu.RUnlock()

	if !exists || !session.Active {
		msg := tgbotapi.NewMessage(chatID, "📊 Status: No active session")
		tb.bot.Send(msg)
		return
	}

	duration := time.Since(session.StartedAt).Round(time.Second)
	status := fmt.Sprintf("📊 Active Session\n\n"+
		"Command: %s\n"+
		"Duration: %s\n"+
		"Started: %s",
		session.Command,
		duration,
		session.StartedAt.Format("15:04:05"))

	msg := tgbotapi.NewMessage(chatID, status)
	tb.bot.Send(msg)
}

func (tb *TelegramBridge) streamSessionOutput(chatID int64) {
	tb.mu.RLock()
	session, exists := tb.sessions[chatID]
	tb.mu.RUnlock()

	if !exists {
		log.Printf("Session output stream: session not found for chat %d\n", chatID)
		return
	}

	log.Printf("Session streaming started for chat %d\n", chatID)

	defer func() {
		log.Printf("Session streaming ended for chat %d\n", chatID)
		// Cleanup on exit
		tb.mu.Lock()
		if session.Active {
			session.Active = false
			delete(tb.sessions, chatID)
		}
		tb.mu.Unlock()
		// Close terminal WITHOUT holding the lock (blocking operation)
		session.Terminal.Close()
	}()

	// Virtual terminal emulator — interprets ANSI cursor positioning
	// so TUI apps like Claude Code render correctly as text
	screen := NewScreenReader(120, 50)

	ticker := time.NewTicker(200 * time.Millisecond) // Check every 200ms
	defer ticker.Stop()

	hasNewData := false
	lastOutput := time.Now()
	lastSend := time.Now()
	lastTyping := time.Now()                           // Track last "typing..." action sent
	lastCleanedScreen := ""                            // Track cleaned content already sent
	sentLines := make(map[string]bool)                 // Track all lines ever sent (dedup fallback)
	maxIdleTime := 30 * time.Minute                   // Auto-stop after 30 min idle
	sendDelay := 1500 * time.Millisecond              // Wait 1.5s after last output before sending
	maxSendInterval := 5 * time.Second                // Force send every 5s during continuous streaming
	typingInterval := 4 * time.Second                  // Refresh typing indicator every 4s (expires at 5s)

	// flushNewContent cleans the current screen and sends only new content
	flushNewContent := func() {
		rawScreen := screen.Screen()
		cleaned := cleanTUIChrome(rawScreen)
		if cleaned == "" {
			return
		}
		newContent := findNewContent(lastCleanedScreen, cleaned)
		if newContent == "" {
			return
		}

		// If suffix matching failed (returned entire screen), apply line-level
		// dedup against previously sent content. This handles TUI full-redraws
		// where old content is collapsed/summarized by Claude Code.
		if newContent == cleaned && lastCleanedScreen != "" {
			log.Printf("[DEDUP] suffix match failed, applying line dedup (tracked=%d lines)", len(sentLines))
			lines := strings.Split(newContent, "\n")
			var unsent []string
			for _, line := range lines {
				key := strings.TrimSpace(line)
				if key == "" {
					continue
				}
				if !sentLines[key] {
					unsent = append(unsent, line)
				}
			}
			if len(unsent) > 0 {
				newContent = strings.TrimSpace(strings.Join(unsent, "\n"))
			} else {
				newContent = ""
			}
		}

		if newContent != "" {
			// Track sent lines for future dedup
			for _, line := range strings.Split(newContent, "\n") {
				key := strings.TrimSpace(line)
				if key != "" {
					sentLines[key] = true
				}
			}
			session.Sink.SendOutput(newContent)
		}
		lastCleanedScreen = cleaned
	}

	for {
		select {
		case <-session.done:
			// Session manually stopped
			log.Printf("Session manually stopped for chat %d\n", chatID)
			if hasNewData {
				flushNewContent()
			}
			return

		case output, ok := <-session.Terminal.outputChan:
			if !ok {
				// Channel closed, terminal died (command exited)
				log.Printf("Terminal exited for chat %d\n", chatID)
				if hasNewData {
					flushNewContent()
				}
				msg := tgbotapi.NewMessage(chatID, "🔴 Session ended (program exited)")
				tb.bot.Send(msg)
				return
			}
			// Feed raw output into virtual terminal
			screen.Write([]byte(output))
			hasNewData = true
			lastOutput = time.Now()

		case <-ticker.C:
			// Keep "typing..." indicator alive while accumulating output
			if hasNewData && time.Since(lastTyping) > typingInterval {
				typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
				tb.bot.Send(typing)
				lastTyping = time.Now()
			}

			// Send new content when output settles OR on a regular interval.
			// - sendDelay: send after 1.5s of silence (quick for short responses)
			// - maxSendInterval: force send every 5s during continuous streaming
			//   (so the user sees partial progress for long responses)
			settled := hasNewData && time.Since(lastOutput) > sendDelay
			forceSend := hasNewData && time.Since(lastSend) > maxSendInterval
			if settled || forceSend {
				flushNewContent()
				hasNewData = false
				lastSend = time.Now()
			}

			// Auto-timeout after long idle (no new output)
			if time.Since(lastOutput) > maxIdleTime {
				log.Printf("Session idle timeout for chat %d\n", chatID)
				msg := tgbotapi.NewMessage(chatID, "⏱️ Session timed out (30min idle)")
				tb.bot.Send(msg)
				return
			}
		}
	}
}

func (tb *TelegramBridge) executeCommand(chatID int64, username, command string) {
	fmt.Printf("📱 @%s → [one-shot] %s\n\n", username, command)

	// Show "typing..." while command runs
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	tb.bot.Send(typing)

	// Create fresh terminal for one-shot command
	sink := &TelegramSink{
		bot:    tb.bot,
		chatID: chatID,
	}

	terminal, err := NewTerminal(sink)
	if err != nil {
		log.Printf("Error creating terminal: %v\n", err)
		msg := tgbotapi.NewMessage(chatID, "❌ Error creating terminal")
		tb.bot.Send(msg)
		return
	}
	defer terminal.Close()

	// Send command
	terminal.SendCommand(command)

	// Stream output
	terminal.StreamOutput()

	fmt.Println("✓ Complete")
	fmt.Println()
}

// CleanupAllSessions stops all active sessions and cleans up resources
func (tb *TelegramBridge) CleanupAllSessions() {
	tb.mu.Lock()
	log.Printf("Cleaning up %d active sessions...\n", len(tb.sessions))
	// Copy sessions to local slice and clear the map while holding the lock
	activeSessions := make([]*Session, 0)
	for chatID, session := range tb.sessions {
		if session.Active {
			log.Printf("Stopping session for chat %d\n", chatID)
			session.Active = false
			activeSessions = append(activeSessions, session)
		}
	}
	tb.sessions = make(map[int64]*Session)
	tb.mu.Unlock()

	// Close each session WITHOUT holding the lock (blocking operations)
	for _, session := range activeSessions {
		session.safeCloseDone()
		session.Terminal.Close()
	}
	log.Println("All sessions cleaned up")
}

// cleanTUIChrome removes terminal UI chrome from VTE screen output.
// Strips separator lines, status bars, empty prompts, and duplicated
// status text that are part of TUI layout but noise in Telegram messages.
func cleanTUIChrome(output string) string {
	lines := strings.Split(output, "\n")
	var cleaned []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip lines that are only or mostly box-drawing separator characters.
		// "Only" catches pure separators like ─────────────────
		// "Mostly" catches prompt bars like ────what─is─2+2 ────────────
		if trimmed != "" && (isOnlySeparators(trimmed) || isMostlySeparators(trimmed)) {
			continue
		}

		// Skip prompt lines — both empty prompts and echoed user input.
		// The user already sees their own message in the chat, so the
		// echoed "❯ what is 2+2" line is redundant.
		if strings.HasPrefix(trimmed, "❯") {
			continue
		}

		// Skip Claude Code response bracket lines (⎿ prefix = UI element)
		if strings.HasPrefix(trimmed, "⎿") {
			continue
		}

		// Skip known TUI status/chrome lines (including wrapped fragments)
		if strings.Contains(trimmed, "? for shortcuts") ||
			strings.Contains(trimmed, "Chrome extension not detected") ||
			strings.Contains(trimmed, "chrome to install") ||
			strings.Contains(trimmed, "claude.ai/chrome") ||
			strings.Contains(trimmed, "ctrl+g to edit in VS Code") ||
			strings.Contains(trimmed, "MCP server needs auth") ||
			strings.HasPrefix(trimmed, "Tip:") ||
			strings.Contains(trimmed, "/plugin marketplace") ||
			strings.Contains(trimmed, "/plugin install") {
			continue
		}

		// Skip keyboard hint lines — not actionable in Telegram
		// Use case-insensitive check since VTE may render as "esc" or "Esc"
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "esc to cancel") ||
			strings.Contains(lower, "esc to interrupt") ||
			strings.Contains(lower, "tab to amend") ||
			strings.Contains(lower, "ctrl+o to") ||
			strings.Contains(lower, "ctrl+e to") ||
			strings.Contains(lower, "shift+tab to cycle") {
			continue
		}

		// Skip TUI status messages
		if trimmed == "Checking for updates" {
			continue
		}

		// Skip TUI navigation elements (⏵⏵ arrows, edit acceptance, menu chrome)
		if strings.HasPrefix(trimmed, "⏵") || strings.Contains(trimmed, "accept edits on") {
			continue
		}

		// Skip duplicated status bar text (VTE rendering artifact)
		// e.g., "Claude Code Claude Code" or "Basic Arithmetic Basic Arithmetic"
		if isDuplicatedText(trimmed) {
			continue
		}

		// Clean response bullet: "● text" → "text"
		// The blockquote format already indicates it's a response.
		if strings.HasPrefix(trimmed, "●") {
			line = strings.TrimSpace(strings.TrimPrefix(trimmed, "●"))
			if line == "" {
				continue
			}
		}

		// Skip thinking indicator lines (✶ ✻ ✦ ✧ ✢ ✽ · * Thinking…)
		// These are transient TUI progress indicators, not useful in Telegram.
		// The * variant appears when VTE renders certain Unicode chars as ASCII.
		if len(trimmed) > 0 {
			first := []rune(trimmed)[0]
			if first == '✶' || first == '✻' || first == '✦' || first == '✧' || first == '✢' || first == '✽' || first == '·' {
				continue
			}
			// Match "* Verb…" pattern — thinking indicator with ASCII asterisk
			if first == '*' && strings.Contains(trimmed, "…") {
				continue
			}
		}

		// Skip short Title Case lines — conversation titles from Claude Code's status bar.
		// e.g., "Basic Math", "Simple Calculation", "File Operations"
		if isStatusBarTitle(trimmed) {
			continue
		}

		cleaned = append(cleaned, line)
	}

	result := strings.TrimSpace(strings.Join(cleaned, "\n"))

	// Collapse 3+ consecutive blank lines into one
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	return result
}

// isOnlySeparators returns true if the string contains only box-drawing
// separator characters (─, ━, ═) and spaces.
func isOnlySeparators(s string) bool {
	for _, r := range s {
		switch r {
		case '─', '━', '═', '—', '╌', '╍', '┄', '┅', '┈', '┉', ' ':
			// Box-drawing separators (solid, dashed, dotted) and whitespace
		default:
			return false
		}
	}
	return true
}

// isMostlySeparators returns true if >60% of non-space runes are box-drawing
// separator characters. This catches Claude Code's prompt bar, which renders
// as "────what─is─2+2 ────────────" in VTE output.
func isMostlySeparators(s string) bool {
	var sepCount, totalCount int
	for _, r := range s {
		if r == ' ' {
			continue
		}
		totalCount++
		switch r {
		case '─', '━', '═', '—', '╌', '╍', '┄', '┅', '┈', '┉':
			sepCount++
		}
	}
	if totalCount < 10 {
		return false // Short lines need exact match
	}
	return float64(sepCount)/float64(totalCount) > 0.6
}

// findNewContent extracts only new content from a cleaned screen by finding
// where old content ends and returning everything after it.
// Uses suffix matching to handle terminal scrolling — old content may have
// shifted up in the new screen, and new content appears after it.
func findNewContent(old, current string) string {
	if old == "" {
		return current
	}
	if current == old {
		return ""
	}

	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(current, "\n")

	// Normalize lines for comparison — VTE rendering can leave
	// different trailing whitespace between screen snapshots.
	norm := func(s string) string {
		return strings.TrimRight(s, " \t")
	}

	// Try to find the longest suffix of oldLines as a contiguous block
	// in newLines. Start with the full old content, then progressively
	// shorter suffixes. The first match is the longest, most reliable.
	for suffixStart := 0; suffixStart < len(oldLines); suffixStart++ {
		suffix := oldLines[suffixStart:]

		for nStart := 0; nStart+len(suffix) <= len(newLines); nStart++ {
			match := true
			for j := 0; j < len(suffix); j++ {
				if norm(newLines[nStart+j]) != norm(suffix[j]) {
					match = false
					break
				}
			}
			if match {
				// Old content found at nStart. New content follows.
				afterIdx := nStart + len(suffix)
				if afterIdx >= len(newLines) {
					return "" // No new content after match
				}
				result := strings.Join(newLines[afterIdx:], "\n")
				return strings.TrimSpace(result)
			}
		}
	}

	// No suffix match — screen completely changed (TUI full redraw).
	// Return all current content; caller applies line-level dedup.
	return current
}

// isStatusBarTitle returns true if a line looks like a Claude Code conversation
// title from the status bar — short Title Case phrases like "Basic Math".
func isStatusBarTitle(s string) bool {
	words := strings.Fields(s)
	if len(words) < 1 || len(words) > 4 {
		return false
	}
	if len(s) > 40 {
		return false
	}
	for _, w := range words {
		runes := []rune(w)
		if len(runes) == 0 {
			return false
		}
		// First char must be uppercase letter
		if runes[0] < 'A' || runes[0] > 'Z' {
			return false
		}
		// Rest must be letters (allow common title chars)
		for _, r := range runes[1:] {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '-' && r != '\'' {
				return false
			}
		}
	}
	return true
}

// isDuplicatedText returns true if a line consists of a phrase repeated twice.
// This catches VTE status bar rendering artifacts like "Claude Code Claude Code"
// or "Basic Arithmetic Basic Arithmetic".
func isDuplicatedText(s string) bool {
	words := strings.Fields(s)
	if len(words) < 2 || len(words)%2 != 0 {
		return false
	}
	half := len(words) / 2
	for i := 0; i < half; i++ {
		if words[i] != words[half+i] {
			return false
		}
	}
	return true
}
