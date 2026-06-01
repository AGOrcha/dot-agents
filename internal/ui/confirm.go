package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// StdinIsInteractive reports whether stdin is connected to a terminal.
// Callers use it to avoid blocking on an interactive prompt when running
// non-interactively (CI, `da refresh`, piped/redirected input), where they
// should fall back to a deterministic default instead.
func StdinIsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ConfirmInteractive is Confirm for prompts that must never block when no
// human is present. Under autoYes it auto-confirms; on a real terminal it
// prompts; otherwise (CI, `da refresh`, redirected/piped stdin) it returns
// false without reading, so the caller's safe default applies. note, when
// non-interactive, is logged so the skip is visible.
func ConfirmInteractive(prompt, note string, autoYes bool) bool {
	if autoYes {
		return Confirm(prompt, true)
	}
	if !StdinIsInteractive() {
		Bullet("info", note)
		return false
	}
	return Confirm(prompt, false)
}

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
