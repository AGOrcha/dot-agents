package agents

import "os"

// Test seams: replaced in tests to drive the defensive TOCTOU error branches
// in ensureImportRepoAgentsSlot and cleanupManagedAgentRepoPath. Production
// code never overrides these. Each branch follows an os.Lstat that has just
// confirmed the path is a symlink, so the matching os.Readlink failure is
// otherwise unreachable from a unit test.
var (
	osReadlink = os.Readlink
)
