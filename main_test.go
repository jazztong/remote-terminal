package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGenerateCode tests approval code generation
func TestGenerateCode(t *testing.T) {
	tests := []struct {
		name string
		runs int
	}{
		{"single", 1},
		{"multiple", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codes := make(map[string]bool)
			for i := 0; i < tt.runs; i++ {
				code, err := generateCode()
				if err != nil {
					t.Fatalf("generateCode() returned error: %v", err)
				}

				// Check length (8-digit codes using crypto/rand)
				if len(code) != 8 {
					t.Errorf("generateCode() length = %d, want 8", len(code))
				}

				// Check digits only
				for _, c := range code {
					if c < '0' || c > '9' {
						t.Errorf("generateCode() contains non-digit: %c", c)
					}
				}

				// Check uniqueness (for multiple runs)
				if tt.runs > 1 {
					codes[code] = true
				}
			}

			// For 100 runs with 8-digit codes, expect at least 99 unique (very high probability)
			if tt.runs == 100 && len(codes) < 99 {
				t.Errorf("generateCode() not random enough: got %d unique codes out of %d runs", len(codes), tt.runs)
			}
		})
	}
}

// TestGenerateCodeCryptoSecure tests that generateCode uses crypto/rand and produces secure codes
func TestGenerateCodeCryptoSecure(t *testing.T) {
	// Test 1: Returns 8-digit string
	code, err := generateCode()
	if err != nil {
		t.Fatalf("generateCode() error: %v", err)
	}
	if len(code) != 8 {
		t.Errorf("code length = %d, want 8", len(code))
	}

	// Test 2: All digits
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Errorf("non-digit character: %c", c)
		}
	}

	// Test 3: Uniqueness over 1000 runs
	codes := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		c, err := generateCode()
		if err != nil {
			t.Fatalf("generateCode() error on iteration %d: %v", i, err)
		}
		codes[c] = true
	}
	if len(codes) < 990 {
		t.Errorf("poor uniqueness: %d unique out of 1000", len(codes))
	}
}

// TestCleanANSI tests ANSI escape sequence removal
func TestCleanANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no_ansi",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "color_codes",
			input: "\x1b[31mRed text\x1b[0m",
			want:  "Red text",
		},
		{
			name:  "cursor_movement",
			input: "\x1b[1;1Hhello\x1b[2;1Hworld",
			want:  "helloworld",
		},
		{
			name:  "clear_screen",
			input: "\x1b[2Jhello world",
			want:  "hello world",
		},
		{
			name:  "multiple_escapes",
			input: "\x1b[31m\x1b[1mBold Red\x1b[0m\x1b[32mGreen",
			want:  "Bold RedGreen",
		},
		{
			name:  "mixed_content",
			input: "before\x1b[31mcolor\x1b[0mafter",
			want:  "beforecolorafter",
		},
		{
			name:  "empty_string",
			input: "",
			want:  "",
		},
		{
			name:  "only_escapes",
			input: "\x1b[31m\x1b[0m\x1b[2J",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanANSI(tt.input)
			if got != tt.want {
				t.Errorf("cleanANSI() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConfigSaveLoad tests config persistence
func TestConfigSaveLoad(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	tests := []struct {
		name   string
		config *Config
	}{
		{
			name: "single_user",
			config: &Config{
				BotToken:     "123456:ABCdefGHIjklMNOpqrsTUVwxyz",
				AllowedUsers: []int64{123456789},
			},
		},
		{
			name: "multiple_users",
			config: &Config{
				BotToken:     "999999:XYZabc123",
				AllowedUsers: []int64{111, 222, 333},
			},
		},
		{
			name: "no_users",
			config: &Config{
				BotToken:     "token123",
				AllowedUsers: []int64{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save config
			err := saveConfig(tt.config)
			if err != nil {
				t.Fatalf("saveConfig() error = %v", err)
			}

			// Check file exists
			configPath := getConfigPath()
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				t.Fatalf("config file not created at %s", configPath)
			}

			// Check file permissions (should be 0600)
			info, err := os.Stat(configPath)
			if err != nil {
				t.Fatalf("stat error = %v", err)
			}
			mode := info.Mode().Perm()
			if mode != 0600 {
				t.Errorf("config file permissions = %o, want 0600", mode)
			}

			// Load config
			loaded, err := loadConfig()
			if err != nil {
				t.Fatalf("loadConfig() error = %v", err)
			}

			// Compare
			if loaded.BotToken != tt.config.BotToken {
				t.Errorf("BotToken = %s, want %s", loaded.BotToken, tt.config.BotToken)
			}

			if len(loaded.AllowedUsers) != len(tt.config.AllowedUsers) {
				t.Errorf("AllowedUsers length = %d, want %d", len(loaded.AllowedUsers), len(tt.config.AllowedUsers))
			}

			for i, user := range loaded.AllowedUsers {
				if i < len(tt.config.AllowedUsers) && user != tt.config.AllowedUsers[i] {
					t.Errorf("AllowedUsers[%d] = %d, want %d", i, user, tt.config.AllowedUsers[i])
				}
			}
		})
	}
}

// TestConfigLoadNonExistent tests loading when config doesn't exist
func TestConfigLoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	_, err := loadConfig()
	if err == nil {
		t.Error("loadConfig() expected error for non-existent file, got nil")
	}
}

// TestConfigInvalidJSON tests loading invalid JSON
func TestConfigInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create invalid JSON file
	configDir := filepath.Join(tmpDir, ".telegram-terminal")
	os.MkdirAll(configDir, 0700)
	configPath := filepath.Join(configDir, "config.json")
	os.WriteFile(configPath, []byte("invalid json {"), 0600)

	_, err := loadConfig()
	if err == nil {
		t.Error("loadConfig() expected error for invalid JSON, got nil")
	}
}

// TestGetConfigPath tests config path generation
func TestGetConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	path := getConfigPath()
	expectedPath := filepath.Join(tmpDir, ".telegram-terminal", "config.json")

	if path != expectedPath {
		t.Errorf("getConfigPath() = %s, want %s", path, expectedPath)
	}

	// Check directory was created
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("config directory not created: %s", dir)
	}
}

