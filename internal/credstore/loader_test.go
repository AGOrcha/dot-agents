package credstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeResolver is a hermetic OIDCResolver returning a fixed result.
type fakeResolver struct {
	secret string
	err    error
}

func (f fakeResolver) Resolve(string) (string, error) { return f.secret, f.err }

// mapLookupEnv builds a lookupEnv seam over an in-memory map.
func mapLookupEnv(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

func TestStubResolverNotImplemented(t *testing.T) {
	_, err := StubOIDCResolver().Resolve("any")
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

func TestEnvKeyNormalization(t *testing.T) {
	cases := map[string]string{
		"okta-token":     "OKTA_TOKEN",
		"acme.registry":  "ACME_REGISTRY",
		"already_snake":  "ALREADY_SNAKE",
		"weird//id--end": "WEIRD_ID_END",
		"MiXeD":          "MIXED",
	}
	for in, want := range cases {
		if got := envKey(in); got != want {
			t.Errorf("envKey(%q) = %q want %q", in, got, want)
		}
	}
}

func TestResolveFromEnvWins(t *testing.T) {
	l := NewLoader()
	l.lookupEnv = mapLookupEnv(map[string]string{
		envPrefix + "OKTA_TOKEN": "from-env",
		envFileVar:               "/should/not/be/read",
	})
	l.readFile = func(string) ([]byte, error) { return nil, errors.New("must not read file") }
	got, err := l.Resolve("okta-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-env" {
		t.Fatalf("got %q want from-env", got)
	}
}

func TestResolveFromPlaintextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(path, []byte(`{"okta-token":"from-file"}`), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	l := NewLoader(WithStorePath("/unused"))
	l.lookupEnv = mapLookupEnv(map[string]string{envFileVar: path})
	got, err := l.Resolve("okta-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-file" {
		t.Fatalf("got %q want from-file", got)
	}
}

func TestResolvePlaintextFileMissIsCleanFallthrough(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(path, []byte(`{"other":"x"}`), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	l := NewLoader()
	l.lookupEnv = mapLookupEnv(map[string]string{envFileVar: path})
	if _, err := l.Resolve("okta-token"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected fallthrough to not-found, got %v", err)
	}
}

func TestResolvePlaintextFileEmptyIsMiss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(path, []byte("   \n"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	l := NewLoader()
	l.lookupEnv = mapLookupEnv(map[string]string{envFileVar: path})
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected not-found, got %v", err)
	}
}

func TestResolvePlaintextFileUnreadableIsHardError(t *testing.T) {
	l := NewLoader()
	l.lookupEnv = mapLookupEnv(map[string]string{envFileVar: filepath.Join(t.TempDir(), "missing.json")})
	_, err := l.Resolve("id")
	if err == nil || errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected hard read error, got %v", err)
	}
}

func TestResolvePlaintextFileMalformedIsHardError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	l := NewLoader()
	l.lookupEnv = mapLookupEnv(map[string]string{envFileVar: path})
	if _, err := l.Resolve("id"); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestResolveFromEncryptedStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("okta-token", "from-store")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	l := NewLoader(WithKeyring(ring), WithStorePath(path))
	l.lookupEnv = mapLookupEnv(map[string]string{})
	got, err := l.Resolve("okta-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-store" {
		t.Fatalf("got %q want from-store", got)
	}
}

func TestResolveStoreMissFallsThroughToResolver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("other", "x")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	l := NewLoader(
		WithKeyring(ring),
		WithStorePath(path),
		WithResolver(fakeResolver{secret: "from-resolver"}),
	)
	l.lookupEnv = mapLookupEnv(map[string]string{})
	got, err := l.Resolve("okta-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-resolver" {
		t.Fatalf("got %q want from-resolver", got)
	}
}

func TestResolveStoreOpenErrorIsHard(t *testing.T) {
	ring := newFakeKeyring()
	ring.getErr = errors.New("keychain locked")
	l := NewLoader(WithKeyring(ring), WithStorePath(filepath.Join(t.TempDir(), "c.json")))
	l.lookupEnv = mapLookupEnv(map[string]string{})
	if _, err := l.Resolve("id"); err == nil {
		t.Fatalf("expected open error to propagate")
	}
}

func TestResolveStoreMissViaInjectedStoreFallsThrough(t *testing.T) {
	l := NewLoader(
		WithKeyring(newFakeKeyring()),
		WithStorePath("/unused"),
		WithResolver(fakeResolver{secret: "after"}),
	)
	l.lookupEnv = mapLookupEnv(map[string]string{})
	l.openStore = func(string, Keyring) (*Store, error) {
		return &Store{credentials: map[string]string{}}, nil
	}
	got, err := l.Resolve("id")
	if err != nil || got != "after" {
		t.Fatalf("expected fallthrough to resolver, got %q err=%v", got, err)
	}
}

func TestResolveStoreSkippedWithoutKeyring(t *testing.T) {
	l := NewLoader(WithResolver(fakeResolver{secret: "resolved"}))
	l.lookupEnv = mapLookupEnv(map[string]string{})
	got, err := l.Resolve("id")
	if err != nil || got != "resolved" {
		t.Fatalf("store step should be skipped without keyring, got %q err=%v", got, err)
	}
}

func TestResolveResolverNotImplementedIsMiss(t *testing.T) {
	l := NewLoader() // stub resolver
	l.lookupEnv = mapLookupEnv(map[string]string{})
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("stub resolver should yield not-found, got %v", err)
	}
}

func TestResolveResolverHardErrorPropagates(t *testing.T) {
	l := NewLoader(WithResolver(fakeResolver{err: errors.New("idp down")}))
	l.lookupEnv = mapLookupEnv(map[string]string{})
	_, err := l.Resolve("id")
	if err == nil || errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected hard resolver error, got %v", err)
	}
}

func TestResolveNilResolverIsMiss(t *testing.T) {
	l := NewLoader()
	l.resolver = nil
	l.lookupEnv = mapLookupEnv(map[string]string{})
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("nil resolver should yield not-found, got %v", err)
	}
}

func TestResolveStorePathDefaultsToDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	l := NewLoader(WithKeyring(newFakeKeyring()))
	l.lookupEnv = mapLookupEnv(map[string]string{})
	// No store file exists at DefaultPath -> empty store -> miss -> stub -> not found.
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected not-found via default path, got %v", err)
	}
}

func TestResolveEnvFileVarBlankIsSkipped(t *testing.T) {
	l := NewLoader()
	l.lookupEnv = mapLookupEnv(map[string]string{envFileVar: ""})
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("blank file var should be skipped, got %v", err)
	}
}
