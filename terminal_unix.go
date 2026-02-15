//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// getShell returns the shell command and arguments for Unix systems
func getShell() (string, []string) {
	shellCmd := "/bin/bash"
	shellArgs := []string{"--norc", "--noprofile"}
	if _, err := os.Stat(shellCmd); err != nil {
		shellCmd = "/bin/sh"
		shellArgs = []string{}
	}
	return shellCmd, shellArgs
}

// setProcAttr sets Unix-specific process attributes for TTY support
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,  // Create new session (TTY requirement)
		Setctty: true,  // Make this the controlling terminal
	}
}

// killProcessGroup terminates the process and its children using Unix signals.
// Since we used Setsid, killing the negative PID targets the entire session group.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil || cmd.Process.Pid <= 0 {
		return
	}

	// Send SIGHUP to the session group — proper TTY termination
	syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)

	// Give processes time to cleanup
	time.Sleep(100 * time.Millisecond)

	// Force kill if still alive
	cmd.Process.Signal(syscall.SIGTERM)
	time.Sleep(50 * time.Millisecond)
	cmd.Process.Kill()
}