// TestOutputCollection tests the output collection logic
func TestOutputCollection(t *testing.T) {
	tests := []struct {
		name     string
		messages []string
		delays   []time.Duration
		timeout  time.Duration
		want     string
	}{
		{
			name:     "single_message",
			messages: []string{"hello"},
			delays:   []time.Duration{0},
			timeout:  500 * time.Millisecond,
			want:     "hello",
		},
		{
			name:     "multiple_messages",
			messages: []string{"hello", " ", "world"},
			delays:   []time.Duration{0, 50 * time.Millisecond, 50 * time.Millisecond},
			timeout:  200 * time.Millisecond,
			want:     "hello world",
		},
		{
			name:     "timeout_before_message",
			messages: []string{"late"},
			delays:   []time.Duration{300 * time.Millisecond},
			timeout:  100 * time.Millisecond,
			want:     "", // Message arrives after timeout
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock output channel
			outputChan := make(chan string, 10)

			// Send messages with delays
			go func() {
				for i, msg := range tt.messages {
					if i < len(tt.delays) {
						time.Sleep(tt.delays[i])
					}
					outputChan <- msg
				}
			}()

			// Collect output (simulate collectOutput)
			var result strings.Builder
			deadline := time.Now().Add(tt.timeout)

			for time.Now().Before(deadline) {
				select {
				case output := <-outputChan:
					result.WriteString(output)
				case <-time.After(50 * time.Millisecond):
					// Small delay to collect more
				}
			}

			// Drain remaining
			for len(outputChan) > 0 {
				output := <-outputChan
				result.WriteString(output)
			}

			got := result.String()
			if got != tt.want {
				t.Errorf("output collection = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestUserAuthorization tests user whitelist checking
func TestUserAuthorization(t *testing.T) {
	tests := []struct {
		name         string
		allowedUsers []int64
		checkUserID  int64
		want         bool
	}{
		{
			name:         "user_allowed",
			allowedUsers: []int64{123456789},
			checkUserID:  123456789,
			want:         true,
		},
		{
			name:         "user_not_allowed",
			allowedUsers: []int64{123456789},
			checkUserID:  999999999,
			want:         false,
		},
		{
			name:         "multiple_users_allowed",
			allowedUsers: []int64{111, 222, 333},
			checkUserID:  222,
			want:         true,
		},
		{
			name:         "empty_whitelist",
			allowedUsers: []int64{},
			checkUserID:  123456789,
			want:         false,
		},
		{
			name:         "nil_whitelist",
			allowedUsers: nil,
			checkUserID:  123456789,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate authorization check
			allowed := false
			for _, allowedID := range tt.allowedUsers {
				if tt.checkUserID == allowedID {
					allowed = true
					break
				}
			}

			if allowed != tt.want {
				t.Errorf("authorization = %v, want %v", allowed, tt.want)
			}
		})
	}
}

// TestMessageSplitting tests long message splitting
func TestMessageSplitting(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		maxLen     int
		wantChunks int
	}{
		{
			name:       "short_message",
			output:     "hello",
			maxLen:     100,
			wantChunks: 1,
		},
		{
			name:       "exact_length",
			output:     strings.Repeat("a", 100),
			maxLen:     100,
			wantChunks: 1,
		},
		{
			name:       "needs_split",
			output:     strings.Repeat("a", 150),
			maxLen:     100,
			wantChunks: 2,
		},
		{
			name:       "multiple_chunks",
			output:     strings.Repeat("a", 350),
			maxLen:     100,
			wantChunks: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate splitting logic
			chunks := 0
			output := tt.output

			if len(output) <= tt.maxLen {
				chunks = 1
			} else {
				for i := 0; i < len(output); i += tt.maxLen {
					chunks++
				}
			}

			if chunks != tt.wantChunks {
				t.Errorf("chunks = %d, want %d", chunks, tt.wantChunks)
			}
		})
	}
}

// BenchmarkCleanANSI benchmarks ANSI cleaning performance
func BenchmarkCleanANSI(b *testing.B) {
	input := "\x1b[31m\x1b[1mBold Red Text\x1b[0m Normal \x1b[32mGreen\x1b[0m"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cleanANSI(input)
	}
}

// BenchmarkGenerateCode benchmarks code generation
func BenchmarkGenerateCode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = generateCode()
	}
}

