package harness

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/gen/gencore"
	gogen "github.com/AGOrcha/dot-agents/internal/eval/gen/golang"
	"github.com/AGOrcha/dot-agents/internal/eval/kgquery"
	"github.com/AGOrcha/dot-agents/internal/eval/runner"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	"github.com/AGOrcha/dot-agents/internal/eval/scoringbridge"
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
	goverifier "github.com/AGOrcha/dot-agents/internal/eval/verifier/golang"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// ---- fakes -------------------------------------------------------------------

// fakeReader is a minimal in-memory graphstore.CodeGraphReader — just enough
// for kgquery.Querier to drive the real Go generator over a fixture graph.
type fakeReader struct {
	files  []string
	byFile map[string][]graphstore.GraphNode
	nodes  map[string]graphstore.GraphNode
}

func newFakeReader() *fakeReader {
	return &fakeReader{
		byFile: map[string][]graphstore.GraphNode{},
		nodes:  map[string]graphstore.GraphNode{},
	}
}

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
	if _, seen := f.byFile[file]; !seen {
		f.files = append(f.files, file)
	}
	f.byFile[file] = append(f.byFile[file], n)
	f.nodes[qn] = n
}

func (f *fakeReader) GetAllFiles() ([]string, error) { return f.files, nil }
func (f *fakeReader) GetNodesByFile(file string) ([]graphstore.GraphNode, error) {
	return f.byFile[file], nil
}
func (f *fakeReader) GetNode(qn string) (*graphstore.GraphNode, error) {
	n, ok := f.nodes[qn]
	if !ok {
		return nil, nil
	}
	cp := n
	return &cp, nil
}
func (f *fakeReader) GetEdgesBySource(string) ([]graphstore.GraphEdge, error) { return nil, nil }
func (f *fakeReader) GetEdgesByTarget(string) ([]graphstore.GraphEdge, error) { return nil, nil }
func (f *fakeReader) GetEdgesAmong([]string) ([]graphstore.GraphEdge, error)  { return nil, nil }
func (f *fakeReader) SearchNodes(string, int) ([]graphstore.GraphNode, error) { return nil, nil }
func (f *fakeReader) GetMetadata(string) (string, error)                      { return "", nil }
func (f *fakeReader) GetStats() (graphstore.GraphStats, error)                { return graphstore.GraphStats{}, nil }
func (f *fakeReader) GetImpactRadius([]string, int, int) (graphstore.ImpactResult, error) {
	return graphstore.ImpactResult{}, nil
}

var _ graphstore.CodeGraphReader = (*fakeReader)(nil)

// fakeGenerator is a scripted eval.Generator for the unit paths.
type fakeGenerator struct {
	lang eval.Language
	spec *eval.TaskSpec
	err  error
}

func (g fakeGenerator) Language() eval.Language { return g.lang }
func (g fakeGenerator) Generate(context.Context, eval.GenerateOptions) (*eval.TaskSpec, error) {
	return g.spec, g.err
}

// fakeSandbox is a scripted sandbox.Sandbox that hands back a pre-built
// instance (its Cleanup is a no-op because the unexported cleanup hook is nil).
type fakeSandbox struct {
	inst       *sandbox.Instance
	err        error
	provisions int
}

func (s *fakeSandbox) Provision(context.Context, *eval.TaskSpec) (*sandbox.Instance, error) {
	s.provisions++
	return s.inst, s.err
}
func (s *fakeSandbox) PruneStale(context.Context) ([]string, error) { return nil, nil }

// fakeVerifier is a scripted verifier.Verifier for the unit paths.
type fakeVerifier struct {
	lang        eval.Language
	res         *verifier.VerifyResult
	err         error
	calls       int
	lastWorkdir string
	lastEnv     []string
}

func (v *fakeVerifier) Language() eval.Language { return v.lang }
func (v *fakeVerifier) Verify(_ context.Context, _ *eval.TaskSpec, workdir string, env []string) (*verifier.VerifyResult, error) {
	v.calls++
	v.lastWorkdir = workdir
	v.lastEnv = env
	return v.res, v.err
}

// ---- helpers -----------------------------------------------------------------

