//go:build !darwin

package credstore

// NewOSKeyring returns nil on platforms without a supported OS keyring
// implementation. The Loader treats a nil keyring as "skip the encrypted-store
// step" and falls through to env-var and OIDC resolver steps, so CI and
// non-macOS dev environments continue to work unchanged.
func NewOSKeyring() Keyring { return nil }
