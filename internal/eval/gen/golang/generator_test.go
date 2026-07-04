package gogen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/gen/gencore"
	"github.com/AGOrcha/dot-agents/internal/eval/kgquery"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ---- fake KG reader ------------------------------------------------------------

// fakeReader is a minimal in-memory graphstore.CodeGraphReader seeded with one
// easy Go symbol, enough to drive a full Generate through the shared engine and
// assert the Go-specific spec output.
type fakeReader struct{}

func (fakeReader) GetAllFiles() ([]string, error) { return []string{"pkg/foo/foo.go"}, nil }
func (fakeReader) GetNodesByFile(string) ([]graphstore.GraphNode, error) {
	return []graphstore.GraphNode{{
		Kind:          graphstore.NodeKindFunction,
		Name:          "pkg/foo.Bar",
		QualifiedName: "pkg/foo.Bar",
		FilePath:      "pkg/foo/foo.go",
		Language:      "go",
		LineStart:     10,
		LineEnd:       30,
	}}, nil
}
func (fakeReader) GetNode(qn string) (*graphstore.GraphNode, error) {
	if qn != "pkg/foo.Bar" {
		return nil, nil
	}
	return &graphstore.GraphNode{
		Kind:          graphstore.NodeKindFunction,
		QualifiedName: "pkg/foo.Bar",
		FilePath:      "pkg/foo/foo.go",
		Language:      "go",
		LineStart:     10,
		LineEnd:       30,
	}, nil
}
func (fakeReader) GetEdgesBySource(string) ([]graphstore.GraphEdge, error) { return nil, nil }
func (fakeReader) GetEdgesByTarget(string) ([]graphstore.GraphEdge, error) { return nil, nil }
func (fakeReader) GetEdgesAmong([]string) ([]graphstore.GraphEdge, error)  { return nil, nil }
func (fakeReader) SearchNodes(string, int) ([]graphstore.GraphNode, error) { return nil, nil }
func (fakeReader) GetMetadata(string) (string, error)                      { return "", nil }
func (fakeReader) GetStats() (graphstore.GraphStats, error)                { return graphstore.GraphStats{}, nil }
func (fakeReader) GetImpactRadius([]string, int, int) (graphstore.ImpactResult, error) {
	return graphstore.ImpactResult{}, nil
}

var _ graphstore.CodeGraphReader = fakeReader{}

func mustQuerier(t *testing.T) *kgquery.Querier {
	t.Helper()
	q, err := kgquery.New(fakeReader{})
	if err != nil {
		t.Fatalf("kgquery.New: %v", err)
	}
	return q
}

// ---- construction (profile wiring) --------------------------------------------

func newGen(t *testing.T) *gencore.Generator {
	t.Helper()
	g, err := gencore.New(mustQuerier(t), Profile)
	if err != nil {
		t.Fatalf("gencore.New: %v", err)
	}
	return g
}

func TestProfile_Registers(t *testing.T) {
	r := eval.NewRegistry()
	if err := gencore.Register(r, mustQuerier(t), Profile); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := r.Lookup(eval.LanguageGo); !ok {
		t.Fatal("generator not found in registry after Register")
	}
}

// ---- Go-specific spec output --------------------------------------------------

// TestGenerate_GoSpec drives the shared engine through the Go profile and pins
// the Go-specific outputs: language, task-ID prefix, verification commands, and
// the *_test.go add-test-coverage artifact.
func TestGenerate_GoSpec(t *testing.T) {
	g := newGen(t)
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: gencore.TemplateImplPureFn})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if spec.Language != eval.LanguageGo {
		t.Errorf("Language = %q, want %q", spec.Language, eval.LanguageGo)
	}
	if !strings.HasPrefix(spec.TaskID, "kg-go-impl-") {
		t.Errorf("TaskID %q should start with kg-go-impl-", spec.TaskID)
	}
	if got := spec.Verification.BuildCmd; len(got) == 0 || got[0] != goCmd {
		t.Errorf("BuildCmd = %v, want first element %q", got, goCmd)
	}
	hasRace := false
	for _, a := range spec.Verification.TestCmd {
		if a == raceFlag {
			hasRace = true
		}
	}
	if !hasRace {
		t.Errorf("TestCmd %v missing %q", spec.Verification.TestCmd, raceFlag)
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("spec.Validate(): %v", err)
	}
}

// TestGenerate_GoPromptGolden pins the exact impl-pure-fn prompt text the Go
// profile renders, so the refactor onto gencore is provably byte-identical to
// the pre-refactor generator's output for a fixed seed.
func TestGenerate_GoPromptGolden(t *testing.T) {
	g := newGen(t)
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: gencore.TemplateImplPureFn})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "Implement the function `pkg/foo.Bar` in `pkg/foo/foo.go` so that the existing tests pass.\n\n" +
		"Nearby symbols (within 2 hops): (none)\n\n" +
		"Constraints:\n" +
		"- Do not modify any existing *_test.go file.\n" +
		"- The solution must satisfy: go test -race ./pkg/foo/..."
	if spec.Prompt != want {
		t.Errorf("prompt mismatch:\n got: %q\nwant: %q", spec.Prompt, want)
	}
}

func TestGenerate_AddTestCoverageArtifact(t *testing.T) {
	g := newGen(t)
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: gencore.TemplateAddTestCoverage})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	art := spec.SolutionArtifacts[0].Path
	if !strings.HasSuffix(art, "_test.go") {
		t.Errorf("add-test-coverage artifact = %q, want a *_test.go file", art)
	}
	if !strings.Contains(spec.Prompt, art) {
		t.Errorf("prompt should reference the artifact %q", art)
	}
}

// ---- code-review-graph absolute-path reproduction -----------------------------

