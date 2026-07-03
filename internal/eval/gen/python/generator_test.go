package pygen

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/kgquery"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ---- fakeReader ----------------------------------------------------------------

// fakeReader is a minimal in-memory graphstore.CodeGraphReader for unit tests.
// Only the four methods used by kgquery.Querier are non-trivial; the rest
// return zero values so the fakeReader satisfies the full interface.
type fakeReader struct {
	nodes       map[string]graphstore.GraphNode
	outEdges    map[string][]graphstore.GraphEdge
	inEdges     map[string][]graphstore.GraphEdge
	files       []string
	byFile      map[string][]graphstore.GraphNode
	filesErr    error
	getNodeErrs map[string]error
	outErrs     map[string]error
	inErrs      map[string]error
}

func newFakeReader() *fakeReader {
	return &fakeReader{
		nodes:       map[string]graphstore.GraphNode{},
		outEdges:    map[string][]graphstore.GraphEdge{},
		inEdges:     map[string][]graphstore.GraphEdge{},
		byFile:      map[string][]graphstore.GraphNode{},
		getNodeErrs: map[string]error{},
		outErrs:     map[string]error{},
		inErrs:      map[string]error{},
	}
}

// addPyFn registers a Python function node in both the per-file and per-name maps.
func (f *fakeReader) addPyFn(qn, file string, start, end int) {
	n := graphstore.GraphNode{
		Kind:          graphstore.NodeKindFunction,
		Name:          qn,
		QualifiedName: qn,
		FilePath:      file,
		Language:      "python",
		LineStart:     start,
		LineEnd:       end,
	}
	f.nodes[qn] = n
	if _, seen := f.byFile[file]; !seen {
		f.files = append(f.files, file)
	}
	f.byFile[file] = append(f.byFile[file], n)
}

// addCall records a CALLS edge in both directions.
func (f *fakeReader) addCall(src, tgt string) {
	e := graphstore.GraphEdge{Kind: graphstore.EdgeKindCalls, SourceQualified: src, TargetQualified: tgt}
	f.outEdges[src] = append(f.outEdges[src], e)
	f.inEdges[tgt] = append(f.inEdges[tgt], e)
}

func (f *fakeReader) GetAllFiles() ([]string, error) {
	return f.files, f.filesErr
}
func (f *fakeReader) GetNodesByFile(file string) ([]graphstore.GraphNode, error) {
	return f.byFile[file], nil
}
func (f *fakeReader) GetNode(qn string) (*graphstore.GraphNode, error) {
	if err := f.getNodeErrs[qn]; err != nil {
		return nil, err
	}
	n, ok := f.nodes[qn]
	if !ok {
		return nil, nil
	}
	cp := n
	return &cp, nil
}
func (f *fakeReader) GetEdgesBySource(qn string) ([]graphstore.GraphEdge, error) {
	if err := f.outErrs[qn]; err != nil {
		return nil, err
	}
	return f.outEdges[qn], nil
}
func (f *fakeReader) GetEdgesByTarget(qn string) ([]graphstore.GraphEdge, error) {
	if err := f.inErrs[qn]; err != nil {
		return nil, err
	}
	return f.inEdges[qn], nil
}

// Unused CodeGraphReader methods.
func (f *fakeReader) GetEdgesAmong([]string) ([]graphstore.GraphEdge, error)  { return nil, nil }
func (f *fakeReader) SearchNodes(string, int) ([]graphstore.GraphNode, error) { return nil, nil }
func (f *fakeReader) GetMetadata(string) (string, error)                      { return "", nil }
func (f *fakeReader) GetStats() (graphstore.GraphStats, error) {
	return graphstore.GraphStats{}, nil
}
func (f *fakeReader) GetImpactRadius([]string, int, int) (graphstore.ImpactResult, error) {
	return graphstore.ImpactResult{}, nil
}

var _ graphstore.CodeGraphReader = (*fakeReader)(nil)

// countedFakeReader wraps fakeReader and injects an error on the Nth call to
// GetEdgesBySource for a specific qualified name, letting NeighborhoodFor
// succeed (first call) while ComplexityProxy fails (second call).
type countedFakeReader struct {
	*fakeReader
	failSource string
	failAfter  int
	calls      int
	failErr    error
}

