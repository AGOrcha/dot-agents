package skills

import (
	"os"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

// skillsIO is the narrow filesystem + config-loader collaborator the skills
// package's free-function units need. It is the interface-DI replacement for
// the legacy `var osMkdirAll = os.MkdirAll`-style package-level func-var seams
// formerly defined in seams.go (see docs/TEST_SEAMS.md and the
// seam-interface-di-migration plan).
//
// Scope: exactly the four operations whose err != nil branches cannot be
// triggered from a writable tmp fixture (MkdirAll on a tmp dir, WriteFile to
// a writable path, Symlink onto a fresh target) plus the config.Load wrapper
// whose disk-IO failure is otherwise hard to drive deterministically. Every
// production call site passes stdSkillsIO{}; tests pass fakeSkillsIO with the
// specific operation overridden to return an injected error.
//
// The whole skills package shares one role-named interface rather than
// per-file duplicates because every file's seam need is the same role —
// "the filesystem + config-loader the skills package uses for scaffolding".
// Splitting it per file would be mechanical duplication, not narrower
// scoping. The skills package IS the unit (matching the convention's
// "convert the whole package as one unit" rule).
type skillsIO interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(name string, data []byte, perm os.FileMode) error
	Symlink(oldname, newname string) error
	ConfigLoad() (*config.Config, error)
}

// stdSkillsIO is the production skillsIO backed by the os package and
// config.Load. It is what every exported wrapper passes when there is no
// test override.
type stdSkillsIO struct{}

func (stdSkillsIO) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (stdSkillsIO) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

// Symlink routes through internal/links.Symlink so the managed-link
// fallback chain applies on Windows: POSIX gets a real os.Symlink, Windows
// gets a directory junction (for dir targets) or hard link (for file
// targets), neither of which requires SeCreateSymbolicLinkPrivilege /
// Developer Mode. Matches the existing commands/agents/import.go pattern;
// fixes the EnsureUserSkillLinks path which previously raw-called
// os.Symlink and silently no-op'd on stock Windows.
func (stdSkillsIO) Symlink(oldname, newname string) error {
	return links.Symlink(oldname, newname)
}

func (stdSkillsIO) ConfigLoad() (*config.Config, error) {
	return config.Load()
}
