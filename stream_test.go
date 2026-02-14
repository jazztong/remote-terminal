package main

import (
	"strings"
	"testing"
	"time"
)

// TestStreamOutputBasic tests basic streaming functionality
func TestStreamOutputBasic(t *testing.T) {
	tests := []struct {
		name           string
		messages       []string
		delays         []time.Duration
		expectedChunks int
		minContent     string
	}{
		{
			name:           "single_quick_output",
			messages:       []string{"hello world"},
			delays:         []time.Duration{0},
			expectedChunks: 1,
			minContent:     "hello world",
		},
		{
			name:           "multiple_quick_messages",
			messages:       []string{"line1\n", "line2\n", "line3\n"},
			delays:         []time.Duration{0, 100 * time.Millisecond, 100 * time.Millisecond},
			expectedChunks: 1,
			minContent:     "line1",
		},
		{
			name: "messages_with_pauses",
			messages: []string{
				"First chunk",
				"Second chunk after pause",
			},
			delays:         []time.Duration{0, 2 * time.Second},
			expectedChunks: 2,
			minContent:     "First chunk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock output channel
			outputChan := make(chan string, 10)
			
			// Mock bot that captures sent messages
			sentMessages := []string{}
			mockSend := func(content string) {
				sentMessages = append(sentMessages, content)
			}

			// Send messages with delays
			go func() {
				for i, msg := range tt.messages {
					if i < len(tt.delays) {
						time.Sleep(tt.delays[i])
					}
					outputChan <- msg
				}
			}()

			// Simulate streaming
			streamOutputMock(outputChan, mockSend)

			// Verify
			if len(sentMessages) == 0 {
				t.Errorf("No messages sent")
			}

			// Check content
			allContent := strings.Join(sentMessages, "")
			if !strings.Contains(allContent, tt.minContent) {
				t.Errorf("Expected content %q not found in output: %q", tt.minContent, allContent)
			}
		})
	}
}

// Mock streaming function for testing - matches main implementation
func streamOutputMock(outputChan chan string, sendFunc func(string)) {
	var buffer strings.Builder
	lastOutputTime := time.Now()
	silenceThreshold := 500 * time.Millisecond    // Send chunk after 0.5s silence  
	finalSilenceThreshold := 1500 * time.Millisecond // Stop after 1.5s total silence
	maxWaitTime := 5 * time.Second
	startTime := time.Now()
	
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case output := <-outputChan:
			buffer.WriteString(output)
			lastOutputTime = time.Now()
			
		case <-ticker.C:
			// Send chunk if we have output and it's been quiet
			if buffer.Len() > 0 && time.Since(lastOutputTime) > silenceThreshold {
				sendFunc(buffer.String())
				buffer.Reset()
			}
			
			// Stop if max total time reached
			if time.Since(startTime) > maxWaitTime {
				if buffer.Len() > 0 {
					sendFunc(buffer.String())
				}
				return
			}
			
			// Stop only after long silence with empty buffer
			if buffer.Len() == 0 && time.Since(lastOutputTime) > finalSilenceThreshold {
				return
			}
		}
	}
}

// TestStreamOutputSilenceDetection tests silence-based chunking
func TestStreamOutputSilenceDetection(t *testing.T) {
	outputChan := make(chan string, 10)
	sentMessages := []string{}
	
	mockSend := func(content string) {
		sentMessages = append(sentMessages, content)
	}

	// Send burst of messages
	go func() {
		outputChan <- "Chunk 1 part 1\n"
		time.Sleep(50 * time.Millisecond)
		outputChan <- "Chunk 1 part 2\n"
		
		// Long pause
		time.Sleep(1 * time.Second)
		
		outputChan <- "Chunk 2\n"
	}()

	streamOutputMock(outputChan, mockSend)

	// Should have received 2 chunks
	if len(sentMessages) < 2 {
		t.Errorf("Expected at least 2 chunks, got %d", len(sentMessages))
	}

	// First chunk should have both parts
	if !strings.Contains(sentMessages[0], "Chunk 1 part 1") {
		t.Errorf("First chunk missing part 1")
	}
	if !strings.Contains(sentMessages[0], "Chunk 1 part 2") {
		t.Errorf("First chunk missing part 2")
	}
}

