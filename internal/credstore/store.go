// Package credstore implements the shared encrypted credential store and the
// CI-aware loader described in external-agent-sources design §4.1.
//
// The local store keeps secrets in an encrypted file at
// ~/.config/da/credentials.json. On first use a hybrid post-quantum recipient
// keypair (X25519 + ML-KEM-768) is generated; its private seed material lives in
// the OS keychain via a credential helper (macOS Keychain / Windows Credential
// Manager / Linux Secret Service) behind the Keyring seam. The file is sealed
// with AES-256-GCM under a key derived from a hybrid KEM so the data stays
// confidential if EITHER the classical (X25519) or the post-quantum (ML-KEM)
// primitive remains unbroken. Call sites address credentials by id and never
// see the raw key; tests inject a fake Keyring and never touch the real store.
package credstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

const (
	// dataKeyLen is the AES-256 key length in bytes.
	dataKeyLen = 32
	// errPrefix tags every error this package returns.
	errPrefix = "credstore"
	// formatVersion is the on-disk envelope version. v2 is the hybrid-KEM
	// envelope; v1 (single AES key) is intentionally unsupported — #219 has not
	// shipped, so there is no v1 data to migrate.
	formatVersion = 2
	// keyringService is the account/service name under which the hybrid private
	// seed blob lives in the OS keychain.
	keyringService = "da-credstore-hybridkey"
	// hkdfInfo is the domain-separation label binding a derived key to this
	// scheme and version, so a key derived here cannot be confused with one from
	// any other context or future format.
	hkdfInfo = "da-credstore-hybrid-v2"
	// x25519ScalarLen is the X25519 private scalar length in bytes.
	x25519ScalarLen = 32
	// seedBlobLen is the keyring seed blob: x25519 scalar || ml-kem seed.
	seedBlobLen = x25519ScalarLen + mlkem.SeedSize
)

var (
	// ErrCredentialNotFound is returned when no credential matches an id.
	ErrCredentialNotFound = errors.New(errPrefix + ": credential not found")
	// ErrBadSeedLength is returned when the keyring yields a seed blob of the
	// wrong size (corrupt or foreign keychain entry).
	ErrBadSeedLength = errors.New(errPrefix + ": key seed has wrong length")
	// ErrBadFormatVersion is returned when the on-disk envelope is not a format
	// version this build can decrypt.
	ErrBadFormatVersion = errors.New(errPrefix + ": unsupported envelope format version")
	// ErrBadNonceLength is returned when the envelope's GCM nonce is not the
	// AEAD nonce size, i.e. the file is truncated or not a credstore envelope.
	ErrBadNonceLength = errors.New(errPrefix + ": gcm nonce has wrong length")
	// ErrKeyNotFound is the sentinel a Keyring returns (directly or wrapped)
	// when a key is absent, so the store mints a fresh one rather than fail.
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
	RandRead(b []byte) (int, error)
	MkdirAll(path string, perm os.FileMode) error
	CreateTemp(dir, pattern string) (tempFile, error)
	Rename(oldpath, newpath string) error
	Remove(name string) error
	ReadFile(name string) ([]byte, error)
	Stat(name string) (fs.FileInfo, error)
	HomeDir() (string, error)
	// LookupEnv mirrors os.LookupEnv (value, set) for the loader's env steps.
	LookupEnv(key string) (string, bool)
	// DeriveKey runs the HKDF combine; seamed so its defensive error branch
	// (unreachable with the fixed SHA-256/length parameters) is provable.
	DeriveKey(secret []byte) ([]byte, error)
	// NewAEAD builds the AES-256-GCM AEAD; seamed so the cipher-construction
	// error branches (unreachable with a valid 32-byte key) are provable.
	NewAEAD(key []byte) (cipher.AEAD, error)
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

func (stdSys) DeriveKey(secret []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, secret, nil, hkdfInfo, dataKeyLen)
}

