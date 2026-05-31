package credstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// envPrefix is the per-id environment variable prefix the CI-default resolution
// step reads (DA_CREDENTIAL_<ID>).
const envPrefix = "DA_CREDENTIAL_"

// envFileVar names the env var pointing at an ephemeral plaintext credentials
// file a CI job writes from a secret and removes in always()/trap.
const envFileVar = "DA_CREDENTIALS_FILE"

// ErrNotImplemented is returned by the stub OIDC resolver until the
// workload-identity resolver lands. It is also the marker the loader uses to
// fall through a resolver that has nothing for a given id.
var ErrNotImplemented = errors.New(errPrefix + ": resolver not implemented")

// OIDCResolver is the pluggable last-resort resolution step: mint a short-lived
// token from the runner's OIDC/workload-identity JWT, with no static secret.
// The stub returns ErrNotImplemented; a real resolver is wired later.
type OIDCResolver interface {
	// Resolve returns the credential for id, or an error. Returning
	// ErrCredentialNotFound (or ErrNotImplemented) lets the loader report a
	// clean miss rather than a hard failure.
	Resolve(id string) (string, error)
}

// stubOIDCResolver always reports "not implemented" so the resolution chain has
// a typed terminal step today without minting anything.
type stubOIDCResolver struct{}

// Resolve implements OIDCResolver for the stub: nothing is minted yet.
func (stubOIDCResolver) Resolve(id string) (string, error) {
	return "", fmt.Errorf("%w: id %q", ErrNotImplemented, id)
}

// StubOIDCResolver returns the not-yet-implemented resolver used until the
// workload-identity resolver is built.
func StubOIDCResolver() OIDCResolver { return stubOIDCResolver{} }

// Loader resolves credentials by id using a first-hit-wins chain (design §4.1):
//
//  1. DA_CREDENTIAL_<id> env (CI default: in-memory, no disk, no keychain)
//  2. DA_CREDENTIALS_FILE (ephemeral plaintext a CI job writes then removes)
//  3. the encrypted store (keychain-unwrapped, local dev)
//  4. a pluggable OIDC/workload-identity resolver (stub for now)
//
// Raw secrets are never logged; only ids and the winning source appear in any
// error or diagnostic.
type Loader struct {
	keyring   Keyring
	storePath string
	resolver  OIDCResolver
	// lookupEnv / readFile are seams so the env and plaintext-file steps are
	// hermetic in tests without touching the real process environment or disk.
	lookupEnv func(string) (string, bool)
	readFile  func(string) ([]byte, error)
	openStore func(path string, ring Keyring) (*Store, error)
}

// LoaderOption customizes a Loader at construction.
type LoaderOption func(*Loader)

// WithKeyring sets the Keyring used by the encrypted-store step.
func WithKeyring(ring Keyring) LoaderOption {
	return func(l *Loader) { l.keyring = ring }
}

// WithStorePath overrides the encrypted-store path (defaults to DefaultPath()).
func WithStorePath(path string) LoaderOption {
	return func(l *Loader) { l.storePath = path }
}

// WithResolver sets the pluggable OIDC/workload-identity resolver.
func WithResolver(r OIDCResolver) LoaderOption {
	return func(l *Loader) { l.resolver = r }
}

