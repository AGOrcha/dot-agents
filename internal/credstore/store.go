// Package credstore implements the shared encrypted credential store and the
// CI-aware loader described in external-agent-sources design §4.1.
//
// The local store keeps secrets in an encrypted file at
// ~/.config/da/credentials.json. A per-host data key is generated on first use
// and stored in the OS keychain via a credential helper (macOS Keychain /
// Windows Credential Manager / Linux Secret Service); the file is sealed with
// AES-256-GCM using that key. Call sites address credentials by id and never
// see the raw key — keychain access lives behind the Keyring seam so tests can
// inject a fake and never touch the real platform store.
package credstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// dataKeyLen is the AES-256 key length in bytes.
const dataKeyLen = 32

// errPrefix tags every error this package returns.
const errPrefix = "credstore"

// storeSchemaVersion is the current on-disk credentials-file schema version.
const storeSchemaVersion = 1

// keyringService is the account/service name under which the data key lives in
// the OS keychain.
const keyringService = "da-credstore-datakey"

var (
	// ErrCredentialNotFound is returned when no credential matches an id.
	ErrCredentialNotFound = errors.New(errPrefix + ": credential not found")
	// ErrBadKeyLength is returned when the keyring yields a key of the wrong
	// size for AES-256-GCM (corrupt or foreign keychain entry).
	ErrBadKeyLength = errors.New(errPrefix + ": data key has wrong length")
	// ErrCiphertextTooShort is returned when a sealed file is shorter than the
	// GCM nonce it must carry, i.e. it is truncated or not a credstore blob.
	ErrCiphertextTooShort = errors.New(errPrefix + ": ciphertext shorter than nonce")
	// ErrKeyNotFound is the sentinel a Keyring returns (directly or wrapped)
	// when a key is absent, so EnsureDataKey mints a fresh one rather than fail.
	ErrKeyNotFound = errors.New(errPrefix + ": keyring key not found")
)

// Keyring is the seam over the OS credential helper. Production wires it to the
// platform keychain; tests inject a fake so they never touch the real store.
type Keyring interface {
	// Get returns the secret stored under key. It returns ErrKeyNotFound (or a
	// wrapped error satisfying errors.Is) when the key is absent.
	Get(key string) ([]byte, error)
	// Set stores secret under key, overwriting any existing value.
	Set(key string, secret []byte) error
}

// tempFile is the subset of *os.File the atomic write relies on, narrowed so a
// fake can force the chmod/write/close error branches in tests.
type tempFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Close() error
}

// sysShim is the narrow OS collaborator the store and loader depend on, per the
// interface-DI convention in docs/TEST_SEAMS.md. Production injects stdSys;
// tests inject a fake whose nil func-fields delegate to the real call, so a
// single otherwise-unreachable error branch (entropy/temp-file/rename failure)
// is fault-injectable without package-level func-var seams.
type sysShim interface {
	envLookup
	RandRead(b []byte) (int, error)
	MkdirAll(path string, perm os.FileMode) error
	CreateTemp(dir, pattern string) (tempFile, error)
	Rename(oldpath, newpath string) error
	Remove(name string) error
	ReadFile(name string) ([]byte, error)
	Stat(name string) (fs.FileInfo, error)
	HomeDir() (string, error)
}

// stdSys is the production sysShim backed by the real os/crypto-rand/config
// calls. It is the default everywhere a sysShim is not explicitly injected.
type stdSys struct{}

func (stdSys) RandRead(b []byte) (int, error)               { return rand.Read(b) }
func (stdSys) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (stdSys) Rename(oldpath, newpath string) error         { return os.Rename(oldpath, newpath) }
func (stdSys) Remove(name string) error                     { return os.Remove(name) }
func (stdSys) ReadFile(name string) ([]byte, error)         { return os.ReadFile(name) }
func (stdSys) Stat(name string) (fs.FileInfo, error)        { return os.Stat(name) }
func (stdSys) HomeDir() (string, error)                     { return config.UserHomeDir() }
func (stdSys) CreateTemp(dir, pattern string) (tempFile, error) {
	return os.CreateTemp(dir, pattern)
}

// storeFile is the on-disk shape of the encrypted credentials file. Only
// Sealed (the GCM nonce + ciphertext) is ever written; the plaintext map of
// credentials lives in memory after Open decrypts it.
type storeFile struct {
	SchemaVersion int    `json:"schema_version"`
	Sealed        []byte `json:"sealed"`
}

// Store is an opened, decrypted credential store. Mutations are persisted with
// Save, which re-seals the whole map.
type Store struct {
	path        string
	key         []byte
	credentials map[string]string
	sys         sysShim
}

// DefaultPath returns ~/.config/da/credentials.json, honoring XDG_CONFIG_HOME
// first so the store lands in the same local-secrets home as review auth state
// (never in the git-synced AGENTS_HOME tree).
func DefaultPath() (string, error) { return defaultPath(stdSys{}) }

// defaultPath is DefaultPath with an injected sysShim for hermetic tests.
func defaultPath(sys sysShim) (string, error) {
	if cfg := os.Getenv("XDG_CONFIG_HOME"); cfg != "" {
		return filepath.Join(cfg, "da", "credentials.json"), nil
	}
	home, err := sys.HomeDir()
	if err != nil {
		return "", fmt.Errorf("%s: resolve home dir: %w", errPrefix, err)
	}
	return filepath.Join(home, ".config", "da", "credentials.json"), nil
}

// EnsureDataKey returns the per-host AES-256 data key, minting and persisting a
// fresh one in the keyring on first use. The key never leaves process memory
// except sealed inside the keyring.
func EnsureDataKey(ring Keyring) ([]byte, error) { return ensureDataKey(ring, stdSys{}) }