func (c *countedFakeReader) GetEdgesBySource(qn string) ([]graphstore.GraphEdge, error) {
	if qn == c.failSource {
		c.calls++
		if c.calls > c.failAfter {
			return nil, c.failErr
		}
	}
	return c.fakeReader.GetEdgesBySource(qn)
}

var _ graphstore.CodeGraphReader = (*countedFakeReader)(nil)

// ---- helpers ------------------------------------------------------------------

// mustQuerier wraps kgquery.New for tests, failing the test on error.
func mustQuerier(t *testing.T, r graphstore.CodeGraphReader) *kgquery.Querier {
	t.Helper()
	q, err := kgquery.New(r)
	if err != nil {
		t.Fatalf("kgquery.New: %v", err)
	}
	return q
}

// mustGenerator wraps New for tests, failing the test on error.
func mustGenerator(t *testing.T, r graphstore.CodeGraphReader) *Generator {
	t.Helper()
	g, err := New(mustQuerier(t, r))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g
}

// simpleFakeReader returns a fakeReader seeded with a single easy Python symbol:
//
//	pkg/foo.bar_fn (calls pkg/foo.helper_fn)
//
// NodeCount=2, EdgeCount=1, Cyclomatic=2 → easy.
func simpleFakeReader() *fakeReader {
	f := newFakeReader()
	f.addPyFn("pkg/foo.bar_fn", "pkg/foo/utils.py", 10, 30)
	f.addPyFn("pkg/foo.helper_fn", "pkg/foo/utils.py", 35, 45)
	f.addCall("pkg/foo.bar_fn", "pkg/foo.helper_fn")
	return f
}

// mediumFakeReader returns a fakeReader with a medium-difficulty symbol in
// addition to an easy one. The medium symbol has four neighborhood nodes,
// which exceeds easyMaxNodes=3.
//
//	pkg/baz.process calls pkg/baz.a_fn, pkg/baz.b_fn, pkg/baz.c_fn
//	NodeCount=4, EdgeCount=3, Cyclomatic=4 → medium (edge_count gives medium).
func mediumFakeReader() *fakeReader {
	f := simpleFakeReader()
	f.addPyFn("pkg/baz.process", "pkg/baz/core.py", 1, 60)
	f.addPyFn("pkg/baz.a_fn", "pkg/baz/core.py", 70, 80)
	f.addPyFn("pkg/baz.b_fn", "pkg/baz/core.py", 85, 95)
	f.addPyFn("pkg/baz.c_fn", "pkg/baz/core.py", 100, 110)
	f.addCall("pkg/baz.process", "pkg/baz.a_fn")
	f.addCall("pkg/baz.process", "pkg/baz.b_fn")
	f.addCall("pkg/baz.process", "pkg/baz.c_fn")
	// Add calls among helpers to increase edge count into medium range.
	f.addCall("pkg/baz.a_fn", "pkg/baz.b_fn")
	f.addCall("pkg/baz.b_fn", "pkg/baz.c_fn")
	return f
}

// ---- New / Register -----------------------------------------------------------

func TestNew_NilQuerier(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("New(nil) should return error")
	}
}

func TestNew_OK(t *testing.T) {
	g, err := New(mustQuerier(t, simpleFakeReader()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g == nil {
		t.Fatal("New returned nil generator")
	}
}

func TestRegister_OK(t *testing.T) {
	r := eval.NewRegistry()
	err := Register(r, mustQuerier(t, simpleFakeReader()))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := r.Lookup(eval.LanguagePython); !ok {
		t.Fatal("generator not found in registry after Register")
	}
}

func TestRegister_Duplicate(t *testing.T) {
	r := eval.NewRegistry()
	q := mustQuerier(t, simpleFakeReader())
	if err := Register(r, q); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := Register(r, q)
	if err == nil {
		t.Fatal("second Register should error on duplicate language")
	}
}

func TestRegister_NilQuerier(t *testing.T) {
	err := Register(eval.NewRegistry(), nil)
	if err == nil {
		t.Fatal("Register(nil) should error")
	}
}

// ---- Language -----------------------------------------------------------------

func TestLanguage(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	if g.Language() != eval.LanguagePython {
		t.Errorf("Language() = %q, want %q", g.Language(), eval.LanguagePython)
	}
}

// ---- Generate: template selection ---------------------------------------------

func TestGenerate_DefaultTemplate(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if spec.GeneratedFrom.TemplateID != TemplateImplPureFn {
		t.Errorf("default template = %q, want %q", spec.GeneratedFrom.TemplateID, TemplateImplPureFn)
	}
}

func TestGenerate_UnknownTemplate(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	_, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: "nonexistent"})
	if err == nil {
		t.Fatal("unknown template should error")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error %q should mention template name", err)
	}
}

