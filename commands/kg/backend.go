package kg

import (
	"errors"
	"fmt"
	"os"
	"strings"

	crgadapter "github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	crgbridge "github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg-bridge"
	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/none"
	"github.com/AGOrcha/dot-agents/internal/codegraph"
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Adapter-refs for the selectable code-graph backends
// (graph-backend-adapter-contract §8 ref grammar).
const (
	// RefCRGNative is the default: the kg-native, in-process CRG adapter.
	RefCRGNative = "dotagents-builtin:graph/crg@^1.0"
	// RefCRGBridge selects the legacy Python `code-review-graph` subprocess.
	// It is the §11.4 rollback path and stays available until the
	// decommissioning gate passes and t6d deletes the bridge.
	RefCRGBridge = "dotagents-builtin:graph/crg-bridge@^0.1"
	// RefNone disables code-graph operations entirely.
	RefNone = "dotagents-builtin:graph/none@^1.0"
)

// graphBackendEnv overrides the configured backend for one process. It exists
// for operators bisecting a native-vs-bridge behaviour difference (and for the
// e2e harness) without editing committed config.
const graphBackendEnv = "DA_KG_GRAPH_BACKEND"

// errBackendUnavailable marks "the selected backend's tooling is not installed
// on this machine". It is the generalization of the bridge-era
// `DiscoverCRGBin` probe: callers that must degrade gracefully (the kg update
// post_tool_use hook, the warm code lane, the bridge query intents) check for
// it and no-op instead of failing the session.
var errBackendUnavailable = errors.New("code graph backend unavailable")

// graphBackendRef returns the adapter-ref selecting this project's code-graph
// backend: the DA_KG_GRAPH_BACKEND override, else `.agentsrc.json`'s
// `kg.graph_backend`, else the kg-native default. A bare adapter name
// ("crg", "crg-bridge", "none") is accepted anywhere a ref is and expanded to
// the canonical ref, because operators type the name far more often than the
// full grammar.
func graphBackendRef(repoRoot string) string {
	if v := strings.TrimSpace(os.Getenv(graphBackendEnv)); v != "" {
		return expandBackendRef(v)
	}
	if rc, err := config.LoadAgentsRC(repoRoot); err == nil && rc != nil && rc.KG != nil {
		if v := strings.TrimSpace(rc.KG.GraphBackend); v != "" {
			return expandBackendRef(v)
		}
	}
	return RefCRGNative
}

// expandBackendRef expands a bare adapter name to its canonical ref.
func expandBackendRef(v string) string {
	switch v {
	case crgadapter.Name:
		return RefCRGNative
	case crgbridge.Name:
		return RefCRGBridge
	case none.Name:
		return RefNone
	}
	return v
}

// resolvedBackendName resolves the selected ref against the built-in adapter
// registry and returns the adapter's name. Going through the registry is what
// makes registration load-bearing: an unregistered or version-incompatible ref
// is rejected here rather than silently falling back to a default backend.
func resolvedBackendName(ref string) (string, error) {
	adapter, err := resolveBackend(ref)
	if err != nil {
		return "", fmt.Errorf("kg: graph_backend %q: %w", ref, err)
	}
	return adapter.Name(), nil
}

// codeGraphProvider opens the code-graph backend selected for repoRoot and
// returns it with a release function. The returned provider is the ONLY way the
// `da kg` code commands and `da kg serve` reach a code graph, so the backend
// choice is made in exactly one place.
//
// The default is the kg-native engine: no Python, no subprocess. Selecting the
// crg-bridge family routes to the legacy `code-review-graph` CLI, and a missing
// CLI is reported as errBackendUnavailable so graceful-degradation callers can
// no-op exactly as they did before the cutover.
func codeGraphProvider(repoRoot string) (graphstore.CodeGraphProvider, func(), error) {
	name, err := resolvedBackendName(graphBackendRef(repoRoot))
	if err != nil {
		return nil, func() {}, err
	}
	switch name {
	case crgbridge.Name:
		bridge, berr := graphstore.NewCRGBridge(repoRoot)
		if berr != nil {
			return nil, func() {}, fmt.Errorf("%w: %s", errBackendUnavailable, berr)
		}
		return bridge, func() {}, nil
	case none.Name:
		return codegraph.NullProvider{}, func() {}, nil
	default:
		engine := codegraph.Open(repoRoot)
		return engine, func() { _ = engine.Close() }, nil
	}
}

// codeGraphUnavailable reports whether err means the selected backend's tooling
// is absent, as opposed to a real failure of a present backend.
func codeGraphUnavailable(err error) bool { return errors.Is(err, errBackendUnavailable) }
