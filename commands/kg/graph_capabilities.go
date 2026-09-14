package kg

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/codegraph"
	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// bridgeInstallHint is the remediation printed when a repository needs the
// Python bridge and it is not installed. It repeats DiscoverCRGBin's advice so
// the diagnostic is actionable without a second command.
const bridgeInstallHint = "install it with: uv pip install code-review-graph==" + crgrelease.Version

// capabilityDiagnostic is the `--json` payload: the raw CapabilityReport
// (flattened) plus the routing verdict and the environment facts that verdict
// depends on. Everything a caller needs to reproduce the decision is present,
// so the JSON is a contract rather than a rendering of the human output.
type capabilityDiagnostic struct {
	// CRGRelease is the upstream code-review-graph release the language
	// inventory — and therefore the routing decision — is pinned to.
	CRGRelease string `json:"crg_release"`
	// Backend is the configured code-graph adapter's name.
	Backend string `json:"backend"`
	// Routing is codegraph.RoutingNative or codegraph.RoutingBridge.
	Routing string `json:"routing"`
	// Reason names the languages that forced the routing decision.
	Reason string `json:"reason"`
	// BridgeAvailable reports whether the `code-review-graph` CLI was found.
	BridgeAvailable bool `json:"bridge_available"`
	// BridgeRequired is true when Routing is bridge, restated as a field so a
	// consumer never has to string-compare Routing.
	BridgeRequired bool `json:"bridge_required"`
	// Tools is the per-tool routing decision for this repository: which of
	// the release's published tools `da kg serve` would answer in-process
	// here, which it would hand to the retained bridge, and why.
	//
	// The repository-level verdict above is necessary but not sufficient:
	// several tools are bridge-only regardless of the sources, because they
	// depend on an upstream capability the native backend does not have
	// (embeddings, the packaged docs reference, sampled betweenness, git's
	// own diff hunk boundaries, the cross-repo registry, wiki generation and
	// refactor application). Listing them per tool is what makes the Phase-A
	// coverage claim checkable instead of a promise.
	Tools []toolRouting `json:"tools"`
	// NativeTools / BridgeTools are the Tools counts, restated so a consumer
	// does not have to tally them.
	NativeTools int `json:"native_tools"`
	BridgeTools int `json:"bridge_tools"`

	codegraph.CapabilityReport
}

// toolRouting is one published tool's resolved backend.
type toolRouting struct {
	Tool    string `json:"tool"`
	Backend string `json:"backend"`
	Reason  string `json:"reason,omitempty"`
}

// runKGCodeCapabilities answers "can the kg-native backend serve this
// repository, and if not, why?".
//
// It is a DIAGNOSTIC: an accurate "this repository needs the Python bridge"
// answer is a success. A non-zero exit means the question could not be
// answered — an unreadable root or an unresolvable backend — never that the
// answer was unwelcome.
func runKGCodeCapabilities(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("repo")
	if root == "" {
		root = crgRepoRoot()
	}
	backend, err := resolvedBackendName(graphBackendRef(root))
	if err != nil {
		return err
	}
	report, err := codegraph.ScanCapability(root)
	if err != nil {
		return err
	}

	_, bridgeErr := graphstore.DiscoverCRGBin(root)
	diagnostic := capabilityDiagnostic{
		CRGRelease:       crgrelease.Version,
		Backend:          backend,
		Routing:          report.Routing(),
		Reason:           report.Reason(),
		BridgeAvailable:  bridgeErr == nil,
		BridgeRequired:   report.Routing() == codegraph.RoutingBridge,
		CapabilityReport: report,
	}
	if err := diagnostic.addToolRouting(report); err != nil {
		return err
	}

	if commandJSON(cmd) {
		payload, encErr := json.MarshalIndent(diagnostic, "", "  ")
		if encErr != nil {
			return fmt.Errorf("kg code-capabilities: encode report: %w", encErr)
		}
		fmt.Println(string(payload))
		return nil
	}
	renderCapabilityDiagnostic(diagnostic)
	return nil
}

