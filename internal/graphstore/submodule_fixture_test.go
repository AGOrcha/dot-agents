package graphstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/execabs"
)

// This file builds REAL git fixtures — an actual superproject with an actual
// `git submodule add`ed child — because the defect under test is entirely
// about what git's own enumeration does with a gitlink. A mocked repository
// layout would reproduce the fix and not the bug.

// requireGit skips the test when git is unavailable.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := execabs.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// git runs a git command in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// protocol.file.allow=always is required for `submodule add` from a local
	// path (git blocks the file transport for submodules by default).
	full := append([]string{"-C", dir, "-c", "protocol.file.allow=always"}, args...)
	cmd := execabs.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// writeFiles materializes name→content under dir, creating parent dirs.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// initRepo creates a git repository at dir containing files, with one commit.
func initRepo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "--quiet", "--initial-branch=main")
	git(t, dir, "config", "user.email", "fixture@example.test")
	git(t, dir, "config", "user.name", "Fixture")
	git(t, dir, "config", "commit.gpgsign", "false")
	writeFiles(t, dir, files)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "fixture")
	return dir
}

// addSubmodule adds subRepo into super at the given repo-relative path and
// commits the gitlink.
func addSubmodule(t *testing.T, super, subRepo, at string) {
	t.Helper()
	git(t, super, "submodule", "add", "--quiet", filepath.ToSlash(subRepo), at)
	git(t, super, "commit", "--quiet", "-m", "add submodule "+at)
}

// superprojectFixture builds the canonical fixture for these tests: a
// superproject with two tracked files plus a submodule at `vendor/lib`
// carrying three tracked files of its own. It returns the superproject path.
//
// Plain `git ls-files` in the superproject sees 4 entries (2 files, the
// gitlink, and .gitmodules) and NONE of the submodule's three source files —
// that gap is the defect.
func superprojectFixture(t *testing.T) string {
	t.Helper()
	requireGit(t)
	base := t.TempDir()
	sub := initRepo(t, filepath.Join(base, "lib-origin"), map[string]string{
		"lib.go":        "package lib\n",
		"widget.go":     "package lib\n",
		"internal/x.go": "package internal\n",
	})
	super := initRepo(t, filepath.Join(base, "super"), map[string]string{
		"main.go":   "package main\n",
		"README.md": "fixture\n",
	})
	addSubmodule(t, super, sub, "vendor/lib")
	return super
}