// TestStreamOutputTimeout tests max timeout
func TestStreamOutputTimeout(t *testing.T) {
	outputChan := make(chan string, 10)
	sentMessages := []string{}
	
	mockSend := func(content string) {
		sentMessages = append(sentMessages, content)
	}

	// Send continuous slow output
	go func() {
		for i := 0; i < 100; i++ {
			outputChan <- "output\n"
			time.Sleep(100 * time.Millisecond)
		}
	}()

	start := time.Now()
	streamOutputMock(outputChan, mockSend)
	elapsed := time.Since(start)

	// Should timeout around 5 seconds (max wait in mock)
	if elapsed > 6*time.Second {
		t.Errorf("Streaming took too long: %v", elapsed)
	}

	// Should have received some messages
	if len(sentMessages) == 0 {
		t.Error("No messages sent before timeout")
	}
}

// TestStreamOutputEmptyBuffer tests handling of no output
func TestStreamOutputEmptyBuffer(t *testing.T) {
	outputChan := make(chan string, 10)
	sentMessages := []string{}
	
	mockSend := func(content string) {
		sentMessages = append(sentMessages, content)
	}

	// No output sent
	go func() {
		// Channel stays empty
	}()

	start := time.Now()
	
	// Run in goroutine with timeout
	done := make(chan bool)
	go func() {
		streamOutputMock(outputChan, mockSend)
		done <- true
	}()

	select {
	case <-done:
		elapsed := time.Since(start)
		// Should finish quickly (within 1 second due to silence threshold)
		if elapsed > 2*time.Second {
			t.Errorf("Empty stream took too long: %v", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Error("Stream function did not return for empty input")
	}

	if len(sentMessages) != 0 {
		t.Errorf("Expected no messages for empty input, got %d", len(sentMessages))
	}
}

// TestStreamOutputInteractiveSimulation simulates Claude-like behavior
func TestStreamOutputInteractiveSimulation(t *testing.T) {
	outputChan := make(chan string, 100)
	sentMessages := []string{}
	
	mockSend := func(content string) {
		sentMessages = append(sentMessages, content)
		t.Logf("Sent chunk %d: %d bytes", len(sentMessages), len(content))
	}

	// Simulate Claude-like output (slow start, then chunks)
	go func() {
		// Initial startup messages (quick)
		outputChan <- "Claude Code v2.1.41\n"
		outputChan <- "Welcome back!\n"
		
		time.Sleep(500 * time.Millisecond)
		
		// User command echo
		outputChan <- "> create hello.py\n"
		
		// Thinking pause
		time.Sleep(800 * time.Millisecond)
		
		// Response starts
		outputChan <- "I'll create that for you.\n"
		time.Sleep(200 * time.Millisecond)
		outputChan <- "\n"
		outputChan <- "[Created: hello.py]\n"
		time.Sleep(100 * time.Millisecond)
		outputChan <- "print('Hello, World!')\n"
		
		// Done
		time.Sleep(600 * time.Millisecond)
		outputChan <- "\n> "
	}()

	streamOutputMock(outputChan, mockSend)

	// Should have received multiple chunks
	if len(sentMessages) == 0 {
		t.Fatal("No messages sent")
	}

	// Check all content was sent
	allContent := strings.Join(sentMessages, "")
	
	mustContain := []string{
		"Claude Code",
		"Welcome back",
		"create hello.py",
		"I'll create",
		"hello.py",
		"Hello, World",
	}

	for _, expected := range mustContain {
		if !strings.Contains(allContent, expected) {
			t.Errorf("Expected content %q not found in output", expected)
		}
	}

	t.Logf("Total chunks sent: %d", len(sentMessages))
	t.Logf("Total content length: %d bytes", len(allContent))
}

// BenchmarkStreamOutput benchmarks streaming performance
func BenchmarkStreamOutput(b *testing.B) {
	for i := 0; i < b.N; i++ {
		outputChan := make(chan string, 10)
		mockSend := func(content string) {}

		go func() {
			for j := 0; j < 10; j++ {
				outputChan <- "test output\n"
				time.Sleep(50 * time.Millisecond)
			}
		}()

		streamOutputMock(outputChan, mockSend)
	}
}
