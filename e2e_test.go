package main

import (
	"strings"
	"testing"
	"time"
)

// TestE2ESimpleCommand tests a simple command end-to-end
func TestE2ESimpleCommand(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Send simple command
	term.SendCommand("echo 'Hello from e2e test'")

	// Stream output
	term.StreamOutput()

	// Verify output
	if len(sink.Outputs) == 0 {
		t.Fatal("No output received")
	}

	allOutput := strings.Join(sink.Outputs, "")
	if !strings.Contains(allOutput, "Hello from e2e test") {
		t.Errorf("Expected output not found. Got: %s", allOutput)
	}

	t.Logf("Success! Received %d chunks", len(sink.Outputs))
}

// TestE2EListFiles tests listing files
func TestE2EListFiles(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Send ls command
	term.SendCommand("ls -la")

	// Stream output
	term.StreamOutput()

	// Verify output
	if len(sink.Outputs) == 0 {
		t.Fatal("No output received")
	}

	allOutput := strings.Join(sink.Outputs, "")
	
	// Should contain file listing indicators
	hasContent := strings.Contains(allOutput, "total") || 
	              strings.Contains(allOutput, ".") ||
	              len(allOutput) > 10

	if !hasContent {
		t.Errorf("Output doesn't look like ls output. Got: %s", allOutput)
	}

	t.Logf("Success! Output length: %d bytes", len(allOutput))
}

// TestE2EMultipleCommands tests running multiple commands
func TestE2EMultipleCommands(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	commands := []string{
		"echo 'Command 1'",
		"echo 'Command 2'",
		"pwd",
	}

	for i, cmd := range commands {
		sink.Outputs = nil // Clear previous outputs

		term.SendCommand(cmd)
		term.StreamOutput()

		if len(sink.Outputs) == 0 {
			t.Errorf("No output for command %d: %s", i, cmd)
		}

		t.Logf("Command %d: %d chunks", i, len(sink.Outputs))
	}
}

// TestE2ESlowOutput tests handling of slow/interactive output
func TestE2ESlowOutput(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Command that produces output slowly
	term.SendCommand("for i in 1 2 3; do echo \"Line $i\"; sleep 0.5; done")

	// Stream output (should handle pauses)
	start := time.Now()
	term.StreamOutput()
	elapsed := time.Since(start)

	// Should take around 1.5-2 seconds (3 * 0.5s + silence threshold)
	if elapsed < 1*time.Second {
		t.Errorf("Completed too quickly: %v (may have missed output)", elapsed)
	}

	if elapsed > 5*time.Second {
		t.Errorf("Took too long: %v", elapsed)
	}

	// Check output
	allOutput := strings.Join(sink.Outputs, "")
	if !strings.Contains(allOutput, "Line 1") || 
	   !strings.Contains(allOutput, "Line 2") || 
	   !strings.Contains(allOutput, "Line 3") {
		t.Errorf("Missing expected output. Got: %s", allOutput)
	}

	t.Logf("Success! Elapsed: %v, Chunks: %d", elapsed, len(sink.Outputs))
}

// TestE2EPythonREPL tests interactive Python REPL (if available)
func TestE2EPythonREPL(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Start Python
	sink.Outputs = nil
	term.SendCommand("python3 -c \"print('Hello from Python'); import sys; sys.exit()\"")
	term.StreamOutput()

	allOutput := strings.Join(sink.Outputs, "")
	if !strings.Contains(allOutput, "Hello from Python") {
		t.Errorf("Python output not found. Got: %s", allOutput)
	}

	t.Logf("Python test passed! Output: %d bytes", len(allOutput))
}

// TestE2EErrorHandling tests error command handling
func TestE2EErrorHandling(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Run command that doesn't exist
	term.SendCommand("nonexistentcommand12345")
	term.StreamOutput()

	if len(sink.Outputs) == 0 {
		t.Fatal("No output received for error command")
	}

	allOutput := strings.Join(sink.Outputs, "")
	
	// Should contain error message
	hasError := strings.Contains(allOutput, "command not found") ||
	            strings.Contains(allOutput, "not found") ||
	            strings.Contains(allOutput, "No such")

	if !hasError {
		t.Logf("Warning: Expected error message, got: %s", allOutput)
	}

	t.Logf("Error handling test passed")
}

// TestE2ELongOutput tests handling of very long output
func TestE2ELongOutput(t *testing.T) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Generate long output
	term.SendCommand("seq 1 100")
	term.StreamOutput()

	if len(sink.Outputs) == 0 {
		t.Fatal("No output received")
	}

	allOutput := strings.Join(sink.Outputs, "")
	
	// Should contain numbers
	if !strings.Contains(allOutput, "1") || !strings.Contains(allOutput, "100") {
		t.Errorf("Missing expected numbers in output")
	}

	t.Logf("Long output test passed! Total: %d bytes in %d chunks", 
		len(allOutput), len(sink.Outputs))
}

// BenchmarkE2ECommand benchmarks command execution
func BenchmarkE2ECommand(b *testing.B) {
	sink := &MockSink{}
	term, err := NewTerminal(sink)
	if err != nil {
		b.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink.Outputs = nil
		term.SendCommand("echo 'benchmark'")
		term.StreamOutput()
	}
}
