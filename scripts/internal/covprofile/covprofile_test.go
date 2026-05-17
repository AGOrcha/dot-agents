package covprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_ValidAndMalformedLines(t *testing.T) {
	prof := filepath.Join(t.TempDir(), "coverage.out")
	content := "mode: set\n" +
		"github.com/x/y/a.go:10.2,12.4 3 1\n" + // covered block
		"github.com/x/y/a.go:20.1,21.9 2 0\n" + // uncovered block, same file
		"github.com/x/y/b.go:1.1,2.2 1 5\n" + // different file
		"no-colon-line\n" + // colon < 0 → skip
		"github.com/x/y/c.go:bad fields here too many\n" + // len(parts) != 3 → skip
		"github.com/x/y/d.go:1.1-2.2 1 1\n" + // no comma → skip
		"github.com/x/y/e.go:1,2 1 1\n" // ll/rr lack a dot → skip
	if err := os.WriteFile(prof, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := Parse(prof)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 files parsed, got %d: %v", len(got), got)
	}

	a := got["github.com/x/y/a.go"]
	if len(a) != 2 {
		t.Fatalf("a.go: expected 2 blocks, got %d", len(a))
	}
	want := Block{StartLine: 10, StartCol: 2, EndLine: 12, EndCol: 4, Stmts: 3, Count: 1}
	if a[0] != want {
		t.Errorf("a.go[0] = %+v, want %+v", a[0], want)
	}
	if a[1].Count != 0 || a[1].Stmts != 2 {
		t.Errorf("a.go[1] = %+v, want Stmts=2 Count=0", a[1])
	}
	if b := got["github.com/x/y/b.go"]; len(b) != 1 || b[0].Count != 5 {
		t.Errorf("b.go = %+v, want one block Count=5", b)
	}
	for _, skipped := range []string{
		"github.com/x/y/c.go", "github.com/x/y/d.go", "github.com/x/y/e.go",
	} {
		if _, ok := got[skipped]; ok {
			t.Errorf("expected %s to be skipped as malformed", skipped)
		}
	}
}

func TestParse_MissingFileErrors(t *testing.T) {
	if _, err := Parse(filepath.Join(t.TempDir(), "does-not-exist.out")); err == nil {
		t.Fatal("expected error opening a missing profile")
	}
}

func TestParse_EmptyProfileHeaderOnly(t *testing.T) {
	prof := filepath.Join(t.TempDir(), "empty.out")
	if err := os.WriteFile(prof, []byte("mode: set\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := Parse(prof)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no blocks from header-only profile, got %v", got)
	}
}
