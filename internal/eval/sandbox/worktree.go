package sandbox

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"go.yaml.in/yaml/v3"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/gitwt"
)

// Layout of one run dir under RunsRoot. worktreeDirName holds the linked git
// worktree the agent operates in, homeDirName is the scratch HOME, and
// markerName is the sandbox metadata sidecar retention pruning keys off.
const (
	worktreeDirName = "worktree"
	homeDirName     = "home"
	markerName      = "sandbox.yaml"
)

// dash separates the run-id components and is the sanitizer's replacement
// byte for characters that are not directory-name safe on every CI OS.
const dash = "-"

// runStampFormat is the compact, Windows-safe (colon-free) UTC timestamp
// embedded in run IDs for human navigation of the runs root.
const runStampFormat = "20060102T150405"

// marker is the per-run metadata sidecar written at provision time. It is
// what makes retention pruning possible after the process that provisioned
// the run has exited: it carries the gitwt worktree name (needed to release
// the admin metadata) and the provision timestamp (needed for staleness).
type marker struct {
	RunID         string `yaml:"run_id"`
	WorktreeName  string `yaml:"worktree_name"`
	BaseCommit    string `yaml:"base_commit"`
	ProvisionedAt string `yaml:"provisioned_at"`
}

// worktreeSandbox is the v1 Sandbox: a linked git worktree checked out
// detached at the source repo's HEAD, plus a scratch HOME, both under
// RunsRoot/<run-id>. See the package doc for why detached HEAD (not an
// ephemeral branch) is the ephemeral-checkout mechanism.
type worktreeSandbox struct {
	repoPath  string
	runsRoot  string
	retention time.Duration

	// mu serializes gitwt manager lifecycle calls (add/remove/prune); the
	// manager is not documented as safe for concurrent mutation.
	mu  sync.Mutex
	mgr gitwt.Manager

	// Determinism seams for tests.
	now      func() time.Time
	randRead func([]byte) (int, error)
}

var _ Sandbox = (*worktreeSandbox)(nil)

// NewWorktreeSandbox opens the source repository and returns the v1 worktree
// Sandbox for it, applying the Config defaults (runs root, retention).
func NewWorktreeSandbox(cfg Config) (Sandbox, error) {
	if strings.TrimSpace(cfg.RepoPath) == "" {
		return nil, ErrRepoPathRequired
	}
	mgr, err := gitwt.NewManager(cfg.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %w", err)
	}
	root := cfg.RunsRoot
	if root == "" {
		root = filepath.Join(cfg.RepoPath, ".agents", "eval", "runs")
	}
	retention := cfg.Retention
	if retention <= 0 {
		retention = DefaultRetention
	}
	return &worktreeSandbox{
		repoPath:  cfg.RepoPath,
		runsRoot:  root,
		retention: retention,
		mgr:       mgr,
		now:       time.Now,
		randRead:  cryptorand.Read,
	}, nil
}

// Provision implements Sandbox. The working tree is checked out at the
// source repo's current HEAD, which is recorded in the instance and the
// on-disk marker for reproducibility (R10).
func (s *worktreeSandbox) Provision(ctx context.Context, spec *eval.TaskSpec) (*Instance, error) {
	if spec == nil {
		return nil, ErrNilTaskSpec
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sandbox: provision: %w", err)
	}
	base, err := headCommit(s.repoPath)
	if err != nil {
		return nil, err
	}
	runID, err := s.newRunID(spec.TaskID)
	if err != nil {
		return nil, err
	}
	return s.materialize(runID, base)
}

// materialize creates the run dir contents (scratch home, linked worktree,
// marker) and assembles the Instance. On a mid-flight failure it rolls the
// already-created pieces back so a failed Provision leaks nothing (R8).
func (s *worktreeSandbox) materialize(runID string, base plumbing.Hash) (*Instance, error) {
	runDir := filepath.Join(s.runsRoot, runID)
	workdir := filepath.Join(runDir, worktreeDirName)
	home := filepath.Join(runDir, homeDirName)
	wtName := gitwt.SafeName(runID)

	if err := fsops.MkdirAll(home, 0o755); err != nil {
		return nil, fmt.Errorf("sandbox: create scratch home for run %q: %w", runID, err)
	}
	s.mu.Lock()
	err := s.mgr.AddDetached(wtName, workdir, base)
	s.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("sandbox: provision worktree for run %q: %w", runID, err)
	}
	if err := s.writeMarker(runDir, runID, wtName, base); err != nil {
		// Best-effort rollback; the marker failure is the error to surface.
		_ = s.removeRunTrees(runID, wtName)
		return nil, err
	}
	return &Instance{
		RunID:      runID,
		RunDir:     runDir,
		Workdir:    workdir,
		BaseCommit: base.String(),
		Env:        []string{"HOME=" + home, "USERPROFILE=" + home},
		cleanup:    func() error { return s.removeRunTrees(runID, wtName) },
	}, nil
}

