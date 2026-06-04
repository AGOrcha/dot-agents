package credstore

import (
	"crypto/cipher"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// fakeKeyring is a hermetic in-memory Keyring. It never touches the OS
// keychain. getErr/setErr force the error branches of the key-seed path.
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
	deriveKey  func([]byte) ([]byte, error)
	newAEAD    func([]byte) (cipher.AEAD, error)
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

func (f fakeSys) DeriveKey(secret []byte) ([]byte, error) {
	if f.deriveKey != nil {
		return f.deriveKey(secret)
	}
	return stdSys{}.DeriveKey(secret)
}

func (f fakeSys) NewAEAD(key []byte) (cipher.AEAD, error) {
	if f.newAEAD != nil {
		return f.newAEAD(key)
	}
	return stdSys{}.NewAEAD(key)
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

func TestEnsureHybridKeyMintsOnFirstUse(t *testing.T) {
	ring := newFakeKeyring()
	hk, err := ensureHybridKey(ring, stdSys{}, "")
	if err != nil {
		t.Fatalf("ensureHybridKey: %v", err)
	}
	if hk.x25519 == nil || hk.mlkem == nil {
		t.Fatalf("hybrid key not fully reconstructed: %+v", hk)
	}
	if ring.sets != 1 {
		t.Fatalf("expected one keyring Set, got %d", ring.sets)
	}
	if got := len(ring.store[keyringService]); got != seedBlobLen {
		t.Fatalf("persisted seed length = %d want %d", got, seedBlobLen)
	}
}

func TestEnsureHybridKeyReusesExisting(t *testing.T) {
	ring := newFakeKeyring()
	if _, err := ensureHybridKey(ring, stdSys{}, ""); err != nil {
		t.Fatalf("first ensureHybridKey: %v", err)
	}
	ring.sets = 0
	if _, err := ensureHybridKey(ring, stdSys{}, ""); err != nil {
		t.Fatalf("second ensureHybridKey: %v", err)
	}
	if ring.sets != 0 {
		t.Fatalf("expected reuse without a Set, got %d", ring.sets)
	}
}

func TestEnsureHybridKeyErrorBranches(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*fakeKeyring)
		randFails bool
		wantIs    error
	}{
		{name: "wrong-length stored seed", setup: func(r *fakeKeyring) {
			r.store[keyringService] = []byte("short")
		}, wantIs: ErrBadSeedLength},
		{name: "non-notfound get error", setup: func(r *fakeKeyring) {
			r.getErr = errors.New("locked keychain")
		}},
		{name: "set error", setup: func(r *fakeKeyring) {
			r.setErr = errors.New("write denied")
		}},
		{name: "rand failure", randFails: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sys := fakeSys{}
			if tc.randFails {
				sys.randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
			}
			ring := newFakeKeyring()
			if tc.setup != nil {
				tc.setup(ring)
			}
			_, err := ensureHybridKey(ring, sys, "")
			assertErrorBranch(t, err, tc.wantIs)
		})
	}
}

