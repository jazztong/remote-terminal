package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
	},
}

type WebUIServer struct {
	sessions     map[int64]*Session
	authSessions map[string]time.Time // auth token → expiry
	mu           sync.Mutex
	nextID       int64
	config       *Config
}

func NewWebUIServer(config *Config) *WebUIServer {
	return &WebUIServer{
		sessions:     make(map[int64]*Session),
		authSessions: make(map[string]time.Time),
		nextID:       1,
		config:       config,
	}
}

type WebMessage struct {
	Type    string `json:"type"`    // "command", "input", "output", "status", "error", "resize"
	Content string `json:"content"` // Message content
	ChatID  int64  `json:"chatId"`  // Session ID
	Rows    int    `json:"rows"`    // Terminal rows (for resize)
	Cols    int    `json:"cols"`    // Terminal cols (for resize)
}

// WebSocketSink sends output to WebSocket
type WebSocketSink struct {
	conn   *websocket.Conn
	chatID int64
	mu     sync.Mutex
}

func (w *WebSocketSink) SendOutput(output string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	msg := WebMessage{
		Type:    "output",
		Content: output,
		ChatID:  w.chatID,
	}

	if err := w.conn.WriteJSON(msg); err != nil {
		log.Printf("WebSocket write error: %v\n", err)
	}
}

func (w *WebSocketSink) SendStatus(status string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	msg := WebMessage{
		Type:    "status",
		Content: status,
		ChatID:  w.chatID,
	}

	if err := w.conn.WriteJSON(msg); err != nil {
		log.Printf("WebSocket write error: %v\n", err)
	}
}

func (s *WebUIServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v\n", err)
		return
	}
	defer conn.Close()

	// Assign session ID
	s.mu.Lock()
	chatID := s.nextID
	s.nextID++
	s.mu.Unlock()

	log.Printf("WebUI client connected (session %d)\n", chatID)

	// Create WebSocket sink
	sink := &WebSocketSink{
		conn:   conn,
		chatID: chatID,
	}

	// Automatically start a shell session for the user
	s.startShellSession(chatID, sink)

	// Handle incoming messages
	for {
		var msg WebMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Printf("WebSocket read error: %v\n", err)
			// Clean up session on disconnect
			s.cleanup(chatID)
			break
		}

		if msg.Type == "command" {
			s.handleCommand(chatID, msg.Content, sink)
		} else if msg.Type == "input" {
			// Handle raw input (character-by-character) for interactive programs
			s.handleRawInput(chatID, msg.Content, sink)
		} else if msg.Type == "resize" {
			// Handle terminal resize
			s.handleResize(chatID, msg)
		} else if msg.Type == "stop" {
			s.stopSession(chatID, sink)
		} else if msg.Type == "status" {
			s.showStatus(chatID, sink)
		}
	}

	log.Printf("WebUI client disconnected (session %d)\n", chatID)
}

func (s *WebUIServer) handleCommand(chatID int64, command string, sink *WebSocketSink) {
	s.mu.Lock()
	session, hasSession := s.sessions[chatID]
	s.mu.Unlock()

	// If session exists and active, send to it
	if hasSession && session.Active {
		log.Printf("[WebUI-%d] → [session] %s\n", chatID, command)
		session.Terminal.SendCommand(command)
		return
	}

	// No session - decide if we need one
	if isInteractiveCommand(command) {
		s.startSession(chatID, command, sink)
	} else {
		s.executeCommand(chatID, command, sink)
	}
}

func (s *WebUIServer) handleRawInput(chatID int64, input string, sink *WebSocketSink) {
	s.mu.Lock()
	session, hasSession := s.sessions[chatID]
	s.mu.Unlock()

	if !hasSession || !session.Active {
		// No active session - ignore raw input
		return
	}

	// Send raw input directly to PTY (no newline added)
	session.Terminal.SendRawInput(input)
}

