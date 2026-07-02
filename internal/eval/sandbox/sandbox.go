package sandbox

import (
	"context"
	"errors"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
)

// DefaultRetention is how long a provisioned working tree may linger on disk
// before PruneStale removes it. Seven days is the R4 spec's OQ6
// recommendation; sidecars under the run dir are never pruned by this
// package.
const DefaultRetention = 7 * 24 * time.Hour

// Typed errors callers can match with errors.Is.
var (
	// ErrRepoPathRequired is returned by NewWorktreeSandbox when the config
	// names no source repository.
	ErrRepoPathRequired = errors.New("sandbox: repo path is required")

	// ErrNilTaskSpec is returned by Provision when the spec is nil.
	ErrNilTaskSpec = errors.New("sandbox: task spec is nil")
)

// Config configures a worktree sandbox.
type Config struct {
	// RepoPath is the main working tree of the source repository eval runs
	// are provisioned from (the repo whose KG generated the tasks). Required.
	RepoPath string

	// RunsRoot is the directory run dirs are created under. Defaults to
	// <RepoPath>/.agents/eval/runs, the eval-namespaced root from the R4
	// spec.
	RunsRoot string

	// Retention is the working-tree retention window PruneStale enforces.
	// Zero or negative means DefaultRetention.
	Retention time.Duration
}

// Instance is one provisioned sandbox: the isolated working tree an agent
// operates in, plus the scratch environment that keeps it out of the
// operator's home. Instances are produced by Sandbox.Provision.
type Instance struct {
	// RunID uniquely identifies this eval run; it is the run dir's name.
	RunID string

	// RunDir is <RunsRoot>/<RunID>. Sidecar writers (taskspec.yaml,
	// eval-run.yaml) target this directory; it survives Cleanup.
	RunDir string

	// Workdir is the isolated working tree the agent operates in
	// (<RunDir>/worktree).
	Workdir string

	// BaseCommit is the source repo HEAD the working tree was checked out
	// at, recorded for reproducibility (R4 requirement R10).
	BaseCommit string

	// Env holds KEY=VALUE entries that pin the agent process's environment
	// to the sandbox (HOME and USERPROFILE point at the scratch home).
	// Append them after os.Environ() so they win.
	Env []string

	cleanup func() error
}

// Cleanup removes the instance's working tree, scratch home, and sandbox
// marker while preserving the run dir and any sidecars written into it. It
// is idempotent: a second call is a no-op.
func (in *Instance) Cleanup() error {
	if in.cleanup == nil {
		return nil
	}
	return in.cleanup()
}

// Sandbox provisions isolated per-run working directories (R4 spec D4.2).
// It is the provider swap point: the v1 worktree implementation, a future
// DockerSandbox, or the worktree-platform managed provider all sit behind
// this interface.
type Sandbox interface {
	// Provision creates an isolated working tree and scratch HOME for one
	// eval run of spec. The returned instance's Cleanup releases everything
	// except the run dir and its sidecars.
	Provision(ctx context.Context, spec *eval.TaskSpec) (*Instance, error)

	// PruneStale removes the working trees of runs older than the retention
	// window (R4 spec OQ6: default 7 days, pruned on the next eval run) and
	// returns the pruned run IDs. Sidecars are preserved.
	PruneStale(ctx context.Context) ([]string, error)
}
