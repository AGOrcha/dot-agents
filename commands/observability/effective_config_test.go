package observability

// Layer-awareness coverage for the observability command surface.
//
// Every admission path (login, status, explicit sync, best-effort outbox
// drain) used to read the FLAT repo-local .agentsrc.json. An org/team layer
// that supplied the `observability` block — the normal way a fleet configures
// one endpoint once — was invisible to all of them: `da observability status`
// reported "not configured" on a repo whose effective config plainly had it.
//
// These tests exercise the PRODUCTION loadConfig default (the effective
// loader), not an injected stub, so they fail if the default is ever moved
// back to a flat read.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// layeredObsRepo writes an isolated agents home plus a repo that extends a
// layer carrying layerObs, and (when repoObs is non-empty) a repo-local
// `observability` object. The lock is resolved so the read-only effective
// loader can replay the layer offline. Returns the repo path.
func layeredObsRepo(t *testing.T, layerObs, repoObs string) string {
	t.Helper()
	t.Setenv("AGENTS_HOME", t.TempDir())

	layerRoot := t.TempDir()
	writeObsFile(t, filepath.Join(layerRoot, "org", "base.json"), `{"observability":`+layerObs+`}`)

	repo := t.TempDir()
	manifest := `{"version":2,"project":"p","repo_id":"github.com/AGOrcha/dot-agents",` +
		`"sources":[{"id":"org","type":"local","path":` + jsonQuotedPath(layerRoot) + `}],` +
		`"extends":["org:org/base.json"]`
	if repoObs != "" {
		manifest += `,"observability":` + repoObs
	}
	writeObsFile(t, filepath.Join(repo, cfg.AgentsRCFile), manifest+"}")

	if _, err := cfg.EnsureResolved(repo, cfg.EnsureOpts{}); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	return repo
}

func writeObsFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonQuotedPath(p string) string {
	b, _ := json.Marshal(p)
	return string(b)
}