func (s *WebUIServer) handleResize(chatID int64, msg WebMessage) {
	s.mu.Lock()
	session, hasSession := s.sessions[chatID]
	s.mu.Unlock()

	if !hasSession || !session.Active {
		return
	}

	// Resize the PTY to match terminal size
	if msg.Rows > 0 && msg.Cols > 0 {
		log.Printf("[WebUI-%d] Resizing terminal to %dx%d\n", chatID, msg.Rows, msg.Cols)
		session.Terminal.Resize(msg.Rows, msg.Cols)
	}
}

func (s *WebUIServer) startShellSession(chatID int64, sink *WebSocketSink) {
	log.Printf("[WebUI-%d] → [starting shell session]\n", chatID)

	terminal, err := NewTerminal(sink)
	if err != nil {
		log.Printf("Error creating terminal: %v\n", err)
		sink.SendStatus("❌ Error creating terminal")
		return
	}

	session := &Session{
		Terminal:  terminal,
		Sink:      sink,
		Active:    true,
		Command:   "shell",
		StartedAt: time.Now(),
		done:      make(chan struct{}),
	}

	s.mu.Lock()
	s.sessions[chatID] = session
	s.mu.Unlock()

	// Stream output in background (shell is already running)
	go s.streamSessionOutput(chatID, sink)
}

func (s *WebUIServer) startSession(chatID int64, command string, sink *WebSocketSink) {
	log.Printf("[WebUI-%d] → [new session] %s\n", chatID, command)

	terminal, err := NewTerminal(sink)
	if err != nil {
		log.Printf("Error creating terminal: %v\n", err)
		sink.SendStatus("❌ Error creating session")
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

	s.mu.Lock()
	s.sessions[chatID] = session
	s.mu.Unlock()

	// Send initial command
	terminal.SendCommand(command)

	// Stream output in background
	go s.streamSessionOutput(chatID, sink)

	sink.SendStatus(fmt.Sprintf("🔄 Interactive session started: %s", command))
}

func (s *WebUIServer) stopSession(chatID int64, sink *WebSocketSink) {
	s.mu.Lock()
	session, exists := s.sessions[chatID]
	s.mu.Unlock()

	if !exists || !session.Active {
		sink.SendStatus("⚠️ No active session")
		return
	}

	log.Printf("[WebUI-%d] → [stop session]\n", chatID)
	session.Active = false
	close(session.done)
	session.Terminal.Close()

	s.mu.Lock()
	delete(s.sessions, chatID)
	s.mu.Unlock()

	sink.SendStatus("✅ Session ended")
}

func (s *WebUIServer) showStatus(chatID int64, sink *WebSocketSink) {
	s.mu.Lock()
	session, exists := s.sessions[chatID]
	s.mu.Unlock()

	if !exists || !session.Active {
		sink.SendStatus("📊 Status: No active session")
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

	sink.SendStatus(status)
}

func (s *WebUIServer) executeCommand(chatID int64, command string, sink *WebSocketSink) {
	log.Printf("[WebUI-%d] → [one-shot] %s\n", chatID, command)

	terminal, err := NewTerminal(sink)
	if err != nil {
		log.Printf("Error creating terminal: %v\n", err)
		sink.SendStatus("❌ Error creating terminal")
		return
	}
	defer terminal.Close()

	// Send command
	terminal.SendCommand(command)

	// Stream output
	terminal.StreamOutput()

	log.Printf("[WebUI-%d] ✓ Complete\n", chatID)
}

func (s *WebUIServer) streamSessionOutput(chatID int64, sink *WebSocketSink) {
	s.mu.Lock()
	session, exists := s.sessions[chatID]
	s.mu.Unlock()

	if !exists {
		log.Printf("Session output stream: session not found for %d\n", chatID)
		return
	}

	log.Printf("Session streaming started for WebUI-%d\n", chatID)

	defer func() {
		log.Printf("Session streaming ended for WebUI-%d\n", chatID)
		// Cleanup on exit
		s.mu.Lock()
		if session.Active {
			session.Active = false
			session.Terminal.Close()
			delete(s.sessions, chatID)
		}
		s.mu.Unlock()
	}()

	ticker := time.NewTicker(5 * time.Millisecond)  // Check every 5ms for instant response
	defer ticker.Stop()

	var buffer string
	lastOutput := time.Now()
	maxIdleTime := 30 * time.Minute

	for {
		select {
		case <-session.done:
			log.Printf("Session manually stopped for WebUI-%d\n", chatID)
			if buffer != "" {
				// Send raw output for xterm.js terminal emulator
				sink.SendOutput(buffer)
			}
			return

		case output, ok := <-session.Terminal.outputChan:
			if !ok {
				log.Printf("Terminal exited for WebUI-%d\n", chatID)
				if buffer != "" {
					// Send raw output for xterm.js terminal emulator
					sink.SendOutput(buffer)
				}
				sink.SendStatus("🔴 Session ended (program exited)")
				return
			}
			buffer += output
			lastOutput = time.Now()

		case <-ticker.C:
			if buffer != "" && time.Since(lastOutput) > 1*time.Millisecond {
				// Send RAW output immediately for instant typing (1ms delay)
				sink.SendOutput(buffer)
				buffer = ""
			}

			if time.Since(lastOutput) > maxIdleTime {
				log.Printf("Session idle timeout for WebUI-%d\n", chatID)
				sink.SendStatus("⏱️ Session timed out (30min idle)")
				return
			}
		}
	}
}

func (s *WebUIServer) cleanup(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, exists := s.sessions[chatID]; exists && session.Active {
		session.Active = false
		session.Terminal.Close()
		delete(s.sessions, chatID)
		log.Printf("Cleaned up session for WebUI-%d\n", chatID)
	}
}

func (s *WebUIServer) Start(port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/setup-password", s.handleSetupPassword)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/ws", s.handleWebSocket)

	addr := fmt.Sprintf("localhost:%d", port)
	log.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	log.Printf("🌐 WebUI started: http://%s\n", addr)
	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("WebUI server error: %v\n", err)
	}
}

