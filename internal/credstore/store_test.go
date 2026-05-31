package credstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeKeyring is a hermetic in-memory Keyring. It never touches the OS
// keychain. getErr/setErr force the error branches of EnsureDataKey.
type fakeKeyring struct {
	store  map[string][]byte
	getErr error
	setErr error
	sets   int
}

func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{store: map[string][]byte{}}
}

func (f *fakeKeyring) Get(key string) ([]byte, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	v, ok := f.store[key]
	if !ok {
		return nil, ErrKeyNotFound
	}
	return v, nil
}

func (f *fakeKeyring) Set(key string, secret []byte) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.sets++
	cp := make([]byte, len(secret))
	copy(cp, secret)
	f.store[key] = cp
	return nil
}

// withStubRandRead swaps randRead for the duration of the test.
func withStubRandRead(t *testing.T, fn func([]byte) (int, error)) {
	t.Helper()
	orig := randRead
	randRead = fn
	t.Cleanup(func() { randRead = orig })
}

func TestDefaultPath(t *testing.T) {
	t.Run("honors XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
		got, err := DefaultPath()
		if err != nil {
			t.Fatalf("DefaultPath: %v", err)
		}
		if want := filepath.Join("/tmp/xdg", "da", "credentials.json"); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
	t.Run("falls back to home/.config", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/home/tester")
		got, err := DefaultPath()
		if err != nil {
			t.Fatalf("DefaultPath: %v", err)
		}
		if want := filepath.Join("/home/tester", ".config", "da", "credentials.json"); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
}

func TestEnsureDataKey(t *testing.T) {
	t.Run("mints and persists on first use", func(t *testing.T) {
		ring := newFakeKeyring()
		key, err := EnsureDataKey(ring)
		if err != nil {
			t.Fatalf("EnsureDataKey: %v", err)
		}
		if len(key) != dataKeyLen {
			t.Fatalf("key len = %d want %d", len(key), dataKeyLen)
		}
		if ring.sets != 1 {
			t.Fatalf("expected 1 keyring Set, got %d", ring.sets)
		}
	})
	t.Run("reuses an existing key", func(t *testing.T) {
		ring := newFakeKeyring()
		ring.store[keyringService] = make([]byte, dataKeyLen)
		key, err := EnsureDataKey(ring)
		if err != nil {
			t.Fatalf("EnsureDataKey: %v", err)
		}
		if len(key) != dataKeyLen || ring.sets != 0 {
			t.Fatalf("unexpected mint: len=%d sets=%d", len(key), ring.sets)
		}
	})
	t.Run("rejects a wrong-length stored key", func(t *testing.T) {
		ring := newFakeKeyring()
		ring.store[keyringService] = []byte("short")
		if _, err := EnsureDataKey(ring); !errors.Is(err, ErrBadKeyLength) {
			t.Fatalf("expected ErrBadKeyLength, got %v", err)
		}
	})
	t.Run("propagates a non-notfound get error", func(t *testing.T) {
		ring := newFakeKeyring()
		ring.getErr = errors.New("locked keychain")
		if _, err := EnsureDataKey(ring); err == nil || errors.Is(err, ErrKeyNotFound) {
			t.Fatalf("expected hard get error, got %v", err)
		}
	})
	t.Run("propagates a set error", func(t *testing.T) {
		ring := newFakeKeyring()
		ring.setErr = errors.New("write denied")
		if _, err := EnsureDataKey(ring); err == nil {
			t.Fatalf("expected set error")
		}
	})
	t.Run("propagates a rand failure", func(t *testing.T) {
		withStubRandRead(t, func([]byte) (int, error) { return 0, errors.New("no entropy") })
		ring := newFakeKeyring()
		if _, err := EnsureDataKey(ring); err == nil {
			t.Fatalf("expected rand error")
		}
	})
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "credentials.json")
	ring := newFakeKeyring()

	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open (fresh): %v", err)
	}
	st.Set("okta-token", "s3cr3t")
	st.Set("temp", "delete-me")
	st.Delete("temp")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// On-disk file must not contain the plaintext secret.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	if containsSubstring(raw, "s3cr3t") {
		t.Fatalf("plaintext secret leaked to disk: %s", raw)
	}

	reopened, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open (reopen): %v", err)
	}
	got, err := reopened.Get("okta-token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("round-trip secret = %q want %q", got, "s3cr3t")
	}
	if _, err := reopened.Get("temp"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected deleted credential to be missing, got %v", err)
	}
	if _, err := reopened.Get("absent"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("expected ErrCredentialNotFound, got %v", err)
	}
}

func TestOpenMissingFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	st, err := Open(path, newFakeKeyring())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := st.Get("anything"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("fresh store should be empty, got %v", err)
	}
}

func TestOpenPropagatesKeyringError(t *testing.T) {
	ring := newFakeKeyring()
	ring.getErr = errors.New("boom")
	if _, err := Open(filepath.Join(t.TempDir(), "c.json"), ring); err == nil {
		t.Fatalf("expected keyring error to propagate")
	}
}

func TestOpenRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := Open(path, newFakeKeyring()); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	good := newFakeKeyring()
	st, err := Open(path, good)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "value")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A keyring that mints a different key cannot decrypt the sealed file.
	other := newFakeKeyring()
	other.store[keyringService] = make([]byte, dataKeyLen)
	other.store[keyringService][0] = 0xff
	if _, err := Open(path, other); err == nil {
		t.Fatalf("expected decryption failure with wrong key")
	}
}

func TestDecryptTruncatedCiphertext(t *testing.T) {
	key := make([]byte, dataKeyLen)
	if _, err := decrypt(key, []byte{0x01, 0x02}); !errors.Is(err, ErrCiphertextTooShort) {
		t.Fatalf("expected ErrCiphertextTooShort, got %v", err)
	}
}

func TestDecryptEmptyIsNil(t *testing.T) {
	out, err := decrypt(make([]byte, dataKeyLen), nil)
	if err != nil || out != nil {
		t.Fatalf("empty sealed should decrypt to nil, got %v err=%v", out, err)
	}
}

func TestEncryptBadKey(t *testing.T) {
	if _, err := encrypt([]byte("short"), []byte("x")); !errors.Is(err, ErrBadKeyLength) {
		t.Fatalf("expected ErrBadKeyLength, got %v", err)
	}
}

func TestEncryptNonceFailure(t *testing.T) {
	withStubRandRead(t, func([]byte) (int, error) { return 0, errors.New("no entropy") })
	if _, err := encrypt(make([]byte, dataKeyLen), []byte("x")); err == nil {
		t.Fatalf("expected nonce generation error")
	}
}

func TestSaveRejectsBadKey(t *testing.T) {
	st := &Store{path: filepath.Join(t.TempDir(), "c.json"), key: []byte("short"), credentials: map[string]string{}}
	if err := st.Save(); !errors.Is(err, ErrBadKeyLength) {
		t.Fatalf("expected ErrBadKeyLength from Save, got %v", err)
	}
}

// fakeTempFile drives the chmod/write/close error branches of finishTempWrite.
type fakeTempFile struct {
	name     string
	chmodErr error
	writeErr error
	closeErr error
}

func (f *fakeTempFile) Name() string                { return f.name }
func (f *fakeTempFile) Chmod(os.FileMode) error     { return f.chmodErr }
func (f *fakeTempFile) Write(b []byte) (int, error) { return len(b), f.writeErr }
func (f *fakeTempFile) Close() error                { return f.closeErr }

// withFSStubs swaps the filesystem seams for the duration of the test.
func withFSStubs(t *testing.T, mk func(string, os.FileMode) error, ct func(string, string) (tempFile, error), rn func(string, string) error) {
	t.Helper()
	origMk, origCt, origRn := mkdirAll, createTemp, rename
	mkdirAll, createTemp, rename = mk, ct, rn
	t.Cleanup(func() { mkdirAll, createTemp, rename = origMk, origCt, origRn })
}

func newSavableStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "credentials.json"), newFakeKeyring())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "v")
	return st
}

