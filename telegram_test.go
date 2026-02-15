package main

import (
	"sync"
	"testing"
)

// TestSessionSafeCloseDone verifies that safeCloseDone() doesn't panic when called twice
func TestSessionSafeCloseDone(t *testing.T) {
	session := &Session{
		done: make(chan struct{}),
	}

	// First close should work
	session.safeCloseDone()

	// Second close should NOT panic
	session.safeCloseDone()

	// Channel should be closed
	select {
	case <-session.done:
		// OK - channel is closed
	default:
		t.Error("done channel should be closed")
	}
}

// TestSessionSafeCloseDoneConcurrent verifies safeCloseDone() is safe under concurrent access
func TestSessionSafeCloseDoneConcurrent(t *testing.T) {
	session := &Session{
		done: make(chan struct{}),
	}

	var wg sync.WaitGroup
	// Call safeCloseDone from 100 goroutines simultaneously
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session.safeCloseDone()
		}()
	}
	wg.Wait()

	// Channel should be closed after all goroutines finish
	select {
	case <-session.done:
		// OK - channel is closed
	default:
		t.Error("done channel should be closed after concurrent safeCloseDone calls")
	}

	// doneClosed flag should be true
	session.closeMu.Lock()
	if !session.doneClosed {
		t.Error("doneClosed should be true after safeCloseDone")
	}
	session.closeMu.Unlock()
}

// TestTelegramBridgeSessionMapMutex verifies the mutex protects session map access
func TestTelegramBridgeSessionMapMutex(t *testing.T) {
	tb := &TelegramBridge{
		sessions: make(map[int64]*Session),
	}

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			tb.mu.Lock()
			tb.sessions[id] = &Session{
				Active:    true,
				Command:   "test",
				done:      make(chan struct{}),
			}
			tb.mu.Unlock()
		}(int64(i))
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			tb.mu.RLock()
			_ = tb.sessions[id]
			tb.mu.RUnlock()
		}(int64(i))
	}

	wg.Wait()

	tb.mu.RLock()
	count := len(tb.sessions)
	tb.mu.RUnlock()

	if count != 50 {
		t.Errorf("expected 50 sessions, got %d", count)
	}
}

// TestIsInteractiveCommand tests interactive command detection
func TestIsInteractiveCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"claude", true},
		{"python3", true},
		{"node", true},
		{"vim", true},
		{"ssh user@host", true},
		{"ls", false},
		{"pwd", false},
		{"cat file.txt", false},
		{"", false},
		{"claude-code", true},
		{"psql", true},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := isInteractiveCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("isInteractiveCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}