func TestGenerate_AllThreeTemplates(t *testing.T) {
	templates := []string{TemplateImplPureFn, TemplateRefactorExtract, TemplateAddTestCoverage}
	for _, tid := range templates {
		t.Run(tid, func(t *testing.T) {
			g := mustGenerator(t, simpleFakeReader())
			spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: tid})
			if err != nil {
				t.Fatalf("Generate(%q): %v", tid, err)
			}
			if spec.GeneratedFrom.TemplateID != tid {
				t.Errorf("TemplateID = %q, want %q", spec.GeneratedFrom.TemplateID, tid)
			}
			if err := spec.Validate(); err != nil {
				t.Errorf("spec.Validate(): %v", err)
			}
		})
	}
}

// ---- Generate: spec invariants ------------------------------------------------

func TestGenerate_SpecValidates(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("returned spec failed Validate: %v", err)
	}
}

func TestGenerate_SpecLanguage(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if spec.Language != eval.LanguagePython {
		t.Errorf("Language = %q, want %q", spec.Language, eval.LanguagePython)
	}
}

func TestGenerate_SpecVersion(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if spec.TaskSpecVersion != eval.CurrentTaskSpecVersion {
		t.Errorf("TaskSpecVersion = %d, want %d", spec.TaskSpecVersion, eval.CurrentTaskSpecVersion)
	}
}

func TestGenerate_SpecKGQuery(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if spec.GeneratedFrom.KGQuery == nil {
		t.Fatal("KGQuery is nil")
	}
	if spec.GeneratedFrom.KGQuery.SeedSymbol == "" {
		t.Error("KGQuery.SeedSymbol is empty")
	}
	if spec.GeneratedFrom.Kind != eval.KindKGTemplate {
		t.Errorf("Kind = %q, want %q", spec.GeneratedFrom.Kind, eval.KindKGTemplate)
	}
}

func TestGenerate_VerificationCmdsIncludePython(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(spec.Verification.TestCmd) == 0 || spec.Verification.TestCmd[0] != pythonCmd {
		t.Errorf("TestCmd = %v, want first element %q", spec.Verification.TestCmd, pythonCmd)
	}
	if len(spec.Verification.BuildCmd) == 0 || spec.Verification.BuildCmd[0] != pythonCmd {
		t.Errorf("BuildCmd = %v, want first element %q", spec.Verification.BuildCmd, pythonCmd)
	}
}

func TestGenerate_VerificationTestCmdIncludesPytest(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	found := false
	for _, arg := range spec.Verification.TestCmd {
		if arg == pytestPkg {
			found = true
		}
	}
	if !found {
		t.Errorf("TestCmd %v missing %q sub-command", spec.Verification.TestCmd, pytestPkg)
	}
}

func TestGenerate_DifficultySignalsPresent(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	required := []string{
		eval.SignalInvolvedSymbols,
		eval.SignalEdgeCount,
		eval.SignalCyclomaticComplexity,
	}
	for _, k := range required {
		if _, ok := spec.DifficultySignals[k]; !ok {
			t.Errorf("DifficultySignals missing %q", k)
		}
	}
}

func TestGenerate_SolutionArtifactPresent(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(spec.SolutionArtifacts) == 0 {
		t.Fatal("SolutionArtifacts is empty")
	}
	if spec.SolutionArtifacts[0].Path == "" {
		t.Error("SolutionArtifacts[0].Path is empty")
	}
}

