package links_test

// Coverage for the two halves the managed-gitignore block grew for
// install-time projection maintenance: the always-ignored backup-sidecar glob,
// and RemoveManagedGitignore (the `gitignore_projections: false` opt-out).
// Shares the marker consts and readGitignore/managedSection helpers with
// gitignore_test.go (same package).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/links"
)

// backupPattern is the always-ignored glob covering the sibling copies install
// writes when it displaces a pre-existing user file (AGENTS.md.dot-agents-backup
// and friends). Like the overlay it is unconditional — the set of displaced
// files is not knowable ahead of the projection.
const backupPattern = "*.dot-agents-backup"

func TestEnsureManagedGitignore_AlwaysIgnoresBackupSidecars(t *testing.T) {
	tests := []struct {
		name    string
		outputs []string
	}{
		{name: "no outputs", outputs: nil},
		{name: "with outputs", outputs: []string{".claude/", "AGENTS.md"}},
		{name: "caller also passes it", outputs: []string{backupPattern, ".codex/"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := links.EnsureManagedGitignore(root, tc.outputs); err != nil {
				t.Fatalf("EnsureManagedGitignore: %v", err)
			}
			section := managedSection(t, readGitignore(t, root))
			if !strings.Contains(section, "\n"+backupPattern+"\n") {
				t.Errorf("backup sidecar pattern must always be ignored:\n%s", section)
			}
			if n := strings.Count(section, backupPattern); n != 1 {
				t.Errorf("backup pattern must appear exactly once, got %d:\n%s", n, section)
			}
			// The glob must not swallow the committed contract files.
			for _, forbidden := range []string{".agentsrc.json", ".agentsrc.lock"} {
				if strings.Contains(section, "\n"+forbidden+"\n") {
					t.Errorf("contract file %q must stay tracked:\n%s", forbidden, section)
				}
			}
		})
	}
}

func TestRemoveManagedGitignore(t *testing.T) {
	userContent := "node_modules/\n*.log\n"

	tests := []struct {
		name string
		// seed writes the pre-state; a no-op seed means "no .gitignore at all".
		seed      func(t *testing.T, root string)
		wantFile  bool
		wantExact string
	}{
		{
			name:     "no gitignore at all is a no-op",
			seed:     func(*testing.T, string) {},
			wantFile: false,
		},
		{
			name: "block-only file is removed entirely",
			seed: func(t *testing.T, root string) {
				if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err != nil {
					t.Fatalf("seed: %v", err)
				}
			},
			wantFile: false,
		},
		{
			name: "user content is preserved byte-for-byte",
			seed: func(t *testing.T, root string) {
				writeGitignore(t, root, userContent)
				if err := links.EnsureManagedGitignore(root, []string{".claude/", ".codex/"}); err != nil {
					t.Fatalf("seed: %v", err)
				}
			},
			wantFile:  true,
			wantExact: userContent,
		},
		{
			name: "file with no managed block is left alone",
			seed: func(t *testing.T, root string) {
				writeGitignore(t, root, userContent)
			},
			wantFile:  true,
			wantExact: userContent,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.seed(t, root)

			if err := links.RemoveManagedGitignore(root); err != nil {
				t.Fatalf("RemoveManagedGitignore: %v", err)
			}

			got := readGitignore(t, root)
			if !tc.wantFile {
				if got != "" {
					t.Errorf("expected no .gitignore left behind, got:\n%s", got)
				}
				return
			}
			if got != tc.wantExact {
				t.Errorf("user content not preserved:\ngot  %q\nwant %q", got, tc.wantExact)
			}
			if strings.Contains(got, blockBegin) || strings.Contains(got, blockEnd) {
				t.Errorf("managed markers must be gone:\n%s", got)
			}
		})
	}
}

func TestRemoveManagedGitignore_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	writeGitignore(t, root, "dist/\n")
	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := links.RemoveManagedGitignore(root); err != nil {
		t.Fatalf("first remove: %v", err)
	}
	first := readGitignore(t, root)
	if err := links.RemoveManagedGitignore(root); err != nil {
		t.Fatalf("second remove: %v", err)
	}
	if second := readGitignore(t, root); second != first {
		t.Errorf("second remove must be byte-stable:\ngot  %q\nwant %q", second, first)
	}
}

func TestRemoveManagedGitignore_EmptyRepoRootIsError(t *testing.T) {
	if err := links.RemoveManagedGitignore(""); err == nil {
		t.Fatal("expected an error for an empty repo root")
	}
}

func TestRemoveManagedGitignore_ReadErrorIsPropagated(t *testing.T) {
	root := t.TempDir()
	// Make .gitignore a directory so os.ReadFile fails with a non-NotExist
	// error, exercising the read-error branch rather than the absent-file one.
	if err := os.Mkdir(filepath.Join(root, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := links.RemoveManagedGitignore(root); err == nil {
		t.Error("expected a read error when .gitignore is a directory, got nil")
	}
}

func TestRemoveManagedGitignore_RemoveErrorIsPropagated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits do not gate directory writes the same way on windows")
	}
	root := t.TempDir()
	// A block-only .gitignore takes the delete path; a read-only parent
	// directory makes that unlink fail, so the error must surface rather than
	// leaving the caller believing the block was retracted.
	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)

	if err := links.RemoveManagedGitignore(root); err == nil {
		t.Error("expected a remove error under a read-only parent directory, got nil")
	}
}

func TestRemoveManagedGitignore_WriteErrorIsPropagated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits do not gate directory writes the same way on windows")
	}
	root := t.TempDir()
	// User content survives the strip, so this takes the rewrite path instead
	// of the delete path — a read-only parent makes the atomic write fail.
	writeGitignore(t, root, "node_modules/\n")
	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)

	if err := links.RemoveManagedGitignore(root); err == nil {
		t.Error("expected a write error under a read-only parent directory, got nil")
	}
}

// writeGitignore seeds <root>/.gitignore with exact content.
func writeGitignore(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}
}
