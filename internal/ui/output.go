package ui

import (
	"fmt"
	"os"
	"strings"
)

// ANSI color codes
const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Cyan   = "\033[36m"
	White  = "\033[37m"
)

var noColor bool

func init() {
	// Disable color when not a terminal or NO_COLOR is set
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		noColor = true
	}
}

func color(code, s string) string {
	if noColor {
		return s
	}
	return code + s + Reset
}

const ThreeStringPlaceHolder = "\n%s%s%s\n"

// Header prints a section header line.
func Header(title string) {
	fmt.Fprintf(os.Stdout, ThreeStringPlaceHolder, Bold, title, Reset)
	fmt.Fprintln(os.Stdout, strings.Repeat("─", 40))
}

// Section prints a subsection label.
func Section(label string) {
	fmt.Fprintf(os.Stdout, ThreeStringPlaceHolder, Bold, label, Reset)
}

// Step prints a numbered step.
func Step(msg string) {
	fmt.Fprintf(os.Stdout, ThreeStringPlaceHolder, Bold, msg, Reset)
}

// StepN prints a numbered step with [n/total] prefix.
func StepN(n, total int, msg string) {
	prefix := fmt.Sprintf("[%d/%d]", n, total)
	fmt.Fprintf(os.Stdout, "\n%s%s %s%s\n", Bold, color(Dim, prefix), msg, Reset)
}

// Bullet prints a status bullet.
//
//	style: "ok", "warn", "error", "skip", "none", "found", "dry"
func Bullet(style, msg string) {
	switch style {
	case "ok":
		fmt.Fprintf(os.Stdout, "  %s✓%s %s\n", color(Green, ""), Reset, msg)
	case "warn":
		fmt.Fprintf(os.Stdout, "  %s!%s %s\n", color(Yellow, ""), Reset, msg)
	case "error":
		fmt.Fprintf(os.Stdout, "  %s✗%s %s\n", color(Red, ""), Reset, msg)
	case "skip":
		fmt.Fprintf(os.Stdout, "  %s-%s %s\n", color(Dim, ""), Reset, msg)
	case "none":
		fmt.Fprintf(os.Stdout, "  %s○%s %s\n", color(Dim, ""), Reset, msg)
	case "found":
		fmt.Fprintf(os.Stdout, "  %s◆%s %s\n", color(Cyan, ""), Reset, msg)
	case "dry":
		fmt.Fprintf(os.Stdout, "  %s~%s %s %s(dry run)%s\n", color(Dim, ""), Reset, msg, Dim, Reset)
	default:
		fmt.Fprintf(os.Stdout, "  · %s\n", msg)
	}
}

// PreviewSection prints a labeled list of preview items.
func PreviewSection(title string, items ...string) {
	fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", Bold, title, Reset)
	for _, item := range items {
		fmt.Fprintf(os.Stdout, "    %s%s%s\n", Dim, item, Reset)
	}
}

// SuccessBox prints a success message with next steps.
func SuccessBox(msg string, nextSteps ...string) {
	fmt.Fprintf(os.Stdout, "\n%s✓ %s%s\n", color(Green, ""), msg, Reset)
	if len(nextSteps) > 0 {
		fmt.Fprintln(os.Stdout, "\nNext steps:")
		for _, step := range nextSteps {
			fmt.Fprintf(os.Stdout, "  • %s\n", step)
		}
	}
	fmt.Fprintln(os.Stdout)
}

// WarnBox prints a warning box.
func WarnBox(title string, lines ...string) {
	fmt.Fprintf(os.Stdout, "\n%s⚠  %s%s\n", color(Yellow, ""), title, Reset)
	for _, line := range lines {
		fmt.Fprintf(os.Stdout, "   %s\n", line)
	}
}

// InfoBox prints an info box.
func InfoBox(title string, lines ...string) {
	fmt.Fprintf(os.Stdout, "\n%sℹ  %s%s\n", color(Cyan, ""), title, Reset)
	for _, line := range lines {
		fmt.Fprintf(os.Stdout, "   %s\n", line)
	}
}

// Error prints an error to stderr.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "%s✗ Error: %s%s\n", color(Red, ""), msg, Reset)
}

// Errorf prints a formatted error to stderr.
func Errorf(format string, args ...interface{}) {
	Error(fmt.Sprintf(format, args...))
}

// Warn prints a warning.
func Warn(msg string) {
	fmt.Fprintf(os.Stdout, "%s! %s%s\n", color(Yellow, ""), msg, Reset)
}

// Info prints an info message.
func Info(msg string) {
	fmt.Fprintf(os.Stdout, "  %s\n", msg)
}

// Success prints a success message.
func Success(msg string) {
	fmt.Fprintf(os.Stdout, "%s✓ %s%s\n", color(Green, ""), msg, Reset)
}

// DryRun prints a dry run action.
func DryRun(msg string) {
	fmt.Fprintf(os.Stdout, "  %s~ %s (dry run)%s\n", Dim, msg, Reset)
}

// Create prints a created item.
func Create(msg string) {
	fmt.Fprintf(os.Stdout, "  %s+ %s%s\n", color(Green, ""), msg, Reset)
}

// Skip prints a skipped item.
func Skip(msg string) {
	fmt.Fprintf(os.Stdout, "  %s- %s%s\n", Dim, msg, Reset)
}

// Bold wraps s in bold ansi.
func BoldText(s string) string {
	if noColor {
		return s
	}
	return Bold + s + Reset
}

// DimText wraps s in dim ansi.
func DimText(s string) string {
	if noColor {
		return s
	}
	return Dim + s + Reset
}

// ColorText applies a color to text.
func ColorText(code, s string) string {
	return color(code, s)
}
