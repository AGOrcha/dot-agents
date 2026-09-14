package graphstore

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/execabs"
)

// This file ports the version-control probes upstream code-review-graph
// v2.3.8 keeps in `code_review_graph/incremental.py`. Both code-graph
// providers depend on them — the bridge to answer `status --json` without
// shelling out to the Python CLI, the kg-native engine to resolve an
// incremental diff base and stamp build metadata — so there is exactly one
// spelling of each rule and the two backends cannot drift.

// VCS kinds `status --json` reports in its `vcs` field.
const (
	VCSGit  = "git"
	VCSSVN  = "svn"
	VCSNone = "none"
)

// gitRevParseCmd is the git subcommand every ref/SHA probe below drives.
const gitRevParseCmd = "rev-parse"

// gitTimeout bounds every probe below. Upstream reads CRG_GIT_TIMEOUT with a
// 30 second default; a probe that hangs must not wedge a build.
var gitTimeout = gitTimeoutFromEnv()

func gitTimeoutFromEnv() time.Duration {
	if raw := os.Getenv("CRG_GIT_TIMEOUT"); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 30 * time.Second
}

// safeGitRef is upstream's `_SAFE_GIT_REF`. A ref that does not match is
// rejected outright rather than handed to git, so a caller-supplied base can
// never smuggle an option or a shell metacharacter into the diff.
var safeGitRef = regexp.MustCompile(`^[A-Za-z0-9_.~^/@{}\-]+$`)

// RecurseSubmodulesDefault is upstream's CRG_RECURSE_SUBMODULES fallback,
// applied when a caller leaves the tri-state option unset.
func RecurseSubmodulesDefault() bool {
	switch strings.ToLower(os.Getenv("CRG_RECURSE_SUBMODULES")) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// RecurseSubmodules resolves the tri-state option: nil defers to the
// environment, non-nil forces the caller's choice.
func RecurseSubmodules(opt *bool) bool {
	if opt == nil {
		return RecurseSubmodulesDefault()
	}
	return *opt
}

// DetectVCS reports the version-control system backing root: "git" when a
// .git entry exists (worktrees carry a .git file, not a directory), "svn"
// when a .svn directory does, else "none".
func DetectVCS(root string) string {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return VCSGit
	}
	if _, err := os.Stat(filepath.Join(root, ".svn")); err == nil {
		return VCSSVN
	}
	return VCSNone
}

// GitBranchInfo returns root's current branch name and HEAD sha. Either is
// empty when git cannot answer; the probes only label a graph, so a failure
// is never fatal.
func GitBranchInfo(root string) (branch, sha string) {
	return gitOutput(root, gitRevParseCmd, "--abbrev-ref", "HEAD"),
		gitOutput(root, gitRevParseCmd, "HEAD")
}

// SVNInfo returns root's SVN branch path and revision string, mirroring
// upstream's parse of `svn info`: the branch is the trunk/branches/tags
// segment of the URL, falling back to its last path element.
func SVNInfo(root string) (branch, revision string) {
	out, err := runVCS(root, "svn", "info", "--non-interactive")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "URL: "):
			branch = svnBranchFromURL(strings.TrimSpace(line[len("URL: "):]))
		case strings.HasPrefix(line, "Revision: "):
			revision = strings.TrimSpace(line[len("Revision: "):])
		}
	}
	return branch, revision
}

// svnBranchFromURL extracts the branch segment upstream reports.
func svnBranchFromURL(url string) string {
	for _, marker := range []string{"/branches/", "/tags/", "/trunk"} {
		if idx := strings.Index(url, marker); idx >= 0 {
			return strings.TrimLeft(url[idx:], "/")
		}
	}
	if url == "" {
		return ""
	}
	parts := strings.Split(strings.TrimRight(url, "/"), "/")
	return parts[len(parts)-1]
}

// GitCommitExists reports whether ref resolves to a commit object that is
// still present in root. This is deliberately an object-existence check and
// not an ancestry check: a commit reachable only from a branch the working
// copy has since left is still a valid diff base.
func GitCommitExists(root, ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") || !safeGitRef.MatchString(ref) {
		return false
	}
	_, err := runVCS(root, "git", gitRevParseCmd, "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// gitOutput runs a git command and returns its trimmed stdout, or "".
func gitOutput(root string, args ...string) string {
	out, err := runVCS(root, "git", args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// runVCS runs a VCS binary in root and returns its stdout as text.
func runVCS(root, bin string, args ...string) (string, error) {
	out, err := runVCSRaw(root, bin, args...)
	return string(out), err
}

// runVCSRaw runs a VCS binary in root, bounded by gitTimeout, with stdin
// closed so a credential prompt can never block a build. stdout is returned
// verbatim because `-z` output is not line-oriented text.
func runVCSRaw(root, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := execabs.CommandContext(ctx, bin, args...)
	cmd.Dir = root
	cmd.Stdin = nil
	return cmd.Output()
}