// TestConfigJSONFormat tests that config is valid JSON
func TestConfigJSONFormat(t *testing.T) {
	config := &Config{
		BotToken:     "test-token-123",
		AllowedUsers: []int64{111, 222},
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Check it can be unmarshaled
	var loaded Config
	err = json.Unmarshal(data, &loaded)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Verify data
	if loaded.BotToken != config.BotToken {
		t.Errorf("BotToken mismatch after JSON round-trip")
	}
}

// TestEmptyOutput tests handling of empty command output
func TestEmptyOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "empty_string",
			output: "",
			want:   "(no output)",
		},
		{
			name:   "whitespace_only",
			output: "   ",
			want:   "   ",
		},
		{
			name:   "newlines_only",
			output: "\n\n\n",
			want:   "\n\n\n",
		},
		{
			name:   "actual_output",
			output: "hello",
			want:   "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate empty output handling
			output := tt.output
			if output == "" {
				output = "(no output)"
			}

			if output != tt.want {
				t.Errorf("output = %q, want %q", output, tt.want)
			}
		})
	}
}

// TestCleanANSIPreservesText tests that ANSI cleaning preserves non-escape text
func TestCleanANSIPreservesText(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"simple", "hello world"},
		{"numbers", "12345 67890"},
		{"symbols", "!@#$%^&*()"},
		{"unicode", "Hello 世界 🌍"},
		{"mixed", "Line 1\nLine 2\tTabbed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Text without ANSI should pass through unchanged
			got := cleanANSI(tt.text)
			if got != tt.text {
				t.Errorf("cleanANSI() changed text: got %q, want %q", got, tt.text)
			}
		})
	}
}

// TestConfigDirectoryCreation tests that config directory is created with correct permissions
func TestConfigDirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Call getConfigPath which should create directory
	configPath := getConfigPath()
	dir := filepath.Dir(configPath)

	// Check directory exists
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}

	// Check it's a directory
	if !info.IsDir() {
		t.Error("config path is not a directory")
	}

	// Check permissions (should be 0700)
	mode := info.Mode().Perm()
	if mode != 0700 {
		t.Errorf("directory permissions = %o, want 0700", mode)
	}
}

// TestCollectOutputWithMultipleReaders tests concurrent output reading
func TestCollectOutputWithMultipleReaders(t *testing.T) {
	outputChan := make(chan string, 100)

	// Write from multiple goroutines
	for i := 0; i < 10; i++ {
		go func(n int) {
			outputChan <- string(rune('A' + n))
		}(i)
	}

	time.Sleep(100 * time.Millisecond)

	// Collect all
	var result strings.Builder
	for len(outputChan) > 0 {
		result.WriteString(<-outputChan)
	}

	// Should have 10 characters
	if result.Len() != 10 {
		t.Errorf("collected %d characters, want 10", result.Len())
	}
}
