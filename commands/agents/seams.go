package agents

import "os"

// readlinker is the narrow collaborator interface that ensureImportRepoAgentsSlot
// and cleanupManagedAgentRepoPath need to drive their defensive os.Readlink
// error branches. Each call site has just observed via os.Lstat that the path
// is a symlink, so the matching os.Readlink failure is unreachable in normal
// fixtures — interface-DI lets a test inject a fake Readlink that returns a
// sentinel error.
//
// Scope: exactly one method (os.Readlink). Per docs/TEST_SEAMS.md naming, a
// single-method collaborator uses the method-name + "-er" form (Sonar S8196).
// Both files in this package that need this seam share the same role
// ("the Readlink the agents-import / agents-remove flows use"), so the
// agents package shares one role-named interface rather than per-file
// duplicates — matching the platform package precedent (see
// internal/platform/io.go's platformIO comment) and the convention's
// "convert the whole package as one unit" rule.
type readlinker interface {
	Readlink(name string) (string, error)
}

// stdReadlinker is the production readlinker backed by os.Readlink. It is
// what ImportAgentIn and RemoveAgentIn pass into the unexported helpers
// when there is no test override.
type stdReadlinker struct{}

func (stdReadlinker) Readlink(name string) (string, error) { return os.Readlink(name) }
