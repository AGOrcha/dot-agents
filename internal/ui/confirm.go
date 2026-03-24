package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Confirm prompts the user for yes/no confirmation.
// Returns true if the user confirms (y/Y/yes), false otherwise.
// If autoYes is true, it auto-confirms without prompting.
func Confirm(prompt string, autoYes bool) bool {
	if autoYes {
		fmt.Fprintf(os.Stdout, "  %s [y/N] y (auto-confirmed)\n", prompt)
		return true
	}

	fmt.Fprintf(os.Stdout, "\n  %s [y/N] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
