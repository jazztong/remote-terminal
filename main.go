package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Config struct {
	BotToken     string  `json:"bot_token"`
	AllowedUsers []int64 `json:"allowed_users"`
}

func main() {
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
		server := NewWebUIServer()
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

func getConfigPath() string {
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

func generateCode() string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("%05d", rand.Intn(100000))
}

func setupWithApproval() {
	fmt.Println("Remote Terminal v2.0")
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

			approvalCode := generateCode()

			fmt.Println("✅ Connected!")
			fmt.Printf("🤖 Bot: @%s\n", bot.Self.UserName)
			fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("🔐 SECURITY: First Connection Setup")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("\nGo to Telegram and message @%s\n", bot.Self.UserName)
			fmt.Println("Then send this approval code:")
			fmt.Println()
			fmt.Printf("    👉 %s\n\n", approvalCode)
			fmt.Println("Waiting for approval...")

			// Wait for approval
			u := tgbotapi.NewUpdate(0)
			u.Timeout = 60
			updates := bot.GetUpdatesChan(u)

			approved := false

			for update := range updates {
				if update.Message == nil {
					continue
				}

				userID := update.Message.From.ID
				username := update.Message.From.UserName
				text := update.Message.Text

				if text == approvalCode {
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
					msg := tgbotapi.NewMessage(update.Message.Chat.ID,
						"❌ Invalid approval code. Check your terminal.")
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

	fmt.Println("Remote Terminal v2.0")
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
