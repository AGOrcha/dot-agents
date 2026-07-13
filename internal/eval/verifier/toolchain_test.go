package verifier

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
)

// lookPathFrom returns a lookPathFn that resolves only the names in ok and
// reports exec.ErrNotFound for everything else.
func lookPathFrom(t *testing.T, ok ...string) lookPathFn {
	t.Helper()
	set := map[string]bool{}
	for _, n := range ok {
		set[n] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

// pythonSpec returns a minimal Python TaskSpec that exercises both the build
// (py_compile) and test (pytest) steps, each invoked as bare "python".
func pythonSpec() *eval.TaskSpec {
	s := minimalSpec()
	s.Language = eval.LanguagePython
	s.Verification.BuildCmd = []string{"python", "-m", "py_compile", "pkg/x.py"}
	s.Verification.TestCmd = []string{"python", "-m", "pytest", "-v", "pkg"}
	return s
}

// ---- ToolchainError -----------------------------------------------------------

func TestToolchainError_Error(t *testing.T) {
	e := &ToolchainError{Language: eval.LanguagePython, Binary: "python", Tried: []string{"python3", "python"}}
	msg := e.Error()
	for _, want := range []string{"python", "toolchain unavailable", "python3", "install it"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
}

// ---- toolchainCandidates ------------------------------------------------------

func TestToolchainCandidates(t *testing.T) {
	cases := map[string][][]string{
		"python":  {{"python3"}, {"python"}},
		"python3": {{"python3"}, {"python"}},
		"tsc":     {{"tsc"}, {"npx", "tsc"}},
		"go":      {{"go"}},
		"node":    {{"node"}},
	}
	for bin, want := range cases {
		if got := toolchainCandidates(bin); !reflect.DeepEqual(got, want) {
			t.Errorf("toolchainCandidates(%q) = %v, want %v", bin, got, want)
		}
	}
}

// ---- resolveToolchain ---------------------------------------------------------

type resolveCase struct {
	name string
	lang eval.Language
	cmd  []string
	ok   []string
	want []string
	err  bool
}

func assertResolve(t *testing.T, tc resolveCase) {
	t.Helper()
	got, err := resolveToolchain(tc.lang, tc.cmd, lookPathFrom(t, tc.ok...))
	if tc.err {
		var te *ToolchainError
		if !errors.As(err, &te) {
			t.Fatalf("%s: want *ToolchainError, got %v", tc.name, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", tc.name, err)
	}
	if !reflect.DeepEqual(got, tc.want) {
		t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
	}
}

func TestResolveToolchain(t *testing.T) {
	cases := []resolveCase{
		{"go direct", eval.LanguageGo, []string{"go", "test", "./..."}, []string{"go"}, []string{"go", "test", "./..."}, false},
		{"python3 preferred", eval.LanguagePython, []string{"python", "-m", "pytest"}, []string{"python3", "python"}, []string{"python3", "-m", "pytest"}, false},
		{"python fallback", eval.LanguagePython, []string{"python", "-m", "pytest"}, []string{"python"}, []string{"python", "-m", "pytest"}, false},
		{"npx tsc fallback", eval.LanguageTypeScript, []string{"tsc", "--noEmit"}, []string{"npx"}, []string{"npx", "tsc", "--noEmit"}, false},
		{"none resolves", eval.LanguagePython, []string{"python", "-m", "pytest"}, nil, nil, true},
		{"empty cmd", eval.LanguageGo, nil, nil, nil, false},
	}
	for _, tc := range cases {
		assertResolve(t, tc)
	}
}

// ---- Verify preflight: NewBase seam -------------------------------------------

func TestNewBase_SetsLookPath(t *testing.T) {
	if NewBase(eval.LanguageGo).lookPath == nil {
		t.Fatal("NewBase must set lookPath to a non-nil resolver")
	}
}

// ---- Verify preflight: toolchain missing (test-only spec) ---------------------

func TestVerify_ToolchainMissingNoBuild(t *testing.T) {
	b := newEngine()
	b.lookPath = lookPathFrom(t) // resolves nothing
	called := false
	b.runCmd = func(context.Context, string, []string, []string) (string, string, int, time.Duration, error) {
		called = true
		return "", "", 0, 0, nil
	}
	_, err := b.Verify(context.Background(), minimalSpec(), t.TempDir(), nil)
	var te *ToolchainError
	if !errors.As(err, &te) {
		t.Fatalf("want *ToolchainError, got %T: %v", err, err)
	}
	if te.Language != eval.LanguageGo || called {
		t.Fatalf("lang=%q called=%v; want go / no exec", te.Language, called)
	}
}

// ---- Verify preflight: toolchain missing on the build step --------------------

func TestVerify_ToolchainMissingBuild(t *testing.T) {
	b := newEngine()
	b.lookPath = lookPathFrom(t) // resolves nothing
	spec := minimalSpec()
	spec.Verification.BuildCmd = []string{"go", "build", "./..."}
	_, err := b.Verify(context.Background(), spec, t.TempDir(), nil)
	var te *ToolchainError
	if !errors.As(err, &te) {
		t.Fatalf("want *ToolchainError from build preflight, got %T: %v", err, err)
	}
}

// ---- Verify preflight: resolved binary flows into the executed command --------

func TestVerify_ResolvedBinaryFlows(t *testing.T) {
	b := NewBase(eval.LanguagePython)
	b.lookPath = lookPathFrom(t, "python3") // only python3, not python
	var gotHeads []string
	b.runCmd = func(_ context.Context, _ string, _ []string, cmd []string) (string, string, int, time.Duration, error) {
		gotHeads = append(gotHeads, cmd[0])
		return "", "", 0, time.Millisecond, nil
	}
	if _, err := b.Verify(context.Background(), pythonSpec(), t.TempDir(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"python3", "python3"}; !reflect.DeepEqual(gotHeads, want) {
		t.Fatalf("executed heads = %v, want %v (python3 preferred over python)", gotHeads, want)
	}
}
