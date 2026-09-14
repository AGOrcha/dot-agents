package crgbehavior

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/go-git/go-git/v6/plumbing"

	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/gitwt"
)

// TaskState is the dual-read input for ONE pinned review task, materialized at
// that task's OWN commit.
//
// Every field is produced from a graph built at the task's SHA — never from a
// graph built at HEAD and queried with historical paths. Replaying a historical
// changed-file list against a current graph cannot detect a historical-output
// regression at all: the symbols it resolves are today's symbols, and a path
// that moved silently resolves to a different symbol, so agreement is an
// artifact of the shared input rather than evidence about the release.
type TaskState struct {
	// Commit is the SHA both sides were materialized at.
	Commit string `json:"commit"`
	// Release and Schema record which bridge produced this state.
	Release ReleaseInfo  `json:"release"`
	Schema  SchemaReport `json:"schema"`
	// Views is everything the release persisted at Commit.
	Views BridgeViews `json:"views"`
	// Impact is the release's own blast-radius answer for the task.
	Impact BridgeImpact `json:"-"`
	// FTS maps each of the task's changed identifiers to the release's own
	// FTS5 MATCH result set.
	FTS map[string][]string `json:"fts"`
	// Lifecycle is the release's observed build/postprocess staleness
	// contract at this commit.
	Lifecycle LifecycleObservation `json:"lifecycle"`
	// beforeState is the post-build summary-table sample the lifecycle probe
	// compares against; it is internal plumbing between the two probe halves.
	beforeState FlowState
}

// Materializer produces one pinned task's state. Production materializes an
// isolated worktree per SHA; tests inject a recorded double.
type Materializer interface {
	Materialize(task Task) (TaskState, error)
}

// WorktreeMaterializer builds BOTH sides' state at each pinned SHA by checking
// that commit out into an isolated linked worktree and running a full pinned-
// release build there.
//
// The worktree lifecycle is native (internal/gitwt over go-git); the gate runs
// no git subprocess. It is deliberately expensive: one worktree and one full
// graph build per corpus task. That cost is the price of the criterion the gate
// claims — a historical review task is only replayed honestly against the
// repository as it was at that commit.
type WorktreeMaterializer struct {
	// RepoRoot is the repository whose history the corpus is pinned from.
	RepoRoot string
	// WorkDir is where the per-commit worktrees are created.
	WorkDir string
	// Release is the pinned release every worktree must be built with.
	Release Release
	// Depth and MaxResults parameterize the release's impact query.
	Depth, MaxResults int
	// Log receives progress lines; materialization takes minutes per commit.
	Log func(string)

	repo      repoReader
	worktrees gitwt.Manager
}

// NewWorktreeMaterializer binds a materializer to a repository.
func NewWorktreeMaterializer(repoRoot, workDir string, rel Release, depth, maxResults int,
	log func(string)) (*WorktreeMaterializer, error) {
	repo, err := openRepo(repoRoot)
	if err != nil {
		return nil, err
	}
	worktrees, err := gitwt.NewManager(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: open worktree manager for %s: %w", repoRoot, err)
	}
	return &WorktreeMaterializer{
		RepoRoot: repoRoot, WorkDir: workDir, Release: rel,
		Depth: depth, MaxResults: maxResults, Log: log,
		repo: repo, worktrees: worktrees,
	}, nil
}