// handleRoot serves the appropriate page based on auth state
func (s *WebUIServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	// No password set yet → setup page
	if s.config == nil || s.config.WebUIPasswordHash == "" {
		fmt.Fprint(w, setupPasswordHTML)
		return
	}

	// Not authenticated → login page
	if !s.isAuthenticated(r) {
		fmt.Fprint(w, loginHTML)
		return
	}

	// Authenticated → terminal
	fmt.Fprint(w, htmlContent)
}

// handleSetupPassword creates the initial password (first-time setup)
func (s *WebUIServer) handleSetupPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Block if password already set
	if s.config != nil && s.config.WebUIPasswordHash != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	if password == "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, setupPasswordHTMLWithError("Password cannot be empty"))
		return
	}

	if password != confirm {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, setupPasswordHTMLWithError("Passwords do not match"))
		return
	}

	// Hash with bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Initialize config if nil
	if s.config == nil {
		s.config = &Config{}
	}
	s.config.WebUIPasswordHash = string(hash)

	// Save to disk
	if err := saveConfig(s.config); err != nil {
		log.Printf("Warning: could not save config: %v", err)
	}

	// Auto-login: create session
	token := s.createAuthSession()
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLogin validates password and creates a session
func (s *WebUIServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	password := r.FormValue("password")

	if s.config == nil || s.config.WebUIPasswordHash == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	err := bcrypt.CompareHashAndPassword([]byte(s.config.WebUIPasswordHash), []byte(password))
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, loginHTMLWithError("Invalid password"))
		return
	}

	token := s.createAuthSession()
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLogout clears the session
func (s *WebUIServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		s.mu.Lock()
		delete(s.authSessions, cookie.Value)
		s.mu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// isAuthenticated checks whether the request has a valid session cookie
func (s *WebUIServer) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie("session")
	if err != nil {
		return false
	}

	s.mu.Lock()
	expiry, exists := s.authSessions[cookie.Value]
	s.mu.Unlock()

	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		// Expired — clean up
		s.mu.Lock()
		delete(s.authSessions, cookie.Value)
		s.mu.Unlock()
		return false
	}

	return true
}