// addToolRouting resolves every published tool's backend for this
// repository.
//
// The decision has two independent halves and both must hold for a tool to
// be answered natively: the tool needs a native implementation that
// reproduces the release's response, AND the repository's sources must be
// fully covered by the native scanner — a native answer computed from a
// graph that is missing files is wrong rather than partial.
func (d *capabilityDiagnostic) addToolRouting(report codegraph.CapabilityReport) error {
	capabilities, err := crgrelease.Capabilities()
	if err != nil {
		return fmt.Errorf("kg code-capabilities: %w", err)
	}
	d.Tools = make([]toolRouting, 0, len(capabilities))
	for _, capability := range capabilities {
		routing := toolRouting{Tool: capability.Tool, Backend: string(crgrelease.BackendBridge)}
		switch {
		case !capability.NativeBackend:
			routing.Reason = capability.BridgeOnlyReason
		case report.Routing() != codegraph.RoutingNative:
			routing.Reason = crgrelease.SourcesUnsupportedReason(report.BridgeLanguages())
		default:
			routing.Backend = string(crgrelease.BackendNative)
		}
		if routing.Backend == string(crgrelease.BackendNative) {
			d.NativeTools++
		} else {
			d.BridgeTools++
		}
		d.Tools = append(d.Tools, routing)
	}
	return nil
}

// alwaysBridgeTools names the tools the native backend cannot serve for ANY
// repository, as opposed to those routed away only because this repository
// has sources the native scanner does not extract. Separating the two is
// what stops a reader concluding that a Go-only repository would get them.
func (d capabilityDiagnostic) alwaysBridgeTools() []string {
	var out []string
	for _, routing := range d.Tools {
		capability, ok := crgrelease.Capability(routing.Tool)
		if ok && !capability.NativeBackend {
			out = append(out, routing.Tool)
		}
	}
	return out
}

// renderCapabilityDiagnostic prints the human report: the pinned release and
// selected backend, one row per language, then the verdict.
func renderCapabilityDiagnostic(d capabilityDiagnostic) {
	ui.Header(fmt.Sprintf("Code Graph Capabilities  [%s]", strings.ToUpper(d.Routing)))
	ui.Info(fmt.Sprintf("  Root:         %s", d.Root))
	ui.Info(fmt.Sprintf("  CRG release:  %s", d.CRGRelease))
	ui.Info(fmt.Sprintf("  Backend:      %s", d.Backend))

	if len(d.Languages) == 0 {
		ui.Info("  Languages:    (none — no files under the repository root)")
	} else {
		ui.Info("  Languages:")
		for _, lang := range d.Languages {
			ui.Info(fmt.Sprintf("    %-12s %5d file(s)  %-16s %s",
				lang.Language, lang.Files, capabilityServedBy(lang),
				strings.Join(lang.Extensions, " ")))
		}
	}
	ui.Info(fmt.Sprintf("  Files:        %d native, %d bridge, %d not indexed",
		d.NativeFiles, d.BridgeFiles, d.UnsupportedFiles))
	ui.Info(fmt.Sprintf("  Routing:      %s", d.Routing))
	ui.Info(fmt.Sprintf("  Reason:       %s", d.Reason))
	ui.Info(fmt.Sprintf("  Tools:        %d of %d served natively, %d via the bridge",
		d.NativeTools, d.NativeTools+d.BridgeTools, d.BridgeTools))
	// A tool that is bridge-only regardless of the sources is a standing
	// Phase-A limit, not a property of this repository, so it is named
	// separately from the source-coverage verdict.
	if always := d.alwaysBridgeTools(); len(always) > 0 {
		ui.Info(fmt.Sprintf("  Bridge-only:  %s", strings.Join(always, ", ")))
	}

	switch {
	case !d.BridgeRequired && d.BridgeTools == 0:
		ui.Success("Served entirely by the kg-native backend — no Python required.")
	case !d.BridgeRequired:
		// Every SOURCE is natively covered, but some tools are bridge-only
		// for any repository. Saying "no Python required" here would be false
		// exactly where a reader is most likely to trust it.
		ui.Success(fmt.Sprintf(
			"All sources served natively — %d of %d tools answered in-process; "+
				"the rest need the bridge.",
			d.NativeTools, d.NativeTools+d.BridgeTools))
	case d.BridgeAvailable:
		ui.Info("  Bridge:       available")
	default:
		ui.WarnBox("Python bridge required but not installed",
			d.Reason,
			"The kg-native backend cannot serve this repository on its own.",
			bridgeInstallHint,
		)
	}
}

// capabilityServedBy labels one language row with who indexes it.
func capabilityServedBy(lang codegraph.SourceCapability) string {
	switch {
	case lang.Native:
		return "native"
	case lang.UpstreamSupported:
		return "bridge"
	default:
		return "not indexed"
	}
}