func (stdSys) NewAEAD(key []byte) (cipher.AEAD, error) {
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

// hybridKey is the recipient's reconstructed private key material: the X25519
// private key and the ML-KEM-768 decapsulation key, both rebuilt from the
// keyring seed blob. It never leaves process memory.
type hybridKey struct {
	x25519 *ecdh.PrivateKey
	mlkem  *mlkem.DecapsulationKey768
}

// envelope is the self-describing on-disk sealed form. All byte fields are
// base64-encoded by encoding/json. Only ciphertext/public material is stored;
// the plaintext map lives in memory after openSealed decrypts it.
type envelope struct {
	FormatVersion   int    `json:"format_version"`
	X25519EphPub    []byte `json:"x25519_ephemeral_pub"`
	MLKEMCiphertext []byte `json:"mlkem_ciphertext"`
	GCMNonce        []byte `json:"gcm_nonce"`
	GCMCiphertext   []byte `json:"gcm_ciphertext"`
}

// Store is an opened, decrypted credential store. Mutations are persisted with
// Save, which re-seals the whole map under the hybrid KEM.
type Store struct {
	path        string
	hk          *hybridKey
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

// ensureHybridKey returns the recipient hybrid key, minting and persisting a
// fresh seed blob in the keyring on first use.
func ensureHybridKey(ring Keyring, sys sysShim) (*hybridKey, error) {
	seed, err := ring.Get(keyringService)
	if err == nil {
		return hybridKeyFromSeed(seed)
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return nil, fmt.Errorf("%s: read key seed: %w", errPrefix, err)
	}
	return mintHybridKey(ring, sys)
}

// mintHybridKey generates a fresh hybrid keypair and persists its seed blob.
func mintHybridKey(ring Keyring, sys sysShim) (*hybridKey, error) {
	xScalar := make([]byte, x25519ScalarLen)
	if _, err := sys.RandRead(xScalar); err != nil {
		return nil, fmt.Errorf("%s: generate x25519 seed: %w", errPrefix, err)
	}
	mlSeed := make([]byte, mlkem.SeedSize)
	if _, err := sys.RandRead(mlSeed); err != nil {
		return nil, fmt.Errorf("%s: generate ml-kem seed: %w", errPrefix, err)
	}
	seed := make([]byte, 0, seedBlobLen)
	seed = append(append(seed, xScalar...), mlSeed...)
	hk, err := hybridKeyFromSeed(seed)
	if err != nil {
		return nil, err
	}
	if err := ring.Set(keyringService, seed); err != nil {
		return nil, fmt.Errorf("%s: store key seed: %w", errPrefix, err)
	}
	return hk, nil
}

// hybridKeyFromSeed reconstructs both private keys from a seed blob.
func hybridKeyFromSeed(seed []byte) (*hybridKey, error) {
	if len(seed) != seedBlobLen {
		return nil, ErrBadSeedLength
	}
	xPriv, err := ecdh.X25519().NewPrivateKey(seed[:x25519ScalarLen])
	if err != nil {
		return nil, fmt.Errorf("%s: rebuild x25519 key: %w", errPrefix, err)
	}
	mlDecap, err := mlkem.NewDecapsulationKey768(seed[x25519ScalarLen:])
	if err != nil {
		return nil, fmt.Errorf("%s: rebuild ml-kem key: %w", errPrefix, err)
	}
	return &hybridKey{x25519: xPriv, mlkem: mlDecap}, nil
}

// Open reads and decrypts the store at path, minting the hybrid key via ring on
// first use. A missing file yields an empty store so first-run callers can Set
// without a separate init step.
func Open(path string, ring Keyring) (*Store, error) { return openWith(path, ring, stdSys{}) }

// openWith is Open with an injected sysShim for hermetic tests.
func openWith(path string, ring Keyring, sys sysShim) (*Store, error) {
	hk, err := ensureHybridKey(ring, sys)
	if err != nil {
		return nil, err
	}
	creds, err := readCredentials(sys, path, hk)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, hk: hk, credentials: creds, sys: sys}, nil
}

// readCredentials loads and decrypts the credential map, returning an empty map
// when the file does not yet exist.
func readCredentials(sys sysShim, path string, hk *hybridKey) (map[string]string, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("%s: read store %s: %w", errPrefix, path, err)
	}
	var env envelope
	if uerr := json.Unmarshal(data, &env); uerr != nil {
		return nil, fmt.Errorf("%s: parse store %s: %w", errPrefix, path, uerr)
	}
	plain, err := openSealed(hk, &env, sys)
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

// Save re-seals the credential map under the hybrid KEM and writes it
// atomically (temp file + rename) with 0600 perms because it holds secrets.
func (s *Store) Save() error {
	plain, err := json.Marshal(s.credentials)
	if err != nil {
		return fmt.Errorf("%s: marshal credentials: %w", errPrefix, err)
	}
	env, err := seal(s.hk, plain, s.sys)
	if err != nil {
		return err
	}
	data, err := json.Marshal(env)
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

// seal encrypts plain to the recipient hybrid public key, producing a versioned
// envelope. The AES key is derived from BOTH the ML-KEM and X25519 shared
// secrets, so confidentiality holds if either primitive is unbroken. The
// derived key and shared secrets are zeroed after use.
func seal(hk *hybridKey, plain []byte, sys sysShim) (*envelope, error) {
	mlSS, mlCT := hk.mlkem.EncapsulationKey().Encapsulate()
	defer zero(mlSS)
	ephPub, xSS, err := x25519Encapsulate(hk, sys)
	if err != nil {
		return nil, err
	}
	defer zero(xSS)
	key, err := combineKey(xSS, mlSS, sys)
	if err != nil {
		return nil, err
	}
	defer zero(key)
	nonce, ciphertext, err := gcmSeal(key, plain, sys)
	if err != nil {
		return nil, err
	}
	return &envelope{
		FormatVersion:   formatVersion,
		X25519EphPub:    ephPub,
		MLKEMCiphertext: mlCT,
		GCMNonce:        nonce,
		GCMCiphertext:   ciphertext,
	}, nil
}

// openSealed reverses seal: it rebuilds both shared secrets from the recipient
// private keys, re-derives the AES key, and AEAD-opens the ciphertext (which
// fails closed on tamper or wrong key). Derived material is zeroed after use.
func openSealed(hk *hybridKey, env *envelope, sys sysShim) ([]byte, error) {
	if env.FormatVersion != formatVersion {
		return nil, fmt.Errorf("%w: %d", ErrBadFormatVersion, env.FormatVersion)
	}
	mlSS, err := hk.mlkem.Decapsulate(env.MLKEMCiphertext)
	if err != nil {
		return nil, fmt.Errorf("%s: ml-kem decapsulate: %w", errPrefix, err)
	}
	defer zero(mlSS)
	xSS, err := x25519Decapsulate(hk, env.X25519EphPub)
	if err != nil {
		return nil, err
	}
	defer zero(xSS)
	key, err := combineKey(xSS, mlSS, sys)
	if err != nil {
		return nil, err
	}
	defer zero(key)
	return gcmOpen(key, env.GCMNonce, env.GCMCiphertext, sys)
}

// x25519Encapsulate generates an ephemeral X25519 keypair and runs ECDH against
// the recipient public key, returning the ephemeral public key and the secret.
func x25519Encapsulate(hk *hybridKey, sys sysShim) (ephPub, sharedSecret []byte, err error) {
	scalar := make([]byte, x25519ScalarLen)
	if _, rerr := sys.RandRead(scalar); rerr != nil {
		return nil, nil, fmt.Errorf("%s: generate ephemeral x25519 key: %w", errPrefix, rerr)
	}
	defer zero(scalar)
	eph, err := ecdh.X25519().NewPrivateKey(scalar)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: build ephemeral x25519 key: %w", errPrefix, err)
	}
	ss, err := eph.ECDH(hk.x25519.PublicKey())
	if err != nil {
		return nil, nil, fmt.Errorf("%s: x25519 ecdh: %w", errPrefix, err)
	}
	return eph.PublicKey().Bytes(), ss, nil
}

// x25519Decapsulate runs ECDH between the recipient private key and the sealed
// ephemeral public key, recovering the X25519 shared secret.
func x25519Decapsulate(hk *hybridKey, ephPubBytes []byte) ([]byte, error) {
	ephPub, err := ecdh.X25519().NewPublicKey(ephPubBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: parse ephemeral x25519 key: %w", errPrefix, err)
	}
	ss, err := hk.x25519.ECDH(ephPub)
	if err != nil {
		return nil, fmt.Errorf("%s: x25519 ecdh: %w", errPrefix, err)
	}
	return ss, nil
}

// combineKey derives the AES-256 key from the concatenated X25519 and ML-KEM
// shared secrets via HKDF-SHA256, domain-separated by hkdfInfo (see
// stdSys.DeriveKey). The error branch is unreachable with the fixed parameters
// but provable through the seam.
func combineKey(x25519SS, mlkemSS []byte, sys sysShim) ([]byte, error) {
	secret := make([]byte, 0, len(x25519SS)+len(mlkemSS))
	secret = append(append(secret, x25519SS...), mlkemSS...)
	defer zero(secret)
	key, err := sys.DeriveKey(secret)
	if err != nil {
		return nil, fmt.Errorf("%s: derive key: %w", errPrefix, err)
	}
	return key, nil
}

// gcmSeal AES-256-GCM encrypts plain under key with a fresh random nonce.
func gcmSeal(key, plain []byte, sys sysShim) (nonce, ciphertext []byte, err error) {
	gcm, err := newGCM(key, sys)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, rerr := sys.RandRead(nonce); rerr != nil {
		return nil, nil, fmt.Errorf("%s: generate nonce: %w", errPrefix, rerr)
	}
	return nonce, gcm.Seal(nil, nonce, plain, nil), nil
}

// gcmOpen AES-256-GCM decrypts ciphertext under key; AEAD fails closed on any
// tamper or wrong key.
func gcmOpen(key, nonce, ciphertext []byte, sys sysShim) ([]byte, error) {
	gcm, err := newGCM(key, sys)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, ErrBadNonceLength
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: decrypt credentials: %w", errPrefix, err)
	}
	return plain, nil
}

// newGCM builds the AES-256-GCM AEAD from key, validating its length first then
// delegating construction to the seam.
func newGCM(key []byte, sys sysShim) (cipher.AEAD, error) {
	if len(key) != dataKeyLen {
		return nil, ErrBadSeedLength
	}
	return sys.NewAEAD(key)
}

// zero best-effort wipes sensitive bytes after use.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