// createAuthSession generates a crypto/rand session token and stores it
func (s *WebUIServer) createAuthSession() string {
	token := generateSessionToken()
	s.mu.Lock()
	s.authSessions[token] = time.Now().Add(24 * time.Hour)
	s.mu.Unlock()
	return token
}

// generateSessionToken creates a cryptographically random 32-byte hex token
func generateSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func setupPasswordHTMLWithError(errMsg string) string {
	errorBlock := ""
	if errMsg != "" {
		errorBlock = `<div class="error">` + errMsg + `</div>`
	}
	return `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Setup - Remote Terminal</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'SF Mono', 'Monaco', 'Courier New', monospace;
            background: #1a1a1a;
            color: #c0c0c0;
            height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card {
            background: #0a0a0a;
            border: 1px solid #333;
            border-radius: 8px;
            padding: 40px;
            width: 400px;
        }
        h1 { color: #00ff00; font-size: 18px; margin-bottom: 8px; }
        .subtitle { color: #888; font-size: 13px; margin-bottom: 24px; }
        label { display: block; margin-bottom: 6px; font-size: 13px; color: #888; }
        input[type="password"] {
            width: 100%;
            padding: 10px;
            background: #1a1a1a;
            border: 1px solid #333;
            border-radius: 4px;
            color: #c0c0c0;
            font-family: inherit;
            font-size: 14px;
            margin-bottom: 16px;
        }
        input[type="password"]:focus { outline: none; border-color: #00ff00; }
        button {
            width: 100%;
            padding: 10px;
            background: #00ff00;
            color: #0a0a0a;
            border: none;
            border-radius: 4px;
            font-family: inherit;
            font-size: 14px;
            font-weight: bold;
            cursor: pointer;
        }
        button:hover { background: #00cc00; }
        .error { background: #3a1010; border: 1px solid #ff4444; color: #ff6666; padding: 10px; border-radius: 4px; margin-bottom: 16px; font-size: 13px; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Create Password</h1>
        <div class="subtitle">Set a password for Remote Terminal WebUI</div>
        ` + errorBlock + `
        <form method="POST" action="/setup-password">
            <label for="password">Password</label>
            <input type="password" id="password" name="password" required autofocus>
            <label for="confirm">Confirm Password</label>
            <input type="password" id="confirm" name="confirm" required>
            <button type="submit">Set Password</button>
        </form>
    </div>
</body>
</html>`
}

var setupPasswordHTML = setupPasswordHTMLWithError("")

func loginHTMLWithError(errMsg string) string {
	errorBlock := ""
	if errMsg != "" {
		errorBlock = `<div class="error">` + errMsg + `</div>`
	}
	return `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Login - Remote Terminal</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'SF Mono', 'Monaco', 'Courier New', monospace;
            background: #1a1a1a;
            color: #c0c0c0;
            height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .card {
            background: #0a0a0a;
            border: 1px solid #333;
            border-radius: 8px;
            padding: 40px;
            width: 400px;
        }
        h1 { color: #00ff00; font-size: 18px; margin-bottom: 8px; }
        .subtitle { color: #888; font-size: 13px; margin-bottom: 24px; }
        label { display: block; margin-bottom: 6px; font-size: 13px; color: #888; }
        input[type="password"] {
            width: 100%;
            padding: 10px;
            background: #1a1a1a;
            border: 1px solid #333;
            border-radius: 4px;
            color: #c0c0c0;
            font-family: inherit;
            font-size: 14px;
            margin-bottom: 16px;
        }
        input[type="password"]:focus { outline: none; border-color: #00ff00; }
        button {
            width: 100%;
            padding: 10px;
            background: #00ff00;
            color: #0a0a0a;
            border: none;
            border-radius: 4px;
            font-family: inherit;
            font-size: 14px;
            font-weight: bold;
            cursor: pointer;
        }
        button:hover { background: #00cc00; }
        .error { background: #3a1010; border: 1px solid #ff4444; color: #ff6666; padding: 10px; border-radius: 4px; margin-bottom: 16px; font-size: 13px; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Remote Terminal</h1>
        <div class="subtitle">Enter your password to continue</div>
        ` + errorBlock + `
        <form method="POST" action="/login">
            <label for="password">Password</label>
            <input type="password" id="password" name="password" required autofocus>
            <button type="submit">Login</button>
        </form>
    </div>
</body>
</html>`
}

