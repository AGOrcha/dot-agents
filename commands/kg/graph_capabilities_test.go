package kg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/codegraph"
	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/spf13/cobra"
)

// capabilitiesCmd builds the flag set `kg code-capabilities` is registered
// with: its own --repo plus the root command's persistent --json.
func capabilitiesCmd(repo string, asJSON bool) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("repo", repo, "")
	cmd.Flags().Bool("json", asJSON, "")
	return cmd
}

// capabilityRepo materializes empty files at the given repo-relative paths and
// empties PATH, so bridge availability is decided by the test rather than by
// whatever happens to be installed on the machine running it.
func capabilityRepo(t *testing.T, paths ...string) string {
	t.Helper()
	repo := t.TempDir()
	for _, rel := range paths {
		path := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", t.TempDir())
	return repo
}

// runCapabilities runs the command and returns its stdout, failing the test if
// the diagnostic itself errored.
func runCapabilities(t *testing.T, cmd *cobra.Command) []byte {
	t.Helper()
	return captureStdout(t, func() {
		if err := runKGCodeCapabilities(cmd, nil); err != nil {
			t.Errorf("runKGCodeCapabilities: %v", err)
		}
	})
}

// assertCapabilityEnvelopeShape pins the generic --json envelope: every
// documented key is present, and the release/backend/routing header fields
// describe a mixed-language repository with no bridge on PATH.
func assertCapabilityEnvelopeShape(t *testing.T, envelope map[string]any, out []byte) {
	t.Helper()
	for _, key := range []string{
		"crg_release", "backend", "routing", "reason",
		"bridge_available", "bridge_required",
		"tools", "native_tools", "bridge_tools",
		"root", "languages", "native_files", "bridge_files",
		"unsupported_files", "fully_native",
	} {
		if _, ok := envelope[key]; !ok {
			t.Fatalf("--json payload is missing %q: %s", key, out)
		}
	}

	if envelope["crg_release"] != crgrelease.Version {
		t.Fatalf("crg_release = %v, want %q", envelope["crg_release"], crgrelease.Version)
	}
	if envelope["backend"] == "" {
		t.Fatal("backend is empty")
	}
	if envelope["routing"] != codegraph.RoutingBridge || envelope["bridge_required"] != true {
		t.Fatalf("routing = %v bridge_required = %v, want bridge/true",
			envelope["routing"], envelope["bridge_required"])
	}
	if envelope["bridge_available"] != false {
		t.Fatal("bridge_available = true with an empty PATH and no .venv")
	}
	reason, _ := envelope["reason"].(string)
	for _, want := range []string{"python", "rust", "typescript"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("reason %q does not name the offending language %q", reason, want)
		}
	}
}

// assertMixedRepoCapabilityReport pins the typed half of the payload for the
// mixed-language fixture: the scanned root, the native/bridge/unsupported file
// totals, and the per-language rows behind them.
func assertMixedRepoCapabilityReport(t *testing.T, report codegraph.CapabilityReport, repo string) {
	t.Helper()
	if report.Root != repo || report.FullyNative {
		t.Fatalf("report = %+v, want root %q and fully_native false", report, repo)
	}
	if report.NativeFiles != 1 || report.BridgeFiles != 3 || report.UnsupportedFiles != 1 {
		t.Fatalf("totals = %d/%d/%d, want 1/3/1",
			report.NativeFiles, report.BridgeFiles, report.UnsupportedFiles)
	}
	rows := map[string]codegraph.SourceCapability{}
	for _, lang := range report.Languages {
		rows[lang.Language] = lang
	}
	if row := rows["go"]; row.Files != 1 || !row.Native || !row.UpstreamSupported {
		t.Fatalf("go row = %+v", row)
	}
	if row := rows["python"]; row.Files != 1 || row.Native || !row.UpstreamSupported {
		t.Fatalf("python row = %+v", row)
	}
	if row := rows[codegraph.LanguageUnindexed]; row.Files != 1 || row.UpstreamSupported {
		t.Fatalf("unindexed row = %+v", row)
	}
}