// Materialize checks the task's commit out into an isolated worktree, builds
// the pinned release's graph there, and reads every view, query answer and
// lifecycle observation the comparison needs — all at that one SHA.
func (m *WorktreeMaterializer) Materialize(task Task) (state TaskState, err error) {
	commit, err := m.repo.Resolve(task.Commit)
	if err != nil {
		return TaskState{}, err
	}
	// The resolved hash is always 40 hex characters, so this name always
	// satisfies go-git's ^[a-zA-Z0-9-]+$ worktree-name constraint — and it
	// stays readable, which matters when a run is being debugged.
	name := "crg-" + short(commit.String())
	dir := filepath.Join(m.WorkDir, name)
	if err := m.addWorktree(name, dir, commit); err != nil {
		return TaskState{}, err
	}
	defer func() {
		if rmErr := m.removeWorktree(name, dir); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	bridge, err := NewLiveBridge(dir, m.Release)
	if err != nil {
		return TaskState{}, err
	}
	m.logf("building %s %s graph at %s", PackageName, m.Release.Version, short(task.Commit))
	if err := bridge.Build(); err != nil {
		return TaskState{}, err
	}
	state, err = m.readBuiltState(bridge, task)
	if err != nil {
		return TaskState{}, err
	}
	state.Lifecycle, err = m.probeLifecycle(bridge, state.beforeState)
	state.beforeState = FlowState{}
	return state, err
}

// readBuiltState reads everything the comparison needs out of the freshly built
// graph, BEFORE the lifecycle probe mutates it. Reading first is what lets the
// probe run in place without a second build or a database copy.
func (m *WorktreeMaterializer) readBuiltState(bridge *LiveBridge, task Task) (TaskState, error) {
	store, err := bridge.Open(m.Release)
	if err != nil {
		return TaskState{}, err
	}
	defer store.Close()
	views, err := store.Views()
	if err != nil {
		return TaskState{}, err
	}
	fts, err := searchIdentifiers(store, task.Identifiers)
	if err != nil {
		return TaskState{}, err
	}
	impact, err := bridge.ImpactRadius(store.Normalizer(), task.ChangedFiles, m.Depth, m.MaxResults)
	if err != nil {
		return TaskState{}, err
	}
	before, err := store.FlowState()
	if err != nil {
		return TaskState{}, err
	}
	return TaskState{
		Commit:      task.Commit,
		Release:     store.Release(),
		Schema:      store.Schema(),
		Views:       views,
		Impact:      impact,
		FTS:         fts,
		beforeState: before,
	}, nil
}

// probeLifecycle runs the release's standalone postprocess against the built
// graph and samples the summary tables again, so the gate asserts the
// release's actual staleness contract rather than a documented claim.
func (m *WorktreeMaterializer) probeLifecycle(bridge *LiveBridge, before FlowState) (LifecycleObservation, error) {
	if err := bridge.Postprocess(); err != nil {
		return LifecycleObservation{}, err
	}
	store, err := bridge.Open(m.Release)
	if err != nil {
		return LifecycleObservation{}, err
	}
	defer store.Close()
	after, err := store.FlowState()
	if err != nil {
		return LifecycleObservation{}, err
	}
	return ObserveLifecycle(before, after), nil
}

// searchIdentifiers runs the release's own FTS5 search for every declaration
// identifier the task changed.
func searchIdentifiers(store *BridgeStore, identifiers []string) (map[string][]string, error) {
	out := make(map[string][]string, len(identifiers))
	for _, term := range identifiers {
		hits, err := store.SearchFTS(term)
		if err != nil {
			return nil, err
		}
		sort.Strings(hits)
		out[term] = hits
	}
	return out, nil
}

// addWorktree creates an isolated detached worktree at commit, clearing
// anything a previously interrupted run left behind.
func (m *WorktreeMaterializer) addWorktree(name, dir string, commit plumbing.Hash) error {
	if err := fsops.MkdirAll(m.WorkDir, 0o755); err != nil {
		return fmt.Errorf("crgbehavior: create worktree parent %s: %w", m.WorkDir, err)
	}
	if err := m.removeWorktree(name, dir); err != nil {
		return err
	}
	if err := fsops.RemoveAll(dir); err != nil {
		return fmt.Errorf("crgbehavior: clear stale worktree %s: %w", dir, err)
	}
	if err := m.worktrees.AddDetached(name, dir, commit); err != nil {
		return fmt.Errorf("crgbehavior: materialize %s: %w", short(commit.String()), err)
	}
	return nil
}

// removeWorktree tears the isolated worktree down. A worktree that was never
// registered is not an error — that is the first-run state.
func (m *WorktreeMaterializer) removeWorktree(name, dir string) error {
	if err := m.worktrees.Remove(name, dir); err != nil && !errors.Is(err, gitwt.ErrWorktreeNotFound) {
		return fmt.Errorf("crgbehavior: remove worktree %s: %w", dir, err)
	}
	return nil
}

// logf reports progress when the caller asked for it.
func (m *WorktreeMaterializer) logf(format string, args ...any) {
	if m.Log != nil {
		m.Log(fmt.Sprintf(format, args...))
	}
}
