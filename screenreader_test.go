package main

import (
	"strings"
	"testing"
	"time"
)

// --- ScreenReader Basic Tests ---

// TestScreenReaderPlainText verifies simple text appears in screen
func TestScreenReaderPlainText(t *testing.T) {
	sr := NewScreenReader(80, 24)
	sr.WriteString("Hello, World!")

	screen := sr.Screen()
	if !strings.Contains(screen, "Hello, World!") {
		t.Errorf("Screen missing text. Got: %q", screen)
	}
}

// TestScreenReaderMultipleLines verifies line-by-line output
func TestScreenReaderMultipleLines(t *testing.T) {
	sr := NewScreenReader(80, 24)
	sr.WriteString("Line 1\r\nLine 2\r\nLine 3")

	screen := sr.Screen()
	if !strings.Contains(screen, "Line 1") {
		t.Error("Missing Line 1")
	}
	if !strings.Contains(screen, "Line 2") {
		t.Error("Missing Line 2")
	}
	if !strings.Contains(screen, "Line 3") {
		t.Error("Missing Line 3")
	}
}

// TestScreenReaderANSIColorsStripped verifies colors don't appear in text output
func TestScreenReaderANSIColorsStripped(t *testing.T) {
	sr := NewScreenReader(80, 24)
	sr.WriteString("\x1b[31mRed text\x1b[0m Normal text")

	screen := sr.Screen()
	if !strings.Contains(screen, "Red text") {
		t.Error("Missing colored text content")
	}
	if !strings.Contains(screen, "Normal text") {
		t.Error("Missing normal text content")
	}
	// ANSI codes should not appear in the text
	if strings.Contains(screen, "\x1b[") {
		t.Error("ANSI escape codes leaked into screen text")
	}
}

// TestScreenReaderCursorPositioning verifies cursor moves compose text correctly
// This is THE critical test — it's why we use VTE instead of cleanANSI
func TestScreenReaderCursorPositioning(t *testing.T) {
	sr := NewScreenReader(80, 24)

	// Move to row 1, col 1 and write "Hello"
	sr.WriteString("\x1b[1;1HHello")
	// Move to row 2, col 1 and write "World"
	sr.WriteString("\x1b[2;1HWorld")

	screen := sr.Screen()
	if !strings.Contains(screen, "Hello") {
		t.Error("Missing 'Hello' from cursor-positioned text")
	}
	if !strings.Contains(screen, "World") {
		t.Error("Missing 'World' from cursor-positioned text")
	}

	// Compare with cleanANSI which would lose the positioning
	raw := "\x1b[1;1HHello\x1b[2;1HWorld"
	cleaned := cleanANSI(raw)
	t.Logf("VTE screen: %q", screen)
	t.Logf("cleanANSI:  %q", cleaned)

	// VTE should produce better output than cleanANSI
	if !strings.Contains(screen, "Hello") || !strings.Contains(screen, "World") {
		t.Error("VTE failed to compose cursor-positioned text")
	}
}

// TestScreenReaderCursorRelativeMovement verifies relative cursor moves
func TestScreenReaderCursorRelativeMovement(t *testing.T) {
	sr := NewScreenReader(80, 24)

	// Write "AB" then move cursor left 1 and overwrite with "X"
	sr.WriteString("AB\x1b[1DX")

	screen := sr.Screen()
	// Should show "AX" (B overwritten by X)
	if !strings.Contains(screen, "AX") {
		t.Errorf("Relative cursor move failed. Expected 'AX', got: %q", screen)
	}
}

// TestScreenReaderClearScreen verifies screen clear
func TestScreenReaderClearScreen(t *testing.T) {
	sr := NewScreenReader(80, 24)

	sr.WriteString("Old content")
	sr.WriteString("\x1b[2J\x1b[H") // Clear screen and home cursor
	sr.WriteString("New content")

	screen := sr.Screen()
	if strings.Contains(screen, "Old content") {
		t.Error("Old content still visible after screen clear")
	}
	if !strings.Contains(screen, "New content") {
		t.Error("New content missing after screen clear")
	}
}

// TestScreenReaderAlternateScreenBuffer verifies alt screen buffer support
// Claude Code uses this when starting up
func TestScreenReaderAlternateScreenBuffer(t *testing.T) {
	sr := NewScreenReader(80, 24)

	// Write to main screen
	sr.WriteString("Main screen content")

	// Switch to alternate screen
	sr.WriteString("\x1b[?1049h")
	sr.WriteString("Alternate content")

	screen := sr.Screen()
	if !strings.Contains(screen, "Alternate content") {
		t.Error("Alternate screen content not visible")
	}

	// Switch back to main screen
	sr.WriteString("\x1b[?1049l")
	screen = sr.Screen()
	if !strings.Contains(screen, "Main screen content") {
		t.Error("Main screen content not restored after leaving alt screen")
	}
}

// --- Diff Tests ---

// TestScreenReaderDiffFirstCall verifies first diff returns full content
func TestScreenReaderDiffFirstCall(t *testing.T) {
	sr := NewScreenReader(80, 24)
	sr.WriteString("Hello World")

	diff := sr.Diff()
	if !strings.Contains(diff, "Hello World") {
		t.Errorf("First diff should return full content. Got: %q", diff)
	}
}