// PruneStale implements Sandbox: it removes the working trees of runs whose
// marker predates the retention window, then clears any orphaned gitwt
// admin metadata (worktree dirs deleted out-of-band).
func (s *worktreeSandbox) PruneStale(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.runsRoot)
	if os.IsNotExist(err) {
		return nil, nil // no runs root yet — nothing to prune
	}
	if err != nil {
		return nil, fmt.Errorf("sandbox: read runs root: %w", err)
	}
	cutoff := s.now().Add(-s.retention)
	var pruned []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return pruned, fmt.Errorf("sandbox: prune: %w", err)
		}
		m, provisionedAt, ok := s.readMarker(entry)
		if !ok || provisionedAt.After(cutoff) {
			continue
		}
		if err := s.removeRunTrees(entry.Name(), m.WorktreeName); err != nil {
			return pruned, err
		}
		pruned = append(pruned, entry.Name())
	}
	s.mu.Lock()
	_, err = s.mgr.Prune()
	s.mu.Unlock()
	if err != nil {
		return pruned, fmt.Errorf("sandbox: prune worktree metadata: %w", err)
	}
	return pruned, nil
}

// readMarker loads the marker sidecar for one runs-root entry. The boolean
// is false when the entry is not a run dir with a well-formed marker;
// foreign or corrupt entries are skipped rather than deleted, so pruning
// never destroys something this package did not provision.
func (s *worktreeSandbox) readMarker(entry os.DirEntry) (marker, time.Time, bool) {
	var m marker
	if !entry.IsDir() {
		return m, time.Time{}, false
	}
	data, err := os.ReadFile(filepath.Join(s.runsRoot, entry.Name(), markerName))
	if err != nil {
		return m, time.Time{}, false
	}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return m, time.Time{}, false
	}
	provisionedAt, err := time.Parse(time.RFC3339, m.ProvisionedAt)
	if err != nil {
		return m, time.Time{}, false
	}
	return m, provisionedAt, true
}

// removeRunTrees removes a run's working tree, scratch home, and marker
// while preserving the run dir itself and any sidecars in it (OQ6: sidecars
// are retained indefinitely; only working trees are subject to retention).
// It is idempotent so Cleanup and PruneStale can safely race a prior
// partial cleanup.
func (s *worktreeSandbox) removeRunTrees(runID, wtName string) error {
	runDir := filepath.Join(s.runsRoot, runID)
	workdir := filepath.Join(runDir, worktreeDirName)
	s.mu.Lock()
	err := s.mgr.Remove(wtName, workdir)
	s.mu.Unlock()
	if errors.Is(err, gitwt.ErrWorktreeNotFound) {
		// Admin metadata already gone (prior partial cleanup); still remove
		// any leftover working tree directory.
		err = fsops.RemoveAll(workdir)
	}
	if err != nil {
		return fmt.Errorf("sandbox: remove worktree for run %q: %w", runID, err)
	}
	if err := fsops.RemoveAll(filepath.Join(runDir, homeDirName)); err != nil {
		return fmt.Errorf("sandbox: remove scratch home for run %q: %w", runID, err)
	}
	markerPath := filepath.Join(runDir, markerName)
	if _, err := os.Stat(markerPath); err == nil {
		if err := fsops.Remove(markerPath); err != nil {
			return fmt.Errorf("sandbox: remove marker for run %q: %w", runID, err)
		}
	}
	return nil
}

// writeMarker persists the run's sandbox metadata sidecar atomically so a
// half-written marker can never be mistaken for a prunable run record.
func (s *worktreeSandbox) writeMarker(runDir, runID, wtName string, base plumbing.Hash) error {
	data, err := yaml.Marshal(&marker{
		RunID:         runID,
		WorktreeName:  wtName,
		BaseCommit:    base.String(),
		ProvisionedAt: s.now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("sandbox: marshal marker for run %q: %w", runID, err)
	}
	if err := fsops.WriteFileAtomic(filepath.Join(runDir, markerName), data); err != nil {
		return fmt.Errorf("sandbox: write marker for run %q: %w", runID, err)
	}
	return nil
}

// newRunID derives a unique, Windows-safe run identifier from the task id, a
// compact UTC timestamp, and a random suffix. Uniqueness comes from the
// suffix; the task id and timestamp exist for human navigation of the runs
// root.
func (s *worktreeSandbox) newRunID(taskID string) (string, error) {
	buf := make([]byte, 4)
	if _, err := s.randRead(buf); err != nil {
		return "", fmt.Errorf("sandbox: derive run id: %w", err)
	}
	stamp := s.now().UTC().Format(runStampFormat)
	return sanitizeID(taskID) + dash + stamp + dash + hex.EncodeToString(buf), nil
}

// headCommit resolves the source repo's current HEAD commit. This is a
// read-only repository query outside gitwt's worktree-lifecycle seam, so it
// goes straight to go-git — the same underlying mechanism gitwt wraps.
func headCommit(repoPath string) (plumbing.Hash, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("sandbox: open repo %q: %w", repoPath, err)
	}
	head, err := repo.Head()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("sandbox: resolve HEAD of %q: %w", repoPath, err)
	}
	return head.Hash(), nil
}

// sanitizeID lowercases id and maps every character outside [a-z0-9-] to a
// dash so the result is safe as a directory name on every OS in the CI
// matrix. An id that sanitizes to nothing falls back to "task".
func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteString(dash)
		}
	}
	out := strings.Trim(b.String(), dash)
	if out == "" {
		return "task"
	}
	return out
}
