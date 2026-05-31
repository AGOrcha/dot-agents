package credstore

import (
	"encoding/json"
	"errors"
	"io/fs"
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

// fakeSys is a sysShim whose nil func-fields delegate to the real call, so a
// test overrides only the operation it wants to fault-inject (docs/TEST_SEAMS.md).
type fakeSys struct {
	randRead   func([]byte) (int, error)
	mkdirAll   func(string, os.FileMode) error
	createTemp func(string, string) (tempFile, error)
	rename     func(string, string) error
	remove     func(string) error
	readFile   func(string) ([]byte, error)
	stat       func(string) (fs.FileInfo, error)
	homeDir    func() (string, error)
	lookupEnv  func(string) (string, bool)
}

func (f fakeSys) RandRead(b []byte) (int, error) {
	if f.randRead != nil {
		return f.randRead(b)
	}
	return stdSys{}.RandRead(b)
}

func (f fakeSys) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return stdSys{}.MkdirAll(path, perm)
}

func (f fakeSys) CreateTemp(dir, pattern string) (tempFile, error) {
	if f.createTemp != nil {
		return f.createTemp(dir, pattern)
	}
	return stdSys{}.CreateTemp(dir, pattern)
}

func (f fakeSys) Rename(oldpath, newpath string) error {
	if f.rename != nil {
		return f.rename(oldpath, newpath)
	}
	return stdSys{}.Rename(oldpath, newpath)
}

func (f fakeSys) Remove(name string) error {
	if f.remove != nil {
		return f.remove(name)
	}
	return stdSys{}.Remove(name)
}

func (f fakeSys) ReadFile(name string) ([]byte, error) {
	if f.readFile != nil {
		return f.readFile(name)
	}
	return stdSys{}.ReadFile(name)
}

func (f fakeSys) Stat(name string) (fs.FileInfo, error) {
	if f.stat != nil {
		return f.stat(name)
	}
	return stdSys{}.Stat(name)
}

func (f fakeSys) HomeDir() (string, error) {
	if f.homeDir != nil {
		return f.homeDir()
	}
	return stdSys{}.HomeDir()
}

func (f fakeSys) LookupEnv(key string) (string, bool) {
	if f.lookupEnv != nil {
		return f.lookupEnv(key)
	}
	return stdSys{}.LookupEnv(key)
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

func TestDefaultPathHomeError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	sys := fakeSys{homeDir: func() (string, error) { return "", errors.New("no home") }}
	if _, err := defaultPath(sys); err == nil {
		t.Fatalf("expected home-resolution error")
	}
}

func TestEnsureDataKeyMintsOnFirstUse(t *testing.T) {
	ring := newFakeKeyring()
	key, err := EnsureDataKey(ring)
	if err != nil {
		t.Fatalf("EnsureDataKey: %v", err)
	}
	if len(key) != dataKeyLen || ring.sets != 1 {
		t.Fatalf("expected one fresh %d-byte key, got len=%d sets=%d", dataKeyLen, len(key), ring.sets)
	}
}

func TestEnsureDataKeyReusesExisting(t *testing.T) {
	ring := newFakeKeyring()
	ring.store[keyringService] = make([]byte, dataKeyLen)
	key, err := EnsureDataKey(ring)
	if err != nil {
		t.Fatalf("EnsureDataKey: %v", err)
	}
	if len(key) != dataKeyLen || ring.sets != 0 {
		t.Fatalf("unexpected mint: len=%d sets=%d", len(key), ring.sets)
	}
}

func TestEnsureDataKeyErrorBranches(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*fakeKeyring)
		stubbedRand bool
		wantIs      error
	}{
		{name: "wrong-length stored key", setup: func(r *fakeKeyring) {
			r.store[keyringService] = []byte("short")
		}, wantIs: ErrBadKeyLength},
		{name: "non-notfound get error", setup: func(r *fakeKeyring) {
			r.getErr = errors.New("locked keychain")
		}},
		{name: "set error", setup: func(r *fakeKeyring) {
			r.setErr = errors.New("write denied")
		}},
		{name: "rand failure", stubbedRand: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sys := fakeSys{}
			if tc.stubbedRand {
				sys.randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
			}
			ring := newFakeKeyring()
			if tc.setup != nil {
				tc.setup(ring)
			}
			_, err := ensureDataKey(ring, sys)
			assertErrorBranch(t, err, tc.wantIs)
		})
	}
}

// assertErrorBranch fails unless err is non-nil and, when wantIs is set,
// matches it via errors.Is. It keeps the table loop's complexity low.
func assertErrorBranch(t *testing.T, err, wantIs error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error")
	}
	if wantIs != nil && !errors.Is(err, wantIs) {
		t.Fatalf("got %v want errors.Is %v", err, wantIs)
	}
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

