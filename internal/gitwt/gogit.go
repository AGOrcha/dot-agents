package gitwt

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	gogitwt "github.com/go-git/go-git/v6/x/plumbing/worktree"
)

// worktreeNameRE mirrors the charset go-git's x/plumbing/worktree enforces on
// worktree names. We validate eagerly so callers get the typed ErrInvalidName
// instead of go-git's untyped fmt.Errorf.
var worktreeNameRE = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)

// baseRefFile is the admin-dir file (under .git/worktrees/<name>/) where the
// recorded base ref lives until the wt2 registry takes over richer metadata.
const baseRefFile = "base-ref"

// errFmtWrap wraps a sentinel error with a contextual name/string argument.
const errFmtWrap = "%w: %s"

// manager is the go-git v6 implementation of Manager. It owns a worktree
// manager bound to the repository's shared storer.
type manager struct {
	repo *git.Repository
	mgr  *gogitwt.Worktree
}

// NewManager opens the repository at repoPath and returns a go-git-backed
// Manager for its linked worktrees. repoPath is the path to the main worktree
// (the dir containing .git), not a bare repo's git dir.
func NewManager(repoPath string) (Manager, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("gitwt: open repository %q: %w", repoPath, err)
	}
	return newManagerFromRepo(repo)
}

// newManagerFromRepo builds a manager from an already-open repository. Split out
// so tests can inject an in-process repo without a second PlainOpen.
func newManagerFromRepo(repo *git.Repository) (Manager, error) {
	wt, err := gogitwt.New(repo.Storer)
	if err != nil {
		return nil, fmt.Errorf("gitwt: init worktree manager: %w", err)
	}
	return &manager{repo: repo, mgr: wt}, nil
}

// SafeName encodes an arbitrary caller string (e.g. a branch or task name) into
// a worktree name that satisfies go-git's ^[a-zA-Z0-9-]+$ constraint. It is
// deterministic: the same input always yields the same name, so callers can
// re-derive the worktree name without storing it. The human-readable
// branch/path mapping belongs in the wt2 registry.
func SafeName(input string) string {
	sum := sha256.Sum256([]byte(input))
	return "wt-" + hex.EncodeToString(sum[:8])
}

func validateName(name string) error {
	if !worktreeNameRE.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return nil
}

func (m *manager) AddBranch(name, path string, base plumbing.Hash) error {
	if err := validateName(name); err != nil {
		return err
	}
	branchRef := plumbing.NewBranchReferenceName(name)
	if _, err := m.repo.Reference(branchRef, false); err == nil {
		return fmt.Errorf(errFmtWrap, ErrBranchExists, name)
	}
	if err := m.add(name, path, base, false); err != nil {
		return err
	}
	return m.RecordBaseRef(name, base)
}

func (m *manager) AddDetached(name, path string, commit plumbing.Hash) error {
	if err := validateName(name); err != nil {
		return err
	}
	if err := m.add(name, path, commit, true); err != nil {
		return err
	}
	return m.RecordBaseRef(name, commit)
}

// add is the shared create path. detached selects swarm-cd-style detached HEAD;
// otherwise go-git creates a new branch named identically to the worktree.
func (m *manager) add(name, path string, commit plumbing.Hash, detached bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("gitwt: create worktree parent dir: %w", err)
	}
	opts := []gogitwt.Option{gogitwt.WithCommit(commit)}
	if detached {
		opts = append(opts, gogitwt.WithDetachedHead())
	}
	err := m.mgr.Add(osfs.New(path), name, opts...)
	if errors.Is(err, gogitwt.ErrWorktreeAlreadyExists) {
		return fmt.Errorf(errFmtWrap, ErrWorktreeExists, name)
	}
	if err != nil {
		return fmt.Errorf("gitwt: add worktree %q: %w", name, err)
	}
	return nil
}

func (m *manager) Remove(name, path string) error {
	if err := validateName(name); err != nil {
		return err
	}
	err := m.mgr.Remove(name)
	if errors.Is(err, gogitwt.ErrWorktreeNotFound) {
		return fmt.Errorf(errFmtWrap, ErrWorktreeNotFound, name)
	}
	if err != nil {
		return fmt.Errorf("gitwt: remove worktree %q: %w", name, err)
	}
	if path != "" {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("gitwt: remove worktree dir %q: %w", path, err)
		}
	}
	return nil
}

func (m *manager) List() ([]string, error) {
	names, err := m.mgr.List()
	if err != nil {
		return nil, fmt.Errorf("gitwt: list worktrees: %w", err)
	}
	return names, nil
}

func (m *manager) Prune() ([]string, error) {
	names, err := m.List()
	if err != nil {
		return nil, err
	}
	var pruned []string
	for _, name := range names {
		path, ok := m.worktreeDir(name)
		if !ok {
			continue
		}
		if _, statErr := os.Stat(path); statErr == nil {
			continue // working tree still present — not stale
		} else if !os.IsNotExist(statErr) {
			return pruned, fmt.Errorf("gitwt: stat worktree dir %q: %w", path, statErr)
		}
		if err := m.mgr.Remove(name); err != nil && !errors.Is(err, gogitwt.ErrWorktreeNotFound) {
			return pruned, fmt.Errorf("gitwt: prune worktree %q: %w", name, err)
		}
		pruned = append(pruned, name)
	}
	return pruned, nil
}

