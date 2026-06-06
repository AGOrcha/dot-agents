package links_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/links"
)

const (
	blockBegin = "# >>> dot-agents managed (project outputs) >>>"
	blockEnd   = "# <<< dot-agents managed (project outputs) <<<"
	overlay    = ".agentsrc.local.json"
)

// readGitignore reads <root>/.gitignore, failing the test on any error other
// than a missing file (which it reports as empty so callers can assert absence).
func readGitignore(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read .gitignore: %v", err)
	}
	return string(data)
}

// managedSection returns the text strictly between the managed markers (markers
// excluded), failing the test if the block is not well-formed.
func managedSection(t *testing.T, content string) string {
	t.Helper()
	i := strings.Index(content, blockBegin)
	j := strings.Index(content, blockEnd)
	if i < 0 || j < 0 || j < i {
		t.Fatalf("managed block markers not well-formed in:\n%s", content)
	}
	return content[i+len(blockBegin) : j]
}

func TestEnsureManagedGitignore_CreatesBlockWithOverlayAndOutputs(t *testing.T) {
	root := t.TempDir()

	if err := links.EnsureManagedGitignore(root, []string{".claude/", ".cursor/rules", "AGENTS.md"}); err != nil {
		t.Fatalf("EnsureManagedGitignore: %v", err)
	}

	got := readGitignore(t, root)
	if !strings.Contains(got, blockBegin) || !strings.Contains(got, blockEnd) {
		t.Fatalf("expected managed markers, got:\n%s", got)
	}
	section := managedSection(t, got)
	for _, want := range []string{overlay, ".claude/", ".cursor/rules", "AGENTS.md"} {
		if !strings.Contains(section, "\n"+want+"\n") {
			t.Errorf("managed section missing %q:\n%s", want, section)
		}
	}
	// Exactly one trailing newline, never a growing tail of blanks.
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("file must end in exactly one newline, got %q", got)
	}
}

func TestEnsureManagedGitignore_AlwaysIgnoresOverlayEvenWithNoOutputs(t *testing.T) {
	root := t.TempDir()

	if err := links.EnsureManagedGitignore(root, nil); err != nil {
		t.Fatalf("EnsureManagedGitignore(nil): %v", err)
	}

	section := managedSection(t, readGitignore(t, root))
	if !strings.Contains(section, overlay) {
		t.Errorf("overlay must always be present even with no outputs:\n%s", section)
	}
}

func TestEnsureManagedGitignore_NeverIgnoresCommittedContract(t *testing.T) {
	root := t.TempDir()

	// A caller mistakenly passes the committed contract files; they must be
	// filtered out so git keeps tracking them (uv.lock analog).
	err := links.EnsureManagedGitignore(root, []string{
		".agentsrc.json", ".agentsrc.lock", ".agentsrc.lock/", ".claude/",
	})
	if err != nil {
		t.Fatalf("EnsureManagedGitignore: %v", err)
	}

	section := managedSection(t, readGitignore(t, root))
	for _, forbidden := range []string{".agentsrc.json", ".agentsrc.lock"} {
		// As whole lines — substring of ".agentsrc.local.json" would false-match.
		if strings.Contains(section, "\n"+forbidden+"\n") {
			t.Errorf("committed contract file %q must never be ignored:\n%s", forbidden, section)
		}
	}
	if !strings.Contains(section, ".claude/") {
		t.Errorf("legitimate output should still be ignored:\n%s", section)
	}
}

