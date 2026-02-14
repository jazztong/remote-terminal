package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// OutputSink is the interface for sending output (Telegram, console, mock, etc)
type OutputSink interface {
	SendOutput(output string)
}

// Terminal manages the PTY and streaming
type Terminal struct {
	ptmx       *os.File
	cmd        *exec.Cmd
	outputChan chan string
	sink       OutputSink
	done       chan struct{} // Signal to stop reading
}

// getCleanEnvironment returns environment variables filtered for clean terminal sessions
func getCleanEnvironment() []string {
	env := os.Environ()
	cleaned := make([]string, 0, len(env))

	// Filter out variables that should not be inherited by terminal sessions
	for _, e := range env {
		// Skip CLAUDECODE to allow nested Claude sessions
		if strings.HasPrefix(e, "CLAUDECODE=") {
			continue
		}
		// Skip other session-specific variables if needed
		// if strings.HasPrefix(e, "OTHER_VAR=") {
		//     continue
		// }
		cleaned = append(cleaned, e)
	}

	return cleaned
}

// NewTerminal creates a new terminal instance
func NewTerminal(sink OutputSink) (*Terminal, error) {
	// Determine shell
	shellCmd := "/bin/bash"
	shellArgs := []string{"--norc", "--noprofile"}
	if _, err := os.Stat(shellCmd); err != nil {
		shellCmd = "/bin/sh"
		shellArgs = []string{} // sh doesn't support --norc
	}

	// Start shell in PTY with full TTY environment
	// Use cleaned environment to allow independent sessions (e.g., Claude in browser while running in Claude)
	cmd := exec.Command(shellCmd, shellArgs...)
	cmd.Env = append(getCleanEnvironment(),
		// Terminal type and capabilities
		"TERM=xterm-256color",
		"COLORTERM=truecolor",

		// Shell configuration
		"PS1=", // Disable prompt

		// TTY environment
		"FORCE_COLOR=1",
		"CLICOLOR=1",
		"CLICOLOR_FORCE=1",

		// Disable problematic features
		"NO_UPDATE_NOTIFIER=1",
		"DISABLE_AUTO_UPDATE=1",

		// Interactive shell markers
		"INTERACTIVE=1",
		"IS_TTY=1",
	)
	
	// Enable TTY mode - allows proper signal handling
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,  // Create new session (TTY requirement)
		Setctty: true,  // Make this the controlling terminal
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	// Set terminal window size - crucial for interactive programs
	// Using generous dimensions for modern terminal applications
	ws := &pty.Winsize{
		Rows: 50,   // Height - enough for most interactive UIs
		Cols: 120,  // Width - standard wide terminal
		X:    0,    // Pixel width (optional)
		Y:    0,    // Pixel height (optional)
	}
	if err := pty.Setsize(ptmx, ws); err != nil {
		log.Printf("Warning: couldn't set terminal size: %v\n", err)
	}
	
	// Set terminal to raw mode for proper interactive handling
	// This allows programs to handle their own input/output processing
	// Note: We don't set the master PTY to raw since we're reading from it

	term := &Terminal{
		ptmx:       ptmx,
		cmd:        cmd,
		outputChan: make(chan string, 100),
		sink:       sink,
		done:       make(chan struct{}),
	}

	// Start reading output first
	go term.readOutput()

	// Small delay for shell to initialize
	time.Sleep(100 * time.Millisecond)
	
	// Note: We don't disable echo - some interactive programs (like Claude) need it
	// Shell prompts are handled by setting PS1="" in environment
	
	return term, nil
}

func (t *Terminal) readOutput() {
	// Large buffer for streaming responses from LLMs and interactive programs
	buf := make([]byte, 8192)
	
	for {
		select {
		case <-t.done:
			// Terminal closing, stop reading
			close(t.outputChan)
			return
		default:
			// Set read deadline for periodic done-channel checking
			// Longer timeout allows for better throughput on slow connections
			t.ptmx.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			
			n, err := t.ptmx.Read(buf)
			if err != nil {
				if err == io.EOF {
					// Terminal closed cleanly
					close(t.outputChan)
					return
				}
				// Timeout or temporary error - continue
				// This is normal for interactive programs with pauses
				continue
			}

			if n > 0 {
				output := string(buf[:n])
				select {
				case t.outputChan <- output:
					// Sent successfully
				case <-t.done:
					// Terminal closing while sending
					close(t.outputChan)
					return
				}
			}
		}
	}
}