var loginHTML = loginHTMLWithError("")

const htmlContent = `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Remote Terminal</title>
    <!-- xterm.js Terminal Emulator -->
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css" />
    <script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/xterm-addon-fit@0.8.0/lib/xterm-addon-fit.js"></script>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'SF Mono', 'Monaco', 'Courier New', monospace;
            background: #1a1a1a;
            color: #00ff00;
            height: 100vh;
            max-height: 100vh;
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }
        header {
            background: #0a0a0a;
            padding: 15px 20px;
            border-bottom: 2px solid #00ff00;
        }
        h1 { font-size: 18px; letter-spacing: 2px; }
        .status { 
            font-size: 12px; 
            color: #888; 
            margin-top: 5px;
        }
        .status.connected { color: #00ff00; }
        .status.disconnected { color: #ff0000; }
        
        main {
            flex: 1;
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }
        
        #terminal {
            flex: 1;
            overflow: hidden;
            padding: 10px;
            background: #0a0a0a;
            cursor: text;
        }
        
        ::-webkit-scrollbar {
            width: 10px;
        }
        
        ::-webkit-scrollbar-track {
            background: #0a0a0a;
        }
        
        ::-webkit-scrollbar-thumb {
            background: #333;
        }
        
        ::-webkit-scrollbar-thumb:hover {
            background: #00ff00;
        }
    </style>
</head>
<body>
    <header>
        <h1>REMOTE TERMINAL</h1>
        <div class="status" id="status">Connecting...</div>
    </header>
    
    <main>
        <div id="terminal"></div>
    </main>
    
    <script>
        let ws = null;
        let chatId = null;
        let term = null;
        let fitAddon = null;
        const statusEl = document.getElementById('status');

        // Initialize xterm.js terminal
        function initTerminal() {
            term = new Terminal({
                cursorBlink: true,
                cursorStyle: 'block',
                fontSize: 14,
                fontFamily: "'SF Mono', 'Monaco', 'Courier New', monospace",
                theme: {
                    background: '#0a0a0a',
                    foreground: '#00ff00',
                    cursor: '#00ff00',
                    cursorAccent: '#1a1a1a',
                    selection: 'rgba(0, 255, 0, 0.3)',
                    black: '#000000',
                    brightBlack: '#808080',
                    red: '#ff0000',
                    brightRed: '#ff6666',
                    green: '#00ff00',
                    brightGreen: '#66ff66',
                    yellow: '#ffaa00',
                    brightYellow: '#ffdd66',
                    blue: '#0066ff',
                    brightBlue: '#6699ff',
                    magenta: '#ff00ff',
                    brightMagenta: '#ff66ff',
                    cyan: '#00ffff',
                    brightCyan: '#66ffff',
                    white: '#ffffff',
                    brightWhite: '#ffffff'
                },
                rows: 50,
                cols: 120,
                scrollback: 10000,
                allowTransparency: false,
                screenReaderMode: false,
                // Better TUI support
                allowProposedApi: true,
                windowsMode: false,
                macOptionIsMeta: true,
                altClickMovesCursor: false
            });

            // Add FitAddon for responsive sizing
            fitAddon = new FitAddon.FitAddon();
            term.loadAddon(fitAddon);

            // Open terminal in container
            term.open(document.getElementById('terminal'));
            fitAddon.fit();

            // Handle window resize and communicate to backend
            window.addEventListener('resize', () => {
                if (fitAddon) {
                    fitAddon.fit();

                    // Send terminal size to backend
                    if (ws && ws.readyState === WebSocket.OPEN) {
                        ws.send(JSON.stringify({
                            type: 'resize',
                            rows: term.rows,
                            cols: term.cols
                        }));
                    }
                }
            });

            // No welcome banner - keep terminal clean for TUI apps like Claude Code
            // that use absolute cursor positioning

            // Enable direct keyboard input with buffering for smooth TUI experience
            let inputBuffer = '';
            let inputTimer = null;

            term.onData((data) => {
                if (ws && ws.readyState === WebSocket.OPEN) {
                    // Buffer rapid keystrokes to keep TUI apps in sync
                    inputBuffer += data;

                    // Clear existing timer
                    if (inputTimer) {
                        clearTimeout(inputTimer);
                    }

                    // Send buffered input after 10ms of no typing (or immediately for Enter/special keys)
                    const shouldSendImmediately = data === '\r' || data === '\n' || data.charCodeAt(0) < 32;

                    if (shouldSendImmediately) {
                        // Send immediately for Enter and control characters
                        ws.send(JSON.stringify({
                            type: 'input',
                            content: inputBuffer
                        }));
                        inputBuffer = '';
                    } else {
                        // Buffer regular typing for 10ms
                        inputTimer = setTimeout(() => {
                            if (inputBuffer) {
                                ws.send(JSON.stringify({
                                    type: 'input',
                                    content: inputBuffer
                                }));
                                inputBuffer = '';
                            }
                        }, 10);
                    }
                }
            });

            // Auto-focus terminal when clicked
            document.getElementById('terminal').addEventListener('click', () => {
                term.focus();
            });

            // Auto-focus terminal on load
            term.focus();
        }

        function connect() {
            const wsUrl = 'ws://' + window.location.host + '/ws';
            ws = new WebSocket(wsUrl);

            ws.onopen = () => {
                statusEl.textContent = '✅ Connected';
                statusEl.className = 'status connected';

                // Immediately sync terminal size with backend PTY
                // This must happen before any interaction so Claude Code
                // gets the correct dimensions for cursor positioning
                if (term && fitAddon) {
                    fitAddon.fit();
                    ws.send(JSON.stringify({
                        type: 'resize',
                        rows: term.rows,
                        cols: term.cols
                    }));
                }
            };

            ws.onclose = () => {
                statusEl.textContent = '❌ Disconnected - Refresh to reconnect';
                statusEl.className = 'status disconnected';

                if (term) {
                    term.writeln('\r\n\x1b[31m❌ WebSocket disconnected - Refresh page\x1b[0m\r\n');
                }
            };

            ws.onerror = (error) => {
                console.error('WebSocket error:', error);
                if (term) {
                    term.writeln('\r\n\x1b[31m❌ WebSocket error\x1b[0m\r\n');
                }
            };

            ws.onmessage = (event) => {
                const msg = JSON.parse(event.data);

                if (msg.chatId && !chatId) {
                    chatId = msg.chatId;
                }

                if (msg.type === 'output') {
                    // Write raw ANSI output directly to xterm.js
                    term.write(msg.content);
                } else if (msg.type === 'status') {
                    // Status messages in yellow
                    term.writeln('\r\n\x1b[33m' + msg.content + '\x1b[0m\r\n');
                } else if (msg.type === 'error') {
                    // Error messages in red with newlines
                    term.writeln('\r\n\x1b[31m' + msg.content + '\x1b[0m\r\n');
                }
            };
        }

        // Initialize terminal and connect on load
        initTerminal();
        connect();
    </script>
</body>
</html>
`