// TestGenerate_ArtifactMatchesPromptTarget pins the invariant that each
// template's expected solution artifact is the file its prompt directs the
// agent to WRITE — so a downstream verifier/scorer looks for the diff in the
// right file. add-test-coverage must target a test_*.py file (its prompt tells
// the agent to write tests); the two implementation templates must target the
// non-test seed file. A template whose prompt says "write test_foo.py" but
// whose spec names "foo.py" fails here.
func TestGenerate_ArtifactMatchesPromptTarget(t *testing.T) {
	cases := []struct {
		tid          string
		wantTestFile bool
	}{
		{TemplateImplPureFn, false},
		{TemplateRefactorExtract, false},
		{TemplateAddTestCoverage, true},
	}
	for _, tc := range cases {
		t.Run(tc.tid, func(t *testing.T) {
			g := mustGenerator(t, simpleFakeReader())
			spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: tc.tid})
			if err != nil {
				t.Fatalf("Generate(%q): %v", tc.tid, err)
			}
			if len(spec.SolutionArtifacts) == 0 {
				t.Fatalf("%s: no solution artifacts", tc.tid)
			}
			artifact := spec.SolutionArtifacts[0].Path
			isTest := strings.HasPrefix(filepath.Base(artifact), "test_")
			if isTest != tc.wantTestFile {
				t.Errorf("%s: artifact %q isTestFile=%v, want %v", tc.tid, artifact, isTest, tc.wantTestFile)
			}
			// The expected artifact must be the file the prompt actually
			// directs the agent to write, so its path appears in the prompt.
			if !strings.Contains(spec.Prompt, artifact) {
				t.Errorf("%s: artifact %q not referenced by its prompt:\n%s", tc.tid, artifact, spec.Prompt)
			}
		})
	}
}

// ---- Generate: difficulty constraint ------------------------------------------

func TestGenerate_DifficultyConstraint_Easy(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{Difficulty: eval.DifficultyEasy})
	if err != nil {
		t.Fatalf("Generate with easy constraint: %v", err)
	}
	if spec.Difficulty != eval.DifficultyEasy {
		t.Errorf("Difficulty = %q, want %q", spec.Difficulty, eval.DifficultyEasy)
	}
}

func TestGenerate_DifficultyConstraint_Medium(t *testing.T) {
	g := mustGenerator(t, mediumFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{Difficulty: eval.DifficultyMedium})
	if err != nil {
		t.Fatalf("Generate with medium constraint: %v", err)
	}
	if spec.Difficulty != eval.DifficultyMedium {
		t.Errorf("Difficulty = %q, want %q", spec.Difficulty, eval.DifficultyMedium)
	}
}

func TestGenerate_DifficultyConstraint_NoMatch(t *testing.T) {
	// simpleFakeReader has only easy symbols; requesting hard should fail.
	// Every seed derives cleanly here, so exhausting the search is a genuine
	// no-match — the error must be the no-match, not a masked failure.
	g := mustGenerator(t, simpleFakeReader())
	_, err := g.Generate(context.Background(), eval.GenerateOptions{Difficulty: eval.DifficultyHard})
	if err == nil {
		t.Fatal("expected error when no seed matches difficulty hard")
	}
	if !strings.Contains(err.Error(), "no seed matches difficulty") {
		t.Errorf("error %q should report the genuine no-match; seeds derived cleanly", err)
	}
}

// ---- Generate: error propagation ----------------------------------------------

func TestGenerate_EmptyGraph(t *testing.T) {
	g := mustGenerator(t, newFakeReader()) // no nodes at all
	_, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err == nil {
		t.Fatal("expected error for empty graph")
	}
}

func TestGenerate_SeedSymbolsError(t *testing.T) {
	f := simpleFakeReader()
	f.filesErr = errors.New("storage unavailable")
	g := mustGenerator(t, f)
	_, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err == nil {
		t.Fatal("expected error when GetAllFiles fails")
	}
	if !strings.Contains(err.Error(), "storage unavailable") {
		t.Errorf("error %q should mention underlying cause", err)
	}
}