// SendCommand sends a command to the terminal
func (t *Terminal) SendCommand(command string) {
	// Write text and Enter as SEPARATE PTY writes with a small delay.
	//
	// Why: TUI apps like Claude Code use Ink (React for CLI), whose input parser
	// splits chunks only on escape sequences (\x1b), not on control characters.
	// If "text\r" arrives in one read(), Ink sees it as a single event and
	// parseKeypress() checks `if (s === '\r')` — but s is "text\r", not "\r",
	// so Enter is never recognized. Sending them separately forces two read()
	// events: text goes to the input buffer, then \r triggers submission.
	//
	// For shell (cooked mode): the delay is harmless — the shell line-buffers
	// until it sees the newline (PTY translates \r → \n via ICRNL).
	t.ptmx.Write([]byte(command))
	time.Sleep(50 * time.Millisecond)
	t.ptmx.Write([]byte("\r"))
}

// SendRawInput sends raw input to the PTY without adding newline
// Used for character-by-character input from terminal emulator
func (t *Terminal) SendRawInput(input string) {
	t.ptmx.Write([]byte(input))
}

// Resize changes the PTY window size
func (t *Terminal) Resize(rows, cols int) error {
	ws := &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}
	return pty.Setsize(t.ptmx, ws)
}

// StreamOutput streams output to the sink with smart chunking.
// Uses a virtual terminal emulator to correctly interpret ANSI cursor
// positioning, so TUI program output renders as readable text.
func (t *Terminal) StreamOutput() {
	screen := NewScreenReader(120, 50)
	lastOutputTime := time.Now()
	hasNewData := false

	// Tunable parameters
	silenceThreshold := 1500 * time.Millisecond  // Send chunk after 1.5s silence
	finalSilenceThreshold := 3 * time.Second      // Stop after 3s total silence
	maxWaitTime := 30 * time.Second               // Max 30s total

	startTime := time.Now()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case output := <-t.outputChan:
			screen.Write([]byte(output))
			hasNewData = true
			lastOutputTime = time.Now()

		case <-ticker.C:
			// Send screen diff if output has settled
			if hasNewData && time.Since(lastOutputTime) > silenceThreshold {
				diff := screen.Diff()
				if diff != "" {
					t.sink.SendOutput(diff)
				}
				hasNewData = false
			}

			// Stop if max total time reached
			if time.Since(startTime) > maxWaitTime {
				if hasNewData {
					diff := screen.Diff()
					if diff != "" {
						t.sink.SendOutput(diff)
					}
				}
				return
			}

			// Stop only after long silence with no pending data
			if !hasNewData && time.Since(lastOutputTime) > finalSilenceThreshold {
				return
			}
		}
	}
}

// Close closes the terminal and all child processes
func (t *Terminal) Close() {
	// Signal readOutput to stop
	select {
	case <-t.done:
		// Already closed
	default:
		close(t.done)
	}
	
	if t.cmd != nil && t.cmd.Process != nil {
		// Send SIGHUP to the session - proper TTY termination
		// Since we used Setsid, this kills the entire session
		if t.cmd.Process.Pid > 0 {
			// Kill the session group
			syscall.Kill(-t.cmd.Process.Pid, syscall.SIGHUP)
			
			// Give processes time to cleanup
			time.Sleep(100 * time.Millisecond)
			
			// Force kill if still alive
			t.cmd.Process.Signal(syscall.SIGTERM)
			time.Sleep(50 * time.Millisecond)
			t.cmd.Process.Kill()
		}
		
		t.cmd.Wait() // Clean up zombie
	}
	
	if t.ptmx != nil {
		t.ptmx.Close()
	}
	
	// outputChan is closed by readOutput() when it exits
}

// ConsoleSink writes output to console (for testing)
type ConsoleSink struct{}

func (c *ConsoleSink) SendOutput(output string) {
	log.Printf("OUTPUT:\n%s\n", output)
}

// MockSink captures output for testing
type MockSink struct {
	Outputs []string
}

func (m *MockSink) SendOutput(output string) {
	m.Outputs = append(m.Outputs, output)
	log.Printf("MOCK OUTPUT (%d bytes): %s", len(output), output)
}
