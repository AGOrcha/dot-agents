package pygen

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
// easy Python symbol, enough to drive a full Generate through the shared engine
// and assert the Python-specific spec output.
type fakeReader struct{}

func (fakeReader) GetAllFiles() ([]string, error) { return []string{"pkg/foo/utils.py"}, nil }
func (fakeReader) GetNodesByFile(string) ([]graphstore.GraphNode, error) {
	return []graphstore.GraphNode{{
		Kind:          graphstore.NodeKindFunction,
		Name:          "foo.compute",
		QualifiedName: "foo.compute",
		FilePath:      "pkg/foo/utils.py",
		Language:      "python",
		LineStart:     1,
		LineEnd:       20,
	}}, nil
}
func (fakeReader) GetNode(qn string) (*graphstore.GraphNode, error) {
	if qn != "foo.compute" {
		return nil, nil
	}
	return &graphstore.GraphNode{
		Kind:          graphstore.NodeKindFunction,
		QualifiedName: "foo.compute",
		FilePath:      "pkg/foo/utils.py",
		Language:      "python",
		LineStart:     1,
		LineEnd:       20,
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
	if _, ok := r.Lookup(eval.LanguagePython); !ok {
		t.Fatal("generator not found in registry after Register")
	}
}

// ---- code-review-graph absolute-path reproduction -----------------------------

// crgReader is a graphstore.CodeGraphReader seeded the way the code-review-graph
// ingestion stores a symbol: an ABSOLUTE file path and a "<abs-file>::<decl>"
// qualified name. It embeds fakeReader for the unused stubs.
type crgReader struct {
	fakeReader
	absFile string
	qn      string
}

func (r crgReader) node() graphstore.GraphNode {
	return graphstore.GraphNode{
		Kind: graphstore.NodeKindFunction, Name: "compute", QualifiedName: r.qn,
		FilePath: r.absFile, Language: "python", LineStart: 1, LineEnd: 20,
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

// TestGenerate_PyNormalizesAbsoluteCRGPaths proves the shared normalization
// reaches the Python profile: code-review-graph data (absolute path +
// "file::name") yields a repo-relative pytest dir and a clean symbol, with no
// absolute path in the emitted spec.
func TestGenerate_PyNormalizesAbsoluteCRGPaths(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	absFile := filepath.Join(wd, "internal", "eval", "utils.py")
	q, err := kgquery.New(crgReader{absFile: absFile, qn: absFile + "::compute"})
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
	if got := spec.SolutionArtifacts[0].Path; got != "internal/eval/utils.py" {
		t.Errorf("artifact path = %q, want repo-relative", got)
	}
	// pytest dir arg is SlashDir(implPath) — must be the repo-relative dir.
	if got := spec.Verification.TestCmd; len(got) == 0 || got[len(got)-1] != "internal/eval" {
		t.Errorf("TestCmd = %v, want it to end with the repo-relative dir %q", got, "internal/eval")
	}
	if !strings.Contains(spec.Prompt, "`compute`") || !strings.Contains(spec.Prompt, "`internal/eval/utils.py`") {
		t.Errorf("prompt should name the clean symbol and relative file:\n%s", spec.Prompt)
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

// ---- Python-specific spec output ----------------------------------------------

// TestGenerate_PySpec drives the shared engine through the Python profile and
// pins the Python-specific outputs: language, task-ID prefix, and the
// py_compile/pytest verification commands.
func TestGenerate_PySpec(t *testing.T) {
	g := newGen(t)
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: gencore.TemplateImplPureFn})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if spec.Language != eval.LanguagePython {
		t.Errorf("Language = %q, want %q", spec.Language, eval.LanguagePython)
	}
	if !strings.HasPrefix(spec.TaskID, "kg-py-impl-") {
		t.Errorf("TaskID %q should start with kg-py-impl-", spec.TaskID)
	}
	if got := spec.Verification.BuildCmd; len(got) < 3 || got[0] != pythonCmd || got[2] != pyCompile {
		t.Errorf("BuildCmd = %v, want py_compile invocation", got)
	}
	if got := spec.Verification.TestCmd; len(got) < 3 || got[0] != pythonCmd || got[2] != pytestPkg {
		t.Errorf("TestCmd = %v, want pytest invocation", got)
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("spec.Validate(): %v", err)
	}
}

func TestGenerate_AddTestCoverageArtifact(t *testing.T) {
	g := newGen(t)
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: gencore.TemplateAddTestCoverage})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	art := spec.SolutionArtifacts[0].Path
	if !strings.HasPrefix(art, "pkg/foo/test_") || !strings.HasSuffix(art, ".py") {
		t.Errorf("add-test-coverage artifact = %q, want a test_*.py file", art)
	}
	if !strings.Contains(spec.Prompt, art) {
		t.Errorf("prompt should reference the artifact %q", art)
	}
}

// ---- Python path/command helpers ----------------------------------------------

func TestTestFilePath(t *testing.T) {
	tests := []struct{ implPath, want string }{
		{"pkg/foo/utils.py", "pkg/foo/test_utils.py"},
		{"utils.py", "test_utils.py"},
		{"a/b/c/mod.py", "a/b/c/test_mod.py"},
	}
	for _, tc := range tests {
		if got := testFilePath(tc.implPath); got != tc.want {
			t.Errorf("testFilePath(%q) = %q, want %q", tc.implPath, got, tc.want)
		}
	}
}

func TestBuildCmd(t *testing.T) {
	got := buildCmd("pkg/foo/utils.py")
	want := []string{"python", "-m", "py_compile", "pkg/foo/utils.py"}
	if !equalStrs(got, want) {
		t.Errorf("buildCmd = %v, want %v", got, want)
	}
}

func TestTestCmd(t *testing.T) {
	tests := []struct {
		implPath string
		want     []string
	}{
		{"pkg/foo/utils.py", []string{"python", "-m", "pytest", "-v", "pkg/foo"}},
		{"utils.py", []string{"python", "-m", "pytest", "-v", "."}},
	}
	for _, tc := range tests {
		if got := testCmd(tc.implPath); !equalStrs(got, tc.want) {
			t.Errorf("testCmd(%q) = %v, want %v", tc.implPath, got, tc.want)
		}
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