// healthServer records the health requests it served.
func healthServer(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == healthPath {
			*hits++
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"error":{"code":"not_implemented","message":"read model pending"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRunStatus_AdmitsLayerSuppliedObservability: the repo declares nothing,
// the org layer supplies the whole block, and `da observability status` must
// reach that layer's endpoint instead of reporting "not configured".
func TestRunStatus_AdmitsLayerSuppliedObservability(t *testing.T) {
	var hits int
	srv := healthServer(t, &hits)
	repo := layeredObsRepo(t, `{"enabled":true,"endpoint":`+jsonQuotedPath(srv.URL)+`}`, "")

	t.Setenv("DA_OBS_TEST_JWT", "fixture.jwt")
	deps := (Deps{
		newResolver: func() credentialResolver { return &countingResolver{err: errors.New("must not resolve")} },
		httpClient:  srv.Client(),
	}).withDefaults()

	var out bytes.Buffer
	if err := runStatus(context.Background(), &out, repo, deps); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if hits != 1 {
		t.Errorf("health requests = %d, want 1 against the layer-supplied endpoint", hits)
	}
	if got := out.String(); !strings.Contains(got, "reachable: yes") {
		t.Errorf("status output:\n%s", got)
	}
}

// TestRunStatus_RepoLocalObservabilityReplacesTheLayerObject pins the SCALAR
// merge contract through the command: the repo's object replaces the layer's
// wholesale. A fieldwise deep merge would leave the layer's endpoint in place
// for keys the repo omitted, silently publishing to the wrong backend.
func TestRunStatus_RepoLocalObservabilityReplacesTheLayerObject(t *testing.T) {
	var layerHits, repoHits int
	layerSrv := healthServer(t, &layerHits)
	repoSrv := healthServer(t, &repoHits)

	repo := layeredObsRepo(t,
		`{"enabled":true,"endpoint":`+jsonQuotedPath(layerSrv.URL)+`,"push_throttle_seconds":3600}`,
		`{"enabled":true,"endpoint":`+jsonQuotedPath(repoSrv.URL)+`}`)

	t.Setenv("DA_OBS_TEST_JWT", "fixture.jwt")
	deps := (Deps{
		newResolver: func() credentialResolver { return &countingResolver{err: errors.New("must not resolve")} },
		httpClient:  repoSrv.Client(),
	}).withDefaults()

	var out bytes.Buffer
	if err := runStatus(context.Background(), &out, repo, deps); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if repoHits != 1 || layerHits != 0 {
		t.Errorf("health hits repo=%d layer=%d, want 1/0", repoHits, layerHits)
	}

	rc, err := cfg.LoadEffectiveAgentsRC(repo)
	if err != nil {
		t.Fatalf("LoadEffectiveAgentsRC: %v", err)
	}
	if rc.Observability.PushThrottleSeconds != 0 {
		t.Errorf("push_throttle_seconds = %d, want 0: the repo object replaces the layer object wholesale",
			rc.Observability.PushThrottleSeconds)
	}
}

// TestSyncProject_AdmitsLayerSuppliedObservability: the explicit sync path
// shares the same loader, so a layer-only configuration must not abort the
// drain with "observability is not configured".
func TestSyncProject_AdmitsLayerSuppliedObservability(t *testing.T) {
	var hits int
	srv := healthServer(t, &hits)
	repo := layeredObsRepo(t, `{"enabled":true,"endpoint":`+jsonQuotedPath(srv.URL)+`}`, "")

	t.Setenv("DA_OBS_TEST_JWT", "fixture.jwt")
	deps := (Deps{httpClient: srv.Client()}).withDefaults()

	if _, err := syncProject(context.Background(), repo, deps, syncOptions{Explicit: true}); err != nil {
		t.Fatalf("syncProject with an empty outbox must succeed once admission passes: %v", err)
	}
}

// TestRunLogin_UsesLayerSuppliedCredentialRef: login stores the credential
// under the id the LAYER declares, so a fleet can rotate one credential ref
// centrally without every repo restating it.
func TestRunLogin_UsesLayerSuppliedCredentialRef(t *testing.T) {
	repo := layeredObsRepo(t,
		`{"enabled":true,"endpoint":"https://obs.example.com","auth":{"kind":"credential-ref","id":"org-obs"}}`, "")

	t.Setenv("CF_OBS_CLIENT_ID", "client.access")
	t.Setenv("CF_OBS_CLIENT_SECRET", "super-secret")
	store := &recordingStore{}
	deps := (Deps{openStore: func() (credentialStore, error) { return store, nil }}).withDefaults()

	var out bytes.Buffer
	if err := runLogin(&out, repo, deps); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if store.id != "org-obs" {
		t.Errorf("stored credential id = %q, want the layer-declared %q", store.id, "org-obs")
	}
}

// TestRunStatus_UnconfiguredEverywhereStillFailsClosed: routing through the
// layer stack must not invent a configuration. A project with no
// `observability` in any layer still reports the admission error.
func TestRunStatus_UnconfiguredEverywhereStillFailsClosed(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeObsFile(t, filepath.Join(repo, cfg.AgentsRCFile), `{"version":2,"project":"p"}`)

	deps := (Deps{}).withDefaults()
	var out bytes.Buffer
	err := runStatus(context.Background(), &out, repo, deps)
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v, want an admission failure", err)
	}
}

// unresolvableObsRepo writes a repo that DECLARES `extends` but has no lock and
// no reachable layer root, so the read-only effective loader cannot answer.
func unresolvableObsRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeObsFile(t, filepath.Join(repo, cfg.AgentsRCFile), `{"version":2,"project":"p",`+
		`"observability":{"enabled":true,"endpoint":"https://obs.example.com"},`+
		`"sources":[{"id":"org","type":"local","path":"/nonexistent"}],`+
		`"extends":["org:org/base.json"]}`)
	return repo
}

// TestObservabilityEntrypoints_UnresolvableLayerStackFailsLoud: when the layer
// stack cannot be replayed, every admission path must REPORT that rather than
// quietly falling back to the repo-local block. The repo here does carry an
// enabled `observability` object, so a flat-read fallback would happily admit
// and publish under a layer precedence nobody resolved.
func TestObservabilityEntrypoints_UnresolvableLayerStackFailsLoud(t *testing.T) {
	deps := (Deps{}).withDefaults()

	t.Run("status", func(t *testing.T) {
		var out bytes.Buffer
		err := runStatus(context.Background(), &out, unresolvableObsRepo(t), deps)
		if err == nil || !strings.Contains(err.Error(), "resolve effective config") {
			t.Fatalf("err = %v, want a resolve failure", err)
		}
	})

	t.Run("login", func(t *testing.T) {
		var out bytes.Buffer
		err := runLogin(&out, unresolvableObsRepo(t), deps)
		if err == nil || !strings.Contains(err.Error(), "resolve effective config") {
			t.Fatalf("err = %v, want a resolve failure", err)
		}
	})

	t.Run("sync", func(t *testing.T) {
		_, err := syncProject(context.Background(), unresolvableObsRepo(t), deps, syncOptions{Explicit: true})
		if err == nil || !strings.Contains(err.Error(), "resolve effective config") {
			t.Fatalf("err = %v, want a resolve failure", err)
		}
	})
}

// TestRunStatus_LayerDisabledObservabilityFailsClosed: a layer that supplies
// the block with `enabled: false` is an explicit fleet-wide opt-out, and the
// admission error must say DISABLED rather than "not configured" — the two
// send an operator to different places.
func TestRunStatus_LayerDisabledObservabilityFailsClosed(t *testing.T) {
	repo := layeredObsRepo(t, `{"enabled":false,"endpoint":"https://obs.example.com"}`, "")

	var out bytes.Buffer
	err := runStatus(context.Background(), &out, repo, (Deps{}).withDefaults())
	if err == nil || !strings.Contains(err.Error(), "disabled in the effective config") {
		t.Fatalf("err = %v, want the disabled-admission failure", err)
	}
}