func (m *manager) Open(path string) (Worktree, error) {
	// Guard: m.mgr.Open silently falls back to the main repo's storer when the
	// path's .git file is absent or not a valid "gitdir:" pointer (e.g. garbage
	// content), which would hand the caller the MAIN repo instead of the
	// isolated linked worktree — a cross-worktree contamination footgun. Require
	// a .git file that actually points at a linked-worktree admin dir.
	if err := assertLinkedWorktree(path); err != nil {
		return nil, fmt.Errorf("gitwt: open worktree %q: %w", path, err)
	}
	repo, err := m.mgr.Open(osfs.New(path))
	if err != nil {
		return nil, fmt.Errorf("gitwt: open worktree %q: %w", path, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("gitwt: worktree handle %q: %w", path, err)
	}
	return &worktree{repo: repo, wt: wt}, nil
}

// assertLinkedWorktree verifies that path is a linked worktree: its .git is a
// regular file containing a "gitdir: <admindir>" pointer, and that admin dir
// exists and carries the linked-worktree "commondir" marker. This rejects both
// a missing .git and a garbage .git (which would otherwise make go-git's Open
// silently fall back to the main repository).
func assertLinkedWorktree(path string) error {
	gitFile := filepath.Join(path, ".git")
	info, err := os.Stat(gitFile)
	if err != nil {
		return fmt.Errorf("not a linked worktree: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("not a linked worktree: %s is a directory (main worktree?)", gitFile)
	}
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return fmt.Errorf("not a linked worktree: %w", err)
	}
	const prefix = "gitdir:"
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, prefix) {
		return fmt.Errorf("not a linked worktree: .git has no gitdir pointer")
	}
	adminDir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if _, err := os.Stat(filepath.Join(adminDir, "commondir")); err != nil {
		return fmt.Errorf("not a linked worktree: admin dir invalid: %w", err)
	}
	return nil
}

// adminDir returns the .git/worktrees/<name> filesystem path for a worktree's
// admin metadata, and whether the worktree exists.
func (m *manager) adminDir(name string) (string, bool) {
	fs := m.repo.Storer.(interface{ Filesystem() billy.Filesystem }).Filesystem()
	path := filepath.Join(fs.Root(), "worktrees", name)
	if _, err := fs.Lstat(filepath.Join("worktrees", name)); err != nil {
		return "", false
	}
	return path, true
}

// worktreeDir resolves the working-tree directory for a linked worktree by
// reading its admin gitdir pointer (.git/worktrees/<name>/gitdir points at the
// worktree's own .git file).
func (m *manager) worktreeDir(name string) (string, bool) {
	dir, ok := m.adminDir(name)
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, "gitdir"))
	if err != nil {
		return "", false
	}
	gitFile := strings.TrimSpace(string(data))
	// gitFile is ".../<worktree>/.git"; the working tree is its parent.
	return filepath.Dir(gitFile), true
}

func (m *manager) RecordBaseRef(name string, base plumbing.Hash) error {
	dir, ok := m.adminDir(name)
	if !ok {
		return fmt.Errorf(errFmtWrap, ErrWorktreeNotFound, name)
	}
	content := base.String() + "\n"
	if err := os.WriteFile(filepath.Join(dir, baseRefFile), []byte(content), 0o644); err != nil {
		return fmt.Errorf("gitwt: record base ref for %q: %w", name, err)
	}
	return nil
}

func (m *manager) BaseRef(name string) (plumbing.Hash, error) {
	dir, ok := m.adminDir(name)
	if !ok {
		return plumbing.ZeroHash, fmt.Errorf(errFmtWrap, ErrWorktreeNotFound, name)
	}
	data, err := os.ReadFile(filepath.Join(dir, baseRefFile))
	if errors.Is(err, os.ErrNotExist) {
		return plumbing.ZeroHash, fmt.Errorf(errFmtWrap, ErrBaseRefNotRecorded, name)
	}
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("gitwt: read base ref for %q: %w", name, err)
	}
	return plumbing.NewHash(strings.TrimSpace(string(data))), nil
}

// worktree is the go-git implementation of Worktree.
type worktree struct {
	repo *git.Repository
	wt   *git.Worktree
}

func (w *worktree) Stage(path string) error {
	if _, err := w.wt.Add(path); err != nil {
		return fmt.Errorf("gitwt: stage %q: %w", path, err)
	}
	return nil
}

func (w *worktree) Status() (git.Status, error) {
	st, err := w.wt.Status()
	if err != nil {
		return nil, fmt.Errorf("gitwt: status: %w", err)
	}
	return st, nil
}

func (w *worktree) Commit(message string, opts *CommitOptions) (plumbing.Hash, error) {
	gitOpts := &git.CommitOptions{}
	if opts != nil {
		gitOpts.All = opts.All
		gitOpts.AllowEmptyCommits = opts.AllowEmpty
		if opts.AuthorName != "" || opts.AuthorEmail != "" {
			gitOpts.Author = &object.Signature{
				Name:  opts.AuthorName,
				Email: opts.AuthorEmail,
				When:  time.Now(),
			}
		}
	}
	hash, err := w.wt.Commit(message, gitOpts)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("gitwt: commit: %w", err)
	}
	return hash, nil
}

func (w *worktree) Head() (*plumbing.Reference, error) {
	ref, err := w.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("gitwt: head: %w", err)
	}
	return ref, nil
}

func (w *worktree) Branch() (string, error) {
	ref, err := w.Head()
	if err != nil {
		return "", err
	}
	if !ref.Name().IsBranch() {
		return "", nil
	}
	return ref.Name().Short(), nil
}
