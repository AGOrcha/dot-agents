package gencore

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/kgquery"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// crgReaderFor seeds a fakeReader with a symbol shaped the way the
// code-review-graph ingestion stores it: the given ABSOLUTE file path and a
// "<abs-file>::<decl>" qualified name (graphstore makeQualified's no-parent
// fallback). It mirrors simpleFakeReader's easy shape (Bar calls helper) so the
// seed derives cleanly.
func crgReaderFor(absFile string) *fakeReader {
	f := newFakeReader()
	bar := absFile + "::Bar"
	helper := absFile + "::helper"
	f.addGoFn(bar, absFile, 10, 30)
	f.addGoFn(helper, absFile, 35, 45)
	f.addCall(bar, helper)
	return f
}

// crgFakeReader builds a code-review-graph fixture under repoRoot and returns it
// with the seed's absolute file path so a test can assert that path never
// survives into the emitted spec.
func crgFakeReader(repoRoot string) (*fakeReader, string) {
	absFile := filepath.Join(repoRoot, "internal", "eval", "foo.go")
	return crgReaderFor(absFile), absFile
}

// TestGenerate_NormalizesAbsoluteCRGPaths is the release-blocker reproduction:
// feed the generator code-review-graph data (absolute file paths + "file::name"
// symbols) and prove the emitted spec is fully repo-relative with clean symbol
// names and no absolute path anywhere — the pre-fix generator emitted the
// absolute path glued into the package pattern and "<abs>::name" symbols.
func TestGenerate_NormalizesAbsoluteCRGPaths(t *testing.T) {
	repoRoot := t.TempDir()
	f, absFile := crgFakeReader(repoRoot)
	g, err := newForRoot(mustQuerier(t, f), testProfile, repoRoot)
	if err != nil {
		t.Fatalf("newForRoot: %v", err)
	}
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: TemplateImplPureFn})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	const wantRel = "internal/eval/foo.go"
	if got := spec.SolutionArtifacts[0].Path; got != wantRel {
		t.Errorf("artifact path = %q, want repo-relative %q", got, wantRel)
	}
	if got := spec.GeneratedFrom.KGQuery.SeedSymbol; got != "Bar" {
		t.Errorf("seed symbol = %q, want clean decl name %q", got, "Bar")
	}
	if !strings.HasPrefix(spec.TaskID, "kg-zz-impl-bar") {
		t.Errorf("TaskID %q should derive from the clean symbol name, not the abs-path QN", spec.TaskID)
	}
	// The prompt must name the relative file and clean symbol, never the abs path
	// or the "::" separator.
	if !strings.Contains(spec.Prompt, wantRel) {
		t.Errorf("prompt missing relative file %q:\n%s", wantRel, spec.Prompt)
	}
	if !strings.Contains(spec.Prompt, "`Bar`") {
		t.Errorf("prompt missing clean symbol `Bar`:\n%s", spec.Prompt)
	}
	if strings.Contains(spec.Prompt, "helper") && strings.Contains(spec.Prompt, absFile) {
		t.Errorf("prompt leaked an absolute neighbor path:\n%s", spec.Prompt)
	}
	assertNoAbsolute(t, spec, repoRoot, absFile)
}

// TestGenerate_RelativeNativePathsUnchanged pins that native graphstore data
// (repo-relative paths + "pkg.Symbol" names) is passed through untouched, so the
// normalization is a no-op for the native ingestion path.
func TestGenerate_RelativeNativePathsUnchanged(t *testing.T) {
	g, err := newForRoot(mustQuerier(t, simpleFakeReader()), testProfile, t.TempDir())
	if err != nil {
		t.Fatalf("newForRoot: %v", err)
	}
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: TemplateImplPureFn})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := spec.SolutionArtifacts[0].Path; got != "pkg/foo/foo.go" {
		t.Errorf("native artifact path = %q, want it unchanged", got)
	}
	if got := spec.GeneratedFrom.KGQuery.SeedSymbol; got != "pkg/foo.Bar" {
		t.Errorf("native seed symbol = %q, want it unchanged", got)
	}
	if !strings.Contains(spec.Prompt, "pkg/foo.Bar") {
		t.Errorf("native prompt should keep the qualified name:\n%s", spec.Prompt)
	}
}

