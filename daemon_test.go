//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// setTempConfigPath sets configPathOverride to a temp directory and registers cleanup.
// Returns the temp directory path (the parent of the fake config file).
func setTempConfigPath(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	configPathOverride = filepath.Join(tmpDir, "config.json")
	t.Cleanup(func() {
		configPathOverride = ""
	})
	return tmpDir
}

// ---------------------------------------------------------------------------
// PID file lifecycle
// ---------------------------------------------------------------------------

// TestWriteAndReadPIDFile writes a PID to the PID file, reads it back, and
// verifies the value matches.
func TestWriteAndReadPIDFile(t *testing.T) {
	setTempConfigPath(t)

	wantPID := os.Getpid()
	if err := writePIDFile(wantPID); err != nil {
		t.Fatalf("writePIDFile(%d) error: %v", wantPID, err)
	}

	gotPID, err := readPIDFile()
	if err != nil {
		t.Fatalf("readPIDFile() error: %v", err)
	}

	if gotPID != wantPID {
		t.Errorf("readPIDFile() = %d, want %d", gotPID, wantPID)
	}

	// Verify the file on disk contains the expected value.
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error: %v", pidFilePath(), err)
	}
	if string(data) != strconv.Itoa(wantPID) {
		t.Errorf("PID file contents = %q, want %q", string(data), strconv.Itoa(wantPID))
	}
}

// TestReadPIDFileNotFound verifies that reading a non-existent PID file
// returns an error.
func TestReadPIDFileNotFound(t *testing.T) {
	setTempConfigPath(t)

	_, err := readPIDFile()
	if err == nil {
		t.Error("readPIDFile() expected error for non-existent PID file, got nil")
	}
}

// TestRemovePIDFile writes a PID file, removes it, and verifies the file is
// gone from disk.
func TestRemovePIDFile(t *testing.T) {
	setTempConfigPath(t)

	if err := writePIDFile(12345); err != nil {
		t.Fatalf("writePIDFile() error: %v", err)
	}

	// Confirm it exists before removal.
	if _, err := os.Stat(pidFilePath()); os.IsNotExist(err) {
		t.Fatal("PID file was not created")
	}

	removePIDFile()

	if _, err := os.Stat(pidFilePath()); !os.IsNotExist(err) {
		t.Error("PID file still exists after removePIDFile()")
	}
}

// TestRemovePIDFileNonExistent verifies that removing a non-existent PID file
// does not panic or produce unexpected side effects.
func TestRemovePIDFileNonExistent(t *testing.T) {
	setTempConfigPath(t)

	// Should not panic -- removePIDFile is best-effort.
	removePIDFile()
}

// ---------------------------------------------------------------------------
// Process alive check
// ---------------------------------------------------------------------------

// TestIsProcessAlive verifies that the current process (which is obviously
// running) is detected as alive.
func TestIsProcessAlive(t *testing.T) {
	pid := os.Getpid()
	if !isProcessAlive(pid) {
		t.Errorf("isProcessAlive(%d) = false, want true (current process)", pid)
	}
}

// TestIsProcessAliveDeadProcess verifies that a non-existent PID is detected
// as not alive. PID 2147483647 (max int32) is extremely unlikely to be in use.
func TestIsProcessAliveDeadProcess(t *testing.T) {
	// Use a very large PID that is almost certainly not running.
	deadPID := 2147483647
	if isProcessAlive(deadPID) {
		t.Errorf("isProcessAlive(%d) = true, want false (dead/non-existent process)", deadPID)
	}
}

// ---------------------------------------------------------------------------
// Config dir paths
// ---------------------------------------------------------------------------

// TestPidFilePath verifies pidFilePath returns the expected path under the
// config directory.
func TestPidFilePath(t *testing.T) {
	tmpDir := setTempConfigPath(t)

	got := pidFilePath()
	want := filepath.Join(tmpDir, "remote-term.pid")

	if got != want {
		t.Errorf("pidFilePath() = %s, want %s", got, want)
	}
}

