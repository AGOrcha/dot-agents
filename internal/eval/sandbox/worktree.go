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

// claimSuffix names the sibling claim file (<runsRoot>/<run-id>.claim) that
// atomically reserves a run id: it is created with O_EXCL before anything
// shared exists, so two provisions can never share a run dir even if they
// derive identical ids. maxClaimAttempts bounds the regenerate-and-retry
// loop on claim contention.
const (
	claimSuffix      = ".claim"
	maxClaimAttempts = 3
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

	// Determinism seams for tests. addWorktree and pruneMeta default to the
	// gitwt calls under mu; readDir defaults to os.ReadDir. Tests inject
	// failures through these because the corresponding real errors cannot be
	// staged portably — Windows maps directory-shape errors
	// (ERROR_DIRECTORY, ERROR_PATH_NOT_FOUND) to "not exist", so fs-layout
	// tricks that force ENOTDIR on Unix silently succeed there.
	now         func() time.Time
	randRead    func([]byte) (int, error)
	addWorktree func(name, path string, base plumbing.Hash) error
	readDir     func(name string) ([]os.DirEntry, error)
	pruneMeta   func() ([]string, error)
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
	s := &worktreeSandbox{
		repoPath:  cfg.RepoPath,
		runsRoot:  root,
		retention: retention,
		mgr:       mgr,
		now:       time.Now,
		randRead:  cryptorand.Read,
	}
	s.addWorktree = s.addDetachedLocked
	s.readDir = os.ReadDir
	s.pruneMeta = s.pruneMetaLocked
	return s, nil
}

// addDetachedLocked is the default addWorktree seam: gitwt AddDetached under
// the manager mutex.
func (s *worktreeSandbox) addDetachedLocked(name, path string, base plumbing.Hash) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mgr.AddDetached(name, path, base)
}

// pruneMetaLocked is the default pruneMeta seam: gitwt Prune under the
// manager mutex.
func (s *worktreeSandbox) pruneMetaLocked() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mgr.Prune()
}

// Provision implements Sandbox. The working tree is checked out at the
// source repo's current HEAD, which is recorded in the instance and the
// on-disk marker for reproducibility (R10).
func (s *worktreeSandbox) Provision(ctx context.Context, spec *eval.TaskSpec) (*Instance, error) {
	if spec == nil {
		return nil, ErrNilTaskSpec
	}
	if strings.TrimSpace(spec.TaskID) == "" {
		return nil, ErrEmptyTaskID
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sandbox: provision: %w", err)
	}
	base, err := headCommit(s.repoPath)
	if err != nil {
		return nil, err
	}
	runID, err := s.claimRunID(spec.TaskID)
	if err != nil {
		return nil, err
	}
	return s.materialize(runID, base)
}