func TestEnsureManagedGitignore_ConvergesOnRerun(t *testing.T) {
	root := t.TempDir()
	inputs := []string{".cursor/rules", ".claude/", "AGENTS.md"}

	if err := links.EnsureManagedGitignore(root, inputs); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := readGitignore(t, root)

	// Re-run with the SAME inputs in a DIFFERENT order — output must be
	// byte-identical (regenerated + sorted, never appended) per R8.
	if err := links.EnsureManagedGitignore(root, []string{"AGENTS.md", ".claude/", ".cursor/rules"}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second := readGitignore(t, root)

	if first != second {
		t.Errorf("re-run not convergent:\nfirst:\n%q\nsecond:\n%q", first, second)
	}
	if n := strings.Count(second, blockBegin); n != 1 {
		t.Errorf("expected exactly one managed block, found %d", n)
	}
}

func TestEnsureManagedGitignore_RegeneratesBlockWhenOutputsChange(t *testing.T) {
	root := t.TempDir()

	if err := links.EnsureManagedGitignore(root, []string{".claude/", "stale-output"}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err != nil {
		t.Fatalf("second run: %v", err)
	}

	section := managedSection(t, readGitignore(t, root))
	if strings.Contains(section, "stale-output") {
		t.Errorf("a removed output must not survive regeneration:\n%s", section)
	}
	if !strings.Contains(section, ".claude/") {
		t.Errorf("a still-present output must remain:\n%s", section)
	}
}

func TestEnsureManagedGitignore_PreservesUserContent(t *testing.T) {
	root := t.TempDir()
	userContent := "# user ignores\nnode_modules/\n*.log\n"
	gi := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(gi, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err != nil {
		t.Fatalf("EnsureManagedGitignore: %v", err)
	}

	got := readGitignore(t, root)
	for _, want := range []string{"# user ignores", "node_modules/", "*.log"} {
		if !strings.Contains(got, want) {
			t.Errorf("user content %q was not preserved:\n%s", want, got)
		}
	}
	// User content precedes the managed block (the da section is the stable tail).
	if strings.Index(got, "node_modules/") > strings.Index(got, blockBegin) {
		t.Errorf("managed block should follow user content:\n%s", got)
	}

	// A second run must NOT duplicate the user's lines.
	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if n := strings.Count(readGitignore(t, root), "node_modules/"); n != 1 {
		t.Errorf("user line duplicated across runs, count=%d", n)
	}
}

func TestEnsureManagedGitignore_DeduplicatesAndSorts(t *testing.T) {
	root := t.TempDir()

	if err := links.EnsureManagedGitignore(root, []string{"b/", "a/", "b/", "  ", "a/"}); err != nil {
		t.Fatalf("EnsureManagedGitignore: %v", err)
	}

	section := managedSection(t, readGitignore(t, root))
	if n := strings.Count(section, "\na/\n"); n != 1 {
		t.Errorf("duplicate entry not collapsed, count=%d:\n%s", n, section)
	}
	// Sorted: overlay (.) < a/ < b/.
	if strings.Index(section, "\na/\n") > strings.Index(section, "\nb/\n") {
		t.Errorf("entries not sorted:\n%s", section)
	}
}

func TestEnsureManagedGitignore_NormalizesBackslashesToSlash(t *testing.T) {
	root := t.TempDir()

	if err := links.EnsureManagedGitignore(root, []string{`.claude\rules`}); err != nil {
		t.Fatalf("EnsureManagedGitignore: %v", err)
	}

	got := readGitignore(t, root)
	if strings.Contains(got, `\`) {
		t.Errorf("backslash must be normalized to slash for a portable gitignore:\n%s", got)
	}
	if !strings.Contains(got, ".claude/rules") {
		t.Errorf("expected slash-normalized path:\n%s", got)
	}
}

func TestEnsureManagedGitignore_RecoversFromTruncatedBlock(t *testing.T) {
	root := t.TempDir()
	// A begin marker with no matching end (a hand-truncated file) plus stale
	// managed lines below it. Strip must drop everything to EOF so the stale
	// lines do not leak into the user section, then a fresh block is written.
	corrupt := "keep-me\n" + blockBegin + "\nstale-leak\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err != nil {
		t.Fatalf("EnsureManagedGitignore: %v", err)
	}

	got := readGitignore(t, root)
	if !strings.Contains(got, "keep-me") {
		t.Errorf("content before a truncated block must be preserved:\n%s", got)
	}
	if strings.Contains(got, "stale-leak") {
		t.Errorf("stale managed line under a truncated block must not survive:\n%s", got)
	}
	if n := strings.Count(got, blockEnd); n != 1 {
		t.Errorf("expected one well-formed end marker, found %d:\n%s", n, got)
	}
}

func TestEnsureManagedGitignore_EmptyRepoRootIsError(t *testing.T) {
	for _, root := range []string{"", "   "} {
		if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err == nil {
			t.Errorf("expected error for empty repo root %q, got nil", root)
		}
	}
}

func TestEnsureManagedGitignore_ReadErrorIsPropagated(t *testing.T) {
	root := t.TempDir()
	// Make .gitignore a directory so os.ReadFile fails with a non-NotExist
	// error, exercising the read-error branch.
	if err := os.Mkdir(filepath.Join(root, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err == nil {
		t.Error("expected a read error when .gitignore is a directory, got nil")
	}
}

func TestEnsureManagedGitignore_CreatesMissingRepoRoot(t *testing.T) {
	// repoRoot does not exist yet: ReadFile reports the absent .gitignore as
	// empty, then the directory is created before the atomic write, exercising
	// the mkdir branch on the happy path.
	root := filepath.Join(t.TempDir(), "newly", "created", "root")

	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err != nil {
		t.Fatalf("EnsureManagedGitignore on a missing repo root: %v", err)
	}

	section := managedSection(t, readGitignore(t, root))
	if !strings.Contains(section, overlay) {
		t.Errorf("managed block not written into freshly-created repo root:\n%s", section)
	}
}

func TestEnsureManagedGitignore_MkdirErrorIsPropagated(t *testing.T) {
	tmp := t.TempDir()
	// A regular file stands where a directory component of repoRoot must be, so
	// both the read of repoRoot/.gitignore and the mkdir of repoRoot fail
	// because a path component is a file (not a dir) — the error must surface.
	fileAsParent := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(fileAsParent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(fileAsParent, "child")

	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err == nil {
		t.Error("expected an error when a path component is a file, got nil")
	}
}

func TestEnsureManagedGitignore_WriteErrorIsPropagated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits do not gate writes the same way on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	root := t.TempDir()
	// Read-only parent directory: ReadFile sees a missing file (empty), but the
	// atomic write (temp file + rename) cannot create anything, exercising the
	// write-error branch without touching unexported fsops seams.
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	if err := links.EnsureManagedGitignore(root, []string{".claude/"}); err == nil {
		t.Error("expected a write error in a read-only repo root, got nil")
	}
}
