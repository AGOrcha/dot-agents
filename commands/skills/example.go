package skills

import "strings"

// exampleBlock joins example lines with newlines for cobra's Example field.
// Mirrors agents.exampleBlock so the two subpackages have parallel helpers.
func exampleBlock(lines ...string) string {
	return strings.Join(lines, "\n")
}