func validSpec() *eval.TaskSpec {
	return &eval.TaskSpec{
		TaskSpecVersion: eval.CurrentTaskSpecVersion,
		TaskID:          "kg-go-impl-fixture",
		Language:        eval.LanguageGo,
		Difficulty:      eval.DifficultyEasy,
		GeneratedFrom:   eval.GeneratedFrom{Kind: eval.KindKGTemplate},
		Prompt:          "implement the function",
		Verification: eval.Verification{
			BuildCmd:       []string{"go", "build", "./..."},
			TestCmd:        []string{"go", "test", "./..."},
			TimeoutSeconds: 60,
		},
	}
}

func fakeInstance(t *testing.T, runID string) *sandbox.Instance {
	t.Helper()
	return &sandbox.Instance{
		RunID:      runID,
		RunDir:     t.TempDir(),
		Workdir:    t.TempDir(),
		BaseCommit: "0000000000000000000000000000000000000000",
	}
}

func registryWith(t *testing.T, gen eval.Generator) *eval.Registry {
	t.Helper()
	reg := eval.NewRegistry()
	if err := reg.Register(gen); err != nil {
		t.Fatalf("register generator: %v", err)
	}
	return reg
}

// unitHarness wires a harness from scripted seams. The generator is registered
// under go; verifiers are keyed by verifier language.
func unitHarness(t *testing.T, sb sandbox.Sandbox, run runner.Runner, ver *fakeVerifier, gen fakeGenerator) *Harness {
	t.Helper()
	h, err := New(Config{
		Generators: registryWith(t, gen),
		Sandbox:    sb,
		Runner:     run,
		Verifiers:  map[eval.Language]verifier.Verifier{ver.Language(): ver},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeGoModule lays down a tiny, already-passing Go module the real verifier
// can `go build ./foo/...` and `go test -race ./foo/...` against.
func writeGoModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module evalfixture\n\ngo 1.24\n")
	foo := filepath.Join(dir, "foo")
	if err := os.MkdirAll(foo, 0o755); err != nil {
		t.Fatalf("mkdir foo: %v", err)
	}
	writeFile(t, filepath.Join(foo, "foo.go"),
		"package foo\n\n// Bar returns the answer.\nfunc Bar() int { return 42 }\n")
	writeFile(t, filepath.Join(foo, "foo_test.go"),
		"package foo\n\nimport \"testing\"\n\nfunc TestBar(t *testing.T) {\n\tif Bar() != 42 {\n\t\tt.Fatalf(\"Bar() = %d, want 42\", Bar())\n\t}\n}\n")
	return dir
}

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain not available: %v", err)
	}
}

func verifierEntry(t *testing.T, rec scoring.IterationRecord, typ string) scoring.VerifierRecord {
	t.Helper()
	for _, v := range rec.Verifiers {
		if v.Type == typ {
			return v
		}
	}
	t.Fatalf("no %q verifier entry in record: %+v", typ, rec.Verifiers)
	return scoring.VerifierRecord{}
}

// ---- New validation ----------------------------------------------------------

func TestNew_Validation(t *testing.T) {
	reg := registryWith(t, fakeGenerator{lang: eval.LanguageGo, spec: validSpec()})
	sb := &fakeSandbox{}
	run := &runner.FakeRunner{}
	vers := map[eval.Language]verifier.Verifier{eval.LanguageGo: &fakeVerifier{lang: eval.LanguageGo}}

	tests := []struct {
		name string
		cfg  Config
	}{
		{"nil generators", Config{Sandbox: sb, Runner: run, Verifiers: vers}},
		{"nil sandbox", Config{Generators: reg, Runner: run, Verifiers: vers}},
		{"nil runner", Config{Generators: reg, Sandbox: sb, Verifiers: vers}},
		{"no verifiers", Config{Generators: reg, Sandbox: sb, Runner: run}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); err == nil {
				t.Fatalf("New(%s): expected error", tc.name)
			}
		})
	}

	if _, err := New(Config{Generators: reg, Sandbox: sb, Runner: run, Verifiers: vers}); err != nil {
		t.Fatalf("New(valid): unexpected error %v", err)
	}
}

// ---- integration: real generator + real verifier + FakeRunner ---------------

