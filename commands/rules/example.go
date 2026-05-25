package rules

import "strings"

// exampleBlock joins example lines with newlines for cobra's Example field.
// Mirrors agents.exampleBlock / skills.exampleBlock so the subpackages have
// parallel helpers.
func exampleBlock(lines ...string) string {
	return strings.Join(lines, "\n")
}
