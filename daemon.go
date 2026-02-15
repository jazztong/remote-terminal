//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
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

// writePIDFile writes the given PID to the PID file.
func writePIDFile(pid int) error {
	return os.WriteFile(pidFilePath(), []byte(strconv.Itoa(pid)), 0644)
}

// readPIDFile reads and parses the PID from the PID file.
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

// removePIDFile removes the PID file, ignoring errors (best-effort cleanup).
func removePIDFile() {
	os.Remove(pidFilePath())
}

// isProcessAlive checks whether a process with the given PID is still running.
// Uses the Unix convention of sending signal 0 to test for process existence.
func isProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

// daemonize starts the current program as a background daemon process.
// It re-executes the binary with --daemon-child instead of --daemon,
// detaches from the terminal using setsid, and redirects output to a log file.
func daemonize(extraArgs []string) {
	// Check if a daemon is already running
	if pid, err := readPIDFile(); err == nil {
		if isProcessAlive(pid) {
			fmt.Printf("Daemon is already running (PID %d).\n", pid)
			fmt.Println("Use --stop to stop it first.")
			os.Exit(1)
		}
		// Stale PID file from a previous run — clean it up
		removePIDFile()
	}

	// Daemon mode cannot do first-time setup (requires interactive terminal)
	configPath := getConfigPath()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Println("Error: No configuration found.")
		fmt.Println("Run the program interactively first to complete setup,")
		fmt.Printf("then use --daemon to run in the background.\n")
		os.Exit(1)
	}

	// Open log file for daemon output
	logPath := logFilePath()
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Error: Cannot open log file %s: %v\n", logPath, err)
		os.Exit(1)
	}

	// Build child command: replace --daemon with --daemon-child
	args := []string{"--daemon-child"}
	args = append(args, extraArgs...)

	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Detach from controlling terminal
	}

	if err := cmd.Start(); err != nil {
		fmt.Printf("Error: Failed to start daemon: %v\n", err)
		logFile.Close()
		os.Exit(1)
	}

	// Write PID file
	if err := writePIDFile(cmd.Process.Pid); err != nil {
		fmt.Printf("Warning: Failed to write PID file: %v\n", err)
	}

	fmt.Printf("Daemon started (PID %d).\n", cmd.Process.Pid)
	fmt.Printf("Log file: %s\n", logPath)
	fmt.Printf("PID file: %s\n", pidFilePath())
	fmt.Println()
	fmt.Println("Use --status to check status, --stop to stop.")

	logFile.Close()
	os.Exit(0)
}

// daemonStop sends SIGTERM to the running daemon and waits for it to exit.
func daemonStop() {
	pid, err := readPIDFile()
	if err != nil {
		fmt.Println("No daemon is running (PID file not found).")
		return
	}

	if !isProcessAlive(pid) {
		fmt.Printf("Daemon (PID %d) is not running. Removing stale PID file.\n", pid)
		removePIDFile()
		return
	}

	fmt.Printf("Stopping daemon (PID %d)...\n", pid)

	// Send SIGTERM for graceful shutdown
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		fmt.Printf("Error sending SIGTERM: %v\n", err)
		return
	}

	// Wait up to 5 seconds for the process to exit
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			fmt.Println("Daemon stopped.")
			removePIDFile()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Process still alive after 5s — force kill
	fmt.Println("Daemon did not stop gracefully. Sending SIGKILL...")
	syscall.Kill(pid, syscall.SIGKILL)
	time.Sleep(500 * time.Millisecond)

	if !isProcessAlive(pid) {
		fmt.Println("Daemon killed.")
	} else {
		fmt.Printf("Warning: Failed to kill daemon (PID %d).\n", pid)
	}
	removePIDFile()
}

// daemonStatus prints the current status of the daemon.
func daemonStatus() {
	pid, err := readPIDFile()
	if err != nil {
		fmt.Println("Status: Not running (no PID file).")
		return
	}

	if isProcessAlive(pid) {
		fmt.Printf("Status: Running (PID %d)\n", pid)
		fmt.Printf("PID file: %s\n", pidFilePath())
		fmt.Printf("Log file: %s\n", logFilePath())
	} else {
		fmt.Printf("Status: Not running (stale PID %d)\n", pid)
		removePIDFile()
	}
}
