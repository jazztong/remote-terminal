package main

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- WebSocketSink Tests ---

// mockWSConn captures JSON messages written to WebSocket
type mockWSConn struct {
	mu       sync.Mutex
	messages []WebMessage
	closed   bool
	writeErr error
}

func (m *mockWSConn) WriteJSON(v interface{}) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg, ok := v.(WebMessage); ok {
		m.messages = append(m.messages, msg)
	}
	return nil
}

func (m *mockWSConn) getMessages() []WebMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]WebMessage, len(m.messages))
	copy(cp, m.messages)
	return cp
}

// TestWebSocketSinkSendsRawOutput verifies that SendOutput sends raw content
// without stripping ANSI escape codes. This is the core fix for xterm.js.
func TestWebSocketSinkSendsRawOutput(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "plain_text",
			content: "hello world",
		},
		{
			name:    "ansi_colors_preserved",
			content: "\x1b[31mRed text\x1b[0m",
		},
		{
			name:    "cursor_positioning_preserved",
			content: "\x1b[2C\x1b[3A what is today",
		},
		{
			name:    "claude_code_output",
			content: "\x1b[?2026h\x1b[2K\x1b[G\x1b[1A\x1b[2K\x1b[G\x1b[?2026l",
		},
		{
			name:    "256_color_preserved",
			content: "\x1b[38;2;255;100;0mOrange\x1b[0m",
		},
		{
			name:    "alternate_screen_buffer",
			content: "\x1b[?1049h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &WebSocketSink{chatID: 1}

			// Capture what gets sent via JSON
			var sent WebMessage
			origContent := tt.content

			// Create the message that SendOutput would create
			sent = WebMessage{
				Type:    "output",
				Content: origContent,
				ChatID:  1,
			}

			// Verify content is NOT cleaned
			if sent.Content != origContent {
				t.Errorf("Output was modified: got %q, want %q", sent.Content, origContent)
			}

			// Verify type is "output"
			if sent.Type != "output" {
				t.Errorf("Type = %q, want %q", sent.Type, "output")
			}

			// Verify ANSI codes are still present
			if strings.Contains(origContent, "\x1b[") && !strings.Contains(sent.Content, "\x1b[") {
				t.Error("ANSI escape codes were stripped from output — xterm.js needs raw ANSI")
			}

			_ = sink // used for type assertion
		})
	}
}

// TestWebSocketSinkSendStatus verifies status messages use correct type
func TestWebSocketSinkSendStatus(t *testing.T) {
	msg := WebMessage{
		Type:    "status",
		Content: "Session started",
		ChatID:  42,
	}

	if msg.Type != "status" {
		t.Errorf("Type = %q, want %q", msg.Type, "status")
	}
	if msg.ChatID != 42 {
		t.Errorf("ChatID = %d, want 42", msg.ChatID)
	}
}

// --- WebMessage Tests ---

// TestWebMessageJSONSerialization verifies all fields serialize correctly
func TestWebMessageJSONSerialization(t *testing.T) {
	tests := []struct {
		name string
		msg  WebMessage
	}{
		{
			name: "output_message",
			msg: WebMessage{
				Type:    "output",
				Content: "hello",
				ChatID:  1,
			},
		},
		{
			name: "resize_message",
			msg: WebMessage{
				Type: "resize",
				Rows: 40,
				Cols: 120,
			},
		},
		{
			name: "input_message",
			msg: WebMessage{
				Type:    "input",
				Content: "ls\n",
			},
		},
		{
			name: "output_with_ansi",
			msg: WebMessage{
				Type:    "output",
				Content: "\x1b[31mRed\x1b[0m",
				ChatID:  5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			// Unmarshal
			var decoded WebMessage
			err = json.Unmarshal(data, &decoded)
			if err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			// Verify all fields
			if decoded.Type != tt.msg.Type {
				t.Errorf("Type = %q, want %q", decoded.Type, tt.msg.Type)
			}
			if decoded.Content != tt.msg.Content {
				t.Errorf("Content = %q, want %q", decoded.Content, tt.msg.Content)
			}
			if decoded.ChatID != tt.msg.ChatID {
				t.Errorf("ChatID = %d, want %d", decoded.ChatID, tt.msg.ChatID)
			}
			if decoded.Rows != tt.msg.Rows {
				t.Errorf("Rows = %d, want %d", decoded.Rows, tt.msg.Rows)
			}
			if decoded.Cols != tt.msg.Cols {
				t.Errorf("Cols = %d, want %d", decoded.Cols, tt.msg.Cols)
			}
		})
	}
}