func TestGenerate_NeighborhoodError(t *testing.T) {
	f := simpleFakeReader()
	f.getNodeErrs["pkg/foo.bar_fn"] = errors.New("db timeout")
	g := mustGenerator(t, f)
	_, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err == nil {
		t.Fatal("expected error when GetNode fails for root")
	}
}

func TestGenerate_ComplexityError(t *testing.T) {
	// NeighborhoodFor (depth=2) calls GetEdgesBySource("pkg/foo.bar_fn") once
	// during graph traversal. ComplexityProxy calls it a second time. We use
	// countedFakeReader to let the first call succeed (NeighborhoodFor) and
	// fail on the second call (ComplexityProxy), isolating the error path.
	base := simpleFakeReader()
	cfr := &countedFakeReader{
		fakeReader: base,
		failSource: "pkg/foo.bar_fn",
		failAfter:  1,
		failErr:    errors.New("source edge failure"),
	}
	q, err := kgquery.New(cfr)
	if err != nil {
		t.Fatalf("kgquery.New: %v", err)
	}
	g, err := New(q)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = g.Generate(context.Background(), eval.GenerateOptions{})
	if err == nil {
		t.Fatal("expected error when ComplexityProxy's GetEdgesBySource fails")
	}
}

// allDeriveFailReader returns a reader whose every seed fails to derive:
// GetEdgesByTarget errors for both symbols, so ComplexityProxy fails after
// NeighborhoodFor succeeds. Models a KG read/query/storage fault.
func allDeriveFailReader() *fakeReader {
	f := simpleFakeReader()
	f.inErrs["pkg/foo.bar_fn"] = errors.New("target edge failure bar")
	f.inErrs["pkg/foo.helper_fn"] = errors.New("target edge failure helper")
	return f
}

func TestGenerate_AllDeriveFail(t *testing.T) {
	// When all seeds fail derivation and no difficulty constraint is set,
	// the error from the first failure is surfaced — not masked.
	g := mustGenerator(t, allDeriveFailReader())
	_, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err == nil {
		t.Fatal("expected error when all seeds fail derivation")
	}
	if !strings.Contains(err.Error(), "target edge failure") {
		t.Errorf("error %q should surface the underlying derivation failure", err)
	}
}

func TestGenerate_AllDeriveFail_WithDifficultyConstraint(t *testing.T) {
	// Regression (cross-brain gate NOT-SOUND, #262 swallow-class): when a
	// difficulty constraint IS set and every seed fails to derive because of
	// a KG error, the infrastructure error must be surfaced — never masked as
	// an ordinary "no seed matches difficulty" empty result. No seed derived
	// cleanly, so there is no legitimate no-match to report.
	g := mustGenerator(t, allDeriveFailReader())
	_, err := g.Generate(context.Background(), eval.GenerateOptions{Difficulty: eval.DifficultyHard})
	if err == nil {
		t.Fatal("expected error when all seeds fail derivation under a difficulty constraint")
	}
	if strings.Contains(err.Error(), "no seed matches difficulty") {
		t.Errorf("error %q masks a KG failure as a no-match; want the derivation error surfaced", err)
	}
	if !strings.Contains(err.Error(), "target edge failure") {
		t.Errorf("error %q should surface the underlying derivation failure", err)
	}
}

func TestGenerate_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	g := mustGenerator(t, simpleFakeReader())
	_, err := g.Generate(ctx, eval.GenerateOptions{})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---- Generate: TaskID determinism --------------------------------------------

func TestGenerate_TaskIDDeterministic(t *testing.T) {
	// Same graph + same options should produce the same TaskID.
	opts := eval.GenerateOptions{TemplateID: TemplateImplPureFn}
	g1 := mustGenerator(t, simpleFakeReader())
	g2 := mustGenerator(t, simpleFakeReader())
	s1, err := g1.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	s2, err := g2.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	if s1.TaskID != s2.TaskID {
		t.Errorf("TaskIDs differ: %q vs %q", s1.TaskID, s2.TaskID)
	}
}