// TestGenerate_AbsolutePathOutsideRepoFails proves the generator fails with a
// clear error rather than emitting a spec that points outside the sandbox when
// the KG's absolute path cannot be reduced to a path inside the repo root (e.g.
// the KG was built for a different checkout).
func TestGenerate_AbsolutePathOutsideRepoFails(t *testing.T) {
	repoRoot := t.TempDir()
	f, _ := crgFakeReader(repoRoot)
	// Point the generator at a sibling root the abs paths are not under.
	otherRoot := filepath.Join(filepath.Dir(repoRoot), "some-other-checkout")
	g, err := newForRoot(mustQuerier(t, f), testProfile, otherRoot)
	if err != nil {
		t.Fatalf("newForRoot: %v", err)
	}
	_, err = g.Generate(context.Background(), eval.GenerateOptions{})
	if err == nil {
		t.Fatal("expected an error when the seed path is outside the repo root")
	}
	if !strings.Contains(err.Error(), "outside the repository root") {
		t.Errorf("error %q should explain the path is outside the repo root", err)
	}
	if !strings.Contains(err.Error(), testProfile.ErrPrefix) {
		t.Errorf("error %q should carry the profile ErrPrefix", err)
	}
}

// TestGenerate_WindowsAbsoluteCRGSeed proves OS-agnostic handling of a Windows
// absolute CRG seed. On a non-Windows host the foreign-OS absolute path cannot
// be relativized against a runtime-OS repo root and is rejected loudly; on
// Windows the same path normalizes to a repo-relative slash path. Either way, no
// backslash or drive-letter residue survives into a spec.
func TestGenerate_WindowsAbsoluteCRGSeed(t *testing.T) {
	const winRoot = `C:\proj\repo`
	const winFile = `C:\proj\repo\internal\eval\foo.go`
	f := crgReaderFor(winFile)
	g, err := newForRoot(mustQuerier(t, f), testProfile, winRoot)
	if err != nil {
		t.Fatalf("newForRoot: %v", err)
	}
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: TemplateImplPureFn})
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("on Windows a native absolute seed should normalize, got: %v", err)
		}
		if got := spec.SolutionArtifacts[0].Path; got != "internal/eval/foo.go" {
			t.Errorf("artifact path = %q, want repo-relative", got)
		}
		return
	}
	if err == nil {
		t.Fatal("on a non-Windows host a Windows absolute seed must be rejected, not emitted")
	}
	if !strings.Contains(err.Error(), "different OS") {
		t.Errorf("error %q should explain the path is from a different OS", err)
	}
}

// assertNoAbsolute fails if the marshalled spec contains the repo root or the
// seed's absolute file path anywhere — the strongest form of "no absolute path
// leaked into the spec/prompt".
func assertNoAbsolute(t *testing.T, spec *eval.TaskSpec, repoRoot, absFile string) {
	t.Helper()
	data, err := spec.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	y := string(data)
	for _, needle := range []string{filepath.ToSlash(absFile), filepath.ToSlash(repoRoot), "::"} {
		if strings.Contains(y, needle) {
			t.Errorf("emitted spec still contains %q:\n%s", needle, y)
		}
	}
}

// ---- relativizePath ------------------------------------------------------------

func TestRelativizePath(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "internal", "eval", "foo.go")
	outside := filepath.Join(filepath.Dir(root), "sibling-"+filepath.Base(root), "x.go")
	tests := []struct {
		name    string
		path    string
		root    string
		want    string
		wantErr string
	}{
		{name: "absolute under root", path: abs, root: root, want: "internal/eval/foo.go"},
		{name: "relative passthrough", path: "pkg/foo/foo.go", root: root, want: "pkg/foo/foo.go"},
		{name: "relative dot-cleaned", path: "./pkg/foo/../foo/foo.go", root: root, want: "pkg/foo/foo.go"},
		{name: "empty path", path: "", root: root, wantErr: "empty"},
		{name: "absolute no root", path: abs, root: "", wantErr: "repository root is unknown"},
		{name: "absolute path with relative root", path: abs, root: "rel/root", wantErr: "relativize seed path"},
		{name: "absolute outside root", path: outside, root: root, wantErr: "outside the repository root"},
		{name: "relative escapes root", path: "../outside/x.go", root: root, wantErr: "outside the repository"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := relativizePath(tc.path, tc.root)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("relativizePath(%q,%q) err = %v, want containing %q", tc.path, tc.root, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("relativizePath(%q,%q): %v", tc.path, tc.root, err)
			}
			if got != tc.want {
				t.Errorf("relativizePath(%q,%q) = %q, want %q", tc.path, tc.root, got, tc.want)
			}
		})
	}
}

// TestRelativizePath_ForeignOSAbsolute pins that an absolute path from the OTHER
// OS convention than the runtime is rejected loudly (it cannot be relativized
// against a runtime-OS repo root) rather than passed through uncleaned.
func TestRelativizePath_ForeignOSAbsolute(t *testing.T) {
	foreign, root := `C:\win\repo\foo.go`, "/unix/repo"
	if runtime.GOOS == "windows" {
		foreign, root = "/unix/abs/repo/foo.go", `C:\repo`
	}
	_, err := relativizePath(foreign, root)
	if err == nil {
		t.Fatalf("relativizePath(%q,%q) should reject a foreign-OS absolute path", foreign, root)
	}
	if !strings.Contains(err.Error(), "different OS") {
		t.Errorf("error %q should explain the path is from a different OS", err)
	}
}

