package main

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Version is set at build time via ldflags
var version = "dev"

type Config struct {
	BotToken          string  `json:"bot_token"`
	AllowedUsers      []int64 `json:"allowed_users"`
	WebUIPasswordHash string  `json:"webui_password_hash,omitempty"`
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("remote-term v%s\n", version)
		return
	}

	// --stop: stop running daemon
	if len(os.Args) > 1 && os.Args[1] == "--stop" {
		daemonStop()
		return
	}

	// --status: check daemon status
	if len(os.Args) > 1 && os.Args[1] == "--status" {
		daemonStatus()
		return
	}

	// --daemon: start as background daemon
	// --daemon-child: internal flag used by the daemon parent process
	// Check for --daemon or --daemon-child anywhere in args (can combine with --web)
	isDaemon := false
	isDaemonChild := false
	for _, arg := range os.Args[1:] {
		if arg == "--daemon" {
			isDaemon = true
		}
		if arg == "--daemon-child" {
			isDaemonChild = true
		}
	}

	if isDaemon {
		// Build extra args (everything except --daemon)
		var extraArgs []string
		for _, arg := range os.Args[1:] {
			if arg != "--daemon" {
				extraArgs = append(extraArgs, arg)
			}
		}
		daemonize(extraArgs)
		return
	}

	// If this is the daemon child process, set up logging and PID cleanup
	if isDaemonChild {
		// Remove --daemon-child from args for downstream parsing
		var cleanArgs []string
		cleanArgs = append(cleanArgs, os.Args[0])
		for _, arg := range os.Args[1:] {
			if arg != "--daemon-child" {
				cleanArgs = append(cleanArgs, arg)
			}
		}
		os.Args = cleanArgs

		// Set up log output to the log file
		logFile, err := os.OpenFile(logFilePath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			log.SetOutput(logFile)
		}

		// Set cleanup hook for signal-based shutdown (os.Exit bypasses defers)
		daemonCleanupHook = removePIDFile

		// Ensure PID file cleanup on normal exit (defer)
		defer removePIDFile()
	}

	// Check for standalone mode
	if len(os.Args) > 1 && os.Args[1] == "--standalone" {
		RunStandalone()
		return
	}

	// Check for web UI mode
	if len(os.Args) > 1 && os.Args[1] == "--web" {
		port := 8080
		if len(os.Args) > 2 {
			fmt.Sscanf(os.Args[2], "%d", &port)
		}
		config, _ := loadConfig() // nil-safe: config may not exist yet for first-time WebUI
		server := NewWebUIServer(config)
		server.Start(port)
		return
	}

	// Check if config exists
	configPath := getConfigPath()

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// First time setup
		setupWithApproval()
	} else {
		// Start listening
		startListening()
	}
}

// configPathOverride allows tests to redirect config to a temp directory
var configPathOverride string

// daemonCleanupHook is set when running as daemon child to clean up PID file on signal shutdown.
// This is called by TelegramBridge.Listen() signal handler (since os.Exit bypasses defers).
var daemonCleanupHook func()

func getConfigPath() string {
	if configPathOverride != "" {
		dir := filepath.Dir(configPathOverride)
		os.MkdirAll(dir, 0700)
		return configPathOverride
	}
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".telegram-terminal")
	os.MkdirAll(configDir, 0700)
	return filepath.Join(configDir, "config.json")
}

func loadConfig() (*Config, error) {
	data, err := os.ReadFile(getConfigPath())
	if err != nil {
		return nil, err
	}

	var config Config
	err = json.Unmarshal(data, &config)
	return &config, err
}

func saveConfig(config *Config) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	return os.WriteFile(getConfigPath(), data, 0600)
}

func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return "", fmt.Errorf("crypto/rand failed: %w", err)
	}
	return fmt.Sprintf("%08d", n.Int64()), nil
}