func TestMintHybridKeyMLKEMRandFailure(t *testing.T) {
	// Fail only the second RandRead (the ml-kem seed) to cover that branch.
	calls := 0
	sys := fakeSys{randRead: func(b []byte) (int, error) {
		calls++
		if calls == 2 {
			return 0, errors.New("no entropy")
		}
		return stdSys{}.RandRead(b)
	}}
	if _, err := ensureHybridKey(newFakeKeyring(), sys, ""); err == nil {
		t.Fatalf("expected ml-kem seed rand failure")
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

	// On-disk file must not contain any plaintext secret or credential id.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	for _, leak := range []string{"s3cr3t", "delete-me", "okta-token"} {
		if containsSubstring(raw, leak) {
			t.Fatalf("plaintext %q leaked to disk: %s", leak, raw)
		}
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

func TestEnvelopeIsVersionedHybrid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	st, err := Open(path, newFakeKeyring())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "value")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("envelope unmarshal: %v", err)
	}
	if env.FormatVersion != formatVersion {
		t.Fatalf("format_version = %d want %d", env.FormatVersion, formatVersion)
	}
	if len(env.MLKEMCiphertext) == 0 || len(env.X25519EphPub) == 0 || len(env.GCMNonce) == 0 || len(env.GCMCiphertext) == 0 {
		t.Fatalf("envelope missing a required field: %+v", env)
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

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "value")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Flip one byte of the GCM ciphertext: AEAD must fail closed.
	var env envelope
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	env.GCMCiphertext[0] ^= 0xff
	tampered, _ := json.Marshal(env)
	if err := os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := Open(path, ring); err == nil {
		t.Fatalf("expected AEAD tamper detection to fail open")
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	st, err := Open(path, newFakeKeyring())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "value")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A different recipient keypair cannot decapsulate either secret.
	other := newFakeKeyring()
	if _, err := Open(path, other); err == nil {
		t.Fatalf("expected decryption failure with a different recipient key")
	}
}

func TestOpenRejectsBadFormatVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "v")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var env envelope
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &env)
	env.FormatVersion = 1
	downgraded, _ := json.Marshal(env)
	if err := os.WriteFile(path, downgraded, 0600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := Open(path, ring); !errors.Is(err, ErrBadFormatVersion) {
		t.Fatalf("expected ErrBadFormatVersion, got %v", err)
	}
}

func TestOpenRejectsCorruptMLKEMCiphertext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "v")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var env envelope
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &env)
	env.MLKEMCiphertext = env.MLKEMCiphertext[:10] // wrong length -> decapsulate error
	bad, _ := json.Marshal(env)
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := Open(path, ring); err == nil {
		t.Fatalf("expected ml-kem decapsulate error")
	}
}

func TestOpenRejectsCorruptEphemeralPub(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "v")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var env envelope
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &env)
	env.X25519EphPub = env.X25519EphPub[:5] // wrong length -> parse error
	bad, _ := json.Marshal(env)
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := Open(path, ring); err == nil {
		t.Fatalf("expected ephemeral-pub parse error")
	}
}

func TestSealNonceFailure(t *testing.T) {
	ring := newFakeKeyring()
	hk, err := ensureHybridKey(ring, stdSys{}, "")
	if err != nil {
		t.Fatalf("ensureHybridKey: %v", err)
	}
	// Fail the nonce draw: the ephemeral-scalar draw succeeds, so this exercises
	// the gcmSeal nonce branch specifically.
	calls := 0
	sys := fakeSys{randRead: func(b []byte) (int, error) {
		calls++
		if calls >= 2 {
			return 0, errors.New("no entropy")
		}
		return stdSys{}.RandRead(b)
	}}
	if _, err := seal(hk, []byte("x"), sys); err == nil {
		t.Fatalf("expected nonce generation error")
	}
}

func TestSealEphemeralRandFailure(t *testing.T) {
	ring := newFakeKeyring()
	hk, err := ensureHybridKey(ring, stdSys{}, "")
	if err != nil {
		t.Fatalf("ensureHybridKey: %v", err)
	}
	sys := fakeSys{randRead: func([]byte) (int, error) { return 0, errors.New("no entropy") }}
	if _, err := seal(hk, []byte("x"), sys); err == nil {
		t.Fatalf("expected ephemeral x25519 rand error")
	}
}

func TestSaveSealError(t *testing.T) {
	// Open with a working sys, then inject a rand-failing sys so Save's seal
	// step fails (covering Save's error propagation from seal).
	st, err := openWith(filepath.Join(t.TempDir(), "c.json"), newFakeKeyring(), fakeSys{})
	if err != nil {
		t.Fatalf("openWith: %v", err)
	}
	st.Set("id", "v")
	st.sys = fakeSys{randRead: func([]byte) (int, error) { return 0, errors.New("no entropy") }}
	if err := st.Save(); err == nil {
		t.Fatalf("expected Save to surface the seal error")
	}
}