func TestRun_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test shells out to the go toolchain")
	}
	requireGo(t)

	// Fixture KG: a single Go symbol at foo/foo.go so the generated TaskSpec
	// targets ./foo/... — the package the fixture module actually contains.
	reader := newFakeReader()
	reader.addGoFn("foo.Bar", "foo/foo.go", 1, 4)
	q, err := kgquery.New(reader)
	if err != nil {
		t.Fatalf("kgquery.New: %v", err)
	}
	reg := eval.NewRegistry()
	if err := gencore.Register(reg, q, gogen.Profile); err != nil {
		t.Fatalf("gencore.Register: %v", err)
	}

	inst := &sandbox.Instance{
		RunID:      "eval-int-abc123",
		RunDir:     t.TempDir(),
		Workdir:    writeGoModule(t),
		BaseCommit: "0000000000000000000000000000000000000000",
	}
	fakeRun := &runner.FakeRunner{
		Result: runner.Result{
			ExitCode:  0,
			Telemetry: runner.AgentTelemetry{Harness: "fake-harness", Model: "test-model"},
		},
	}
	h, err := New(Config{
		Generators: reg,
		Sandbox:    &fakeSandbox{inst: inst},
		Runner:     fakeRun,
		Verifiers:  map[eval.Language]verifier.Verifier{eval.LanguageGo: goverifier.New()},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := h.Run(context.Background(), Options{Language: eval.LanguageGo})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Per-stage assertions live in helpers to keep this test within the
	// cognitive-complexity gate (S3776).
	assertIntegrationGenerated(t, got)
	assertIntegrationRan(t, fakeRun, got, inst)
	assertIntegrationVerified(t, got)
	assertIntegrationScored(t, got, inst)
}

// assertIntegrationGenerated checks stage 1 (generate): the spec targets Go and
// carries the build command for the fixture package.
func assertIntegrationGenerated(t *testing.T, got EvalRun) {
	t.Helper()
	if got.Spec == nil || got.Spec.Language != eval.LanguageGo {
		t.Fatalf("spec: %+v", got.Spec)
	}
	wantBuild := []string{"go", "build", "./foo/..."}
	if !reflect.DeepEqual(got.Spec.Verification.BuildCmd, wantBuild) {
		t.Fatalf("build cmd = %v, want %v", got.Spec.Verification.BuildCmd, wantBuild)
	}
}

// assertIntegrationRan checks stage 3 (run): the harness passed the generated
// spec and the provisioned instance straight through to the runner.
func assertIntegrationRan(t *testing.T, fakeRun *runner.FakeRunner, got EvalRun, inst *sandbox.Instance) {
	t.Helper()
	if fakeRun.Calls != 1 {
		t.Fatalf("runner calls = %d, want 1", fakeRun.Calls)
	}
	if fakeRun.LastSpec != got.Spec {
		t.Fatalf("runner got a different spec than the generator produced")
	}
	if fakeRun.LastInstance != inst {
		t.Fatalf("runner got a different instance than the sandbox provisioned")
	}
	if got.Run.ExitCode != 0 {
		t.Fatalf("run exit code = %d, want 0", got.Run.ExitCode)
	}
}

// assertIntegrationVerified checks stage 4 (verify): the real go verifier built
// and tested the fixture.
func assertIntegrationVerified(t *testing.T, got EvalRun) {
	t.Helper()
	if got.Verify == nil || !got.Verify.Passed {
		t.Fatalf("verify result = %+v, want passed", got.Verify)
	}
	if got.Verify.Phase != verifier.PhaseTest {
		t.Fatalf("verify phase = %q, want %q", got.Verify.Phase, verifier.PhaseTest)
	}
}

// assertIntegrationScored checks stage 5 (score): run identity threaded from the
// instance, the persisted sidecars, and telemetry mapped into the record.
func assertIntegrationScored(t *testing.T, got EvalRun, inst *sandbox.Instance) {
	t.Helper()
	if got.RunID != inst.RunID || got.RunDir != inst.RunDir || got.BaseCommit != inst.BaseCommit {
		t.Fatalf("run identity not threaded from instance: %+v", got)
	}
	mustExist(t, got.Score.RecordPath)
	mustExist(t, got.Score.ScorePath)
	if build := verifierEntry(t, got.Score.Record, "build"); build.Status != "pass" {
		t.Fatalf("build verifier status = %q, want pass", build.Status)
	}
	if test := verifierEntry(t, got.Score.Record, "test"); test.Status != "pass" {
		t.Fatalf("test verifier status = %q, want pass", test.Status)
	}
	if got.Score.Record.Agent.Harness != "fake-harness" || got.Score.Record.Agent.Model != "test-model" {
		t.Fatalf("agent telemetry not recorded: %+v", got.Score.Record.Agent)
	}
}

// ---- unit: stage failure + cancellation paths --------------------------------

func TestRun_ContextCanceled(t *testing.T) {
	h := unitHarness(t,
		&fakeSandbox{inst: fakeInstance(t, "r1")},
		&runner.FakeRunner{},
		&fakeVerifier{lang: eval.LanguageGo, res: &verifier.VerifyResult{Passed: true, Phase: verifier.PhaseTest}},
		fakeGenerator{lang: eval.LanguageGo, spec: validSpec()},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.Run(ctx, Options{Language: eval.LanguageGo})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(canceled) err = %v, want context.Canceled", err)
	}
}

func TestRun_NoGenerator(t *testing.T) {
	h := unitHarness(t,
		&fakeSandbox{inst: fakeInstance(t, "r1")},
		&runner.FakeRunner{},
		&fakeVerifier{lang: eval.LanguageGo},
		fakeGenerator{lang: eval.LanguageGo, spec: validSpec()},
	)
	// A language with no registered generator fails at the generate stage.
	if _, err := h.Run(context.Background(), Options{Language: eval.LanguagePython}); err == nil {
		t.Fatal("Run(no generator): expected error")
	}
}

func TestRun_GenerateError(t *testing.T) {
	h := unitHarness(t,
		&fakeSandbox{inst: fakeInstance(t, "r1")},
		&runner.FakeRunner{},
		&fakeVerifier{lang: eval.LanguageGo},
		fakeGenerator{lang: eval.LanguageGo, err: errors.New("kg unavailable")},
	)
	_, err := h.Run(context.Background(), Options{Language: eval.LanguageGo})
	if err == nil {
		t.Fatal("Run(generate error): expected error")
	}
}

func TestRun_ProvisionError(t *testing.T) {
	sb := &fakeSandbox{err: errors.New("no worktree")}
	h := unitHarness(t, sb,
		&runner.FakeRunner{},
		&fakeVerifier{lang: eval.LanguageGo},
		fakeGenerator{lang: eval.LanguageGo, spec: validSpec()},
	)
	if _, err := h.Run(context.Background(), Options{Language: eval.LanguageGo}); err == nil {
		t.Fatal("Run(provision error): expected error")
	}
	if sb.provisions != 1 {
		t.Fatalf("provisions = %d, want 1", sb.provisions)
	}
}

func TestRun_RunnerLaunchError(t *testing.T) {
	ver := &fakeVerifier{lang: eval.LanguageGo}
	h := unitHarness(t,
		&fakeSandbox{inst: fakeInstance(t, "r1")},
		&runner.FakeRunner{Err: errors.New("cannot launch agent")},
		ver,
		fakeGenerator{lang: eval.LanguageGo, spec: validSpec()},
	)
	// A launch failure (non-nil runner error) aborts before verification.
	if _, err := h.Run(context.Background(), Options{Language: eval.LanguageGo}); err == nil {
		t.Fatal("Run(launch error): expected error")
	}
	if ver.calls != 0 {
		t.Fatalf("verifier should not run after a launch failure, calls = %d", ver.calls)
	}
}

func TestRun_NoVerifier(t *testing.T) {
	// Generator returns a TypeScript spec, but only a Go verifier is wired.
	spec := validSpec()
	spec.Language = eval.LanguageTypeScript
	h := unitHarness(t,
		&fakeSandbox{inst: fakeInstance(t, "r1")},
		&runner.FakeRunner{},
		&fakeVerifier{lang: eval.LanguageGo},
		fakeGenerator{lang: eval.LanguageGo, spec: spec},
	)
	if _, err := h.Run(context.Background(), Options{Language: eval.LanguageGo}); err == nil {
		t.Fatal("Run(no verifier): expected error")
	}
}

func TestRun_VerifyStepError(t *testing.T) {
	// A VerifyError (a step that could not start) is an infra failure.
	ver := &fakeVerifier{
		lang: eval.LanguageGo,
		err:  &verifier.VerifyError{Phase: verifier.PhaseTest, Cause: errors.New("go: not found")},
	}
	h := unitHarness(t,
		&fakeSandbox{inst: fakeInstance(t, "r1")},
		&runner.FakeRunner{},
		ver,
		fakeGenerator{lang: eval.LanguageGo, spec: validSpec()},
	)
	if _, err := h.Run(context.Background(), Options{Language: eval.LanguageGo}); err == nil {
		t.Fatal("Run(verify step error): expected error")
	}
}

func TestRun_ScoreError(t *testing.T) {
	// An instance with an empty RunID makes the scoring bridge reject the run.
	inst := fakeInstance(t, "")
	h := unitHarness(t,
		&fakeSandbox{inst: inst},
		&runner.FakeRunner{},
		&fakeVerifier{lang: eval.LanguageGo, res: &verifier.VerifyResult{Passed: true, Phase: verifier.PhaseTest}},
		fakeGenerator{lang: eval.LanguageGo, spec: validSpec()},
	)
	_, err := h.Run(context.Background(), Options{Language: eval.LanguageGo})
	if !errors.Is(err, scoringbridge.ErrEmptyRunID) {
		t.Fatalf("Run(score error) err = %v, want ErrEmptyRunID", err)
	}
}

// TestRun_VerifyFailureScored proves a failing build/test is data, not an
// error: the run completes, the verify result carries Passed=false, and the
// score sidecar is still written with the test recorded as a failure.
func TestRun_VerifyFailureScored(t *testing.T) {
	inst := fakeInstance(t, "r-fail")
	ver := &fakeVerifier{
		lang: eval.LanguageGo,
		res:  &verifier.VerifyResult{Passed: false, Phase: verifier.PhaseTest, ExitCode: 1},
	}
	fakeRun := &runner.FakeRunner{Result: runner.Result{ExitCode: 7}}
	h := unitHarness(t, &fakeSandbox{inst: inst}, fakeRun, ver,
		fakeGenerator{lang: eval.LanguageGo, spec: validSpec()})

	got, err := h.Run(context.Background(), Options{Language: eval.LanguageGo})
	if err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}
	if got.Verify.Passed {
		t.Fatal("verify should report Passed=false")
	}
	// The runner passed the spec + instance through to the verifier workdir.
	if ver.lastWorkdir != inst.Workdir {
		t.Fatalf("verifier workdir = %q, want %q", ver.lastWorkdir, inst.Workdir)
	}
	mustExist(t, got.Score.RecordPath)
	if test := verifierEntry(t, got.Score.Record, "test"); test.Status != "fail" {
		t.Fatalf("test verifier status = %q, want fail", test.Status)
	}
}

