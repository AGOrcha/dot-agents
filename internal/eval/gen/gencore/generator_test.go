package gencore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/kgquery"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ---- test profile --------------------------------------------------------------

// testProfile is a deliberately non-language profile whose every field carries
// a distinctive marker, so a test asserting a marker in the output proves the
// engine actually threads that profile field through (parameterisation, not a
// hard-coded language). Language must be a real eval.Language because the KG
// query layer validates it; the fakeReader below seeds "go" symbols to match.
var testProfile = Profile{
	Language:           eval.LanguageGo,
	IDToken:            "zz",
	ErrPrefix:          "zzgen",
	DisplayName:        "Zonk",
	NoTestEditFragment: "- Do not touch existing Zonk tests.\n",
	MustSatisfyCmd:     "zz verify",
	TestFileNoun:       "module",
	TestFilePath:       func(p string) string { return strings.TrimSuffix(p, ".go") + ".spec" },
	VerifyTarget:       func(p string) string { return "TGT[" + SlashDir(p) + "]" },
	BuildCmd:           func(p string) []string { return []string{"zzbuild", p} },
	TestCmd:            func(p string) []string { return []string{"zztest", p} },
}

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

// addGoFn registers a Go function node in both the per-file and per-name maps.
func (f *fakeReader) addGoFn(qn, file string, start, end int) {
	n := graphstore.GraphNode{
		Kind:          graphstore.NodeKindFunction,
		Name:          qn,
		QualifiedName: qn,
		FilePath:      file,
		Language:      "go",
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

func mustQuerier(t *testing.T, r graphstore.CodeGraphReader) *kgquery.Querier {
	t.Helper()
	q, err := kgquery.New(r)
	if err != nil {
		t.Fatalf("kgquery.New: %v", err)
	}
	return q
}

func mustGenerator(t *testing.T, r graphstore.CodeGraphReader) *Generator {
	t.Helper()
	g, err := New(mustQuerier(t, r), testProfile)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g
}

// simpleFakeReader returns a fakeReader seeded with a single easy Go symbol:
//
//	pkg/foo.Bar (calls pkg/foo.helper)
//
// NodeCount=2, EdgeCount=1, Cyclomatic=2 → easy.
func simpleFakeReader() *fakeReader {
	f := newFakeReader()
	f.addGoFn("pkg/foo.Bar", "pkg/foo/foo.go", 10, 30)
	f.addGoFn("pkg/foo.helper", "pkg/foo/foo.go", 35, 45)
	f.addCall("pkg/foo.Bar", "pkg/foo.helper")
	return f
}

// mediumFakeReader adds a medium-difficulty symbol to the easy one.
//
//	pkg/baz.Process calls pkg/baz.a, pkg/baz.b, pkg/baz.c (edge_count → medium).
func mediumFakeReader() *fakeReader {
	f := simpleFakeReader()
	f.addGoFn("pkg/baz.Process", "pkg/baz/baz.go", 1, 60)
	f.addGoFn("pkg/baz.a", "pkg/baz/baz.go", 70, 80)
	f.addGoFn("pkg/baz.b", "pkg/baz/baz.go", 85, 95)
	f.addGoFn("pkg/baz.c", "pkg/baz/baz.go", 100, 110)
	f.addCall("pkg/baz.Process", "pkg/baz.a")
	f.addCall("pkg/baz.Process", "pkg/baz.b")
	f.addCall("pkg/baz.Process", "pkg/baz.c")
	f.addCall("pkg/baz.a", "pkg/baz.b")
	f.addCall("pkg/baz.b", "pkg/baz.c")
	return f
}

// ---- New / Register -----------------------------------------------------------

func TestNew_NilQuerier(t *testing.T) {
	_, err := New(nil, testProfile)
	if err == nil {
		t.Fatal("New(nil) should return error")
	}
	if !strings.Contains(err.Error(), testProfile.ErrPrefix) {
		t.Errorf("error %q should carry the profile ErrPrefix %q", err, testProfile.ErrPrefix)
	}
}

// TestNew_MalformedProfile_NilFunc asserts that gencore.New rejects a Profile
// whose required function fields are nil with an error (not a panic). Each
// nil-func case is checked individually so the error message can be inspected.
func TestNew_MalformedProfile_NilFunc(t *testing.T) {
	q := mustQuerier(t, simpleFakeReader())
	cases := []struct {
		name    string
		profile Profile
	}{
		{
			name: "nil TestFilePath",
			profile: Profile{
				Language: testProfile.Language, IDToken: testProfile.IDToken,
				ErrPrefix: testProfile.ErrPrefix, DisplayName: testProfile.DisplayName,
				VerifyTarget: testProfile.VerifyTarget,
				BuildCmd:     testProfile.BuildCmd, TestCmd: testProfile.TestCmd,
				// TestFilePath intentionally nil
			},
		},
		{
			name: "nil VerifyTarget",
			profile: Profile{
				Language: testProfile.Language, IDToken: testProfile.IDToken,
				ErrPrefix: testProfile.ErrPrefix, DisplayName: testProfile.DisplayName,
				TestFilePath: testProfile.TestFilePath,
				BuildCmd:     testProfile.BuildCmd, TestCmd: testProfile.TestCmd,
				// VerifyTarget intentionally nil
			},
		},
		{
			name: "nil BuildCmd",
			profile: Profile{
				Language: testProfile.Language, IDToken: testProfile.IDToken,
				ErrPrefix: testProfile.ErrPrefix, DisplayName: testProfile.DisplayName,
				TestFilePath: testProfile.TestFilePath, VerifyTarget: testProfile.VerifyTarget,
				TestCmd: testProfile.TestCmd,
				// BuildCmd intentionally nil
			},
		},
		{
			name: "nil TestCmd",
			profile: Profile{
				Language: testProfile.Language, IDToken: testProfile.IDToken,
				ErrPrefix: testProfile.ErrPrefix, DisplayName: testProfile.DisplayName,
				TestFilePath: testProfile.TestFilePath, VerifyTarget: testProfile.VerifyTarget,
				BuildCmd: testProfile.BuildCmd,
				// TestCmd intentionally nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(q, tc.profile)
			if err == nil {
				t.Fatalf("New with %s should return error, not panic", tc.name)
			}
			if !strings.Contains(err.Error(), tc.profile.ErrPrefix) {
				t.Errorf("error %q should carry ErrPrefix %q", err, tc.profile.ErrPrefix)
			}
		})
	}
}

func TestNew_OK(t *testing.T) {
	g, err := New(mustQuerier(t, simpleFakeReader()), testProfile)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g == nil {
		t.Fatal("New returned nil generator")
	}
}

func TestRegister_OK(t *testing.T) {
	r := eval.NewRegistry()
	if err := Register(r, mustQuerier(t, simpleFakeReader()), testProfile); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := r.Lookup(testProfile.Language); !ok {
		t.Fatal("generator not found in registry after Register")
	}
}

func TestRegister_Duplicate(t *testing.T) {
	r := eval.NewRegistry()
	q := mustQuerier(t, simpleFakeReader())
	if err := Register(r, q, testProfile); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := Register(r, q, testProfile); err == nil {
		t.Fatal("second Register should error on duplicate language")
	}
}

func TestRegister_NilQuerier(t *testing.T) {
	if err := Register(eval.NewRegistry(), nil, testProfile); err == nil {
		t.Fatal("Register(nil) should error")
	}
}

// ---- Language -----------------------------------------------------------------

func TestLanguage(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	if g.Language() != testProfile.Language {
		t.Errorf("Language() = %q, want %q", g.Language(), testProfile.Language)
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
	if !strings.Contains(err.Error(), testProfile.ErrPrefix) {
		t.Errorf("error %q should carry the profile ErrPrefix", err)
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
	if spec.Language != testProfile.Language {
		t.Errorf("Language = %q, want %q", spec.Language, testProfile.Language)
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

// TestGenerate_VerificationFromProfile pins that the engine sources both
// verification commands from the profile's BuildCmd/TestCmd, not a hard-coded
// language.
func TestGenerate_VerificationFromProfile(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wantBuild := testProfile.BuildCmd("pkg/foo/foo.go")
	wantTest := testProfile.TestCmd("pkg/foo/foo.go")
	if got := spec.Verification.BuildCmd; !equalStrs(got, wantBuild) {
		t.Errorf("BuildCmd = %v, want %v", got, wantBuild)
	}
	if got := spec.Verification.TestCmd; !equalStrs(got, wantTest) {
		t.Errorf("TestCmd = %v, want %v", got, wantTest)
	}
	if spec.Verification.TimeoutSeconds != defaultTimeout {
		t.Errorf("TimeoutSeconds = %d, want %d", spec.Verification.TimeoutSeconds, defaultTimeout)
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
// agent to WRITE. add-test-coverage must target the profile's test file; the
// two implementation templates must target the non-test seed file. In every
// case the artifact path appears in the prompt.
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
			assertArtifactMatchesPromptTarget(t, tc.tid, tc.wantTestFile)
		})
	}
}

// assertArtifactMatchesPromptTarget checks one template: the solution artifact
// is the expected seed/test file and its path appears in the generated prompt.
func assertArtifactMatchesPromptTarget(t *testing.T, tid string, wantTestFile bool) {
	t.Helper()
	const impl = "pkg/foo/foo.go"
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: tid})
	if err != nil {
		t.Fatalf("Generate(%q): %v", tid, err)
	}
	if len(spec.SolutionArtifacts) == 0 {
		t.Fatalf("%s: no solution artifacts", tid)
	}
	artifact := spec.SolutionArtifacts[0].Path
	wantArtifact := impl
	if wantTestFile {
		wantArtifact = testProfile.TestFilePath(impl)
	}
	if artifact != wantArtifact {
		t.Errorf("%s: artifact = %q, want %q", tid, artifact, wantArtifact)
	}
	if !strings.Contains(spec.Prompt, artifact) {
		t.Errorf("%s: artifact %q not referenced by its prompt:\n%s", tid, artifact, spec.Prompt)
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
	g := mustGenerator(t, newFakeReader())
	_, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err == nil {
		t.Fatal("expected error for empty graph")
	}
	if !strings.Contains(err.Error(), testProfile.DisplayName) {
		t.Errorf("error %q should name the profile DisplayName %q", err, testProfile.DisplayName)
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
	f.getNodeErrs["pkg/foo.Bar"] = errors.New("db timeout")
	g := mustGenerator(t, f)
	_, err := g.Generate(context.Background(), eval.GenerateOptions{})
	if err == nil {
		t.Fatal("expected error when GetNode fails for root")
	}
}

func TestGenerate_ComplexityError(t *testing.T) {
	base := simpleFakeReader()
	cfr := &countedFakeReader{
		fakeReader: base,
		failSource: "pkg/foo.Bar",
		failAfter:  1,
		failErr:    errors.New("source edge failure"),
	}
	g, err := New(mustQuerier(t, cfr), testProfile)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err = g.Generate(context.Background(), eval.GenerateOptions{}); err == nil {
		t.Fatal("expected error when ComplexityProxy's GetEdgesBySource fails")
	}
}

// allDeriveFailReader returns a reader whose every seed fails to derive.
func allDeriveFailReader() *fakeReader {
	f := simpleFakeReader()
	f.inErrs["pkg/foo.Bar"] = errors.New("target edge failure bar")
	f.inErrs["pkg/foo.helper"] = errors.New("target edge failure helper")
	return f
}

func TestGenerate_AllDeriveFail(t *testing.T) {
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
	// Regression (#262 swallow-class): when every seed fails to derive under a
	// difficulty constraint, the KG failure must surface — never be masked as
	// an ordinary "no seed matches difficulty".
	g := mustGenerator(t, allDeriveFailReader())
	_, err := g.Generate(context.Background(), eval.GenerateOptions{Difficulty: eval.DifficultyHard})
	if err == nil {
		t.Fatal("expected error when all seeds fail derivation under a difficulty constraint")
	}
	if strings.Contains(err.Error(), "no seed matches difficulty") {
		t.Errorf("error %q masks a KG failure as a no-match", err)
	}
	if !strings.Contains(err.Error(), "target edge failure") {
		t.Errorf("error %q should surface the underlying derivation failure", err)
	}
}

func TestGenerate_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := mustGenerator(t, simpleFakeReader())
	if _, err := g.Generate(ctx, eval.GenerateOptions{}); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---- Generate: TaskID determinism & format ------------------------------------

func TestGenerate_TaskIDDeterministic(t *testing.T) {
	opts := eval.GenerateOptions{TemplateID: TemplateImplPureFn}
	s1, err := mustGenerator(t, simpleFakeReader()).Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	s2, err := mustGenerator(t, simpleFakeReader()).Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	if s1.TaskID != s2.TaskID {
		t.Errorf("TaskIDs differ: %q vs %q", s1.TaskID, s2.TaskID)
	}
}

func TestGenerate_TaskIDUsesProfileToken(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	spec, err := g.Generate(context.Background(), eval.GenerateOptions{TemplateID: TemplateImplPureFn})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "kg-" + testProfile.IDToken + "-impl-"
	if !strings.HasPrefix(spec.TaskID, want) {
		t.Errorf("TaskID %q should start with %q", spec.TaskID, want)
	}
}

// ---- Unit tests for engine helpers --------------------------------------------

func TestSelectTemplateID_Empty(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	tid, err := g.selectTemplateID(eval.GenerateOptions{})
	if err != nil {
		t.Fatalf("selectTemplateID(empty): %v", err)
	}
	if tid != defaultTemplateID {
		t.Errorf("got %q, want %q", tid, defaultTemplateID)
	}
}

func TestSelectTemplateID_Known(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	for _, k := range []string{TemplateImplPureFn, TemplateRefactorExtract, TemplateAddTestCoverage} {
		t.Run(k, func(t *testing.T) {
			tid, err := g.selectTemplateID(eval.GenerateOptions{TemplateID: k})
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
	g := mustGenerator(t, simpleFakeReader())
	if _, err := g.selectTemplateID(eval.GenerateOptions{TemplateID: "bogus"}); err == nil {
		t.Fatal("expected error for unknown template ID")
	}
}

func TestSolutionArtifactPath(t *testing.T) {
	const impl = "pkg/foo/foo.go"
	g := mustGenerator(t, simpleFakeReader())
	tests := []struct {
		tid  string
		want string
	}{
		{TemplateImplPureFn, impl},
		{TemplateRefactorExtract, impl},
		{TemplateAddTestCoverage, testProfile.TestFilePath(impl)},
	}
	for _, tc := range tests {
		if got := g.solutionArtifactPath(tc.tid, impl); got != tc.want {
			t.Errorf("solutionArtifactPath(%q, %q) = %q, want %q", tc.tid, impl, got, tc.want)
		}
	}
}

func TestTaskID_Format(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	got := g.taskID(TemplateImplPureFn, "pkg/foo.Bar")
	want := "kg-zz-impl-pkg-foo-bar"
	if got != want {
		t.Errorf("taskID = %q, want %q", got, want)
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
		{"anything-else", "testcov"},
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
		{"pkg/foo.Bar", "pkg-foo-bar"},
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
		Root:  graphstore.GraphNode{QualifiedName: "pkg/foo.Bar"},
		Nodes: []graphstore.GraphNode{{QualifiedName: "pkg/foo.Bar"}},
	}
	if got := neighborList(nbhd); got != "(none)" {
		t.Errorf("neighborList with root-only = %q, want %q", got, "(none)")
	}
}

func TestNeighborList_WithNeighbors(t *testing.T) {
	nbhd := kgquery.Neighborhood{
		Root: graphstore.GraphNode{QualifiedName: "pkg/foo.Bar"},
		Nodes: []graphstore.GraphNode{
			{QualifiedName: "pkg/foo.Bar"},
			{QualifiedName: "pkg/foo.helper"},
		},
	}
	if got := neighborList(nbhd); !strings.Contains(got, "pkg/foo.helper") {
		t.Errorf("neighborList %q should contain helper symbol", got)
	}
}

func TestNeighborList_Cap(t *testing.T) {
	nodes := []graphstore.GraphNode{{QualifiedName: "root"}}
	for i := 0; i < maxNeighborNames+5; i++ {
		nodes = append(nodes, graphstore.GraphNode{QualifiedName: fmt.Sprintf("sym%d", i)})
	}
	nbhd := kgquery.Neighborhood{Root: graphstore.GraphNode{QualifiedName: "root"}, Nodes: nodes}
	if parts := strings.Split(neighborList(nbhd), ", "); len(parts) > maxNeighborNames {
		t.Errorf("neighborList returned %d names, want at most %d", len(parts), maxNeighborNames)
	}
}

// TestRenderPrompt_AllTemplates checks every template renders a non-empty
// prompt that mentions the seed and threads the profile's language-specific
// fragments (no-test-edit line, must-satisfy command, verify target).
func TestRenderPrompt_AllTemplates(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	r := seedResult{
		seed: graphstore.GraphNode{
			QualifiedName: "pkg/foo.Bar",
			FilePath:      "pkg/foo/foo.go",
		},
		complexity: kgquery.Complexity{
			QualifiedName: "pkg/foo.Bar",
			SpanLines:     20,
			FanOut:        2,
			FanIn:         1,
			Cyclomatic:    3,
		},
		nbhd: kgquery.Neighborhood{
			Root:  graphstore.GraphNode{QualifiedName: "pkg/foo.Bar"},
			Nodes: []graphstore.GraphNode{{QualifiedName: "pkg/foo.Bar"}},
		},
		band:    eval.DifficultyEasy,
		signals: map[string]int{},
	}
	for _, tid := range []string{TemplateImplPureFn, TemplateRefactorExtract, TemplateAddTestCoverage} {
		t.Run(tid, func(t *testing.T) {
			p := g.renderPrompt(tid, r)
			if p == "" {
				t.Fatalf("renderPrompt(%q) returned empty string", tid)
			}
			if !strings.Contains(p, "pkg/foo.Bar") {
				t.Errorf("prompt %q missing seed symbol", p)
			}
			if !strings.Contains(p, testProfile.MustSatisfyCmd) {
				t.Errorf("prompt %q missing must-satisfy command %q", p, testProfile.MustSatisfyCmd)
			}
			if !strings.Contains(p, testProfile.VerifyTarget(r.seed.FilePath)) {
				t.Errorf("prompt %q missing verify target", p)
			}
		})
	}
}

// TestPrompt_NoTestEditFragment checks the two implementation templates carry
// the profile's no-test-edit fragment while add-test-coverage carries the
// language-agnostic noun-parameterised test-file instruction.
func TestPrompt_Fragments(t *testing.T) {
	g := mustGenerator(t, simpleFakeReader())
	r := seedResult{
		seed: graphstore.GraphNode{QualifiedName: "pkg/foo.Bar", FilePath: "pkg/foo/foo.go"},
		nbhd: kgquery.Neighborhood{Root: graphstore.GraphNode{QualifiedName: "pkg/foo.Bar"}},
	}
	impl := g.renderPrompt(TemplateImplPureFn, r)
	if !strings.Contains(impl, testProfile.NoTestEditFragment) {
		t.Errorf("impl prompt missing no-test-edit fragment:\n%s", impl)
	}
	cov := g.renderPrompt(TemplateAddTestCoverage, r)
	if !strings.Contains(cov, "in the same "+testProfile.TestFileNoun+".") {
		t.Errorf("add-test-coverage prompt missing noun %q:\n%s", testProfile.TestFileNoun, cov)
	}
}

// ---- SlashDir (profile.go) ----------------------------------------------------

func TestSlashDir(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"internal/eval/foo.go", "internal/eval"},
		{"foo.go", "."},
		{"", "."},
		{".", "."},
		{"a/b/c/d.ts", "a/b/c"},
	}
	for _, tc := range tests {
		if got := SlashDir(tc.in); got != tc.want {
			t.Errorf("SlashDir(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