// ensureDataKey is EnsureDataKey with an injected sysShim (for the rand seam).
func ensureDataKey(ring Keyring, sys sysShim) ([]byte, error) {
	key, err := ring.Get(keyringService)
	if err == nil {
		if len(key) != dataKeyLen {
			return nil, ErrBadKeyLength
		}
		return key, nil
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return nil, fmt.Errorf("%s: read data key: %w", errPrefix, err)
	}
	return mintDataKey(ring, sys)
}

// mintDataKey generates a fresh data key and persists it in the keyring.
func mintDataKey(ring Keyring, sys sysShim) ([]byte, error) {
	fresh := make([]byte, dataKeyLen)
	if _, err := sys.RandRead(fresh); err != nil {
		return nil, fmt.Errorf("%s: generate data key: %w", errPrefix, err)
	}
	if err := ring.Set(keyringService, fresh); err != nil {
		return nil, fmt.Errorf("%s: store data key: %w", errPrefix, err)
	}
	return fresh, nil
}

// Open reads and decrypts the store at path, minting the data key via ring on
// first use. A missing file yields an empty store so first-run callers can Set
// without a separate init step.
func Open(path string, ring Keyring) (*Store, error) { return openWith(path, ring, stdSys{}) }

// openWith is Open with an injected sysShim for hermetic tests.
func openWith(path string, ring Keyring, sys sysShim) (*Store, error) {
	key, err := ensureDataKey(ring, sys)
	if err != nil {
		return nil, err
	}
	creds, err := readCredentials(sys, path, key)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, key: key, credentials: creds, sys: sys}, nil
}

// readCredentials loads and decrypts the credential map, returning an empty map
// when the file does not yet exist.
func readCredentials(sys sysShim, path string, key []byte) (map[string]string, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("%s: read store %s: %w", errPrefix, path, err)
	}
	var sf storeFile
	if uerr := json.Unmarshal(data, &sf); uerr != nil {
		return nil, fmt.Errorf("%s: parse store %s: %w", errPrefix, path, uerr)
	}
	plain, err := decrypt(key, sf.Sealed)
	if err != nil {
		return nil, err
	}
	return unmarshalCredentials(plain)
}

// unmarshalCredentials decodes the decrypted credential map; empty plaintext
// (a never-written store) yields an empty map.
func unmarshalCredentials(plain []byte) (map[string]string, error) {
	creds := map[string]string{}
	if len(plain) == 0 {
		return creds, nil
	}
	if err := json.Unmarshal(plain, &creds); err != nil {
		return nil, fmt.Errorf("%s: parse decrypted credentials: %w", errPrefix, err)
	}
	return creds, nil
}

// Get returns the credential stored under id, or ErrCredentialNotFound.
func (s *Store) Get(id string) (string, error) {
	secret, ok := s.credentials[id]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrCredentialNotFound, id)
	}
	return secret, nil
}

// Set records secret under id in memory; call Save to persist.
func (s *Store) Set(id, secret string) {
	s.credentials[id] = secret
}

// Delete removes the credential under id in memory; call Save to persist.
func (s *Store) Delete(id string) {
	delete(s.credentials, id)
}

// Save re-seals the credential map and writes it atomically (temp file +
// rename) with 0600 perms because it holds secrets.
func (s *Store) Save() error {
	plain, err := json.Marshal(s.credentials)
	if err != nil {
		return fmt.Errorf("%s: marshal credentials: %w", errPrefix, err)
	}
	sealed, err := encrypt(s.key, plain, s.sys)
	if err != nil {
		return err
	}
	data, err := json.Marshal(storeFile{SchemaVersion: storeSchemaVersion, Sealed: sealed})
	if err != nil {
		return fmt.Errorf("%s: marshal store file: %w", errPrefix, err)
	}
	return writeFileAtomic(s.sys, s.path, data)
}

// writeFileAtomic writes data to path via a temp file in the same directory
// then renames, so concurrent readers never observe a partial file.
func writeFileAtomic(sys sysShim, path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := sys.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("%s: create store dir: %w", errPrefix, err)
	}
	tmp, err := sys.CreateTemp(dir, ".credentials-*.json.tmp")
	if err != nil {
		return fmt.Errorf("%s: temp store file: %w", errPrefix, err)
	}
	tmpPath := tmp.Name()
	if err := finishTempWrite(tmp, data); err != nil {
		_ = sys.Remove(tmpPath)
		return err
	}
	if err := sys.Rename(tmpPath, path); err != nil {
		_ = sys.Remove(tmpPath)
		return fmt.Errorf("%s: rename store file: %w", errPrefix, err)
	}
	return nil
}

// finishTempWrite chmods, writes, and closes tmp, returning the first error.
func finishTempWrite(tmp tempFile, data []byte) error {
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%s: chmod temp store file: %w", errPrefix, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%s: write temp store file: %w", errPrefix, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%s: close temp store file: %w", errPrefix, err)
	}
	return nil
}

// newGCM builds the AES-256-GCM AEAD from key, validating its length first.
func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != dataKeyLen {
		return nil, ErrBadKeyLength
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%s: init cipher: %w", errPrefix, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%s: init gcm: %w", errPrefix, err)
	}
	return gcm, nil
}

// encrypt seals plain with AES-256-GCM, returning nonce||ciphertext.
func encrypt(key, plain []byte, sys sysShim) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, rerr := sys.RandRead(nonce); rerr != nil {
		return nil, fmt.Errorf("%s: generate nonce: %w", errPrefix, rerr)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// decrypt opens a nonce||ciphertext blob sealed by encrypt. An empty blob
// (never-written store) decrypts to empty plaintext.
func decrypt(key, sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, nil
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, ErrCiphertextTooShort
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: decrypt credentials: %w", errPrefix, err)
	}
	return plain, nil
}