// TestScreenReaderDiffNoChange verifies no diff when nothing changed
func TestScreenReaderDiffNoChange(t *testing.T) {
	sr := NewScreenReader(80, 24)
	sr.WriteString("Static content")

	// First call captures current state
	sr.Diff()

	// Second call without changes should return empty
	diff := sr.Diff()
	if diff != "" {
		t.Errorf("Expected empty diff, got: %q", diff)
	}
}

// TestScreenReaderDiffNewContent verifies diff captures new content
func TestScreenReaderDiffNewContent(t *testing.T) {
	sr := NewScreenReader(80, 24)
	sr.WriteString("Line 1\r\n")

	// Capture first state
	sr.Diff()

	// Add new content
	sr.WriteString("Line 2\r\n")
	diff := sr.Diff()

	if !strings.Contains(diff, "Line 2") {
		t.Errorf("Diff should contain new content 'Line 2'. Got: %q", diff)
	}
}

// TestScreenReaderDiffReset verifies reset makes next diff return full content
func TestScreenReaderDiffReset(t *testing.T) {
	sr := NewScreenReader(80, 24)
	sr.WriteString("Content here")

	sr.Diff() // Consume first diff
	sr.Reset()

	diff := sr.Diff()
	if !strings.Contains(diff, "Content here") {
		t.Errorf("After reset, diff should return full content. Got: %q", diff)
	}
}

// --- Resize Tests ---

func TestScreenReaderResize(t *testing.T) {
	sr := NewScreenReader(80, 24)
	sr.WriteString("Before resize")

	// Should not panic
	sr.Resize(120, 50)

	sr.WriteString("\r\nAfter resize")
	screen := sr.Screen()
	if !strings.Contains(screen, "After resize") {
		t.Error("Content missing after resize")
	}
}

// --- Integration with Real PTY ---

// TestScreenReaderWithRealPTY verifies VTE correctly reads real terminal output
func TestScreenReaderWithRealPTY(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	sr := NewScreenReader(120, 50)

	// Send a command
	term.SendCommand("echo 'VTE_TEST_OUTPUT'")
	time.Sleep(500 * time.Millisecond)

	// Feed PTY output into VTE
	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case data, ok := <-term.outputChan:
			if !ok {
				break drain
			}
			sr.Write([]byte(data))
		case <-timeout:
			break drain
		}
	}

	screen := sr.Screen()
	t.Logf("VTE screen: %q", screen)

	if !strings.Contains(screen, "VTE_TEST_OUTPUT") {
		t.Errorf("Real PTY output not visible in VTE. Got: %q", screen)
	}
}

// TestScreenReaderWithColoredPTYOutput verifies colored ls output renders correctly
func TestScreenReaderWithColoredPTYOutput(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	sr := NewScreenReader(120, 50)

	// Generate colored output
	term.SendCommand("echo -e '\\x1b[31mRED\\x1b[0m \\x1b[32mGREEN\\x1b[0m'")
	time.Sleep(500 * time.Millisecond)

	timeout := time.After(2 * time.Second)
drain:
	for {
		select {
		case data, ok := <-term.outputChan:
			if !ok {
				break drain
			}
			sr.Write([]byte(data))
		case <-timeout:
			break drain
		}
	}

	screen := sr.Screen()
	t.Logf("Colored output via VTE: %q", screen)

	if !strings.Contains(screen, "RED") || !strings.Contains(screen, "GREEN") {
		t.Errorf("Colored text not visible in VTE output")
	}
	// Should NOT contain raw ANSI codes
	if strings.Contains(screen, "\x1b[") {
		t.Error("Raw ANSI codes leaked into screen text")
	}
}

// --- diffScreens Unit Tests ---

func TestDiffScreensEmpty(t *testing.T) {
	result := diffScreens("", "Hello")
	if result != "Hello" {
		t.Errorf("Expected full content when old is empty. Got: %q", result)
	}
}

func TestDiffScreensIdentical(t *testing.T) {
	result := diffScreens("Hello", "Hello")
	if result != "" {
		t.Errorf("Expected empty diff for identical screens. Got: %q", result)
	}
}

func TestDiffScreensNewLines(t *testing.T) {
	old := "Line 1\nLine 2"
	current := "Line 1\nLine 2\nLine 3"

	result := diffScreens(old, current)
	if !strings.Contains(result, "Line 3") {
		t.Errorf("Expected new line in diff. Got: %q", result)
	}
}

func TestDiffScreensChangedLine(t *testing.T) {
	old := "Line 1\nLine 2"
	current := "Line 1\nLine 2 MODIFIED"

	result := diffScreens(old, current)
	if !strings.Contains(result, "MODIFIED") {
		t.Errorf("Expected changed line in diff. Got: %q", result)
	}
}

// --- E2E: One-shot command through VTE ---

// TestOneShotCommandVTE verifies one-shot commands work with VTE
func TestOneShotCommandVTE(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	term.SendCommand("echo 'ONE_SHOT_VTE_TEST'")
	term.StreamOutput()

	allOutput := strings.Join(sink.Outputs, "")
	if !strings.Contains(allOutput, "ONE_SHOT_VTE_TEST") {
		t.Errorf("One-shot VTE output missing. Got: %q", allOutput)
	}
	// Should not contain ANSI codes
	if strings.Contains(allOutput, "\x1b[") {
		t.Error("Raw ANSI codes in one-shot output — VTE should have interpreted them")
	}
}
