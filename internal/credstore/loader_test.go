package credstore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeResolver is a hermetic OIDCResolver returning a fixed result.
type fakeResolver struct {
	secret string
	err    error
}

func (f fakeResolver) Resolve(string) (string, error) { return f.secret, f.err }

// envSys builds a fakeSys whose LookupEnv is backed by an in-memory map and
// whose other operations delegate to the real OS (so real temp files are read).
func envSys(env map[string]string) fakeSys {
	return fakeSys{lookupEnv: func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}}
}

// writeSecureFile writes a 0600 plaintext credentials file and returns its path.
func writeSecureFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return path
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
	sys := envSys(map[string]string{
		envPrefix + "OKTA_TOKEN": "from-env",
		envFileVar:               "/should/not/be/read",
	})
	sys.readFile = func(string) ([]byte, error) { return nil, errors.New("must not read file") }
	l := NewLoader(withSys(sys))
	got, err := l.Resolve("okta-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-env" {
		t.Fatalf("got %q want from-env", got)
	}
}

func TestResolveEmptyEnvIsMissFallsThrough(t *testing.T) {
	// DA_CREDENTIAL_<id>="" must NOT shadow the populated file step.
	path := writeSecureFile(t, `{"okta-token":"from-file"}`)
	sys := envSys(map[string]string{
		envPrefix + "OKTA_TOKEN": "",
		envFileVar:               path,
	})
	got, err := NewLoader(withSys(sys)).Resolve("okta-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-file" {
		t.Fatalf("empty env should fall through; got %q want from-file", got)
	}
}

func TestResolveFromPlaintextFile(t *testing.T) {
	path := writeSecureFile(t, `{"okta-token":"from-file"}`)
	sys := envSys(map[string]string{envFileVar: path})
	got, err := NewLoader(withSys(sys), WithStorePath("/unused")).Resolve("okta-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-file" {
		t.Fatalf("got %q want from-file", got)
	}
}

func TestResolveEmptyFileValueIsMiss(t *testing.T) {
	// An empty value in the file map must be a miss, not a silent empty hit.
	path := writeSecureFile(t, `{"okta-token":""}`)
	sys := envSys(map[string]string{envFileVar: path})
	if _, err := NewLoader(withSys(sys)).Resolve("okta-token"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("empty file value should be a miss, got %v", err)
	}
}

func TestResolvePlaintextFileMissIsCleanFallthrough(t *testing.T) {
	path := writeSecureFile(t, `{"other":"x"}`)
	sys := envSys(map[string]string{envFileVar: path})
	if _, err := NewLoader(withSys(sys)).Resolve("okta-token"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected fallthrough to not-found, got %v", err)
	}
}

func TestResolvePlaintextFileEmptyIsMiss(t *testing.T) {
	path := writeSecureFile(t, "   \n")
	sys := envSys(map[string]string{envFileVar: path})
	if _, err := NewLoader(withSys(sys)).Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected not-found, got %v", err)
	}
}

func TestResolvePlaintextFileUnreadableIsHardError(t *testing.T) {
	// A 0600 file that exists but fails to read is a hard error (stat passes,
	// ReadFile is fault-injected).
	path := writeSecureFile(t, `{"id":"x"}`)
	sys := envSys(map[string]string{envFileVar: path})
	sys.readFile = func(string) ([]byte, error) { return nil, errors.New("read denied") }
	_, err := NewLoader(withSys(sys)).Resolve("id")
	if err == nil || errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected hard read error, got %v", err)
	}
}

func TestResolvePlaintextFileMissingStatIsHardError(t *testing.T) {
	sys := envSys(map[string]string{envFileVar: filepath.Join(t.TempDir(), "missing.json")})
	_, err := NewLoader(withSys(sys)).Resolve("id")
	if err == nil || errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected hard stat error for a missing file, got %v", err)
	}
}

func TestResolvePlaintextFileMalformedIsHardError(t *testing.T) {
	path := writeSecureFile(t, "{not json")
	sys := envSys(map[string]string{envFileVar: path})
	if _, err := NewLoader(withSys(sys)).Resolve("id"); err == nil {
		t.Fatalf("expected parse error")
	}
}

// statResult is a minimal fs.FileInfo carrying only the mode the perm check reads.
type statResult struct{ mode fs.FileMode }

func (statResult) Name() string        { return "creds.json" }
func (statResult) Size() int64         { return 0 }
func (s statResult) Mode() fs.FileMode { return s.mode }
func (statResult) ModTime() time.Time  { return time.Time{} }
func (statResult) IsDir() bool         { return false }
func (statResult) Sys() any            { return nil }