// TestLogFilePath verifies logFilePath returns the expected path under the
// config directory.
func TestLogFilePath(t *testing.T) {
	tmpDir := setTempConfigPath(t)

	got := logFilePath()
	want := filepath.Join(tmpDir, "remote-term.log")

	if got != want {
		t.Errorf("logFilePath() = %s, want %s", got, want)
	}
}

// TestGetConfigDirWithOverride verifies that getConfigDir respects
// configPathOverride and returns the parent directory of the override path.
func TestGetConfigDirWithOverride(t *testing.T) {
	tmpDir := setTempConfigPath(t)

	got := getConfigDir()
	if got != tmpDir {
		t.Errorf("getConfigDir() = %s, want %s", got, tmpDir)
	}
}

// TestGetConfigDirDefault verifies that when configPathOverride is empty,
// getConfigDir falls back to ~/.telegram-terminal.
func TestGetConfigDirDefault(t *testing.T) {
	// Ensure override is empty for this test.
	oldOverride := configPathOverride
	configPathOverride = ""
	t.Cleanup(func() { configPathOverride = oldOverride })

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home directory: %v", err)
	}

	got := getConfigDir()
	want := filepath.Join(home, ".telegram-terminal")

	if got != want {
		t.Errorf("getConfigDir() = %s, want %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// PID file content edge cases
// ---------------------------------------------------------------------------

// TestReadPIDFileInvalidContent verifies that readPIDFile returns an error
// when the file contains non-numeric content.
func TestReadPIDFileInvalidContent(t *testing.T) {
	tmpDir := setTempConfigPath(t)

	pidPath := filepath.Join(tmpDir, "remote-term.pid")
	if err := os.WriteFile(pidPath, []byte("not-a-number"), 0644); err != nil {
		t.Fatalf("failed to write invalid PID file: %v", err)
	}

	_, err := readPIDFile()
	if err == nil {
		t.Error("readPIDFile() expected error for invalid PID content, got nil")
	}
}

// TestWritePIDFileOverwrite verifies that writing a PID file twice overwrites
// the previous value.
func TestWritePIDFileOverwrite(t *testing.T) {
	setTempConfigPath(t)

	if err := writePIDFile(100); err != nil {
		t.Fatalf("writePIDFile(100) error: %v", err)
	}

	if err := writePIDFile(200); err != nil {
		t.Fatalf("writePIDFile(200) error: %v", err)
	}

	gotPID, err := readPIDFile()
	if err != nil {
		t.Fatalf("readPIDFile() error: %v", err)
	}

	if gotPID != 200 {
		t.Errorf("readPIDFile() = %d, want 200 (overwritten value)", gotPID)
	}
}

// ---------------------------------------------------------------------------
// Integration: daemonize guard
// ---------------------------------------------------------------------------

// TestDaemonizeNoConfig verifies that daemonize() detects a missing config
// file. We cannot actually test the full daemonize (it calls os.Exit), but we
// can verify the guard condition that getConfigPath() + os.Stat detects a
// missing config -- which is the same check daemonize() performs before
// attempting to fork/exec.
func TestDaemonizeNoConfig(t *testing.T) {
	setTempConfigPath(t)

	// With a fresh temp dir and no config written, the config file should
	// not exist. This is the same condition daemonize() checks.
	configPath := getConfigPath()
	_, err := os.Stat(configPath)
	if !os.IsNotExist(err) {
		t.Errorf("expected config to not exist in fresh temp dir, got err: %v", err)
	}
}

// TestDaemonCleanupHookVariable verifies the daemonCleanupHook variable
// can be set and called without error (it is used by daemon child process).
func TestDaemonCleanupHookVariable(t *testing.T) {
	setTempConfigPath(t)

	// Write a PID file so removePIDFile has something to remove.
	if err := writePIDFile(99999); err != nil {
		t.Fatalf("writePIDFile() error: %v", err)
	}

	// Set the hook to removePIDFile (same as daemon child does).
	daemonCleanupHook = removePIDFile
	t.Cleanup(func() { daemonCleanupHook = nil })

	// Calling the hook should remove the PID file.
	daemonCleanupHook()

	if _, err := os.Stat(pidFilePath()); !os.IsNotExist(err) {
		t.Error("PID file should be removed after calling daemonCleanupHook")
	}
}