// crgReader is a graphstore.CodeGraphReader seeded the way the code-review-graph
// ingestion stores a symbol: an ABSOLUTE file path and a "<abs-file>::<decl>"
// qualified name. It embeds fakeReader for the unused stubs and overrides the
// three methods kgquery drives, so it exercises the real Go profile end-to-end
// with code-review-graph-shaped data.
type crgReader struct {
	fakeReader
	absFile string
	qn      string
}

func (r crgReader) node() graphstore.GraphNode {
	return graphstore.GraphNode{
		Kind:          graphstore.NodeKindFunction,
		Name:          "Bar",
		QualifiedName: r.qn,
		FilePath:      r.absFile,
		Language:      "go",
		LineStart:     10,
		LineEnd:       30,
	}
}

func (r crgReader) GetAllFiles() ([]string, error) { return []string{r.absFile}, nil }
func (r crgReader) GetNodesByFile(string) ([]graphstore.GraphNode, error) {
	return []graphstore.GraphNode{r.node()}, nil
}
func (r crgReader) GetNode(qn string) (*graphstore.GraphNode, error) {
	if qn != r.qn {
		return nil, nil
	}
	n := r.node()
	return &n, nil
}

var _ graphstore.CodeGraphReader = crgReader{}

// TestGenerate_GoNormalizesAbsoluteCRGPaths is the release-blocker reproduction
// against the REAL Go profile: with code-review-graph data (absolute paths +
// "file::name" symbols) the pre-fix generator emitted `go build .//<abs>/...`
// and a "<abs>::Bar" symbol. This drives the production path (gencore.New →
// resolveRepoRoot → os.Getwd, pinned via t.Chdir) and asserts the emitted spec
// is a well-formed repo-relative Go package pattern with a clean symbol and no
// absolute path anywhere.
func TestGenerate_GoNormalizesAbsoluteCRGPaths(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	wd, err := os.Getwd() // canonical root gencore.New resolves; may differ from repoRoot via symlinks
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	absFile := filepath.Join(wd, "internal", "eval", "foo.go")
	q, err := kgquery.New(crgReader{absFile: absFile, qn: absFile + "::Bar"})
	if err != nil {
		t.Fatalf("kgquery.New: %v", err)
	}
	g, err := gencore.New(q, Profile)
	if err != nil {
		t.Fatalf("gencore.New: %v", err)
	}
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: gencore.TemplateImplPureFn})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	const wantPattern = "./internal/eval/..."
	if !contains(spec.Verification.TestCmd, wantPattern) {
		t.Errorf("TestCmd = %v, want a well-formed repo-relative pattern %q", spec.Verification.TestCmd, wantPattern)
	}
	if !contains(spec.Verification.BuildCmd, wantPattern) {
		t.Errorf("BuildCmd = %v, want a well-formed repo-relative pattern %q", spec.Verification.BuildCmd, wantPattern)
	}
	for _, cmd := range [][]string{spec.Verification.BuildCmd, spec.Verification.TestCmd} {
		for _, tok := range cmd {
			if filepath.IsAbs(tok) || strings.Contains(tok, "//") {
				t.Errorf("verification token %q leaks an absolute path (pre-fix `.//<abs>` bug)", tok)
			}
		}
	}
	if got := spec.SolutionArtifacts[0].Path; got != "internal/eval/foo.go" {
		t.Errorf("artifact path = %q, want repo-relative", got)
	}
	want := "Implement the function `Bar` in `internal/eval/foo.go` so that the existing tests pass.\n\n" +
		"Nearby symbols (within 2 hops): (none)\n\n" +
		"Constraints:\n" +
		"- Do not modify any existing *_test.go file.\n" +
		"- The solution must satisfy: go test -race ./internal/eval/..."
	if spec.Prompt != want {
		t.Errorf("prompt mismatch:\n got: %q\nwant: %q", spec.Prompt, want)
	}
	data, err := spec.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	for _, needle := range []string{filepath.ToSlash(absFile), filepath.ToSlash(wd), "::"} {
		if strings.Contains(string(data), needle) {
			t.Errorf("emitted spec still contains absolute marker %q:\n%s", needle, data)
		}
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// ---- Go path/command helpers --------------------------------------------------

func TestTestFilePath(t *testing.T) {
	tests := []struct{ implPath, want string }{
		{"pkg/foo/foo.go", "pkg/foo/foo_test.go"},
		{"foo.go", "foo_test.go"},
		{"internal/eval/gen/golang/generator.go", "internal/eval/gen/golang/generator_test.go"},
	}
	for _, tc := range tests {
		if got := testFilePath(tc.implPath); got != tc.want {
			t.Errorf("testFilePath(%q) = %q, want %q", tc.implPath, got, tc.want)
		}
	}
}

func TestPkgPattern(t *testing.T) {
	tests := []struct{ filePath, want string }{
		{"internal/eval/foo.go", "./internal/eval/..."},
		{"foo.go", "./..."},
		{"", "./..."},
		{".", "./..."},
		{"a/b/c/d.go", "./a/b/c/..."},
	}
	for _, tc := range tests {
		if got := pkgPattern(tc.filePath); got != tc.want {
			t.Errorf("pkgPattern(%q) = %q, want %q", tc.filePath, got, tc.want)
		}
	}
}

func TestBuildCmd(t *testing.T) {
	got := buildCmd("pkg/foo/foo.go")
	want := []string{"go", "build", "./pkg/foo/..."}
	if !equalStrs(got, want) {
		t.Errorf("buildCmd = %v, want %v", got, want)
	}
}

func TestTestCmd(t *testing.T) {
	got := testCmd("pkg/foo/foo.go")
	want := []string{"go", "test", "-race", "./pkg/foo/..."}
	if !equalStrs(got, want) {
		t.Errorf("testCmd = %v, want %v", got, want)
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
