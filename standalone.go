package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// RunStandalone runs terminal in standalone mode (no Telegram)
func RunStandalone() {
	fmt.Println("Terminal Standalone Mode")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Commands:")
	fmt.Println("  Type any shell command")
	fmt.Println("  'exit' to quit")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Create console sink
	sink := &ConsoleSink{}

	// Create terminal
	term, err := NewTerminal(sink)
	if err != nil {
		fmt.Printf("Error creating terminal: %v\n", err)
		return
	}
	defer term.Close()

	// Read commands from stdin
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("$ ")

	for scanner.Scan() {
		command := strings.TrimSpace(scanner.Text())

		if command == "exit" || command == "quit" {
			break
		}

		if command == "" {
			fmt.Print("$ ")
			continue
		}

		// Send command
		fmt.Printf("\n→ Executing: %s\n\n", command)
		term.SendCommand(command)

		// Stream output
		term.StreamOutput()

		fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Print("$ ")
	}

	fmt.Println("\nGoodbye!")
}
