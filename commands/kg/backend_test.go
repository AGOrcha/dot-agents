package kg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/codegraph"
)

// writeAgentsRC writes a minimal manifest declaring a kg.graph_backend and
// returns the project directory.
func writeAgentsRC(t *testing.T, graphBackend string) string {
	t.Helper()
	dir := t.TempDir()
	body := `{"version":1,"project":"fixture","kg":{"graph_backend":"` + graphBackend + `"}}`
	if err := os.WriteFile(filepath.Join(dir, ".agentsrc.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write .agentsrc.json: %v", err)
	}
	return dir
}

func TestGraphBackendRefDefaultsToNative(t *testing.T) {
	t.Setenv(graphBackendEnv, "")
	if got := graphBackendRef(t.TempDir()); got != RefCRGNative {
		t.Fatalf("graphBackendRef = %q, want %q", got, RefCRGNative)
	}
}

func TestGraphBackendRefFromEnvBareName(t *testing.T) {
	t.Setenv(graphBackendEnv, "crg-bridge")
	if got := graphBackendRef(t.TempDir()); got != RefCRGBridge {
		t.Fatalf("graphBackendRef = %q, want %q", got, RefCRGBridge)
	}
}

func TestGraphBackendRefFromEnvFullRef(t *testing.T) {
	t.Setenv(graphBackendEnv, RefNone)
	if got := graphBackendRef(t.TempDir()); got != RefNone {
		t.Fatalf("graphBackendRef = %q, want %q", got, RefNone)
	}
}

func TestGraphBackendRefFromManifest(t *testing.T) {
	t.Setenv(graphBackendEnv, "")
	dir := writeAgentsRC(t, "none")
	if got := graphBackendRef(dir); got != RefNone {
		t.Fatalf("graphBackendRef = %q, want %q", got, RefNone)
	}
}

func TestGraphBackendRefEnvBeatsManifest(t *testing.T) {
	dir := writeAgentsRC(t, "none")
	t.Setenv(graphBackendEnv, "crg")
	if got := graphBackendRef(dir); got != RefCRGNative {
		t.Fatalf("graphBackendRef = %q, want %q", got, RefCRGNative)
	}
}

func TestExpandBackendRefPassesThroughUnknown(t *testing.T) {
	const custom = "vendor:graph/other@^2.0"
	if got := expandBackendRef(custom); got != custom {
		t.Fatalf("expandBackendRef = %q, want passthrough", got)
	}
}

func TestResolvedBackendNameRejectsUnknownRef(t *testing.T) {
	_, err := resolvedBackendName("dotagents-builtin:graph/nope@^1.0")
	if err == nil || !strings.Contains(err.Error(), "graph_backend") {
		t.Fatalf("resolvedBackendName err = %v, want a graph_backend rejection", err)
	}
}

func TestResolvedBackendNameResolvesEachBuiltin(t *testing.T) {
	for ref, want := range map[string]string{
		RefCRGNative: "crg",
		RefCRGBridge: "crg-bridge",
		RefNone:      "none",
	} {
		got, err := resolvedBackendName(ref)
		if err != nil || got != want {
			t.Errorf("resolvedBackendName(%q) = (%q, %v), want %q", ref, got, err, want)
		}
	}
}

func TestCodeGraphProviderDefaultsToNativeEngine(t *testing.T) {
	t.Setenv(graphBackendEnv, "")
	provider, release, err := codeGraphProvider(t.TempDir())
	if err != nil {
		t.Fatalf("codeGraphProvider: %v", err)
	}
	defer release()
	if _, ok := provider.(*codegraph.Engine); !ok {
		t.Fatalf("provider = %T, want the kg-native engine", provider)
	}
}

func TestCodeGraphProviderNoneSelectsNullProvider(t *testing.T) {
	t.Setenv(graphBackendEnv, "none")
	provider, release, err := codeGraphProvider(t.TempDir())
	if err != nil {
		t.Fatalf("codeGraphProvider: %v", err)
	}
	defer release()
	if _, ok := provider.(codegraph.NullProvider); !ok {
		t.Fatalf("provider = %T, want NullProvider", provider)
	}
}

func TestCodeGraphProviderBridgeWithoutBinaryIsUnavailable(t *testing.T) {
	t.Setenv(graphBackendEnv, "crg-bridge")
	t.Setenv("PATH", t.TempDir())
	_, _, err := codeGraphProvider(t.TempDir())
	if !codeGraphUnavailable(err) {
		t.Fatalf("err = %v, want errBackendUnavailable", err)
	}
}

func TestCodeGraphProviderRejectsUnknownBackend(t *testing.T) {
	t.Setenv(graphBackendEnv, "dotagents-builtin:graph/nope@^1.0")
	_, release, err := codeGraphProvider(t.TempDir())
	release()
	if err == nil || codeGraphUnavailable(err) {
		t.Fatalf("err = %v, want a resolution error (not unavailable)", err)
	}
}

func TestCodeGraphUnavailableIgnoresOtherErrors(t *testing.T) {
	if codeGraphUnavailable(errors.New("boom")) {
		t.Fatal("unrelated error must not read as backend-unavailable")
	}
}
