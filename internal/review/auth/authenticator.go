package auth

import (
	"errors"
)

// Identity is the resolved principal behind a presented token. It is what an
// Authenticator returns on success and what downstream handlers consult for
// authorization decisions.
type Identity struct {
	Email string
	Role  Role
}

// Can reports whether the identity's role grants the given permission.
func (id Identity) Can(p Permission) bool {
	return id.Role.Can(p)
}

// ErrUnauthenticated is returned by Authenticate when the token is missing,
// malformed, or matches no user. Callers map this to a 401.
var ErrUnauthenticated = errors.New("auth: unauthenticated")

// Authenticator resolves a presented bearer token to an Identity. It is the
// pluggable seam that lets an OIDC backend replace the local users file later
// (design D5.3) without changing handler code.
type Authenticator interface {
	// Authenticate resolves a plaintext bearer token to an Identity. It returns
	// ErrUnauthenticated when the token does not correspond to a known user. Any
	// other error indicates an operational failure (e.g. unreadable backing
	// store) and should not be conflated with a 401.
	Authenticate(token string) (Identity, error)
}

// LocalUsersAuthenticator authenticates bearer tokens against a users file on
// disk. The file is reloaded on each call so out-of-band CLI edits (user add /
// remove) take effect without a service restart.
type LocalUsersAuthenticator struct {
	path string
	// load is the users-file loader, injectable for testing. Defaults to
	// LoadUsersFile.
	load func(path string) (*UsersFile, error)
}

// NewLocalUsersAuthenticator constructs an authenticator backed by the users
// file at path.
func NewLocalUsersAuthenticator(path string) *LocalUsersAuthenticator {
	return &LocalUsersAuthenticator{path: path, load: LoadUsersFile}
}

// Authenticate implements Authenticator. It reloads the users file, then scans
// for the user whose stored hash matches the presented token. Because the
// plaintext token does not encode the owning user, every candidate hash is
// compared; a malformed or unmatched token yields ErrUnauthenticated.
func (a *LocalUsersAuthenticator) Authenticate(token string) (Identity, error) {
	if validateTokenShape(token) != nil {
		return Identity{}, ErrUnauthenticated
	}
	load := a.load
	if load == nil {
		load = LoadUsersFile
	}
	uf, err := load(a.path)
	if err != nil {
		return Identity{}, err
	}
	for _, u := range uf.Users {
		ok, verr := VerifyToken(token, u.TokenHash)
		if verr != nil {
			// A corrupt stored hash is an operational fault for that row; skip
			// it rather than failing the whole authentication so one bad row
			// cannot lock everyone out.
			continue
		}
		if ok {
			return Identity{Email: u.Email, Role: u.Role}, nil
		}
	}
	return Identity{}, ErrUnauthenticated
}