func TestRunKGCodeCapabilities_JSONReportsBridgeRoutingForMixedRepo(t *testing.T) {
	repo := capabilityRepo(t, "main.go", "tool.py", "web/app.ts", "lib.rs", "blob.bin")

	out := runCapabilities(t, capabilitiesCmd(repo, true))

	// The payload flattens CapabilityReport, so decoding it directly is itself
	// an assertion about the shape.
	var report codegraph.CapabilityReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("decode report half: %v\n%s", err, out)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("decode envelope half: %v\n%s", err, out)
	}

	assertCapabilityEnvelopeShape(t, envelope, out)
	assertMixedRepoCapabilityReport(t, report, repo)
}

// TestRunKGCodeCapabilities_MissingBridgeIsReportedNotFailed pins the exit
// contract: an accurate "this repo needs the bridge, and it is not installed"
// answer states the remediation and still succeeds.
func TestRunKGCodeCapabilities_MissingBridgeIsReportedNotFailed(t *testing.T) {
	repo := capabilityRepo(t, "main.go", "tool.py")

	text := string(runCapabilities(t, capabilitiesCmd(repo, false)))

	for _, want := range []string{
		"python",
		"uv pip install code-review-graph",
		crgrelease.Version,
		codegraph.RoutingBridge,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("human report is missing %q:\n%s", want, text)
		}
	}
}

func TestRunKGCodeCapabilities_GoOnlyRepoRoutesNative(t *testing.T) {
	// The vendored Python file is pruned by the ingester's own rules, so it
	// must not flip routing.
	repo := capabilityRepo(t, "main.go", "internal/x/x.go", "vendor/dep/setup.py")

	out := runCapabilities(t, capabilitiesCmd(repo, true))

	var report codegraph.CapabilityReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	if envelope["routing"] != codegraph.RoutingNative || envelope["bridge_required"] != false {
		t.Fatalf("routing = %v bridge_required = %v, want native/false",
			envelope["routing"], envelope["bridge_required"])
	}
	if !report.FullyNative || report.BridgeFiles != 0 || report.NativeFiles != 2 {
		t.Fatalf("report = %+v, want fully_native with 2 native and 0 bridge files", report)
	}
}

// decodedTool mirrors one `tools` entry of the --json payload.
type decodedTool struct {
	Tool    string `json:"tool"`
	Backend string `json:"backend"`
	Reason  string `json:"reason"`
}

// decodedToolRouting is the tool half of the payload.
type decodedToolRouting struct {
	Tools       []decodedTool `json:"tools"`
	NativeTools int           `json:"native_tools"`
	BridgeTools int           `json:"bridge_tools"`
}

// toolRoutingOf runs the command against repo and returns the tool half,
// indexed by tool name.
func toolRoutingOf(t *testing.T, repo string) (decodedToolRouting, map[string]decodedTool) {
	t.Helper()
	out := runCapabilities(t, capabilitiesCmd(repo, true))
	var got decodedToolRouting
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode tool routing: %v\n%s", err, out)
	}
	byName := make(map[string]decodedTool, len(got.Tools))
	for _, tool := range got.Tools {
		byName[tool.Tool] = tool
	}
	if len(byName) != len(got.Tools) {
		t.Fatalf("duplicate tool entries: %d rows, %d distinct names", len(got.Tools), len(byName))
	}
	return got, byName
}

