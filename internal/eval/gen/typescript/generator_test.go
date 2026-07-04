package tsgen

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
// easy TypeScript symbol, enough to drive a full Generate through the shared
// engine and assert the TypeScript-specific spec output.
type fakeReader struct{}

func (fakeReader) GetAllFiles() ([]string, error) { return []string{"src/foo/foo.ts"}, nil }
func (fakeReader) GetNodesByFile(string) ([]graphstore.GraphNode, error) {
	return []graphstore.GraphNode{{
		Kind:          graphstore.NodeKindFunction,
		Name:          "foo.bar",
		QualifiedName: "foo.bar",
		FilePath:      "src/foo/foo.ts",
		Language:      "typescript",
		LineStart:     1,
		LineEnd:       20,
	}}, nil
}
func (fakeReader) GetNode(qn string) (*graphstore.GraphNode, error) {
	if qn != "foo.bar" {
		return nil, nil
	}
	return &graphstore.GraphNode{
		Kind:          graphstore.NodeKindFunction,
		QualifiedName: "foo.bar",
		FilePath:      "src/foo/foo.ts",
		Language:      "typescript",
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
	if _, ok := r.Lookup(eval.LanguageTypeScript); !ok {
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
		Kind: graphstore.NodeKindFunction, Name: "bar", QualifiedName: r.qn,
		FilePath: r.absFile, Language: "typescript", LineStart: 1, LineEnd: 20,
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

// TestGenerate_TsNormalizesAbsoluteCRGPaths proves the shared normalization
// reaches the TypeScript profile: code-review-graph data (absolute path +
// "file::name") yields a repo-relative node --test glob and a clean symbol, with
// no absolute path in the emitted spec.
func TestGenerate_TsNormalizesAbsoluteCRGPaths(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	absFile := filepath.Join(wd, "src", "foo", "foo.ts")
	q, err := kgquery.New(crgReader{absFile: absFile, qn: absFile + "::bar"})
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
	if got := spec.SolutionArtifacts[0].Path; got != "src/foo/foo.ts" {
		t.Errorf("artifact path = %q, want repo-relative", got)
	}
	if got := spec.Verification.TestCmd; len(got) == 0 || got[len(got)-1] != "src/foo/*.test.ts" {
		t.Errorf("TestCmd = %v, want it to end with the repo-relative glob %q", got, "src/foo/*.test.ts")
	}
	if !strings.Contains(spec.Prompt, "`bar`") || !strings.Contains(spec.Prompt, "`src/foo/foo.ts`") {
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

// ---- TypeScript-specific spec output ------------------------------------------

// TestGenerate_TsSpec drives the shared engine through the TypeScript profile
// and pins the TS-specific outputs: language, task-ID prefix, and the
// tsc/node verification commands.
func TestGenerate_TsSpec(t *testing.T) {
	g := newGen(t)
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: gencore.TemplateImplPureFn})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if spec.Language != eval.LanguageTypeScript {
		t.Errorf("Language = %q, want %q", spec.Language, eval.LanguageTypeScript)
	}
	if !strings.HasPrefix(spec.TaskID, "kg-ts-impl-") {
		t.Errorf("TaskID %q should start with kg-ts-impl-", spec.TaskID)
	}
	if got := spec.Verification.BuildCmd; len(got) < 2 || got[0] != tscCmd || got[1] != noEmitFlag {
		t.Errorf("BuildCmd = %v, want [%q %q]", got, tscCmd, noEmitFlag)
	}
	if got := spec.Verification.TestCmd; len(got) == 0 || got[0] != nodeCmd {
		t.Errorf("TestCmd = %v, want first element %q", got, nodeCmd)
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
	if !strings.HasSuffix(art, ".test.ts") {
		t.Errorf("add-test-coverage artifact = %q, want a *.test.ts file", art)
	}
	if !strings.Contains(spec.Prompt, art) {
		t.Errorf("prompt should reference the artifact %q", art)
	}
}

// ---- TypeScript path/command helpers ------------------------------------------

func TestTestFilePath(t *testing.T) {
	tests := []struct{ implPath, want string }{
		{"src/foo/foo.ts", "src/foo/foo.test.ts"},
		{"foo.ts", "foo.test.ts"},
		{"internal/eval/gen/typescript/generator.ts", "internal/eval/gen/typescript/generator.test.ts"},
	}
	for _, tc := range tests {
		if got := testFilePath(tc.implPath); got != tc.want {
			t.Errorf("testFilePath(%q) = %q, want %q", tc.implPath, got, tc.want)
		}
	}
}

func TestTestGlobPattern(t *testing.T) {
	tests := []struct{ filePath, want string }{
		{"src/foo/foo.ts", "src/foo/*.test.ts"},
		{"foo.ts", "*.test.ts"},
		{"", "*.test.ts"},
		{".", "*.test.ts"},
		{"a/b/c/d.ts", "a/b/c/*.test.ts"},
	}
	for _, tc := range tests {
		if got := testGlobPattern(tc.filePath); got != tc.want {
			t.Errorf("testGlobPattern(%q) = %q, want %q", tc.filePath, got, tc.want)
		}
	}
}

func TestBuildCmd(t *testing.T) {
	got := buildCmd("src/foo/foo.ts")
	want := []string{"tsc", "--noEmit"}
	if !equalStrs(got, want) {
		t.Errorf("buildCmd = %v, want %v", got, want)
	}
}

func TestTestCmd(t *testing.T) {
	got := testCmd("src/foo/foo.ts")
	want := []string{"node", "--test", "src/foo/*.test.ts"}
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
