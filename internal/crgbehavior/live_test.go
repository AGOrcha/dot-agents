package crgbehavior

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// fakeCRGRepo stages a repository whose .venv holds an executable that answers
// `--version` with the given text.
func fakeCRGRepo(t *testing.T, versionLine string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub bridge is a POSIX shell script")
	}
	root := t.TempDir()
	bin := filepath.Join(root, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("stage venv: %v", err)
	}
	script := "#!/bin/sh\necho '" + versionLine + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "code-review-graph"), []byte(script), 0o755); err != nil {
		t.Fatalf("stage bridge: %v", err)
	}
	return root
}

// A machine without the bridge cannot run the comparison. That is an
// environment fact — reported as unavailable, never as a divergence, and never
// as a pass.
func TestNewLiveBridgeReportsAMissingCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := NewLiveBridge(t.TempDir(), testRelease()); !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("NewLiveBridge error = %v, want ErrBridgeUnavailable", err)
	}
}

// The release is verified BEFORE anything is built: comparing against an
// unpinned build would burn a full graph build and certify nothing.
func TestNewLiveBridgeVerifiesTheReleaseBeforeBuilding(t *testing.T) {
	root := fakeCRGRepo(t, PackageName+" 2.3.8")
	bridge, err := NewLiveBridge(root, testRelease())
	if err != nil {
		t.Fatalf("NewLiveBridge: %v", err)
	}
	if bridge.Version() != PinnedVersion {
		t.Fatalf("version = %q, want %q", bridge.Version(), PinnedVersion)
	}

	off := fakeCRGRepo(t, PackageName+" 2.2.0")
	if _, err := NewLiveBridge(off, testRelease()); !errors.Is(err, ErrReleaseMismatch) {
		t.Fatalf("off-release bridge error = %v, want ErrReleaseMismatch", err)
	}

	mute := fakeCRGRepo(t, "")
	if _, err := NewLiveBridge(mute, testRelease()); err == nil {
		t.Fatal("a bridge that reports no version was accepted as a baseline")
	}
}

// An interpreter that cannot import the package is the same environment fact as
// "not installed", so it must not be reported as a behavior divergence — while
// any other query failure stays a hard error.
func TestClassifyQueryErrorSeparatesEnvironmentFromBehavior(t *testing.T) {
	env := errors.New("Traceback: ModuleNotFoundError: No module named 'code_review_graph'")
	if got := classifyQueryError(env); !errors.Is(got, ErrBridgeUnavailable) {
		t.Fatalf("classifyQueryError(env) = %v, want ErrBridgeUnavailable", got)
	}
	behavior := errors.New("sqlite3.OperationalError: database is locked")
	if got := classifyQueryError(behavior); errors.Is(got, ErrBridgeUnavailable) {
		t.Fatalf("a real query failure was excused as an unavailable bridge: %v", got)
	}
}

// The release answers its impact query in its own absolute id space; the gate
// keys both sides identically or compares nothing.
func TestNativeIDsNormalizeTheReleaseAnswer(t *testing.T) {
	norm := NewNormalizer(graphRoot)
	got, err := nativeIDs(norm, []graphstore.ImpactNode{
		{QualifiedName: "/abs/repo/pkg/a.go::Entry", FilePath: "/abs/repo/pkg/a.go"},
	})
	if err != nil {
		t.Fatalf("nativeIDs: %v", err)
	}
	if want := []string{repoFile("pkg/a.go", "Entry")}; !equalStrings(got, want) {
		t.Fatalf("nativeIDs = %v, want %v", got, want)
	}
	if _, err := nativeIDs(norm, []graphstore.ImpactNode{
		{QualifiedName: "/elsewhere/x.go::Stray", FilePath: "/elsewhere/x.go"},
	}); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("nativeIDs accepted a node outside the graph root: %v", err)
	}
}

// mlTReleaseShim is a POSIX stand-in for an INSTALLED pinned release whose
// sibling interpreter cannot import the package — the exact environment
// `interpreterFailures` names. Its entry point answers `--version` as the
// pinned release, so the release gate passes, and every real subcommand fails
// the way a missing module fails.
const mlTReleaseShim = `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo '` + PackageName + ` ` + PinnedVersion + `'
  exit 0
fi
echo "crgbehavior test shim: ModuleNotFoundError: No module named 'code_review_graph'" >&2
exit 1
`

// mlTStageShim writes the shim as BOTH the release entry point and the sibling
// interpreter the bridge's Python queries run through, so discovery resolves
// each of them from dir and no ambient interpreter is involved.
func mlTStageShim(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub release is a POSIX shell script")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("stage venv bin: %v", err)
	}
	for _, name := range []string{PackageName, "python3"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(mlTReleaseShim), 0o755); err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}
	}
}

// mlTShimBridge binds a LiveBridge to a root whose .venv holds that install.
func mlTShimBridge(t *testing.T) (*LiveBridge, string) {
	t.Helper()
	root := t.TempDir()
	mlTStageShim(t, filepath.Join(root, ".venv", "bin"))
	bridge, err := NewLiveBridge(root, testRelease())
	if err != nil {
		t.Fatalf("NewLiveBridge: %v", err)
	}
	return bridge, root
}

