package eval

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/gen/gencore"
	"github.com/AGOrcha/dot-agents/internal/eval/runner"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	goverifier "github.com/AGOrcha/dot-agents/internal/eval/verifier/golang"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ---- fixture KG reader -------------------------------------------------------

// fakeReader is a minimal in-memory graphstore.CodeGraphReader — enough for
// kgquery + the real Go generator to synthesise a task over a fixture graph.
// It mirrors the fixture reader the harness and generator tests use.
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

// fixtureReader returns a reader carrying a single Go symbol at foo/foo.go so
// the generated TaskSpec targets ./foo/... — the package writeGoModule lays down.
func fixtureReader() *fakeReader {
	r := newFakeReader()
	r.addGoFn("foo.Bar", "foo/foo.go", 1, 4)
	return r
}

// fixtureOpenReader is an openReader seam that hands back the fixture reader with
// no closer (the reader owns no resources).
func fixtureOpenReader() (graphstore.CodeGraphReader, func() error, error) {
	return fixtureReader(), nil, nil
}

// ---- scripted sandbox / verifier --------------------------------------------

// fakeSandbox is a scripted sandbox.Sandbox that hands back a pre-built instance
// (its Cleanup is a no-op because the unexported cleanup hook is nil) and
// records PruneStale invocations so the OQ6 prune wiring is assertable.
type fakeSandbox struct {
	inst *sandbox.Instance
	err  error

	pruneCalls int
	pruned     []string
	pruneErr   error
}

func (s *fakeSandbox) Provision(context.Context, *evalcore.TaskSpec) (*sandbox.Instance, error) {
	return s.inst, s.err
}
func (s *fakeSandbox) PruneStale(context.Context) ([]string, error) {
	s.pruneCalls++
	return s.pruned, s.pruneErr
}

// fakeVerifier is a scripted goverifier.Verifier that returns a canned result
// without shelling out — used by the runEval error-path tests so they need no
// go toolchain.
type fakeVerifier struct {
	res *goverifier.VerifyResult
}

func (v *fakeVerifier) Language() evalcore.Language { return evalcore.LanguageGo }
func (v *fakeVerifier) Verify(context.Context, *evalcore.TaskSpec, string, []string) (*goverifier.VerifyResult, error) {
	return v.res, nil
}

// ---- go module fixture ------------------------------------------------------

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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain not available: %v", err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

// validSpec is a self-contained, valid Go TaskSpec for the fixed-generator and
// runEval error-path tests (no KG needed).
func validSpec() *evalcore.TaskSpec {
	return &evalcore.TaskSpec{
		TaskSpecVersion: evalcore.CurrentTaskSpecVersion,
		TaskID:          "kg-go-impl-fixture",
		Language:        evalcore.LanguageGo,
		Difficulty:      evalcore.DifficultyEasy,
		GeneratedFrom:   evalcore.GeneratedFrom{Kind: evalcore.KindKGTemplate},
		Prompt:          "implement the function",
		Verification: evalcore.Verification{
			BuildCmd:       []string{"go", "build", "./..."},
			TestCmd:        []string{"go", "test", "./..."},
			TimeoutSeconds: 60,
		},
	}
}

// ---- seam swap helpers ------------------------------------------------------

func swapOpenReader(t *testing.T, fn func() (graphstore.CodeGraphReader, func() error, error)) {
	t.Helper()
	prev := openReader
	openReader = fn
	t.Cleanup(func() { openReader = prev })
}

func swapSandbox(t *testing.T, fn func(sandbox.Config) (sandbox.Sandbox, error)) {
	t.Helper()
	prev := newSandbox
	newSandbox = fn
	t.Cleanup(func() { newSandbox = prev })
}

func swapRunner(t *testing.T, fn func(runner.Adapter) (runner.Runner, error)) {
	t.Helper()
	prev := newRunner
	newRunner = fn
	t.Cleanup(func() { newRunner = prev })
}

func swapLanguageProfiles(t *testing.T, profiles []gencore.Profile) {
	t.Helper()
	prev := languageProfiles
	languageProfiles = profiles
	t.Cleanup(func() { languageProfiles = prev })
}

func swapWarnOut(t *testing.T, w io.Writer) {
	t.Helper()
	prev := warnOut
	warnOut = w
	t.Cleanup(func() { warnOut = prev })
}

// failWriter is an io.Writer that always errors — used to exercise the
// confirmation-write error path.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errFixture }

// zeroCommit is a stable, all-zero base commit for fixture instances.
const zeroCommit = "0000000000000000000000000000000000000000"

// errFixture is a sentinel error for seam-failure tests.
var errFixture = errors.New("fixture failure")

// passingVerify is a canned "build+test passed" verifier result.
func passingVerify() *goverifier.VerifyResult {
	return &goverifier.VerifyResult{Passed: true, Phase: goverifier.PhaseTest, Duration: time.Millisecond}
}