func TestGenerate_TaskIDPrefixedCorrectly(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: TemplateImplPureFn})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(spec.TaskID, "kg-py-impl-") {
		t.Errorf("TaskID %q should start with kg-py-impl-", spec.TaskID)
	}
}

// ---- Unit tests for unexported helpers ----------------------------------------

func TestSelectTemplateID_Empty(t *testing.T) {
	tid, err := selectTemplateID(eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("selectTemplateID(empty): %v", err)
	}
	if tid != defaultTemplateID {
		t.Errorf("got %q, want %q", tid, defaultTemplateID)
	}
}

func TestSelectTemplateID_Known(t *testing.T) {
	known := []string{TemplateImplPureFn, TemplateRefactorExtract, TemplateAddTestCoverage}
	for _, k := range known {
		t.Run(k, func(t *testing.T) {
			tid, err := selectTemplateID(eval.GenerateOptions{TemplateID: k})
			if err != nil {
				t.Fatalf("selectTemplateID(%q): %v", k, err)
			}
			if tid != k {
				t.Errorf("got %q, want %q", tid, k)
			}
		})
	}
}

func TestSelectTemplateID_Unknown(t *testing.T) {
	_, err := selectTemplateID(eval.GenerateOptions{TemplateID: "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown template ID")
	}
}

func TestTestFilePath(t *testing.T) {
	tests := []struct {
		implPath string
		want     string
	}{
		{"pkg/foo/utils.py", "pkg/foo/test_utils.py"},
		{"utils.py", "test_utils.py"},
		{"internal/eval/gen/python/generator.py", "internal/eval/gen/python/test_generator.py"},
	}
	for _, tc := range tests {
		if got := testFilePath(tc.implPath); got != tc.want {
			t.Errorf("testFilePath(%q) = %q, want %q", tc.implPath, got, tc.want)
		}
	}
}

func TestSolutionArtifactPath(t *testing.T) {
	const impl = "pkg/foo/utils.py"
	tests := []struct {
		tid  string
		want string
	}{
		{TemplateImplPureFn, impl},
		{TemplateRefactorExtract, impl},
		{TemplateAddTestCoverage, "pkg/foo/test_utils.py"},
	}
	for _, tc := range tests {
		if got := solutionArtifactPath(tc.tid, impl); got != tc.want {
			t.Errorf("solutionArtifactPath(%q, %q) = %q, want %q", tc.tid, impl, got, tc.want)
		}
	}
}

func TestDirPath(t *testing.T) {
	tests := []struct {
		filePath string
		want     string
	}{
		{"internal/eval/foo.py", "internal/eval"},
		{"foo.py", "."},
		{"", "."},
		{".", "."},
		{"a/b/c/d.py", "a/b/c"},
	}
	for _, tc := range tests {
		if got := dirPath(tc.filePath); got != tc.want {
			t.Errorf("dirPath(%q) = %q, want %q", tc.filePath, got, tc.want)
		}
	}
}

func TestTemplateShort(t *testing.T) {
	tests := []struct {
		tid  string
		want string
	}{
		{TemplateImplPureFn, "impl"},
		{TemplateRefactorExtract, "refactor"},
		{TemplateAddTestCoverage, "testcov"},
		{"anything-else", "testcov"}, // default branch
	}
	for _, tc := range tests {
		if got := templateShort(tc.tid); got != tc.want {
			t.Errorf("templateShort(%q) = %q, want %q", tc.tid, got, tc.want)
		}
	}
}

func TestSanitizeQN(t *testing.T) {
	tests := []struct {
		qn   string
		want string
	}{
		{"pkg/foo.bar_fn", "pkg-foo-bar-fn"},
		{"UPPER.CASE", "upper-case"},
		{"  leading  ", "leading"},
		{strings.Repeat("a", 40), strings.Repeat("a", taskIDMaxLen)},
		{"pkg/foo::bar", "pkg-foo-bar"},
	}
	for _, tc := range tests {
		if got := sanitizeQN(tc.qn); got != tc.want {
			t.Errorf("sanitizeQN(%q) = %q, want %q", tc.qn, got, tc.want)
		}
	}
}