func TestResolvePlaintextFileInsecurePermsRefused(t *testing.T) {
	for _, mode := range []fs.FileMode{0o644, 0o640, 0o604, 0o666} {
		path := writeSecureFile(t, `{"id":"secret"}`)
		sys := envSys(map[string]string{envFileVar: path})
		sys.stat = func(string) (fs.FileInfo, error) { return statResult{mode: mode}, nil }
		_, err := NewLoader(withSys(sys)).Resolve("id")
		if !errors.Is(err, ErrInsecurePlaintextFile) {
			t.Fatalf("mode %#o: expected ErrInsecurePlaintextFile, got %v", mode, err)
		}
	}
}

func TestResolvePlaintextFileSecurePermsAccepted(t *testing.T) {
	path := writeSecureFile(t, `{"id":"secret"}`)
	sys := envSys(map[string]string{envFileVar: path})
	sys.stat = func(string) (fs.FileInfo, error) { return statResult{mode: 0o600}, nil }
	got, err := NewLoader(withSys(sys)).Resolve("id")
	if err != nil || got != "secret" {
		t.Fatalf("0600 file should be accepted, got %q err=%v", got, err)
	}
}

func TestResolveFromEncryptedStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("okta-token", "from-store")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	l := NewLoader(withSys(envSys(map[string]string{})), WithKeyring(ring), WithStorePath(path))
	got, err := l.Resolve("okta-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-store" {
		t.Fatalf("got %q want from-store", got)
	}
}

func TestResolveStoreMissFallsThroughToResolver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
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
		withSys(envSys(map[string]string{})),
		WithKeyring(ring),
		WithStorePath(path),
		WithResolver(fakeResolver{secret: "from-resolver"}),
	)
	got, err := l.Resolve("okta-token")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-resolver" {
		t.Fatalf("got %q want from-resolver", got)
	}
}

func TestResolveEmptyStoreValueIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("okta-token", "")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	l := NewLoader(
		withSys(envSys(map[string]string{})),
		WithKeyring(ring),
		WithStorePath(path),
		WithResolver(fakeResolver{secret: "after"}),
	)
	got, err := l.Resolve("okta-token")
	if err != nil || got != "after" {
		t.Fatalf("empty stored value should fall through, got %q err=%v", got, err)
	}
}

func TestResolveStoreOpenErrorIsHard(t *testing.T) {
	ring := newFakeKeyring()
	ring.getErr = errors.New("keychain locked")
	l := NewLoader(withSys(envSys(map[string]string{})), WithKeyring(ring), WithStorePath(filepath.Join(t.TempDir(), "c.json")))
	if _, err := l.Resolve("id"); err == nil {
		t.Fatalf("expected open error to propagate")
	}
}

func TestResolveStoreSkippedWithoutKeyring(t *testing.T) {
	l := NewLoader(withSys(envSys(map[string]string{})), WithResolver(fakeResolver{secret: "resolved"}))
	got, err := l.Resolve("id")
	if err != nil || got != "resolved" {
		t.Fatalf("store step should be skipped without keyring, got %q err=%v", got, err)
	}
}

func TestResolveResolverNotImplementedIsMiss(t *testing.T) {
	l := NewLoader(withSys(envSys(map[string]string{}))) // stub resolver
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("stub resolver should yield not-found, got %v", err)
	}
}

func TestResolveResolverHardErrorPropagates(t *testing.T) {
	l := NewLoader(withSys(envSys(map[string]string{})), WithResolver(fakeResolver{err: errors.New("idp down")}))
	_, err := l.Resolve("id")
	if err == nil || errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected hard resolver error, got %v", err)
	}
}

func TestResolveResolverEmptyValueIsMiss(t *testing.T) {
	// A resolver that returns ("", nil) must be a miss, not an empty hit.
	l := NewLoader(withSys(envSys(map[string]string{})), WithResolver(fakeResolver{secret: ""}))
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("empty resolver value should be a miss, got %v", err)
	}
}

func TestResolveNilResolverIsMiss(t *testing.T) {
	l := NewLoader(withSys(envSys(map[string]string{})))
	l.resolver = nil
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("nil resolver should yield not-found, got %v", err)
	}
}

func TestResolveStorePathDefaultsToDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	l := NewLoader(withSys(envSys(map[string]string{})), WithKeyring(newFakeKeyring()))
	// No store file exists at DefaultPath -> empty store -> miss -> stub -> not found.
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected not-found via default path, got %v", err)
	}
}

func TestResolveEnvFileVarBlankIsSkipped(t *testing.T) {
	l := NewLoader(withSys(envSys(map[string]string{envFileVar: ""})))
	if _, err := l.Resolve("id"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("blank file var should be skipped, got %v", err)
	}
}