// TestRunKGCodeCapabilities_ToolRoutingCoversEveryPublishedTool cross-checks
// the command's tool list against the GENERATED release surface, not against a
// hand-written list: a tool added upstream without a routing decision must
// surface here rather than default to a backend.
func TestRunKGCodeCapabilities_ToolRoutingCoversEveryPublishedTool(t *testing.T) {
	repo := capabilityRepo(t, "main.go", "internal/x/x.go")
	routing, byName := toolRoutingOf(t, repo)

	release, err := crgrelease.Metadata()
	if err != nil {
		t.Fatalf("crgrelease.Metadata: %v", err)
	}
	if len(routing.Tools) != release.ToolCount {
		t.Fatalf("reported %d tools, release publishes %d", len(routing.Tools), release.ToolCount)
	}
	for _, published := range crgrelease.Surface() {
		if _, ok := byName[published.Name]; !ok {
			t.Fatalf("published tool %q has no routing row", published.Name)
		}
	}
	if routing.NativeTools+routing.BridgeTools != len(routing.Tools) {
		t.Fatalf("native %d + bridge %d != %d tools",
			routing.NativeTools, routing.BridgeTools, len(routing.Tools))
	}
}

// assertNativelyServedToolRow pins a natively-implemented tool on a repository
// the native scanner fully covers: it must be answered natively and carry no
// fallback reason at all.
func assertNativelyServedToolRow(t *testing.T, capability crgrelease.ToolCapability, got decodedTool) {
	t.Helper()
	if got.Backend != string(crgrelease.BackendNative) {
		t.Fatalf("%s = %q on a fully native repository, want native (reason %q)",
			capability.Tool, got.Backend, got.Reason)
	}
	if got.Reason != "" {
		t.Fatalf("%s is native but carries reason %q", capability.Tool, got.Reason)
	}
}

// assertStandingBridgeOnlyToolRow pins a tool the native backend does not
// implement at all: it stays on the bridge and keeps its own
// upstream-capability reason rather than a source-coverage one.
func assertStandingBridgeOnlyToolRow(t *testing.T, capability crgrelease.ToolCapability, got decodedTool) {
	t.Helper()
	if got.Backend != string(crgrelease.BackendBridge) {
		t.Fatalf("bridge-only %s = %q on a Go-only repository", capability.Tool, got.Backend)
	}
	if got.Reason != capability.BridgeOnlyReason {
		t.Fatalf("bridge-only %s reason = %q, want its upstream-capability reason %q",
			capability.Tool, got.Reason, capability.BridgeOnlyReason)
	}
}

// assertGoOnlyToolRouting walks every published capability against the routing
// rows of a Go-only repository and returns how many were served natively and
// how many stayed bridge-only.
func assertGoOnlyToolRouting(
	t *testing.T,
	capabilities []crgrelease.ToolCapability,
	byName map[string]decodedTool,
) (nativeTools, bridgeTools int) {
	t.Helper()
	for _, capability := range capabilities {
		got, ok := byName[capability.Tool]
		if !ok {
			t.Fatalf("no routing row for %q", capability.Tool)
		}
		if capability.NativeBackend {
			nativeTools++
			assertNativelyServedToolRow(t, capability, got)
			continue
		}
		bridgeTools++
		assertStandingBridgeOnlyToolRow(t, capability, got)
	}
	return nativeTools, bridgeTools
}

// TestRunKGCodeCapabilities_GoOnlyRepoServesNativeToolsNatively pins both
// halves of the routing rule on a repository the native scanner fully covers:
// natively-implemented tools are answered natively, and the bridge-only tools
// still say bridge WITH their own upstream-capability reason — a fully native
// repository does not earn them.
func TestRunKGCodeCapabilities_GoOnlyRepoServesNativeToolsNatively(t *testing.T) {
	repo := capabilityRepo(t, "main.go", "internal/x/x.go")
	routing, byName := toolRoutingOf(t, repo)

	capabilities, err := crgrelease.Capabilities()
	if err != nil {
		t.Fatalf("crgrelease.Capabilities: %v", err)
	}
	wantNative, wantBridge := assertGoOnlyToolRouting(t, capabilities, byName)
	if wantNative == 0 || wantBridge == 0 {
		t.Fatalf("degenerate expectation: %d native / %d bridge-only tools", wantNative, wantBridge)
	}
	if routing.NativeTools != wantNative || routing.BridgeTools != wantBridge {
		t.Fatalf("counts = %d native / %d bridge, want %d / %d",
			routing.NativeTools, routing.BridgeTools, wantNative, wantBridge)
	}
}

