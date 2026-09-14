package graphstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// discoverableRelease returns a repository root from which the pinned
// code-review-graph release can be discovered, or skips.
//
// This is an integration test against the retained bridge, which is the whole
// point of the bridge leg: the reason bridge-routed tools are trustworthy is
// that they are answered by the release itself, and that claim is only
// verified by running it. CI installs the release for the behaviour gate, so
// the test runs there; a developer machine without it skips with a reason
// rather than passing vacuously.
func discoverableRelease(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// The release validates repo_root and refuses a directory with no VCS or
	// graph marker, so the fixture has to look like a project root.
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Discover the release from THIS repository, not from PATH: CI installs it
	// into the repo-root .venv without putting .venv/bin on PATH, so a
	// PATH-only lookup would skip in exactly the environment the bridge is
	// meant to be exercised in. DiscoverCRGBin is the probe production uses.
	_, testFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(testFile), "..", "..")
	binary, err := DiscoverCRGBin(repoRoot)
	if err != nil {
		t.Skipf("code-review-graph %s is not installed: %v", crgrelease.Version, err)
	}
	// Link the release's WHOLE virtualenv in as the repository's .venv, the
	// layout discovery expects. Linking the individual executables instead
	// would break the interpreter's prefix resolution — it locates its
	// site-packages from pyvenv.cfg beside the bin directory — and the tests
	// would skip for a reason that has nothing to do with the contract.
	venv, err := filepath.Abs(filepath.Join(filepath.Dir(binary), ".."))
	if err != nil {
		t.Skipf("cannot resolve the release's virtualenv: %v", err)
	}
	if _, err := os.Stat(filepath.Join(venv, "pyvenv.cfg")); err != nil {
		t.Skipf("%s is not a virtualenv layout: %v", venv, err)
	}
	if err := os.Symlink(venv, filepath.Join(root, ".venv")); err != nil {
		t.Skipf("cannot stage the release into a temp repo: %v", err)
	}
	return root
}

// A bridge-routed call must return the RELEASE's own envelope — structured
// content plus the matching text block — not a re-derivation of it.
func TestCRGToolBridgeReturnsTheReleaseEnvelope(t *testing.T) {
	root := discoverableRelease(t)
	t.Setenv("HOME", t.TempDir())

	bridge := NewCRGToolBridge(root)
	requireUsableRelease(t, bridge)

	tool, ok := crgrelease.Lookup("list_graph_stats_tool")
	if !ok {
		t.Fatal("list_graph_stats_tool is not published")
	}
	args, toolErr := tool.Bind(nil)
	if toolErr != nil {
		t.Fatalf("bind: %v", toolErr)
	}
	result, err := bridge.CallTool(tool.Name, args.Raw())
	if err != nil {
		t.Fatalf("bridge call: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	if len(result.StructuredContent) == 0 {
		t.Fatal("bridge returned no structured content")
	}
	var payload map[string]any
	if err := json.Unmarshal(result.StructuredContent, &payload); err != nil {
		t.Fatalf("structured content is not an object: %v", err)
	}
	if _, ok := payload["status"]; !ok {
		t.Errorf("release payload has no status field: %v", payload)
	}
	if len(result.Content) == 0 || result.Content[0].Type != "text" {
		t.Fatalf("bridge returned no text block: %#v", result.Content)
	}
}

// The release's own validation must reach the caller unchanged when a call is
// bridge-routed, so a client sees one error vocabulary regardless of which
// backend answered.
func TestCRGToolBridgeSurfacesReleaseValidationErrors(t *testing.T) {
	root := discoverableRelease(t)
	t.Setenv("HOME", t.TempDir())

	bridge := NewCRGToolBridge(root)
	requireUsableRelease(t, bridge)

	result, err := bridge.CallTool("list_graph_stats_tool", map[string]any{"bogus": 1})
	if err != nil {
		t.Fatalf("bridge call: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected the release to reject an unknown argument")
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Unexpected keyword argument") {
		t.Errorf("release validation text not surfaced: %q", text)
	}
	// The same message our own validator produces — this is the check that
	// the two legs of the router speak one error vocabulary.
	tool, _ := crgrelease.Lookup("list_graph_stats_tool")
	_, toolErr := tool.Bind(json.RawMessage(`{"bogus":1}`))
	if toolErr == nil {
		t.Fatal("native validation accepted an unknown argument")
	}
	if toolErr.Message != text {
		t.Errorf("native and bridge validation differ\nnative: %q\nbridge: %q",
			toolErr.Message, text)
	}
}

// An absent release must be reported as an explicit, actionable condition.
func TestCRGToolBridgeUnavailableIsExplicit(t *testing.T) {
	t.Setenv("PATH", "")
	bridge := NewCRGToolBridge(t.TempDir())
	if bridge.Available() {
		t.Fatal("expected the bridge to be unavailable with an empty PATH")
	}
	if bridge.Unavailable() == "" {
		t.Error("an unavailable bridge must explain why")
	}
	if _, err := bridge.CallTool("list_graph_stats_tool", nil); err == nil {
		t.Error("expected an error from an unavailable bridge")
	}
}

// requireUsableRelease skips when the interpreter beside the discovered
// executable cannot import the release.
//
// The distinction matters: a discoverable binary whose sibling interpreter
// lacks the package is an ENVIRONMENT gap (a PATH shim, a different venv),
// not a contract failure, and failing on it would make the suite red on
// developer machines for a reason the code cannot fix. Anything else — a
// crash, a malformed response, a wrong envelope — is a real failure and is
// reported as one.
func requireUsableRelease(t *testing.T, bridge *CRGToolBridge) {
	t.Helper()
	if !bridge.Available() {
		t.Skipf("code-review-graph %s is not usable here: %s",
			crgrelease.Version, bridge.Unavailable())
	}
	if _, err := bridge.CallTool("list_repos_tool", map[string]any{}); err != nil {
		if strings.Contains(err.Error(), "code-review-graph import failed") {
			t.Skipf("the interpreter beside the discovered executable cannot import "+
				"code-review-graph %s: %v", crgrelease.Version, err)
		}
		t.Fatalf("probing the release failed: %v", err)
	}
}
