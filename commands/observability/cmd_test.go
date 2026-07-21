package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

type countingResolver struct {
	value string
	err   error
	calls int
}

func (r *countingResolver) Resolve(string) (string, error) {
	r.calls++
	return r.value, r.err
}

type recordingStore struct {
	setCalls  int
	saveCalls int
	id        string
	value     string
	saveErr   error
}

func (s *recordingStore) Set(id, value string) {
	s.setCalls++
	s.id, s.value = id, value
}

func (s *recordingStore) Save() error {
	s.saveCalls++
	return s.saveErr
}

func TestNewCmdWiresLoginSyncStatus(t *testing.T) {
	cmd := NewCmd(Deps{})
	if cmd.Name() != "observability" {
		t.Fatalf("name = %q", cmd.Name())
	}
	for _, name := range []string{"login", "sync", "status"} {
		if child, _, err := cmd.Find([]string{name}); err != nil || child == cmd || child.Name() != name {
			t.Fatalf("subcommand %q not wired: child=%v err=%v", name, child, err)
		}
	}
}

func TestAuthorizationRejectsHTTPBeforeCredentialResolution(t *testing.T) {
	resolver := &countingResolver{value: `{"kind":"cf-access-service-token","client_id":"id","client_secret":"secret"}`}
	obs := &cfg.AgentsRCObservability{
		Enabled:  true,
		Endpoint: "http://example.com",
		Auth:     &cfg.AgentsRCObservabilityAuth{Kind: "credential-ref", ID: "agorcha-obs"},
	}
	_, _, err := authorization(obs, resolver, "")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("authorization error = %v", err)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver called %d times before HTTPS guard", resolver.calls)
	}
}

func TestAuthorizationLoopbackFixtureNeverResolvesCredential(t *testing.T) {
	resolver := &countingResolver{err: errors.New("must not resolve")}
	obs := &cfg.AgentsRCObservability{Enabled: true, Endpoint: "http://127.0.0.1:8787"}
	headers, _, err := authorization(obs, resolver, "fixture.jwt")
	if err != nil {
		t.Fatalf("authorization: %v", err)
	}
	if got := headers.Get("Cf-Access-Jwt-Assertion"); got != "fixture.jwt" {
		t.Fatalf("fixture header = %q", got)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver called %d times in fixture mode", resolver.calls)
	}
}

func TestAuthorizationRequiresStrictAtomicServiceToken(t *testing.T) {
	obs := &cfg.AgentsRCObservability{
		Enabled:  true,
		Endpoint: "https://obs.example.com",
		Auth:     &cfg.AgentsRCObservabilityAuth{Kind: "credential-ref", ID: "agorcha-obs"},
	}
	for name, value := range map[string]string{
		"partial":    `{"kind":"cf-access-service-token","client_id":"id"}`,
		"wrong-kind": `{"kind":"bearer","client_id":"id","client_secret":"secret"}`,
		"unknown":    `{"kind":"cf-access-service-token","client_id":"id","client_secret":"secret","extra":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			resolver := &countingResolver{value: value}
			if _, _, err := authorization(obs, resolver, ""); err == nil {
				t.Fatal("expected strict credential error")
			}
			if resolver.calls != 1 {
				t.Fatalf("resolver calls = %d, want 1", resolver.calls)
			}
		})
	}
}

func TestRunLoginStoresCompleteCredentialOnce(t *testing.T) {
	t.Setenv("CF_OBS_CLIENT_ID", "client.access")
	t.Setenv("CF_OBS_CLIENT_SECRET", "super-secret")
	store := &recordingStore{}
	deps := (Deps{
		loadConfig: func(string) (*cfg.AgentsRC, error) {
			return configuredRC("https://obs.example.com", true), nil
		},
		openStore: func() (credentialStore, error) { return store, nil },
	}).withDefaults()
	var out bytes.Buffer
	if err := runLogin(&out, t.TempDir(), deps); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if store.setCalls != 1 || store.saveCalls != 1 {
		t.Fatalf("Set/Save calls = %d/%d, want 1/1", store.setCalls, store.saveCalls)
	}
	if store.id != "agorcha-obs" {
		t.Fatalf("stored id = %q", store.id)
	}
	var stored serviceTokenCredential
	if err := json.Unmarshal([]byte(store.value), &stored); err != nil {
		t.Fatalf("stored JSON: %v", err)
	}
	if stored.Kind != serviceTokenKind || stored.ClientID != "client.access" || stored.ClientSecret != "super-secret" {
		t.Fatalf("stored credential = %#v", stored)
	}
	if strings.Contains(out.String(), "super-secret") {
		t.Fatal("login output leaked the secret")
	}
}

func TestStatusReportsReachableAndAuthedForScaffoldHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cf-Access-Jwt-Assertion"); got != "fixture.jwt" {
			t.Errorf("fixture header = %q", got)
		}
		if r.URL.Path != healthPath {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"error":{"code":"not_implemented","message":"read model pending"}}`))
	}))
	defer server.Close()

	resolver := &countingResolver{err: errors.New("must not resolve")}
	deps := (Deps{
		loadConfig:  func(string) (*cfg.AgentsRC, error) { return configuredRC(server.URL, false), nil },
		newResolver: func() credentialResolver { return resolver },
		httpClient:  server.Client(),
	}).withDefaults()
	t.Setenv("DA_OBS_TEST_JWT", "fixture.jwt")
	var out bytes.Buffer
	if err := runStatus(context.Background(), &out, t.TempDir(), deps); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "reachable: yes") || !strings.Contains(got, "authed: yes") || !strings.Contains(got, "HTTP 501") {
		t.Fatalf("status output:\n%s", got)
	}
	if resolver.calls != 0 {
		t.Fatalf("fixture status resolved credential %d times", resolver.calls)
	}
}

func configuredRC(endpoint string, withAuth bool) *cfg.AgentsRC {
	obs := &cfg.AgentsRCObservability{Enabled: true, Endpoint: endpoint}
	if withAuth {
		obs.Auth = &cfg.AgentsRCObservabilityAuth{Kind: "credential-ref", ID: "agorcha-obs"}
	}
	return &cfg.AgentsRC{RepoID: "github.com/AGOrcha/dot-agents", Observability: obs}
}