func TestSaveMkdirFailure(t *testing.T) {
	st := newSavableStore(t)
	withFSStubs(t,
		func(string, os.FileMode) error { return errors.New("mkdir denied") },
		func(string, string) (tempFile, error) { return nil, errors.New("unused") },
		os.Rename,
	)
	if err := st.Save(); err == nil {
		t.Fatalf("expected mkdir error")
	}
}

func TestSaveCreateTempFailure(t *testing.T) {
	st := newSavableStore(t)
	withFSStubs(t,
		func(string, os.FileMode) error { return nil },
		func(string, string) (tempFile, error) { return nil, errors.New("no temp") },
		os.Rename,
	)
	if err := st.Save(); err == nil {
		t.Fatalf("expected create-temp error")
	}
}

func TestSaveTempWriteFailures(t *testing.T) {
	cases := map[string]*fakeTempFile{
		"chmod": {name: filepath.Join(t.TempDir(), "t1"), chmodErr: errors.New("chmod")},
		"write": {name: filepath.Join(t.TempDir(), "t2"), writeErr: errors.New("write")},
		"close": {name: filepath.Join(t.TempDir(), "t3"), closeErr: errors.New("close")},
	}
	for name, ft := range cases {
		t.Run(name, func(t *testing.T) {
			st := newSavableStore(t)
			withFSStubs(t,
				func(string, os.FileMode) error { return nil },
				func(string, string) (tempFile, error) { return ft, nil },
				os.Rename,
			)
			if err := st.Save(); err == nil {
				t.Fatalf("expected %s error", name)
			}
		})
	}
}

func TestSaveRenameFailure(t *testing.T) {
	st := newSavableStore(t)
	ft := &fakeTempFile{name: filepath.Join(t.TempDir(), "tmp")}
	withFSStubs(t,
		func(string, os.FileMode) error { return nil },
		func(string, string) (tempFile, error) { return ft, nil },
		func(string, string) error { return errors.New("rename denied") },
	)
	if err := st.Save(); err == nil {
		t.Fatalf("expected rename error")
	}
}

func TestReadCredentialsRejectsCorruptDecryptedPayload(t *testing.T) {
	// Seal a non-map JSON payload and confirm unmarshalCredentials rejects it.
	key := make([]byte, dataKeyLen)
	sealed, err := encrypt(key, []byte(`["not","a","map"]`))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	data, err := json.Marshal(storeFile{SchemaVersion: storeSchemaVersion, Sealed: sealed})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ring := newFakeKeyring()
	ring.store[keyringService] = key
	if _, err := Open(path, ring); err == nil {
		t.Fatalf("expected decrypted-payload parse error")
	}
}

func TestDefaultPathHomeError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	orig := userHomeDir
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { userHomeDir = orig })
	if _, err := DefaultPath(); err == nil {
		t.Fatalf("expected home-resolution error")
	}
}

func TestReadCredentialsReadErrorPropagates(t *testing.T) {
	// Point the store at a directory so os.ReadFile fails with a non-not-exist
	// error (read of a directory), exercising the hard read branch.
	dir := t.TempDir()
	if _, err := Open(dir, newFakeKeyring()); err == nil {
		t.Fatalf("expected read error when path is a directory")
	}
}

func TestDecryptBadKeyLength(t *testing.T) {
	// A non-empty sealed blob with a wrong-length key must fail at newGCM.
	if _, err := decrypt([]byte("short"), []byte{0x01, 0x02, 0x03}); !errors.Is(err, ErrBadKeyLength) {
		t.Fatalf("expected ErrBadKeyLength, got %v", err)
	}
}

func TestUnmarshalCredentialsEmpty(t *testing.T) {
	creds, err := unmarshalCredentials(nil)
	if err != nil || len(creds) != 0 {
		t.Fatalf("empty plaintext should yield empty map, got %v err=%v", creds, err)
	}
}

func containsSubstring(haystack []byte, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack []byte, needle string) int {
	n := []byte(needle)
	for i := 0; i+len(n) <= len(haystack); i++ {
		if string(haystack[i:i+len(n)]) == needle {
			return i
		}
	}
	return -1
}
