package links

// Managed-resource `.gitignore` auto-fill in consuming projects
// (config-distribution-model §15 / D14, R8).
//
// `da` owns a single delimited, idempotent block in each consuming project's
// `.gitignore`. Inside the block live the things that are *materialized* into
// the repo from the resolved set and must therefore stay out of git: projected
// platform links, generated platform configs, materialized asset units, and the
// machine-local `.agentsrc.local.json` overlay (the `.git/config` analog). The
// committed resolved-state contract — `.agentsrc.json` and `.agentsrc.lock`
// (the `uv.lock` analog) — is deliberately NOT ignored, so a defensively-passed
// committed path is filtered back out rather than silently dropped from git.
//
// The source of truth for what to ignore is the resolved set the caller already
// computed from the lock + commands — never leaked git state. The caller passes
// the materialized output paths; this file converts them into a deterministic,
// converging block. Re-running with the same inputs yields byte-identical
// output (R8: regenerated, not appended), and the user's own ignore lines
// outside the markers are preserved verbatim.
//
// This is the consuming-project counterpart to the `local` source's provenance
// gitignore (internal/config EnsureProvenanceGitignore, D7): that one keeps
// remote-materialized assets out of the `~/.agents` repo; this one keeps every
// projected/generated output out of an arbitrary project consuming dot-agents.
// They own DISTINCT marker blocks so the two never fight over the same lines.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// managedGitignoreBegin / managedGitignoreEnd delimit the da-owned block in a
// consuming project's `.gitignore`. Everything between the markers is
// regenerated on each EnsureManagedGitignore call; anything outside them
// (user-authored ignores) is preserved verbatim. The marker text is distinct
// from the `local` source provenance block so a repo that is BOTH a dot-agents
// consumer and the local source carries two non-overlapping managed blocks.
const (
	managedGitignoreBegin = "# >>> dot-agents managed (project outputs) >>>"
	managedGitignoreEnd   = "# <<< dot-agents managed (project outputs) <<<"
	managedGitignoreFile  = ".gitignore"
)

// alwaysIgnored are the entries the managed block always carries regardless of
// the caller's projected-output set: the machine-local overlay manifest is
// per-machine state and must never be committed (D14). It is added even when
// the caller passes no projected paths, so the block is present (not empty) for
// any resolved project.
var alwaysIgnored = []string{".agentsrc.local.json"}

// neverIgnored are the committed resolved-state contract files. They are the
// `uv.lock`-style record the whole model depends on being tracked, so they are
// filtered out of the managed block even if a caller passes them by mistake —
// the block must never make the lock or manifest invisible to git.
var neverIgnored = map[string]bool{
	".agentsrc.json": true,
	".agentsrc.lock": true,
}

// EnsureManagedGitignore writes the idempotent da-owned block into the
// consuming project's `.gitignore` (rooted at repoRoot) so every materialized
// output in ignorePaths — projected links, generated platform configs,
// materialized asset units — plus the always-ignored machine-local overlay are
// excluded from git, while the committed `.agentsrc.json`/`.agentsrc.lock`
// contract stays tracked (§15 / D14 / R8).
//
// It is convergent: the managed block is regenerated (sorted + de-duplicated)
// on every call, user-authored content outside the markers is preserved, and
// re-running with the same inputs produces byte-identical output (no
// append-duplication). A missing `.gitignore` is created; an empty repoRoot is
// an error. The block is always present (it carries the always-ignored overlay)
// even when ignorePaths is empty.
func EnsureManagedGitignore(repoRoot string, ignorePaths []string) error {
	if strings.TrimSpace(repoRoot) == "" {
		return fmt.Errorf("links: gitignore auto-fill: empty repo root")
	}
	path := filepath.Join(repoRoot, managedGitignoreFile)
	existing, err := readManagedGitignore(path)
	if err != nil {
		return err
	}
	outside := stripManagedGitignoreBlock(existing)
	next := joinManagedGitignore(outside, renderManagedGitignoreBlock(ignorePaths))
	if err := fsops.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("links: gitignore auto-fill: mkdir %s: %w", filepath.Dir(path), err)
	}
	return fsops.WriteFileAtomic(path, []byte(next))
}

// readManagedGitignore reads the .gitignore at path, treating a missing file as
// empty so a first run on a project with no .gitignore still creates one.
func readManagedGitignore(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("links: gitignore auto-fill: read %s: %w", path, err)
	}
	return string(data), nil
}

// stripManagedGitignoreBlock returns content with the da-owned block (markers
// inclusive) removed, leaving user-authored lines intact. Content with no
// managed block is returned unchanged. A begin marker with no matching end (a
// hand-truncated file) drops to end-of-file so a corrupted block cannot leak
// stale managed lines back into the user's section.
func stripManagedGitignoreBlock(content string) string {
	lines := splitGitignoreLines(content)
	kept := make([]string, 0, len(lines))
	inBlock := false
	for _, line := range lines {
		switch {
		case line == managedGitignoreBegin:
			inBlock = true
		case line == managedGitignoreEnd:
			inBlock = false
		case !inBlock:
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// renderManagedGitignoreBlock builds the da-owned block: the always-ignored
// overlay entry plus the caller's projected outputs, minus any committed
// contract files, normalized for a stable diff. The block is always non-empty
// because alwaysIgnored is always present.
func renderManagedGitignoreBlock(ignorePaths []string) string {
	entries := normalizeIgnoreEntries(append(append([]string{}, alwaysIgnored...), ignorePaths...))
	lines := make([]string, 0, len(entries)+2)
	lines = append(lines, managedGitignoreBegin)
	lines = append(lines, entries...)
	lines = append(lines, managedGitignoreEnd)
	return strings.Join(lines, "\n")
}

// normalizeIgnoreEntries slash-normalizes, trims, drops blanks, filters out the
// committed contract files (neverIgnored), de-duplicates, and sorts the entries
// so the managed block is deterministic regardless of caller ordering or
// duplicate inputs. A trailing-slash directory form (e.g. ".claude/") and its
// plain form are distinct gitignore patterns and are NOT collapsed together.
func normalizeIgnoreEntries(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		// Normalize to forward slashes unconditionally (not filepath.ToSlash,
		// which only rewrites the OS-native separator): git patterns always use
		// "/", so a Windows-produced path must read identically on POSIX.
		norm := strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
		if norm == "" || seen[norm] {
			continue
		}
		if neverIgnored[strings.TrimSuffix(norm, "/")] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	sort.Strings(out)
	return out
}

// joinManagedGitignore appends the always-present managed block to the
// preserved (outside-block) content, collapsing trailing blank noise and
// guaranteeing exactly one trailing newline so the file never accretes blank
// lines across runs. The managed block is the stable tail of the file; an empty
// outside (a first run with no .gitignore, or a file that was only the managed
// block) yields just the block. block is always non-empty by construction —
// renderManagedGitignoreBlock always carries the always-ignored overlay — so
// there is no empty-output case to guard.
func joinManagedGitignore(outside, block string) string {
	outside = strings.TrimRight(outside, "\n")
	if strings.TrimSpace(outside) == "" {
		return block + "\n"
	}
	return outside + "\n" + block + "\n"
}

// splitGitignoreLines splits content into lines without a trailing empty
// element when the content ends in a newline, so round-tripping the file does
// not accrete blank lines.
func splitGitignoreLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(content, "\n"), "\n")
}