// claimRunID atomically reserves a fresh run id by exclusively creating its
// claim file. The O_EXCL create is the linearization point: a concurrent
// provision that derives the same id loses the create and regenerates.
// Contention beyond maxClaimAttempts (only possible with a wedged entropy
// source) fails loudly rather than degrading.
func (s *worktreeSandbox) claimRunID(taskID string) (string, error) {
	if err := fsops.MkdirAll(s.runsRoot, 0o755); err != nil {
		return "", fmt.Errorf("sandbox: create runs root: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt < maxClaimAttempts; attempt++ {
		runID, err := s.newRunID(taskID)
		if err != nil {
			return "", err
		}
		claim, err := os.OpenFile(filepath.Join(s.runsRoot, runID+claimSuffix), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = claim.Close()
			return runID, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("sandbox: claim run id %q: %w", runID, err)
		}
		lastErr = err
	}
	return "", fmt.Errorf("sandbox: no unique run id after %d attempts: %w", maxClaimAttempts, lastErr)
}

// releaseClaim best-effort removes a run's claim file. Abandoned provisions
// release it directly; completed runs release it in removeRunTrees.
func (s *worktreeSandbox) releaseClaim(runID string) {
	_ = fsops.Remove(filepath.Join(s.runsRoot, runID+claimSuffix))
}

// materialize creates the run dir contents (scratch home, linked worktree,
// marker) and assembles the Instance for an already-claimed run id. The
// claim guarantees no concurrent twin; the residual dir check guards the
// clock-rewind case where a claimed id matches an old run dir retained for
// its sidecars — building into (or rolling back over) retained data is
// never allowed. On a mid-flight failure the rollback removes everything
// this call created, so a failed Provision leaks nothing (R8).
func (s *worktreeSandbox) materialize(runID string, base plumbing.Hash) (*Instance, error) {
	runDir := filepath.Join(s.runsRoot, runID)
	workdir := filepath.Join(runDir, worktreeDirName)
	home := filepath.Join(runDir, homeDirName)
	wtName := gitwt.SafeName(runID)

	if _, err := os.Stat(runDir); err == nil {
		s.releaseClaim(runID)
		return nil, fmt.Errorf("sandbox: run dir %q already exists (run id collision)", runDir)
	}
	if err := fsops.MkdirAll(home, 0o755); err != nil {
		s.rollback(runID, wtName, false)
		return nil, fmt.Errorf("sandbox: create scratch home for run %q: %w", runID, err)
	}
	if err := s.addWorktree(wtName, workdir, base); err != nil {
		s.rollback(runID, wtName, false)
		return nil, fmt.Errorf("sandbox: provision worktree for run %q: %w", runID, err)
	}
	if err := s.writeMarker(runDir, runID, wtName, base); err != nil {
		s.rollback(runID, wtName, true)
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

// rollback best-effort removes everything a failed materialize created. The
// run dir was claimed fresh by materialize, so no sidecars can exist yet and
// removing the whole dir cannot destroy retained data. worktreeCreated
// selects whether gitwt admin metadata must be released first. Rollback
// errors are deliberately dropped — the original provisioning failure is the
// error the caller must see, and anything left behind is reclaimed by the
// markerless sweep in PruneStale.
func (s *worktreeSandbox) rollback(runID, wtName string, worktreeCreated bool) {
	if worktreeCreated {
		_ = s.removeRunTrees(runID, wtName)
	}
	_ = fsops.RemoveAll(filepath.Join(s.runsRoot, runID))
	s.releaseClaim(runID)
}

// PruneStale implements Sandbox: it removes the working trees of runs past
// the retention window, then clears any orphaned gitwt admin metadata
// (worktree dirs deleted out-of-band).
func (s *worktreeSandbox) PruneStale(ctx context.Context) ([]string, error) {
	entries, err := s.readDir(s.runsRoot)
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
		if !entry.IsDir() {
			s.sweepOrphanClaim(entry, cutoff)
			continue
		}
		wtName, stale := s.staleWorktreeName(entry, cutoff)
		if !stale {
			continue
		}
		removed, err := s.pruneRunTrees(entry.Name(), wtName)
		if err != nil {
			return pruned, err
		}
		if removed {
			pruned = append(pruned, entry.Name())
		}
	}
	if _, err := s.pruneMeta(); err != nil {
		return pruned, fmt.Errorf("sandbox: prune worktree metadata: %w", err)
	}
	return pruned, nil
}

// sweepOrphanClaim removes an aged run-id claim file whose run dir no
// longer exists — the residue of a provision that crashed between claiming
// the id and materializing the run (or whose rollback died). Fresh claims
// and claims whose run dir is live are left alone (the dir's own staleness
// path releases those); non-claim files are foreign and ignored. Best
// effort: a failed removal is retried by the next sweep.
func (s *worktreeSandbox) sweepOrphanClaim(entry os.DirEntry, cutoff time.Time) {
	name := entry.Name()
	if !strings.HasSuffix(name, claimSuffix) {
		return
	}
	info, err := entry.Info()
	if err != nil || info.ModTime().After(cutoff) {
		return
	}
	if _, err := os.Stat(filepath.Join(s.runsRoot, strings.TrimSuffix(name, claimSuffix))); err == nil {
		return
	}
	_ = fsops.Remove(filepath.Join(s.runsRoot, name))
}

// staleWorktreeName decides whether a runs-root dir entry is past the
// retention cutoff and names the gitwt worktree to release. Marker-bearing
// run dirs use the marker's timestamp and recorded worktree name. Markerless
// dirs fall back to the dir's mtime and the deterministic SafeName encoding:
// a markerless run dir is either a leaked partial provision (a crash between
// run-dir creation and marker write) or an already-swept run retaining only
// sidecars — pruneRunTrees only ever removes trees, so sweeping either once
// aged is safe.
func (s *worktreeSandbox) staleWorktreeName(entry os.DirEntry, cutoff time.Time) (string, bool) {
	if m, provisionedAt, ok := s.readMarker(entry); ok {
		return m.WorktreeName, !provisionedAt.After(cutoff)
	}
	info, err := entry.Info()
	if err != nil {
		return "", false
	}
	return gitwt.SafeName(entry.Name()), !info.ModTime().After(cutoff)
}

// pruneRunTrees removes a stale run's trees and reports whether anything was
// actually on disk to remove, so already-swept run dirs (sidecars only) are
// not re-reported on every sweep. A run dir left completely empty afterwards
// is provision litter, not a retained record, and is dropped too.
func (s *worktreeSandbox) pruneRunTrees(runID, wtName string) (bool, error) {
	runDir := filepath.Join(s.runsRoot, runID)
	if !anyExists(
		filepath.Join(runDir, worktreeDirName),
		filepath.Join(runDir, homeDirName),
		filepath.Join(runDir, markerName),
		filepath.Join(s.runsRoot, runID+claimSuffix),
	) {
		return false, nil
	}
	if err := s.removeRunTrees(runID, wtName); err != nil {
		return false, err
	}
	if remaining, err := os.ReadDir(runDir); err == nil && len(remaining) == 0 {
		_ = fsops.RemoveAll(runDir)
	}
	return true, nil
}

// anyExists reports whether any of the given paths exists on disk.
func anyExists(paths ...string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// readMarker loads the marker sidecar for one runs-root dir entry (the
// caller has already established entry is a directory). The boolean is
// false when the dir has no well-formed marker; corrupt markers are treated
// as absent so the markerless-sweep policy decides their fate.
func (s *worktreeSandbox) readMarker(entry os.DirEntry) (marker, time.Time, bool) {
	var m marker
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

// removeRunTrees removes a run's working tree, scratch home, marker, and
// run-id claim while preserving the run dir itself and any sidecars in it
// (OQ6: sidecars are retained indefinitely; only working trees are subject
// to retention). It is idempotent so Cleanup and PruneStale can safely race
// a prior partial cleanup.
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
	s.releaseClaim(runID)
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

// newRunID derives a Windows-safe run identifier from the task id, a compact
// UTC timestamp, and a random suffix; claimRunID makes it unique. A failed
// or short entropy read fails loudly — a silently low-entropy suffix is what
// would make claim collisions real.
func (s *worktreeSandbox) newRunID(taskID string) (string, error) {
	buf := make([]byte, 4)
	n, err := s.randRead(buf)
	if err != nil {
		return "", fmt.Errorf("sandbox: derive run id: %w", err)
	}
	if n != len(buf) {
		return "", fmt.Errorf("sandbox: derive run id: short entropy read (%d of %d bytes)", n, len(buf))
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