func setupWithApproval() {
	fmt.Printf("Remote Terminal v%s\n", version)
	fmt.Println("\nRun: /setup <bot-token>")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "/setup ") {
			token := strings.TrimPrefix(line, "/setup ")

			fmt.Println("\n⏳ Connecting to Telegram...")

			bot, err := tgbotapi.NewBotAPI(token)
			if err != nil {
				fmt.Printf("❌ Error: %v\n", err)
				continue
			}

			approvalCode, err := generateCode()
			if err != nil {
				fmt.Printf("❌ Error generating approval code: %v\n", err)
				return
			}

			fmt.Println("✅ Connected!")
			fmt.Printf("🤖 Bot: @%s\n", bot.Self.UserName)
			fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("🔐 SECURITY: First Connection Setup")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("\nGo to Telegram and message @%s\n", bot.Self.UserName)
			fmt.Println("Then send this approval code:")
			fmt.Println()
			fmt.Printf("    👉 %s\n\n", approvalCode)
			fmt.Println("Waiting for approval (expires in 15 minutes)...")

			// Wait for approval
			u := tgbotapi.NewUpdate(0)
			u.Timeout = 60
			updates := bot.GetUpdatesChan(u)

			approved := false
			attempts := 0
			maxAttempts := 5
			codeExpiry := time.Now().Add(15 * time.Minute)

			for update := range updates {
				if update.Message == nil {
					continue
				}

				// Check expiration
				if time.Now().After(codeExpiry) {
					fmt.Println("\n❌ Approval code expired. Please restart setup.")
					msg := tgbotapi.NewMessage(update.Message.Chat.ID,
						"❌ Approval code expired. Please restart the setup process.")
					bot.Send(msg)
					return
				}

				userID := update.Message.From.ID
				username := update.Message.From.UserName
				text := update.Message.Text

				if subtle.ConstantTimeCompare([]byte(text), []byte(approvalCode)) == 1 {
					approved = true

					// Save config
					config := &Config{
						BotToken:     token,
						AllowedUsers: []int64{userID},
					}
					saveConfig(config)

					fmt.Printf("\n✅ User approved!\n")
					fmt.Printf("   @%s (ID: %d)\n\n", username, userID)
					fmt.Println("Whitelist saved. This user can now connect anytime.")

					// Send confirmation
					msg := tgbotapi.NewMessage(update.Message.Chat.ID,
						fmt.Sprintf("✅ Approved!\n\n"+
							"Terminal connected successfully.\n"+
							"User: @%s (ID: %d)\n\n"+
							"You can now send commands.\n"+
							"Try: ls", username, userID))
					bot.Send(msg)

					fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
					fmt.Println("[Ready] Listening for commands...")
					fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
					fmt.Println()

					break
				} else {
					attempts++
					remaining := maxAttempts - attempts
					if remaining <= 0 {
						fmt.Println("\n❌ Too many failed attempts. Please restart setup.")
						msg := tgbotapi.NewMessage(update.Message.Chat.ID,
							"❌ Too many failed attempts. Approval locked. Please restart the setup process.")
						bot.Send(msg)
						return
					}
					msg := tgbotapi.NewMessage(update.Message.Chat.ID,
						fmt.Sprintf("❌ Invalid approval code. %d attempts remaining.", remaining))
					bot.Send(msg)
				}
			}

			if approved {
				// Start bridge
				config, _ := loadConfig()
				bridge, err := NewTelegramBridge(bot, config)
				if err != nil {
					log.Fatalf("Error creating bridge: %v", err)
				}

				bridge.Listen()
			}

			return
		} else {
			fmt.Println("Usage: /setup <bot-token>")
		}
	}
}

func startListening() {
	config, err := loadConfig()
	if err != nil {
		fmt.Printf("❌ Error loading config: %v\n", err)
		return
	}

	bot, err := tgbotapi.NewBotAPI(config.BotToken)
	if err != nil {
		fmt.Printf("❌ Error connecting: %v\n", err)
		return
	}

	fmt.Printf("Remote Terminal v%s\n", version)
	fmt.Printf("✅ Configuration loaded\n")
	fmt.Printf("👥 Allowed users: %d\n", len(config.AllowedUsers))
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("[Ready] Listening for commands...")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	bridge, err := NewTelegramBridge(bot, config)
	if err != nil {
		log.Fatalf("Error creating bridge: %v", err)
	}

	// Set cleanup hook for daemon mode (PID file removal on signal shutdown)
	if daemonCleanupHook != nil {
		bridge.cleanupHook = daemonCleanupHook
	}

	bridge.Listen()
}

func cleanANSI(s string) string {
	// Comprehensive ANSI escape sequence removal
	result := strings.Builder{}
	i := 0
	
	for i < len(s) {
		if s[i] == '\x1b' || s[i] == '\u001b' { // ESC character
			i++
			if i >= len(s) {
				break
			}
			
			// CSI sequences: ESC [ ... (letter or @)
			if s[i] == '[' {
				i++
				// Skip until we hit a letter (a-zA-Z) or @
				for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') || s[i] == '@') {
					i++
				}
				i++ // Skip the terminating character
				continue
			}
			
			// OSC sequences: ESC ] ... (terminated by BEL or ESC \)
			if s[i] == ']' {
				i++
				for i < len(s) {
					if s[i] == '\x07' { // BEL
						i++
						break
					}
					if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
				continue
			}
			
			// Other escape sequences - skip next char
			if i < len(s) {
				i++
			}
			continue
		}
		
		// Not an escape sequence, keep the character
		result.WriteByte(s[i])
		i++
	}

	// Clean up excessive whitespace and control characters
	cleaned := result.String()
	
	// Remove carriage returns and excessive spaces
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\n")
	cleaned = strings.ReplaceAll(cleaned, "\r", "\n")
	
	// Collapse multiple newlines
	for strings.Contains(cleaned, "\n\n\n") {
		cleaned = strings.ReplaceAll(cleaned, "\n\n\n", "\n\n")
	}
	
	// Collapse multiple spaces
	for strings.Contains(cleaned, "  ") {
		cleaned = strings.ReplaceAll(cleaned, "  ", " ")
	}
	
	return strings.TrimSpace(cleaned)
}
