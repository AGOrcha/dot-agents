package settings

import "github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"

// exampleBlock is the package-local alias for the canonical example-block
// helper. Mirrors agents.exampleBlock / skills.exampleBlock so cmd.go reads
// the same way across the three subpackages.
func exampleBlock(lines ...string) string {
	return cmdutil.CanonicalCmdExampleBlock(lines...)
}