func TestReadCredentialsReadErrorPropagates(t *testing.T) {
	// Point the store at a directory so os.ReadFile fails with a non-not-exist
	// error (read of a directory), exercising the hard read branch.
	dir := t.TempDir()
	if _, err := Open(dir, newFakeKeyring()); err == nil {
		t.Fatalf("expected read error when path is a directory")
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

func TestDecryptBadKeyLength(t *testing.T) {
	// A non-empty sealed blob with a wrong-length key must fail at newGCM.
	if _, err := decrypt([]byte("short"), []byte{0x01, 0x02, 0x03}); !errors.Is(err, ErrBadKeyLength) {
		t.Fatalf("expected ErrBadKeyLength, got %v", err)
	}
}

func TestEncryptBadKey(t *testing.T) {
	if _, err := encrypt([]byte("short"), []byte("x"), stdSys{}); !errors.Is(err, ErrBadKeyLength) {
		t.Fatalf("expected ErrBadKeyLength, got %v", err)
	}
}

func TestEncryptNonceFailure(t *testing.T) {
	sys := fakeSys{randRead: func([]byte) (int, error) { return 0, errors.New("no entropy") }}
	if _, err := encrypt(make([]byte, dataKeyLen), []byte("x"), sys); err == nil {
		t.Fatalf("expected nonce generation error")
	}
}

func TestUnmarshalCredentialsEmpty(t *testing.T) {
	creds, err := unmarshalCredentials(nil)
	if err != nil || len(creds) != 0 {
		t.Fatalf("empty plaintext should yield empty map, got %v err=%v", creds, err)
	}
}

func TestReadCredentialsRejectsCorruptDecryptedPayload(t *testing.T) {
	// Seal a non-map JSON payload and confirm unmarshalCredentials rejects it.
	key := make([]byte, dataKeyLen)
	sealed, err := encrypt(key, []byte(`["not","a","map"]`), stdSys{})
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

// newSavableStore opens a real store and injects a fakeSys so a test can
// fault-inject a single filesystem operation while the rest stay real.
func newSavableStore(t *testing.T, sys fakeSys) *Store {
	t.Helper()
	st, err := openWith(filepath.Join(t.TempDir(), "credentials.json"), newFakeKeyring(), sys)
	if err != nil {
		t.Fatalf("openWith: %v", err)
	}
	st.Set("id", "v")
	return st
}

// TestStdSysDelegatesToOS exercises the production sysShim so its thin
// delegators (incl. Remove, used only on the real-cleanup path) are covered.
func TestStdSysDelegatesToOS(t *testing.T) {
	sys := stdSys{}
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := sys.Stat(path); err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if err := sys.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := sys.Stat(path); err == nil {
		t.Fatalf("expected file removed")
	}
}

func TestSaveEncryptError(t *testing.T) {
	// A store holding a wrong-length key fails in encrypt before any write.
	st := &Store{path: filepath.Join(t.TempDir(), "c.json"), key: []byte("short"), credentials: map[string]string{}, sys: stdSys{}}
	if err := st.Save(); !errors.Is(err, ErrBadKeyLength) {
		t.Fatalf("expected ErrBadKeyLength from Save, got %v", err)
	}
}

func TestSaveMkdirFailure(t *testing.T) {
	st := newSavableStore(t, fakeSys{
		mkdirAll: func(string, os.FileMode) error { return errors.New("mkdir denied") },
	})
	if err := st.Save(); err == nil {
		t.Fatalf("expected mkdir error")
	}
}

func TestSaveCreateTempFailure(t *testing.T) {
	st := newSavableStore(t, fakeSys{
		createTemp: func(string, string) (tempFile, error) { return nil, errors.New("no temp") },
	})
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
			removed := false
			st := newSavableStore(t, fakeSys{
				createTemp: func(string, string) (tempFile, error) { return ft, nil },
				remove:     func(string) error { removed = true; return nil },
			})
			if err := st.Save(); err == nil {
				t.Fatalf("expected %s error", name)
			}
			if !removed {
				t.Fatalf("expected temp cleanup after %s failure", name)
			}
		})
	}
}

func TestSaveRenameFailure(t *testing.T) {
	ft := &fakeTempFile{name: filepath.Join(t.TempDir(), "tmp")}
	removed := false
	st := newSavableStore(t, fakeSys{
		createTemp: func(string, string) (tempFile, error) { return ft, nil },
		rename:     func(string, string) error { return errors.New("rename denied") },
		remove:     func(string) error { removed = true; return nil },
	})
	if err := st.Save(); err == nil {
		t.Fatalf("expected rename error")
	}
	if !removed {
		t.Fatalf("expected temp cleanup after rename failure")
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
