package crgbehavior

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/execabs"
)

// DefaultCommitCount is the default size of the review-task corpus: the last N
// commits on the generation ref. Large enough that a systematic divergence
// shows up on several tasks, small enough that a full dual-read run (one live
// bridge query per task) stays interactive.
const DefaultCommitCount = 25

// DefaultRef is the git ref the corpus window is taken from.
const DefaultRef = "origin/master"

// graphIndexedExts are the source extensions the CRG graph indexes. A commit's
// other files (docs, YAML, lockfiles) carry no symbols, so they are not part of
// a review task's graph query input.
var graphIndexedExts = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".tsx": true,
	".js": true, ".jsx": true, ".rs": true, ".java": true, ".rb": true,
}

// declRe matches a declaration line added or removed by a diff hunk and
// captures the declared identifier. It covers the Go (func/type) and Python
// (def/class) declaration forms the indexed corpus is written in; a receiver
// clause on a Go method is skipped so the method name is captured.
var declRe = regexp.MustCompile(`^[+-]\s*(?:func|type|def|class)\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)

// diffFileRe matches the post-image path header of a diff hunk.
var diffFileRe = regexp.MustCompile(`^\+\+\+ b/(.+)$`)

// gitRunner runs git commands for one repository. The corpus builder depends on
// this seam rather than on exec directly, so every failure branch is reachable
// from a test without staging a broken repository.
type gitRunner interface {
	run(args ...string) (string, error)
}

// execGit is the production gitRunner.
type execGit struct{ repoRoot string }

func (g execGit) run(args ...string) (string, error) {
	full := append([]string{"-C", g.repoRoot}, args...)
	out, err := execabs.Command("git", full...).Output()
	if err != nil {
		return "", fmt.Errorf("crgbehavior: git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// BuildManifest derives a review-task corpus from real repository history: the
// last count commits on ref, with each commit's graph-indexable changed files
// and the declaration identifiers its diff added or removed. The result is
// pinned by commit SHA, so a later gate run replays exactly these tasks.
func BuildManifest(repoRoot, ref string, count int) (Manifest, error) {
	return buildManifest(execGit{repoRoot: repoRoot}, ref, count)
}

// buildManifest is BuildManifest over an injected git seam.
func buildManifest(g gitRunner, ref string, count int) (Manifest, error) {
	if ref == "" {
		ref = DefaultRef
	}
	if count <= 0 {
		count = DefaultCommitCount
	}
	head, err := g.run("rev-parse", ref)
	if err != nil {
		return Manifest{}, err
	}
	shas, err := commitWindow(g, ref, count)
	if err != nil {
		return Manifest{}, err
	}
	tasks, err := tasksFor(g, shas)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		SchemaVersion: ManifestSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		GeneratedFrom: ref,
		Head:          strings.TrimSpace(head),
		Tasks:         tasks,
	}, nil
}

// commitWindow returns the newest count commit SHAs reachable from ref.
func commitWindow(g gitRunner, ref string, count int) ([]string, error) {
	out, err := g.run("rev-list", "--no-merges", fmt.Sprintf("-n%d", count), ref)
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(out), nil
}

// tasksFor builds one review task per commit, dropping commits that touched no
// graph-indexable file (a docs-only commit issues no graph query).
func tasksFor(g gitRunner, shas []string) ([]Task, error) {
	tasks := make([]Task, 0, len(shas))
	for _, sha := range shas {
		task, ok, err := taskFor(g, sha)
		if err != nil {
			return nil, err
		}
		if ok {
			tasks = append(tasks, task)
		}
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("crgbehavior: no commit in the window touched a graph-indexable file")
	}
	return tasks, nil
}

// taskFor builds the review task for one commit from its subject and its
// patch: the patch yields both the changed files (post-image headers) and the
// declaration identifiers, so one diff read pins the whole query input. ok is
// false when the commit touched no graph-indexable source file.
func taskFor(g gitRunner, sha string) (Task, bool, error) {
	subject, err := g.run("show", "-s", "--format=%s", sha)
	if err != nil {
		return Task{}, false, err
	}
	diff, err := g.run("show", "--unified=0", "--format=", sha)
	if err != nil {
		return Task{}, false, err
	}
	files := indexableFiles(diffFiles(diff))
	if len(files) == 0 {
		return Task{}, false, nil
	}
	return Task{
		Commit:       sha,
		Subject:      strings.TrimSpace(subject),
		ChangedFiles: files,
		Identifiers:  changedIdentifiers(diff),
	}, true, nil
}

// diffFiles returns the post-image paths a patch touches. A deletion's
// /dev/null post-image carries no symbols to query and is skipped.
func diffFiles(diff string) []string {
	var out []string
	for _, line := range strings.Split(diff, "\n") {
		if m := diffFileRe.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// indexableFiles keeps the graph-indexed source files, sorted and de-duplicated.
func indexableFiles(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if !graphIndexedExts[strings.ToLower(filepath.Ext(n))] || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// changedIdentifiers extracts the declaration names a diff added or removed —
// the FTS query input for the commit's review task.
func changedIdentifiers(diff string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, line := range strings.Split(diff, "\n") {
		m := declRe.FindStringSubmatch(line)
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// nonEmptyLines splits command output into trimmed, non-empty lines.
func nonEmptyLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	return lines
}
