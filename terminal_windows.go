//go:build windows

package main

import (
	"os/exec"
	"strconv"
	"time"
)

// getShell returns the shell command and arguments for Windows
func getShell() (string, []string) {
	// Prefer PowerShell if available
	if _, err := exec.LookPath("powershell.exe"); err == nil {
		return "powershell.exe", []string{"-NoProfile", "-NoLogo"}
	}
	return "cmd.exe", []string{}
}

// setProcAttr is a no-op on Windows — ConPTY handles terminal setup
func setProcAttr(cmd *exec.Cmd) {
	// No Unix-specific TTY attributes needed on Windows
}

// killProcessGroup terminates the process tree on Windows using taskkill
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}

	// taskkill /F (force) /T (tree — kill children too) /PID <pid>
	kill := exec.Command("taskkill", "/F", "/T", "/PID",
		strconv.Itoa(cmd.Process.Pid))
	kill.Run()

	time.Sleep(100 * time.Millisecond)

	// Ensure process is killed
	cmd.Process.Kill()
}
