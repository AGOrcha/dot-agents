//go:build !windows

package commands

import "github.com/NikashPrakash/dot-agents/commands/lifecycle"

// hasMultipleHardLinks delegates to the canonical implementation in
// commands/lifecycle. The build-tagged file moved with status.go in t08; this
// thin shim keeps doctor.go and add.go (still in root) compiling without
// requiring an out-of-scope edit to either. Per SHAPE.md OD-2 the export is
// reversed in t09 once doctor.go lands in the lifecycle package; this shim is
// deleted in t13 alongside the rest of the root lifecycle shims.
func hasMultipleHardLinks(path string) bool {
	return lifecycle.HasMultipleHardLinks(path)
}