// NewLoader builds a Loader with production seams (real env and filesystem) and
// the stub OIDC resolver, then applies opts. When no store path is supplied the
// encrypted-store step is skipped unless a keyring is provided and DefaultPath
// resolves; callers in CI typically rely on the env steps alone.
func NewLoader(opts ...LoaderOption) *Loader {
	l := &Loader{
		resolver:  StubOIDCResolver(),
		lookupEnv: os.LookupEnv,
		readFile:  os.ReadFile,
		openStore: Open,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Resolve returns the credential for id, walking the resolution chain in order
// and returning the first hit. ErrCredentialNotFound is returned only when no
// step yields a value.
func (l *Loader) Resolve(id string) (string, error) {
	for _, step := range l.steps() {
		secret, ok, err := step(id)
		if err != nil {
			return "", err
		}
		if ok {
			return secret, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrCredentialNotFound, id)
}

// resolveStep yields (secret, found, hardError). found=false with nil error
// means "miss, try the next step"; a non-nil error aborts the chain.
type resolveStep func(id string) (string, bool, error)

// steps returns the ordered resolution chain.
func (l *Loader) steps() []resolveStep {
	return []resolveStep{
		l.fromEnv,
		l.fromPlaintextFile,
		l.fromStore,
		l.fromResolver,
	}
}

// fromEnv reads DA_CREDENTIAL_<ID> from the environment (step 1).
func (l *Loader) fromEnv(id string) (string, bool, error) {
	if v, ok := l.lookupEnv(envPrefix + envKey(id)); ok {
		return v, true, nil
	}
	return "", false, nil
}

// fromPlaintextFile reads id out of the DA_CREDENTIALS_FILE plaintext map when
// that var is set (step 2). A pointed-at file that cannot be read is a hard
// error so a misconfigured CI job fails loudly instead of falling through.
func (l *Loader) fromPlaintextFile(id string) (string, bool, error) {
	path, ok := l.lookupEnv(envFileVar)
	if !ok || path == "" {
		return "", false, nil
	}
	creds, err := readPlaintextFile(l.readFile, path)
	if err != nil {
		return "", false, err
	}
	if v, found := creds[id]; found {
		return v, true, nil
	}
	return "", false, nil
}

// fromStore unwraps the encrypted store via the keychain seam (step 3). It is
// skipped when neither a keyring nor a resolvable store path is configured.
func (l *Loader) fromStore(id string) (string, bool, error) {
	if l.keyring == nil {
		return "", false, nil
	}
	path, err := l.resolveStorePath()
	if err != nil {
		return "", false, err
	}
	store, err := l.openStore(path, l.keyring)
	if err != nil {
		return "", false, err
	}
	// Store.Get only ever reports ErrCredentialNotFound, so any error here is a
	// clean miss that lets the chain advance to the resolver step.
	secret, err := store.Get(id)
	if err != nil {
		return "", false, nil
	}
	return secret, true, nil
}

// fromResolver delegates to the pluggable OIDC resolver (step 4). A
// not-implemented or not-found result is treated as a clean miss.
func (l *Loader) fromResolver(id string) (string, bool, error) {
	if l.resolver == nil {
		return "", false, nil
	}
	secret, err := l.resolver.Resolve(id)
	if err != nil {
		if errors.Is(err, ErrNotImplemented) || errors.Is(err, ErrCredentialNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return secret, true, nil
}

// resolveStorePath returns the configured store path or DefaultPath().
func (l *Loader) resolveStorePath() (string, error) {
	if l.storePath != "" {
		return l.storePath, nil
	}
	return DefaultPath()
}

// readPlaintextFile loads a JSON map of id->secret from a plaintext CI file.
func readPlaintextFile(read func(string) ([]byte, error), path string) (map[string]string, error) {
	data, err := read(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("%s: read plaintext credentials file: %w", errPrefix, err)
	}
	return parsePlaintextCredentials(data)
}

// parsePlaintextCredentials decodes a JSON id->secret map from a CI plaintext
// file. An empty file yields an empty map so a present-but-blank file is a clean
// miss rather than a parse failure.
func parsePlaintextCredentials(data []byte) (map[string]string, error) {
	creds := map[string]string{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return creds, nil
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("%s: parse plaintext credentials file: %w", errPrefix, err)
	}
	return creds, nil
}

// envKey normalizes a credential id into the upper-snake form used in
// DA_CREDENTIAL_<KEY>: non-alphanumeric runs collapse to a single underscore.
func envKey(id string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range id {
		if isEnvWordChar(r) {
			b.WriteRune(toUpperASCII(r))
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// isEnvWordChar reports whether r is kept verbatim in an env-var key.
func isEnvWordChar(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	case r >= '0' && r <= '9':
		return true
	default:
		return false
	}
}

// toUpperASCII upper-cases an ASCII letter, leaving other runes unchanged.
func toUpperASCII(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}