// ---- cleanSymbol ---------------------------------------------------------------

func TestCleanSymbol(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "internal", "foo.go")
	tests := []struct{ qn, want string }{
		{"pkg/foo.Bar", "pkg/foo.Bar"},
		{abs + "::Bar", "Bar"},
		{"a/b/c.go::New", "New"},
		{"", ""},
		{"::leadingsep", "leadingsep"},
	}
	for _, tc := range tests {
		if got := cleanSymbol(tc.qn); got != tc.want {
			t.Errorf("cleanSymbol(%q) = %q, want %q", tc.qn, got, tc.want)
		}
	}
}

// ---- normalizeNeighborhood -----------------------------------------------------

func TestNormalizeNeighborhood(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "foo.go")
	in := kgquery.Neighborhood{
		Root: graphstore.GraphNode{QualifiedName: abs + "::Bar"},
		Nodes: []graphstore.GraphNode{
			{QualifiedName: abs + "::Bar"},
			{QualifiedName: abs + "::helper"},
			{QualifiedName: "pkg/native.Fn"},
		},
	}
	out := normalizeNeighborhood(in)
	if out.Root.QualifiedName != "Bar" {
		t.Errorf("root = %q, want %q", out.Root.QualifiedName, "Bar")
	}
	wantNodes := []string{"Bar", "helper", "pkg/native.Fn"}
	for i, n := range out.Nodes {
		if n.QualifiedName != wantNodes[i] {
			t.Errorf("node[%d] = %q, want %q", i, n.QualifiedName, wantNodes[i])
		}
	}
	// The input slice must not be mutated (normalize returns a fresh copy).
	if in.Nodes[0].QualifiedName != abs+"::Bar" {
		t.Errorf("input neighborhood was mutated: %q", in.Nodes[0].QualifiedName)
	}
}

// ---- isAbsAnyOS ----------------------------------------------------------------

func TestIsAbsAnyOS(t *testing.T) {
	tests := []struct {
		p    string
		want bool
	}{
		{"", false},
		{"pkg/foo/bar.go", false},
		{"./internal/...", false},
		{`internal\eval`, false}, // backslash but relative — not absolute
		{"C:", false},            // drive-relative, no separator
		{"C:foo", false},         // drive-relative
		{"/Users/x/foo.go", true},
		{"//host/share", true}, // POSIX/UNC double-slash root
		{`\\host\share\foo`, true},
		{`\rooted\path`, true},
		{`C:\Users\x`, true},
		{"C:/Users/x", true},
		{"c:/lower", true},
	}
	for _, tc := range tests {
		if got := isAbsAnyOS(tc.p); got != tc.want {
			t.Errorf("isAbsAnyOS(%q) = %v, want %v", tc.p, got, tc.want)
		}
	}
}

// ---- hasAbsPathToken -----------------------------------------------------------

func TestHasAbsPathToken(t *testing.T) {
	absDir := t.TempDir()
	tests := []struct {
		tok  string
		want bool
	}{
		{"./internal/eval/...", false},
		{"go", false},
		{"-race", false},
		{"src/foo/*.test.ts", false},
		{absDir, true},
		{".//Users/x/repo/internal/eval/...", true}, // Go's "./" + abs glued
		{"./..//abs/dir/...", true},
		{"/Users/x/foo.go", true},      // Unix abs regardless of runtime OS
		{`C:\Users\x\foo.go`, true},    // Windows drive abs regardless of runtime OS
		{`\\host\share\pkg\...`, true}, // Windows UNC
		{`pkg\foo\bar.go`, true},       // backslash residue (un-normalized Windows sep)
	}
	for _, tc := range tests {
		if got := hasAbsPathToken(tc.tok); got != tc.want {
			t.Errorf("hasAbsPathToken(%q) = %v, want %v", tc.tok, got, tc.want)
		}
	}
}

// ---- hasAbsPathResidue ---------------------------------------------------------