func TestMatchesDifficulty(t *testing.T) {
	tests := []struct {
		band eval.Difficulty
		want eval.Difficulty
		ok   bool
	}{
		{eval.DifficultyEasy, "", true},
		{eval.DifficultyMedium, "", true},
		{eval.DifficultyEasy, eval.DifficultyEasy, true},
		{eval.DifficultyMedium, eval.DifficultyEasy, false},
		{eval.DifficultyHard, eval.DifficultyMedium, false},
	}
	for _, tc := range tests {
		if got := matchesDifficulty(tc.band, tc.want); got != tc.ok {
			t.Errorf("matchesDifficulty(%q,%q) = %v, want %v", tc.band, tc.want, got, tc.ok)
		}
	}
}

func TestNeighborList_NoNeighbors(t *testing.T) {
	nbhd := kgquery.Neighborhood{
		Root:  graphstore.GraphNode{QualifiedName: "pkg/foo.bar_fn"},
		Nodes: []graphstore.GraphNode{{QualifiedName: "pkg/foo.bar_fn"}},
	}
	if got := neighborList(nbhd); got != "(none)" {
		t.Errorf("neighborList with root-only = %q, want %q", got, "(none)")
	}
}

func TestNeighborList_WithNeighbors(t *testing.T) {
	nbhd := kgquery.Neighborhood{
		Root: graphstore.GraphNode{QualifiedName: "pkg/foo.bar_fn"},
		Nodes: []graphstore.GraphNode{
			{QualifiedName: "pkg/foo.bar_fn"},
			{QualifiedName: "pkg/foo.helper_fn"},
		},
	}
	got := neighborList(nbhd)
	if !strings.Contains(got, "pkg/foo.helper_fn") {
		t.Errorf("neighborList %q should contain helper symbol", got)
	}
}

func TestNeighborList_Cap(t *testing.T) {
	nodes := []graphstore.GraphNode{{QualifiedName: "root"}}
	for i := 0; i < maxNeighborNames+5; i++ {
		nodes = append(nodes, graphstore.GraphNode{
			QualifiedName: fmt.Sprintf("sym%d", i),
		})
	}
	nbhd := kgquery.Neighborhood{Root: graphstore.GraphNode{QualifiedName: "root"}, Nodes: nodes}
	parts := strings.Split(neighborList(nbhd), ", ")
	if len(parts) > maxNeighborNames {
		t.Errorf("neighborList returned %d names, want at most %d", len(parts), maxNeighborNames)
	}
}

func TestRenderPrompt_AllTemplates(t *testing.T) {
	r := seedResult{
		seed: graphstore.GraphNode{
			QualifiedName: "pkg/foo.bar_fn",
			FilePath:      "pkg/foo/utils.py",
		},
		complexity: kgquery.Complexity{
			QualifiedName: "pkg/foo.bar_fn",
			SpanLines:     20,
			FanOut:        2,
			FanIn:         1,
			Cyclomatic:    3,
		},
		nbhd: kgquery.Neighborhood{
			Root:  graphstore.GraphNode{QualifiedName: "pkg/foo.bar_fn"},
			Nodes: []graphstore.GraphNode{{QualifiedName: "pkg/foo.bar_fn"}},
		},
		band:    eval.DifficultyEasy,
		signals: map[string]int{},
	}
	tids := []string{TemplateImplPureFn, TemplateRefactorExtract, TemplateAddTestCoverage}
	for _, tid := range tids {
		t.Run(tid, func(t *testing.T) {
			p := renderPrompt(tid, r)
			if p == "" {
				t.Errorf("renderPrompt(%q) returned empty string", tid)
			}
			if !strings.Contains(p, "pkg/foo.bar_fn") {
				t.Errorf("prompt %q missing seed symbol", p)
			}
		})
	}
}

func TestTaskID_Format(t *testing.T) {
	got := taskID(TemplateImplPureFn, "pkg/foo.bar_fn")
	if got != "kg-py-impl-pkg-foo-bar-fn" {
		t.Errorf("taskID = %q, want %q", got, "kg-py-impl-pkg-foo-bar-fn")
	}
}