// assertReasonNamesUncoveredLanguages pins the source-coverage fallback reason:
// it must name every language the native scanner could not index.
func assertReasonNamesUncoveredLanguages(t *testing.T, tool, reason string) {
	t.Helper()
	for _, language := range []string{"python", "rust", "typescript"} {
		if !strings.Contains(reason, language) {
			t.Fatalf("%s reason %q does not name the offending language %q",
				tool, reason, language)
		}
	}
}

// assertMixedRepoToolRouting walks every published capability against the
// routing rows of a mixed-language repository and returns how many
// natively-implemented tools were blocked by source coverage alone.
func assertMixedRepoToolRouting(
	t *testing.T,
	capabilities []crgrelease.ToolCapability,
	byName map[string]decodedTool,
) (sourceBlocked int) {
	t.Helper()
	for _, capability := range capabilities {
		got := byName[capability.Tool]
		if got.Backend != string(crgrelease.BackendBridge) {
			t.Fatalf("%s = %q in a mixed-language repository, want bridge",
				capability.Tool, got.Backend)
		}
		if !capability.NativeBackend {
			// A standing Phase-A limit keeps its own reason even here.
			if got.Reason != capability.BridgeOnlyReason {
				t.Fatalf("bridge-only %s reason = %q, want %q",
					capability.Tool, got.Reason, capability.BridgeOnlyReason)
			}
			continue
		}
		sourceBlocked++
		assertReasonNamesUncoveredLanguages(t, capability.Tool, got.Reason)
	}
	return sourceBlocked
}

// TestRunKGCodeCapabilities_MixedRepoRoutesEveryToolToBridge is the routing
// rule's second half: a natively-implemented tool still cannot be answered
// natively when the graph it would answer from is missing the repository's
// non-Go sources.
func TestRunKGCodeCapabilities_MixedRepoRoutesEveryToolToBridge(t *testing.T) {
	repo := capabilityRepo(t, "main.go", "tool.py", "web/app.ts", "lib.rs")
	routing, byName := toolRoutingOf(t, repo)

	capabilities, err := crgrelease.Capabilities()
	if err != nil {
		t.Fatalf("crgrelease.Capabilities: %v", err)
	}
	if routing.NativeTools != 0 || routing.BridgeTools != len(routing.Tools) {
		t.Fatalf("counts = %d native / %d bridge, want 0 / %d",
			routing.NativeTools, routing.BridgeTools, len(routing.Tools))
	}
	if assertMixedRepoToolRouting(t, capabilities, byName) == 0 {
		t.Fatal("no natively-implemented tool was source-blocked; the test proves nothing")
	}
}

// TestRunKGCodeCapabilities_HumanReportNamesStandingBridgeOnlyTools guards the
// honesty property of the rendered report: even on a fully native repository
// the reader must be told which tools the bridge still owns.
func TestRunKGCodeCapabilities_HumanReportNamesStandingBridgeOnlyTools(t *testing.T) {
	repo := capabilityRepo(t, "main.go")
	text := string(runCapabilities(t, capabilitiesCmd(repo, false)))

	if !strings.Contains(text, "Bridge-only:") {
		t.Fatalf("fully native report hides the standing bridge-only tools:\n%s", text)
	}
	capabilities, err := crgrelease.Capabilities()
	if err != nil {
		t.Fatalf("crgrelease.Capabilities: %v", err)
	}
	for _, capability := range capabilities {
		if capability.NativeBackend {
			continue
		}
		if !strings.Contains(text, capability.Tool) {
			t.Fatalf("bridge-only %s is not named in the report:\n%s", capability.Tool, text)
		}
	}
}

func TestRunKGCodeCapabilities_UnreadableRootFails(t *testing.T) {
	cmd := capabilitiesCmd(filepath.Join(t.TempDir(), "absent"), true)
	if err := runKGCodeCapabilities(cmd, nil); err == nil {
		t.Fatal("a root that cannot be scanned must exit non-zero")
	}
}
