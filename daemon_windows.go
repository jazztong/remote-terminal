//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// getConfigDir returns the path to the configuration directory (~/.telegram-terminal/).
// Respects configPathOverride for testing.
func getConfigDir() string {
	if configPathOverride != "" {
		return filepath.Dir(configPathOverride)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".telegram-terminal")
}

// pidFilePath returns the path to the PID file used by daemon mode.
func pidFilePath() string {
	return filepath.Join(getConfigDir(), "remote-term.pid")
}

// logFilePath returns the path to the log file used by daemon mode.
func logFilePath() string {
	return filepath.Join(getConfigDir(), "remote-term.log")
}

// writePIDFile writes the given PID to the PID file (no-op stub on Windows).
func writePIDFile(pid int) error {
	return os.WriteFile(pidFilePath(), []byte(strconv.Itoa(pid)), 0644)
}

// readPIDFile reads and parses the PID from the PID file (no-op stub on Windows).
func readPIDFile() (int, error) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, fmt.Errorf("failed to read PID file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}
	return pid, nil
}

// removePIDFile removes the PID file (best-effort cleanup).
func removePIDFile() {
	os.Remove(pidFilePath())
}

// isProcessAlive is a stub on Windows. Always returns false.
func isProcessAlive(pid int) bool {
	return false
}

// daemonize prints an unsupported message on Windows and exits.
func daemonize(extraArgs []string) {
	fmt.Println("Daemon mode is not supported on Windows.")
	fmt.Println("Use 'nohup remote-term &' or run as a Windows service.")
	os.Exit(1)
}

// daemonStop prints an unsupported message on Windows and exits.
func daemonStop() {
	fmt.Println("Daemon mode is not supported on Windows.")
	fmt.Println("Use 'nohup remote-term &' or run as a Windows service.")
	os.Exit(1)
}

// daemonStatus prints an unsupported message on Windows and exits.
func daemonStatus() {
	fmt.Println("Daemon mode is not supported on Windows.")
	fmt.Println("Use 'nohup remote-term &' or run as a Windows service.")
	os.Exit(1)
}