// A release entry point that cannot even answer `--version` is an environment
// fact, not a divergence: it is reported as unavailable, with the binary and
// the probe's own output preserved so an operator can act on it.
func TestMlTCLIVersionSurfacesAFailedProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub release is a POSIX shell script")
	}
	bin := filepath.Join(t.TempDir(), PackageName)
	script := "#!/bin/sh\necho 'cannot open shared object' >&2\nexit 3\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("stage failing bridge: %v", err)
	}
	version, err := cliVersion(bin)
	if !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("cliVersion error = %v, want ErrBridgeUnavailable", err)
	}
	if version != "" {
		t.Fatalf("cliVersion returned %q alongside a failure", version)
	}
	for _, want := range []string{bin, "--version failed", "cannot open shared object"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("cliVersion error %q does not carry %q", err, want)
		}
	}
}

// The lifecycle commands drive a Python release whose own failures are opaque.
// Each one must name the graph root it failed at: a corpus run drives ONE root
// per commit, and a bare "exit status 1" names none of them.
func TestMlTLifecycleCommandsNameTheGraphRootTheyFailedAt(t *testing.T) {
	bridge, root := mlTShimBridge(t)
	for _, tc := range []struct {
		verb string
		run  func() error
	}{
		{verb: "build", run: bridge.Build},
		{verb: "postprocess", run: bridge.Postprocess},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatalf("%s against an unusable interpreter reported success", tc.verb)
			}
			if !strings.Contains(err.Error(), tc.verb) || !strings.Contains(err.Error(), root) {
				t.Fatalf("%s error = %q, want it to name the operation and the graph root %q",
					tc.verb, err, root)
			}
		})
	}
}

// Open must look for the graph the release ITSELF wrote: under the bridge's own
// root, at the release's canonical database path. Before any build there is no
// graph, and that is an unavailable bridge naming the exact path that is absent.
func TestMlTOpenLooksForTheGraphAtTheReleasesCanonicalPath(t *testing.T) {
	bridge, root := mlTShimBridge(t)
	store, err := bridge.Open(testRelease())
	if store != nil {
		store.Close()
		t.Fatal("Open returned a store for a root the release never built")
	}
	if !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("Open error = %v, want ErrBridgeUnavailable", err)
	}
	if want := graphstore.CRGDBPath(root); !strings.Contains(err.Error(), want) {
		t.Fatalf("Open error = %q, want it to name %q", err, want)
	}
}

// An install whose interpreter cannot import the package is an UNAVAILABLE
// bridge end to end through the live query — never a behavior divergence, which
// would be a fabricated finding about the release.
func TestMlTImpactRadiusClassifiesAnUnusableInterpreter(t *testing.T) {
	bridge, _ := mlTShimBridge(t)
	impact, err := bridge.ImpactRadius(NewNormalizer(graphRoot),
		[]string{"pkg/a.go"}, DefaultDepth, DefaultMaxResults)
	if !errors.Is(err, ErrBridgeUnavailable) {
		t.Fatalf("ImpactRadius error = %v, want ErrBridgeUnavailable", err)
	}
	if len(impact.ChangedIDs) != 0 || len(impact.ImpactedIDs) != 0 || impact.Truncated {
		t.Fatalf("ImpactRadius returned %+v alongside a failure", impact)
	}
}

// Every signature the package documents as an unusable interpreter must
// classify, whatever casing the release's traceback happens to use — and a
// signature added to the list without a test would leave the gate guessing.
func TestMlTClassifyQueryErrorCoversEveryDocumentedSignature(t *testing.T) {
	byMarker := map[string]string{
		"ModuleNotFoundError":       "crg-py: ModuleNotFoundError: nothing importable here",
		"No module named":           "crg-py: ImportError: no module named code_review_graph",
		"command not found":         "crg query: /bin/sh: code-review-graph: command not found",
		"executable file not found": `exec: "code-review-graph": executable file not found in $PATH`,
		"no such file or directory": "fork/exec /repo/.venv/bin/python3: no such file or directory",
	}
	for _, marker := range interpreterFailures {
		msg, ok := byMarker[marker]
		if !ok {
			t.Fatalf("documented interpreter signature %q is never exercised", marker)
		}
		got := classifyQueryError(errors.New(msg))
		if !errors.Is(got, ErrBridgeUnavailable) {
			t.Fatalf("classifyQueryError(%q) = %v, want ErrBridgeUnavailable", msg, got)
		}
		if !strings.Contains(got.Error(), msg) {
			t.Fatalf("classifyQueryError dropped the original diagnosis: %v", got)
		}
	}
}

// A node the release reports from outside the graph root is refused rather than
// keyed into the comparison — including an UNRESOLVED bare target, whose
// qualified name carries no path and so cannot be validated by itself.
func TestMlTNativeIDsValidateTheFilePathOfABareTarget(t *testing.T) {
	norm := NewNormalizer(graphRoot)
	if _, err := nativeIDs(norm, []graphstore.ImpactNode{
		{QualifiedName: "append", FilePath: "/elsewhere/x.go"},
	}); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("nativeIDs error = %v, want ErrOutsideRoot for a node outside the graph root", err)
	}
}