func TestX25519DecapsulateLowOrderPoint(t *testing.T) {
	// An all-zero ephemeral public key is a low-order point: NewPublicKey accepts
	// it (length-only check) but ECDH fails closed, covering that error branch.
	ring := newFakeKeyring()
	hk, err := ensureHybridKey(ring, stdSys{}, "")
	if err != nil {
		t.Fatalf("ensureHybridKey: %v", err)
	}
	if _, err := x25519Decapsulate(hk, make([]byte, x25519ScalarLen)); err == nil {
		t.Fatalf("expected ECDH to reject an all-zero ephemeral point")
	}
}

func TestSaveAEADError(t *testing.T) {
	// NewAEAD failing covers gcmSeal's newGCM error path through Save.
	st, err := openWith(filepath.Join(t.TempDir(), "c.json"), newFakeKeyring(), fakeSys{})
	if err != nil {
		t.Fatalf("openWith: %v", err)
	}
	st.Set("id", "v")
	st.sys = fakeSys{newAEAD: func([]byte) (cipher.AEAD, error) { return nil, errors.New("aead failed") }}
	if err := st.Save(); err == nil {
		t.Fatalf("expected AEAD construction error to surface from Save")
	}
}

func TestOpenAEADError(t *testing.T) {
	// NewAEAD failing on reopen covers gcmOpen's newGCM error path.
	path := filepath.Join(t.TempDir(), "c.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "v")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sys := fakeSys{newAEAD: func([]byte) (cipher.AEAD, error) { return nil, errors.New("aead failed") }}
	if _, err := openWith(path, ring, sys); err == nil {
		t.Fatalf("expected AEAD construction error on open")
	}
}

func TestUnmarshalCredentialsEmpty(t *testing.T) {
	creds, err := unmarshalCredentials(nil)
	if err != nil || len(creds) != 0 {
		t.Fatalf("empty plaintext should yield empty map, got %v err=%v", creds, err)
	}
}

func TestUnmarshalCredentialsRejectsNonMap(t *testing.T) {
	if _, err := unmarshalCredentials([]byte(`["not","a","map"]`)); err == nil {
		t.Fatalf("expected parse error for a non-map payload")
	}
}

func TestNewGCMBadKey(t *testing.T) {
	if _, err := newGCM([]byte("short"), stdSys{}); !errors.Is(err, ErrBadSeedLength) {
		t.Fatalf("expected ErrBadSeedLength, got %v", err)
	}
}

func TestGCMOpenBadNonce(t *testing.T) {
	key := make([]byte, dataKeyLen)
	if _, err := gcmOpen(key, []byte{0x00}, []byte("ciphertext"), stdSys{}); !errors.Is(err, ErrBadNonceLength) {
		t.Fatalf("expected ErrBadNonceLength, got %v", err)
	}
}

func TestNewAEADErrorPropagates(t *testing.T) {
	// Fault-inject the AEAD construction to cover newGCM's delegated error path.
	sys := fakeSys{newAEAD: func([]byte) (cipher.AEAD, error) { return nil, errors.New("aead build failed") }}
	if _, err := newGCM(make([]byte, dataKeyLen), sys); err == nil {
		t.Fatalf("expected NewAEAD error to propagate")
	}
}

func TestSaveDeriveKeyError(t *testing.T) {
	// Fault-inject HKDF so seal's combineKey error branch is exercised via Save.
	st, err := openWith(filepath.Join(t.TempDir(), "c.json"), newFakeKeyring(), fakeSys{})
	if err != nil {
		t.Fatalf("openWith: %v", err)
	}
	st.Set("id", "v")
	st.sys = fakeSys{deriveKey: func([]byte) ([]byte, error) { return nil, errors.New("hkdf failed") }}
	if err := st.Save(); err == nil {
		t.Fatalf("expected derive-key error to surface from Save")
	}
}

func TestOpenDeriveKeyError(t *testing.T) {
	// Seal a real envelope, then reopen with an HKDF-failing sys to cover
	// openSealed's combineKey error branch.
	path := filepath.Join(t.TempDir(), "c.json")
	ring := newFakeKeyring()
	st, err := Open(path, ring)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Set("id", "v")
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sys := fakeSys{deriveKey: func([]byte) ([]byte, error) { return nil, errors.New("hkdf failed") }}
	if _, err := openWith(path, ring, sys); err == nil {
		t.Fatalf("expected derive-key error on open")
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

func TestEnsureHybridKey_ExistingStore_MissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	ring := newFakeKeyring() // empty → returns ErrKeyNotFound on Get
	_, err := ensureHybridKey(ring, stdSys{}, path)
	if !errors.Is(err, ErrKeyMissingExistingStore) {
		t.Fatalf("expected ErrKeyMissingExistingStore when store exists but key missing, got %v", err)
	}
	if ring.sets != 0 {
		t.Fatalf("must not mint a new key when encrypted store file exists: sets=%d", ring.sets)
	}
}

func TestEnsureHybridKey_TransientStatError_FailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	ring := newFakeKeyring() // empty → ErrKeyNotFound
	// Stat returns a non-ErrNotExist error (permission denied or I/O failure).
	permErr := &os.PathError{Op: "stat", Path: path, Err: os.ErrPermission}
	sys := fakeSys{stat: func(string) (fs.FileInfo, error) { return nil, permErr }}
	_, err := ensureHybridKey(ring, sys, path)
	if err == nil {
		t.Fatalf("expected fail-closed error on transient stat failure, got nil")
	}
	if errors.Is(err, ErrKeyMissingExistingStore) {
		t.Fatalf("transient stat error must not be reported as ErrKeyMissingExistingStore")
	}
	if ring.sets != 0 {
		t.Fatalf("must not mint when stat fails transiently: sets=%d", ring.sets)
	}
}

func TestEnsureHybridKey_MintsWhenFileAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json") // file does not exist
	ring := newFakeKeyring()
	hk, err := ensureHybridKey(ring, stdSys{}, path)
	if err != nil {
		t.Fatalf("expected minting to succeed when no store file exists: %v", err)
	}
	if hk.x25519 == nil || hk.mlkem == nil {
		t.Fatalf("expected fully reconstructed hybrid key")
	}
	if ring.sets != 1 {
		t.Fatalf("expected one keyring Set on first use, got %d", ring.sets)
	}
}

func TestReadCredentialsNonNotExistError(t *testing.T) {
	ring := newFakeKeyring()
	hk, err := ensureHybridKey(ring, stdSys{}, filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatalf("ensureHybridKey: %v", err)
	}
	sys := fakeSys{readFile: func(string) ([]byte, error) { return nil, errors.New("io error") }}
	if _, err := readCredentials(sys, "/any/path.json", hk); err == nil {
		t.Fatalf("expected non-not-exist read error to propagate")
	}
}

func TestReadCredentialsMalformedEnvelope(t *testing.T) {
	ring := newFakeKeyring()
	hk, err := ensureHybridKey(ring, stdSys{}, filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatalf("ensureHybridKey: %v", err)
	}
	sys := fakeSys{readFile: func(string) ([]byte, error) { return []byte("{bad json"), nil }}
	if _, err := readCredentials(sys, "/any/path.json", hk); err == nil {
		t.Fatalf("expected json parse error to propagate")
	}
}

func TestStdSysNewAEADBadKeyLength(t *testing.T) {
	sys := stdSys{}
	if _, err := sys.NewAEAD([]byte("short")); err == nil {
		t.Fatalf("expected error for invalid AES key length")
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
