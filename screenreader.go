package main

import (
	"strings"

	"github.com/charmbracelet/x/vt"
)

// ScreenReader wraps a virtual terminal emulator to interpret ANSI escape
// sequences and read the composed screen as plain text. This solves the
// problem of TUI applications (Claude Code, vim, htop) using cursor
// positioning — instead of stripping ANSI codes (which destroys the layout),
// we emulate a terminal and read what a human would see.
type ScreenReader struct {
	emu        *vt.SafeEmulator
	lastScreen string // Track last-sent screen for diffing
}

// NewScreenReader creates a virtual terminal with the given dimensions.
// Dimensions should match the PTY size for correct cursor positioning.
func NewScreenReader(cols, rows int) *ScreenReader {
	return &ScreenReader{
		emu: vt.NewSafeEmulator(cols, rows),
	}
}

// Write feeds raw PTY output into the virtual terminal.
// The VTE processes ANSI sequences (cursor moves, colors, clears)
// and updates its internal screen buffer.
func (sr *ScreenReader) Write(data []byte) (int, error) {
	return sr.emu.Write(data)
}

// WriteString feeds a string of raw PTY output into the virtual terminal.
func (sr *ScreenReader) WriteString(s string) (int, error) {
	return sr.emu.Write([]byte(s))
}

// Screen returns the current screen content as plain text.
// Trailing whitespace is trimmed from each line and trailing empty lines
// are removed. This is what a human would see on a terminal.
func (sr *ScreenReader) Screen() string {
	raw := sr.emu.String()

	// The VTE buffer includes all rows (even empty ones).
	// Trim trailing empty lines to get just the visible content.
	lines := strings.Split(raw, "\n")
	lastNonEmpty := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimRight(lines[i], " \t\r") != "" {
			lastNonEmpty = i
			break
		}
	}

	if lastNonEmpty < 0 {
		return ""
	}

	// Trim trailing whitespace from each line
	trimmed := make([]string, lastNonEmpty+1)
	for i := 0; i <= lastNonEmpty; i++ {
		trimmed[i] = strings.TrimRight(lines[i], " \t\r")
	}

	return strings.Join(trimmed, "\n")
}

// Diff returns only the new content since the last call to Diff.
// On first call, returns the full screen.
// Returns empty string if nothing changed.
func (sr *ScreenReader) Diff() string {
	current := sr.Screen()

	if current == sr.lastScreen {
		return ""
	}

	// Find new content by comparing line-by-line
	diff := diffScreens(sr.lastScreen, current)
	sr.lastScreen = current
	return diff
}

// Reset clears the last-sent screen state, so the next Diff returns full content.
func (sr *ScreenReader) Reset() {
	sr.lastScreen = ""
}

// Resize changes the virtual terminal dimensions.
func (sr *ScreenReader) Resize(cols, rows int) {
	sr.emu.Resize(cols, rows)
}

// diffScreens extracts new/changed lines between two screen states.
// Returns only the lines that are different or new.
func diffScreens(old, current string) string {
	if old == "" {
		return current
	}

	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(current, "\n")

	var diff []string
	changed := false

	for i, line := range newLines {
		if i >= len(oldLines) || line != oldLines[i] {
			diff = append(diff, line)
			changed = true
		}
	}

	if !changed {
		return ""
	}

	result := strings.Join(diff, "\n")
	return strings.TrimSpace(result)
}