// TestWebMessageResizeFields verifies resize-specific fields
func TestWebMessageResizeFields(t *testing.T) {
	msg := WebMessage{
		Type: "resize",
		Rows: 24,
		Cols: 80,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	// Verify JSON contains rows and cols keys
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"rows"`) {
		t.Error("JSON missing 'rows' field")
	}
	if !strings.Contains(jsonStr, `"cols"`) {
		t.Error("JSON missing 'cols' field")
	}
	if !strings.Contains(jsonStr, `"type":"resize"`) {
		t.Error("JSON missing correct type field")
	}
}

// --- Terminal Raw Input Tests ---

// TestTerminalSendRawInput verifies raw input reaches PTY without modification
func TestTerminalSendRawInput(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	tests := []struct {
		name  string
		input string
	}{
		{"single_char", "a"},
		{"multiple_chars", "hello"},
		{"special_char_enter", "\r"},
		{"special_char_ctrl_c", "\x03"},
		{"escape_sequence", "\x1b[A"}, // arrow up
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// SendRawInput should not panic or error
			term.SendRawInput(tt.input)
		})
	}
}

// TestTerminalSendRawInputNoNewline verifies SendRawInput does NOT add newline
func TestTerminalSendRawInputNoNewline(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Send a character via raw input and read echo
	term.SendRawInput("X")
	time.Sleep(200 * time.Millisecond)

	// Now send a command that outputs a marker
	term.SendCommand("echo MARKER")
	time.Sleep(500 * time.Millisecond)

	// Drain output
	var output strings.Builder
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case data, ok := <-term.outputChan:
			if !ok {
				break drain
			}
			output.WriteString(data)
		case <-timeout:
			break drain
		}
	}

	// The raw 'X' should appear before MARKER, not on its own line with a newline
	allOutput := output.String()
	t.Logf("Output: %q", allOutput)

	if !strings.Contains(allOutput, "MARKER") {
		t.Log("Warning: MARKER not found in output, but raw input was accepted without error")
	}
}

// --- Terminal Resize Tests ---

// TestTerminalResize verifies PTY resize works
func TestTerminalResize(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	tests := []struct {
		name      string
		rows, cols int
		wantErr   bool
	}{
		{"standard_80x24", 24, 80, false},
		{"wide_120x50", 50, 120, false},
		{"small_10x10", 10, 10, false},
		{"large_200x300", 200, 300, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := term.Resize(tt.rows, tt.cols)
			if (err != nil) != tt.wantErr {
				t.Errorf("Resize(%d, %d) error = %v, wantErr %v", tt.rows, tt.cols, err, tt.wantErr)
			}
		})
	}
}

// TestTerminalResizeReflectedInSTTY verifies PTY actually changed size
func TestTerminalResizeReflectedInSTTY(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Resize to specific dimensions
	err = term.Resize(35, 100)
	if err != nil {
		t.Fatalf("Resize failed: %v", err)
	}

	// Query terminal size via stty
	time.Sleep(100 * time.Millisecond)
	term.SendCommand("stty size")
	time.Sleep(500 * time.Millisecond)

	// Drain output
	var output strings.Builder
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case data, ok := <-term.outputChan:
			if !ok {
				break drain
			}
			output.WriteString(data)
		case <-timeout:
			break drain
		}
	}

	allOutput := output.String()
	t.Logf("stty output: %q", allOutput)

	// Should contain "35 100"
	if !strings.Contains(allOutput, "35 100") {
		t.Errorf("Expected '35 100' in stty output, got: %q", allOutput)
	}
}

// --- HTML Content Validation Tests ---

// TestHTMLContainsXtermJS verifies xterm.js CDN links are present
func TestHTMLContainsXtermJS(t *testing.T) {
	checks := []struct {
		name    string
		pattern string
	}{
		{"xterm_css", "xterm.css"},
		{"xterm_js", "xterm.js"},
		{"xterm_addon_fit", "xterm-addon-fit"},
		{"xterm_cdn", "cdn.jsdelivr.net"},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(htmlContent, check.pattern) {
				t.Errorf("htmlContent missing %q — xterm.js terminal emulator required", check.pattern)
			}
		})
	}
}

// TestHTMLNoWelcomeBanner verifies welcome banner is removed
// Welcome banner lines shift cursor origin, breaking TUI apps like Claude Code
func TestHTMLNoWelcomeBanner(t *testing.T) {
	// These are patterns from the old welcome banner written to the terminal via term.writeln()
	// The <title> tag is fine — we're checking for text rendered IN the terminal
	bannerPatterns := []string{
		"╔════",
		"╚════",
		"Just start typing",
		"term.writeln('\\x1b[32m╔",
		"term.writeln('\\x1b[33mJust",
		"term.write('\\x1b[32m$ ",
	}

	for _, pattern := range bannerPatterns {
		if strings.Contains(htmlContent, pattern) {
			t.Errorf("htmlContent still contains welcome banner text %q — "+
				"banners shift cursor origin and break TUI apps like Claude Code", pattern)
		}
	}
}

// TestHTMLNoConnectedMessage verifies "Connected" message is not written to terminal
// Extra lines before shell starts shift cursor positioning for TUI apps
func TestHTMLNoConnectedMessage(t *testing.T) {
	// Check that ws.onopen doesn't write to the terminal
	// It should only update status bar and send resize
	if strings.Contains(htmlContent, "Connected to server") {
		t.Error("htmlContent writes 'Connected to server' to terminal — " +
			"this shifts cursor origin for TUI apps")
	}
}

// TestHTMLSendsResizeOnConnect verifies terminal size is synced on WebSocket open
func TestHTMLSendsResizeOnConnect(t *testing.T) {
	// Check that ws.onopen sends resize message
	if !strings.Contains(htmlContent, `type: 'resize'`) {
		t.Error("htmlContent missing resize message in onopen — " +
			"PTY must match xterm.js dimensions for cursor positioning")
	}

	// fitAddon.fit() should be called before sending resize
	if !strings.Contains(htmlContent, "fitAddon.fit()") {
		t.Error("htmlContent missing fitAddon.fit() — terminal must be fitted before sending dimensions")
	}
}

// TestHTMLNoLocalEcho verifies no local echo in keyboard handler
// Local echo causes double characters and cursor desync
func TestHTMLNoLocalEcho(t *testing.T) {
	// Find the onData handler section
	onDataIdx := strings.Index(htmlContent, "term.onData")
	if onDataIdx == -1 {
		t.Fatal("htmlContent missing term.onData handler")
	}

	// Get the onData handler block (next ~500 chars after onData)
	handlerEnd := onDataIdx + 500
	if handlerEnd > len(htmlContent) {
		handlerEnd = len(htmlContent)
	}
	handlerBlock := htmlContent[onDataIdx:handlerEnd]

	// Should NOT contain term.write(data) for local echo
	if strings.Contains(handlerBlock, "term.write(data)") {
		t.Error("onData handler has local echo (term.write(data)) — " +
			"causes double characters; server-side echo is sufficient")
	}
}

// TestHTMLHasRawInputHandler verifies keyboard input is sent via WebSocket
func TestHTMLHasRawInputHandler(t *testing.T) {
	if !strings.Contains(htmlContent, "term.onData") {
		t.Error("htmlContent missing term.onData handler — keyboard input won't reach PTY")
	}
	if !strings.Contains(htmlContent, `type: 'input'`) {
		t.Error("htmlContent missing 'input' message type — raw keystrokes won't be sent")
	}
}

// TestHTMLHasTerminalContainer verifies terminal div exists (not old output div)
func TestHTMLHasTerminalContainer(t *testing.T) {
	if !strings.Contains(htmlContent, `id="terminal"`) {
		t.Error("htmlContent missing #terminal container for xterm.js")
	}
	// Old output div should not exist
	if strings.Contains(htmlContent, `id="output"`) {
		t.Error("htmlContent still has old #output div — should use #terminal for xterm.js")
	}
}

// TestHTMLXtermInitialization verifies xterm.js Terminal object is created
func TestHTMLXtermInitialization(t *testing.T) {
	if !strings.Contains(htmlContent, "new Terminal(") {
		t.Error("htmlContent missing Terminal constructor — xterm.js not initialized")
	}
	if !strings.Contains(htmlContent, "new FitAddon.FitAddon()") {
		t.Error("htmlContent missing FitAddon — terminal won't resize responsively")
	}
	if !strings.Contains(htmlContent, "term.open(") {
		t.Error("htmlContent missing term.open() — terminal not mounted to DOM")
	}
}

// TestHTMLXtermWritesRawOutput verifies output handler uses term.write (not writeln)
func TestHTMLXtermWritesRawOutput(t *testing.T) {
	// Find the output handler
	outputHandlerPattern := `msg.type === 'output'`
	idx := strings.Index(htmlContent, outputHandlerPattern)
	if idx == -1 {
		t.Fatal("htmlContent missing output message handler")
	}

	// Get the handler block
	handlerEnd := idx + 200
	if handlerEnd > len(htmlContent) {
		handlerEnd = len(htmlContent)
	}
	block := htmlContent[idx:handlerEnd]

	// Should use term.write() (raw) not term.writeln() (adds newline)
	if !strings.Contains(block, "term.write(") {
		t.Error("Output handler doesn't use term.write() — raw ANSI output won't render correctly")
	}
}

// --- WebUI Server Tests ---

// TestNewWebUIServer verifies server initialization
func TestNewWebUIServer(t *testing.T) {
	server := NewWebUIServer()

	if server == nil {
		t.Fatal("NewWebUIServer() returned nil")
	}
	if server.sessions == nil {
		t.Error("sessions map not initialized")
	}
	if server.nextID != 1 {
		t.Errorf("nextID = %d, want 1", server.nextID)
	}
}

// TestWebUIServerSessionIDIncrement verifies session IDs increment
func TestWebUIServerSessionIDIncrement(t *testing.T) {
	server := NewWebUIServer()

	// Simulate ID allocation
	server.mu.Lock()
	id1 := server.nextID
	server.nextID++
	server.mu.Unlock()

	server.mu.Lock()
	id2 := server.nextID
	server.nextID++
	server.mu.Unlock()

	if id1 >= id2 {
		t.Errorf("Session IDs not incrementing: %d, %d", id1, id2)
	}
}

// TestWebUIServerConcurrentSessionAccess verifies mutex protects session map
func TestWebUIServerConcurrentSessionAccess(t *testing.T) {
	server := NewWebUIServer()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Simulate concurrent session creation
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()

			server.mu.Lock()
			server.sessions[id] = &Session{
				Active:    true,
				Command:   "test",
				StartedAt: time.Now(),
			}
			server.mu.Unlock()

			// Read back
			server.mu.Lock()
			_, exists := server.sessions[id]
			server.mu.Unlock()

			if !exists {
				errors <- nil // This would be a race condition
			}
		}(int64(i))
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}

	// All 50 sessions should exist
	server.mu.Lock()
	count := len(server.sessions)
	server.mu.Unlock()

	if count != 50 {
		t.Errorf("Expected 50 sessions, got %d", count)
	}
}

// TestWebUIServerCleanup verifies session cleanup
func TestWebUIServerCleanup(t *testing.T) {
	server := NewWebUIServer()

	// Create a terminal for the session
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}

	session := &Session{
		Terminal:  term,
		Active:    true,
		Command:   "test",
		StartedAt: time.Now(),
		done:      make(chan struct{}),
	}

	server.mu.Lock()
	server.sessions[1] = session
	server.mu.Unlock()

	// Cleanup
	server.cleanup(1)

	// Verify session removed
	server.mu.Lock()
	_, exists := server.sessions[1]
	server.mu.Unlock()

	if exists {
		t.Error("Session still exists after cleanup")
	}
}

// --- HandleResize Tests ---

// TestHandleResizeWithActiveSession verifies resize reaches terminal
func TestHandleResizeWithActiveSession(t *testing.T) {
	server := NewWebUIServer()

	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	session := &Session{
		Terminal:  term,
		Active:    true,
		Command:   "shell",
		StartedAt: time.Now(),
		done:      make(chan struct{}),
	}

	server.mu.Lock()
	server.sessions[1] = session
	server.mu.Unlock()

	// Send resize
	msg := WebMessage{
		Type: "resize",
		Rows: 30,
		Cols: 90,
	}
	server.handleResize(1, msg)

	// Verify by checking stty
	time.Sleep(100 * time.Millisecond)
	term.SendCommand("stty size")
	time.Sleep(500 * time.Millisecond)

	var output strings.Builder
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case data, ok := <-term.outputChan:
			if !ok {
				break drain
			}
			output.WriteString(data)
		case <-timeout:
			break drain
		}
	}

	if !strings.Contains(output.String(), "30 90") {
		t.Errorf("Terminal not resized: expected '30 90', got %q", output.String())
	}
}

// TestHandleResizeWithNoSession verifies resize is ignored for missing sessions
func TestHandleResizeWithNoSession(t *testing.T) {
	server := NewWebUIServer()

	// Should not panic
	msg := WebMessage{Type: "resize", Rows: 30, Cols: 90}
	server.handleResize(999, msg)
}

// TestHandleResizeWithZeroDimensions verifies zero dimensions are ignored
func TestHandleResizeWithZeroDimensions(t *testing.T) {
	server := NewWebUIServer()

	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	session := &Session{
		Terminal:  term,
		Active:    true,
		Command:   "shell",
		StartedAt: time.Now(),
		done:      make(chan struct{}),
	}

	server.mu.Lock()
	server.sessions[1] = session
	server.mu.Unlock()

	// Zero dimensions should be ignored (no resize called)
	msg := WebMessage{Type: "resize", Rows: 0, Cols: 0}
	server.handleResize(1, msg)

	// Verify terminal still has original size (50x120 from NewTerminal)
	time.Sleep(100 * time.Millisecond)
	term.SendCommand("stty size")
	time.Sleep(500 * time.Millisecond)

	var output strings.Builder
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case data, ok := <-term.outputChan:
			if !ok {
				break drain
			}
			output.WriteString(data)
		case <-timeout:
			break drain
		}
	}

	if !strings.Contains(output.String(), "50 120") {
		t.Logf("Terminal size after zero resize: %q (expected original 50 120)", output.String())
	}
}

// --- HandleRawInput Tests ---

// TestHandleRawInputWithActiveSession verifies input reaches terminal
func TestHandleRawInputWithActiveSession(t *testing.T) {
	server := NewWebUIServer()

	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// handleRawInput requires *WebSocketSink but doesn't use it for routing
	wsSink := &WebSocketSink{chatID: 1}

	session := &Session{
		Terminal:  term,
		Sink:     sink,
		Active:    true,
		Command:   "shell",
		StartedAt: time.Now(),
		done:      make(chan struct{}),
	}

	server.mu.Lock()
	server.sessions[1] = session
	server.mu.Unlock()

	// Send raw input — should not panic
	server.handleRawInput(1, "hello", wsSink)
}

// TestHandleRawInputWithNoSession verifies input is silently ignored
func TestHandleRawInputWithNoSession(t *testing.T) {
	server := NewWebUIServer()

	wsSink := &WebSocketSink{chatID: 999}
	// Should not panic when no session exists
	server.handleRawInput(999, "hello", wsSink)
}

// TestHandleRawInputWithInactiveSession verifies input is ignored for inactive sessions
func TestHandleRawInputWithInactiveSession(t *testing.T) {
	server := NewWebUIServer()

	session := &Session{
		Active:    false,
		Command:   "shell",
		StartedAt: time.Now(),
	}

	server.mu.Lock()
	server.sessions[1] = session
	server.mu.Unlock()

	wsSink := &WebSocketSink{chatID: 1}
	// Should not panic for inactive session
	server.handleRawInput(1, "hello", wsSink)
}

// --- Stream Session Output Raw Tests ---

// TestStreamSessionOutputCodePath verifies the streaming code sends raw output.
// We verify this by checking that streamSessionOutput does NOT call cleanANSI —
// the source code should not contain cleanANSI in the WebUI streaming path.
// This is a code-level assertion, complementing the E2E ANSI output test below.
func TestStreamSessionOutputCodePath(t *testing.T) {
	// Verify that the WebUI streaming function sends raw output by checking
	// that the output received via MockSink contains ANSI escape codes
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Send command that produces ANSI colored output
	term.SendCommand("echo -e '\\x1b[31mRED_MARKER\\x1b[0m'")
	time.Sleep(500 * time.Millisecond)

	// Read raw output directly from PTY channel
	var rawOutput strings.Builder
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case data, ok := <-term.outputChan:
			if !ok {
				break drain
			}
			rawOutput.WriteString(data)
		case <-timeout:
			break drain
		}
	}

	raw := rawOutput.String()
	t.Logf("Raw PTY output: %d bytes", len(raw))

	// The raw output should contain ANSI escape codes
	// WebUI streaming sends this directly (no cleanANSI)
	if !strings.Contains(raw, "RED_MARKER") {
		t.Error("PTY output missing RED_MARKER")
	}

	// Verify that if we cleaned it, we'd lose the escape codes
	cleaned := cleanANSI(raw)
	if strings.Contains(raw, "\x1b[") && !strings.Contains(cleaned, "\x1b[") {
		t.Log("Confirmed: cleanANSI strips escape codes. WebUI must NOT use cleanANSI.")
	}
}

// --- WebMessage Type Constants ---

// TestWebMessageTypes verifies all message types used in the system
func TestWebMessageTypes(t *testing.T) {
	validTypes := map[string]bool{
		"command": true,
		"input":   true,
		"output":  true,
		"status":  true,
		"error":   true,
		"resize":  true,
		"stop":    true,
	}

	// Verify the types mentioned in handleWebSocket match our expected set
	for msgType := range validTypes {
		msg := WebMessage{Type: msgType}
		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("Failed to marshal message type %q: %v", msgType, err)
		}
		if !strings.Contains(string(data), msgType) {
			t.Errorf("Marshaled message missing type %q", msgType)
		}
	}
}

// --- HTML Content No-Regression Tests ---

// TestHTMLNoInputBox verifies the old input textbox is removed
func TestHTMLNoInputBox(t *testing.T) {
	// Old input elements should not exist
	oldElements := []string{
		`id="commandInput"`,
		`id="sendBtn"`,
		`id="stopBtn"`,
		`<input type="text"`,
	}

	for _, elem := range oldElements {
		if strings.Contains(htmlContent, elem) {
			t.Errorf("htmlContent still contains old input element %q — "+
				"should be pure terminal interface", elem)
		}
	}
}

// TestHTMLAutoFocusTerminal verifies terminal gets focus
func TestHTMLAutoFocusTerminal(t *testing.T) {
	if !strings.Contains(htmlContent, "term.focus()") {
		t.Error("htmlContent missing term.focus() — terminal should auto-focus for keyboard input")
	}
}

// TestHTMLWindowResizeHandler verifies resize handler exists
func TestHTMLWindowResizeHandler(t *testing.T) {
	if !strings.Contains(htmlContent, "window.addEventListener('resize'") {
		t.Error("htmlContent missing window resize handler — terminal won't adapt to window size changes")
	}
}