func TestHasAbsPathResidue(t *testing.T) {
	absTok := filepath.Join(t.TempDir(), "foo.go") // this-OS absolute
	tests := []struct {
		s    string
		want bool
	}{
		{"", false},
		{"Implement `pkg/foo.Bar` in `internal/eval/foo.go`", false}, // native + relative — clean
		{"kg-go-impl-declnames", false},
		{"declNames", false},
		{"pkg/foo.Bar", false},                             // native symbol with "/" is not a residue
		{"foo.go::Bar", true},                              // "::" residue on an otherwise-relative token
		{"Implement `/Users/x/foo.go` now", true},          // Unix abs token
		{`Refactor ` + "`" + `C:\repo\foo.go` + "`", true}, // Windows abs / backslash
		{"go test -race .//Users/x/...", true},             // "//" glue
		{"in `" + absTok + "`", true},                      // this-OS abs
	}
	for _, tc := range tests {
		if got := hasAbsPathResidue(tc.s); got != tc.want {
			t.Errorf("hasAbsPathResidue(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// ---- assertSpecRelative --------------------------------------------------------

// relTestCmd is a valid repo-relative test command used as filler in
// assertSpecRelative cases whose subject is a different field.
func relTestCmd() []string { return []string{"go", "test", "./..."} }

func TestAssertSpecRelative(t *testing.T) {
	absPath := filepath.Join(t.TempDir(), "foo.go")
	tests := []struct {
		name    string
		spec    *eval.TaskSpec
		wantErr bool
	}{
		{
			name: "clean spec",
			spec: &eval.TaskSpec{
				SolutionArtifacts: []eval.SolutionArtifact{{Path: "internal/eval/foo.go"}},
				Verification:      eval.Verification{BuildCmd: []string{"go", "build", "./internal/eval/..."}, TestCmd: []string{"go", "test", "./internal/eval/..."}},
			},
		},
		{
			name: "clean native text fields (no false positive)",
			spec: &eval.TaskSpec{
				TaskID:            "kg-go-impl-pkg-foo-bar",
				Prompt:            "Implement `pkg/foo.Bar` in `internal/eval/foo.go` so tests pass.",
				GeneratedFrom:     eval.GeneratedFrom{KGQuery: &eval.KGQuery{SeedSymbol: "pkg/foo.Bar"}},
				SolutionArtifacts: []eval.SolutionArtifact{{Path: "internal/eval/foo.go"}},
				Verification:      eval.Verification{TestCmd: []string{"go", "test", "./internal/eval/..."}},
			},
		},
		{
			name: "absolute artifact",
			spec: &eval.TaskSpec{
				SolutionArtifacts: []eval.SolutionArtifact{{Path: absPath}},
				Verification:      eval.Verification{TestCmd: relTestCmd()},
			},
			wantErr: true,
		},
		{
			name: "glued build token",
			spec: &eval.TaskSpec{
				Verification: eval.Verification{BuildCmd: []string{"go", "build", ".//abs/dir/..."}, TestCmd: relTestCmd()},
			},
			wantErr: true,
		},
		{
			name: "glued test token",
			spec: &eval.TaskSpec{
				Verification: eval.Verification{TestCmd: []string{"go", "test", ".//abs/dir/..."}},
			},
			wantErr: true,
		},
		{
			name: "windows backslash build token",
			spec: &eval.TaskSpec{
				Verification: eval.Verification{BuildCmd: []string{"go", "build", `C:\repo\pkg\...`}, TestCmd: relTestCmd()},
			},
			wantErr: true,
		},
		{
			name: "prompt with absolute path",
			spec: &eval.TaskSpec{
				Prompt:       "Implement the function in `/Users/x/repo/foo.go` now.",
				Verification: eval.Verification{TestCmd: relTestCmd()},
			},
			wantErr: true,
		},
		{
			name: "prompt with :: residue",
			spec: &eval.TaskSpec{
				Prompt:       "Implement `foo.go::Bar` so tests pass.",
				Verification: eval.Verification{TestCmd: relTestCmd()},
			},
			wantErr: true,
		},
		{
			name: "task id absolute",
			spec: &eval.TaskSpec{
				TaskID:       "/abs/leaked/thing",
				Verification: eval.Verification{TestCmd: relTestCmd()},
			},
			wantErr: true,
		},
		{
			name: "seed symbol with abspath",
			spec: &eval.TaskSpec{
				GeneratedFrom: eval.GeneratedFrom{KGQuery: &eval.KGQuery{SeedSymbol: "/Users/x/foo.go::Bar"}},
				Verification:  eval.Verification{TestCmd: relTestCmd()},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := assertSpecRelative(tc.spec)
			if tc.wantErr != (err != nil) {
				t.Fatalf("assertSpecRelative err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ---- resolveRepoRoot -----------------------------------------------------------

// TestResolveRepoRoot confirms New's default root is the process working
// directory (the repo `da eval gen` runs from), matching resolveRepoDir.
func TestResolveRepoRoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got := resolveRepoRoot()
	if got == "" {
		t.Fatal("resolveRepoRoot returned empty for a valid working directory")
	}
	// os.Getwd may canonicalize symlinks (macOS /var -> /private/var), so compare
	// on resolved form rather than the raw TempDir string.
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("resolveRepoRoot() = %q, want working dir %q", gotResolved, wantResolved)
	}
}
