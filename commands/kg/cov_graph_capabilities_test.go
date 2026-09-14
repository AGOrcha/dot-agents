package kg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/codegraph"
	"github.com/AGOrcha/dot-agents/internal/crgrelease"
)

// covCapInstallBridge materializes the file DiscoverCRGBin probes for inside
// the repository's own .venv, on every platform's candidate layout. The probe
// is a stat, so an empty file is a faithful "the bridge is installed here"
// without running anything.
func covCapInstallBridge(t *testing.T, repo string) {
	t.Helper()
	for _, sub := range []string{"bin", "Scripts"} {
		dir := filepath.Join(repo, ".venv", sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "code-review-graph"), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// covCapMissing returns the wanted substrings absent from text.
func covCapMissing(text string, want ...string) []string {
	var out []string
	for _, w := range want {
		if !strings.Contains(text, w) {
			out = append(out, w)
		}
	}
	return out
}

// covCapPresent returns the substrings that must NOT appear but do.
func covCapPresent(text string, unwanted ...string) []string {
	var out []string
	for _, w := range unwanted {
		if strings.Contains(text, w) {
			out = append(out, w)
		}
	}
	return out
}

// covCapLanguageLabels parses the rendered language table into
// language -> served-by label. The row format is
// `    <lang> <n> file(s)  <label> <extensions...>`.
func covCapLanguageLabels(text string) map[string]string {
	labels := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "file(s)" {
			continue
		}
		labels[fields[0]] = strings.Join(fields[3:], " ")
	}
	return labels
}

// covCapServedBy extracts just the served-by label, which is one or two words
// ("native", "bridge", "not indexed") followed by the extension list.
func covCapServedBy(row string) string {
	switch {
	case strings.HasPrefix(row, "not indexed"):
		return "not indexed"
	case strings.HasPrefix(row, "native"):
		return "native"
	case strings.HasPrefix(row, "bridge"):
		return "bridge"
	}
	return row
}

// TestRunKGCodeCapabilities_EmptyRepoFlagDiagnosesTheEnclosingRepository pins
// the flagless contract: `da kg code-capabilities` with no --repo diagnoses the
// repository the operator is standing in, not an empty or relative root.
func TestRunKGCodeCapabilities_EmptyRepoFlagDiagnosesTheEnclosingRepository(t *testing.T) {
	repo := capabilityRepo(t, "main.go", "internal/x/x.go", ".git/HEAD")
	t.Chdir(repo)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	out := runCapabilities(t, capabilitiesCmd("", true))

	var report codegraph.CapabilityReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	if report.Root != cwd {
		t.Fatalf("root = %q, want the enclosing repository %q", report.Root, cwd)
	}
	// The .git metadata directory is pruned exactly as the ingester prunes it,
	// so it must not appear as an unindexed file.
	if report.NativeFiles != 2 || report.UnsupportedFiles != 0 || !report.FullyNative {
		t.Fatalf("report = %+v, want 2 native files, no unindexed files, fully native", report)
	}
}

// TestRunKGCodeCapabilities_UnresolvableBackendFails: the diagnostic answers
// "which backend serves this repository", so a backend selection it cannot
// resolve is an unanswerable question, not a bridge verdict.
func TestRunKGCodeCapabilities_UnresolvableBackendFails(t *testing.T) {
	repo := capabilityRepo(t, "main.go")
	t.Setenv(graphBackendEnv, "dotagents-builtin:graph/not-a-backend@^9.0")

	err := runKGCodeCapabilities(capabilitiesCmd(repo, true), nil)
	if err == nil {
		t.Fatal("an unregistered graph_backend ref must exit non-zero")
	}
	if missing := covCapMissing(err.Error(), "graph_backend", "not-a-backend"); len(missing) > 0 {
		t.Fatalf("error %q does not name %v", err, missing)
	}
}

// TestRunKGCodeCapabilities_EmptyRepositoryIsNotNative guards the degenerate
// root: a repository with nothing to extract must say so in the language table
// and still route to the bridge rather than claim a native verdict off an
// empty inventory.
func TestRunKGCodeCapabilities_EmptyRepositoryIsNotNative(t *testing.T) {
	repo := capabilityRepo(t)

	text := string(runCapabilities(t, capabilitiesCmd(repo, false)))

	missing := covCapMissing(text,
		"(none — no files under the repository root)",
		"Files:        0 native, 0 bridge, 0 not indexed",
		"Routing:      "+codegraph.RoutingBridge,
		"no go source found under the repository root",
		bridgeInstallHint,
	)
	if len(missing) > 0 {
		t.Fatalf("empty-repository report is missing %v:\n%s", missing, text)
	}
	if unwanted := covCapPresent(text, "no Python required"); len(unwanted) > 0 {
		t.Fatalf("empty repository must not be claimed as fully served natively:\n%s", text)
	}
	if labels := covCapLanguageLabels(text); len(labels) != 0 {
		t.Fatalf("empty repository rendered language rows %v:\n%s", labels, text)
	}
}

// TestRunKGCodeCapabilities_InstalledBridgeReplacesTheInstallHint is the other
// half of TestRunKGCodeCapabilities_MissingBridgeIsReportedNotFailed: when the
// repository needs the bridge AND the bridge is present, the report says so
// instead of telling an operator to install what they already have.
//
// It also pins the per-language served-by labels across all three classes.
func TestRunKGCodeCapabilities_InstalledBridgeReplacesTheInstallHint(t *testing.T) {
	repo := capabilityRepo(t, "main.go", "tool.py", "blob.bin")
	covCapInstallBridge(t, repo)

	text := string(runCapabilities(t, capabilitiesCmd(repo, false)))

	if missing := covCapMissing(text, "Bridge:       available"); len(missing) > 0 {
		t.Fatalf("an installed bridge is not reported as available:\n%s", text)
	}
	if unwanted := covCapPresent(text, bridgeInstallHint, "bridge required but not installed"); len(unwanted) > 0 {
		t.Fatalf("report tells the operator to install a bridge it found (%v):\n%s", unwanted, text)
	}
	if missing := covCapMissing(text, "Files:        1 native, 1 bridge, 1 not indexed"); len(missing) > 0 {
		t.Fatalf("file totals wrong:\n%s", text)
	}

	labels := covCapLanguageLabels(text)
	want := map[string]string{
		"go":                        "native",
		"python":                    "bridge",
		codegraph.LanguageUnindexed: "not indexed",
	}
	for language, wantLabel := range want {
		row, ok := labels[language]
		if !ok {
			t.Fatalf("no %s row in the language table:\n%s", language, text)
		}
		if got := covCapServedBy(row); got != wantLabel {
			t.Fatalf("%s served-by = %q, want %q\n%s", language, got, wantLabel, text)
		}
	}
}

// covCapNativeDiagnostic builds a fully-native diagnostic whose tool rows are
// real published tools, so alwaysBridgeTools resolves them against the release
// rather than silently skipping unknown names.
func covCapNativeDiagnostic(t *testing.T, tools []toolRouting) capabilityDiagnostic {
	t.Helper()
	d := capabilityDiagnostic{
		CRGRelease:      crgrelease.Version,
		Backend:         "crg",
		Routing:         codegraph.RoutingNative,
		Reason:          "every indexed file (2) is go, which the kg-native scanner extracts",
		BridgeAvailable: false,
		BridgeRequired:  false,
		Tools:           tools,
		CapabilityReport: codegraph.CapabilityReport{
			Root:        filepath.Join("repo", "root"),
			NativeFiles: 2,
			FullyNative: true,
			Languages: []codegraph.SourceCapability{{
				Language: "go", Extensions: []string{".go"}, Files: 2,
				Native: true, UpstreamSupported: true,
			}},
		},
	}
	for _, tool := range tools {
		capability, ok := crgrelease.Capability(tool.Tool)
		if !ok {
			t.Fatalf("%q is not a published tool; the fixture proves nothing", tool.Tool)
		}
		if capability.NativeBackend != (tool.Backend == string(crgrelease.BackendNative)) {
			t.Fatalf("%q fixture backend %q contradicts the release", tool.Tool, tool.Backend)
		}
		if tool.Backend == string(crgrelease.BackendNative) {
			d.NativeTools++
		} else {
			d.BridgeTools++
		}
	}
	return d
}

// TestRenderCapabilityDiagnostic_NoPythonClaimRequiresEveryToolNative pins the
// strongest sentence the report can print. "No Python required" is only true
// when the sources AND every published tool are served natively; the moment a
// single tool still needs the bridge the report must downgrade to the counted
// claim and name the tool.
//
// The all-native case is a hypothetical for the pinned release — it is the
// state a later phase reaches by implementing the remaining upstream
// capabilities — so it is driven through the renderer directly.
func TestRenderCapabilityDiagnostic_NoPythonClaimRequiresEveryToolNative(t *testing.T) {
	native := toolRouting{Tool: "query_graph_tool", Backend: string(crgrelease.BackendNative)}
	alsoNative := toolRouting{Tool: "list_flows_tool", Backend: string(crgrelease.BackendNative)}
	bridgeOnly := toolRouting{
		Tool:    "embed_graph_tool",
		Backend: string(crgrelease.BackendBridge),
		Reason:  "upstream computes vector embeddings",
	}

	cases := []struct {
		name     string
		tools    []toolRouting
		want     []string
		unwanted []string
	}{
		{
			name:  "every tool native",
			tools: []toolRouting{native, alsoNative},
			want: []string{
				"Code Graph Capabilities  [NATIVE]",
				"  go               2 file(s)  native           .go",
				"Tools:        2 of 2 served natively, 0 via the bridge",
				"Served entirely by the kg-native backend — no Python required.",
			},
			unwanted: []string{"Bridge-only:", bridgeInstallHint, "the rest need the bridge"},
		},
		{
			name:  "one standing bridge-only tool",
			tools: []toolRouting{native, alsoNative, bridgeOnly},
			want: []string{
				"Tools:        2 of 3 served natively, 1 via the bridge",
				"Bridge-only:  embed_graph_tool",
				"All sources served natively — 2 of 3 tools answered in-process",
				"the rest need the bridge",
			},
			unwanted: []string{"no Python required"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := covCapNativeDiagnostic(t, tc.tools)
			text := string(captureStdout(t, func() { renderCapabilityDiagnostic(d) }))
			if missing := covCapMissing(text, tc.want...); len(missing) > 0 {
				t.Fatalf("report is missing %v:\n%s", missing, text)
			}
			if extra := covCapPresent(text, tc.unwanted...); len(extra) > 0 {
				t.Fatalf("report wrongly contains %v:\n%s", extra, text)
			}
		})
	}
}