// ---- unit: telemetry + verify mapping ---------------------------------------

func TestMapTelemetry(t *testing.T) {
	tokens := &scoring.TokenUsage{InputTokens: 10, CacheHitRate: 0.5}
	got := mapTelemetry(runner.AgentTelemetry{
		SessionID: "sess-1", Harness: "claude-code", Model: "opus", Retries: 2, Tokens: tokens,
	})
	want := scoringbridge.AgentTelemetry{
		SessionID: "sess-1", Harness: "claude-code", Model: "opus", Retries: 2, Tokens: tokens,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapTelemetry = %+v, want %+v", got, want)
	}
}

func TestMapVerify(t *testing.T) {
	withBuild := &eval.TaskSpec{Verification: eval.Verification{
		BuildCmd: []string{"go", "build", "./..."}, TestCmd: []string{"go", "test", "./..."},
	}}
	noBuild := &eval.TaskSpec{Verification: eval.Verification{TestCmd: []string{"go", "test", "./..."}}}

	tests := []struct {
		name   string
		spec   *eval.TaskSpec
		result *verifier.VerifyResult
		want   scoringbridge.VerifyResult
	}{
		{
			name:   "build failed short-circuits test",
			spec:   withBuild,
			result: &verifier.VerifyResult{Phase: verifier.PhaseBuild, Passed: false},
			want:   scoringbridge.VerifyResult{BuildRan: true, BuildPassed: false},
		},
		{
			name:   "build passed then test passed",
			spec:   withBuild,
			result: &verifier.VerifyResult{Phase: verifier.PhaseTest, Passed: true},
			want:   scoringbridge.VerifyResult{BuildRan: true, BuildPassed: true, TestRan: true, TestPassed: true},
		},
		{
			name:   "build passed then test failed",
			spec:   withBuild,
			result: &verifier.VerifyResult{Phase: verifier.PhaseTest, Passed: false},
			want:   scoringbridge.VerifyResult{BuildRan: true, BuildPassed: true, TestRan: true, TestPassed: false},
		},
		{
			name:   "no build command, test passed",
			spec:   noBuild,
			result: &verifier.VerifyResult{Phase: verifier.PhaseTest, Passed: true},
			want:   scoringbridge.VerifyResult{BuildRan: false, TestRan: true, TestPassed: true},
		},
		{
			name:   "unset phase yields empty result",
			spec:   noBuild,
			result: &verifier.VerifyResult{},
			want:   scoringbridge.VerifyResult{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapVerify(tc.spec, tc.result)
			if got != tc.want {
				t.Fatalf("mapVerify = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// ---- cleanup contract (spec R8) ---------------------------------------------

func TestNew_NilVerifierEntry(t *testing.T) {
	// A present-but-nil verifier value passes the length check but would panic
	// at verify time, so New must reject it and name the offending language.
	_, err := New(Config{
		Generators: registryWith(t, fakeGenerator{lang: eval.LanguageGo, spec: validSpec()}),
		Sandbox:    &fakeSandbox{},
		Runner:     &runner.FakeRunner{},
		Verifiers:  map[eval.Language]verifier.Verifier{eval.LanguageGo: nil},
	})
	if err == nil {
		t.Fatal("New(nil verifier entry): expected error")
	}
}

func TestFinalizeCleanup(t *testing.T) {
	runErr := errors.New("stage failed")
	cleanupErr := errors.New("worktree removal failed")

	tests := []struct {
		name       string
		cleanupErr error
		runErr     error
		wantNil    bool
		wantIs     error
	}{
		{"both nil", nil, nil, true, nil},
		{"run error only wins", nil, runErr, false, runErr},
		{"cleanup error surfaced", cleanupErr, nil, false, cleanupErr},
		{"run error not masked by cleanup", cleanupErr, runErr, false, runErr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := finalizeCleanup(tc.cleanupErr, tc.runErr)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("finalizeCleanup = %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, tc.wantIs) {
				t.Fatalf("finalizeCleanup = %v, want to wrap %v", got, tc.wantIs)
			}
		})
	}
}

// TestRun_NonZeroExitScoredByVerify pins the corrected failure-policy contract:
// a non-zero agent exit is captured on EvalRun but is NOT a scoring input — a
// run that exits non-zero yet verifies clean still scores its test as a pass.
func TestRun_NonZeroExitScoredByVerify(t *testing.T) {
	inst := fakeInstance(t, "r-exit")
	ver := &fakeVerifier{
		lang: eval.LanguageGo,
		res:  &verifier.VerifyResult{Passed: true, Phase: verifier.PhaseTest},
	}
	fakeRun := &runner.FakeRunner{Result: runner.Result{ExitCode: 3}}
	h := unitHarness(t, &fakeSandbox{inst: inst}, fakeRun, ver,
		fakeGenerator{lang: eval.LanguageGo, spec: validSpec()})

	got, err := h.Run(context.Background(), Options{Language: eval.LanguageGo})
	if err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}
	if got.Run.ExitCode != 3 {
		t.Fatalf("run exit code = %d, want 3 (captured on EvalRun)", got.Run.ExitCode)
	}
	if test := verifierEntry(t, got.Score.Record, "test"); test.Status != "pass" {
		t.Fatalf("test verifier status = %q, want pass (exit code must not drag the score)", test.Status)
	}
}
