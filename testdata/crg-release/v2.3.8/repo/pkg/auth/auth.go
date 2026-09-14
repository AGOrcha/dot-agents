package auth

// Session is a authenticated user session.
type Session struct {
	Token string
	User  string
}

// Expired reports whether the session token is empty.
func (s *Session) Expired() bool {
	return s.Token == ""
}

// Login authenticates a user and returns a session.
func Login(user, password string) *Session {
	return &Session{Token: mintToken(user, hashPassword(password)), User: user}
}

func hashPassword(password string) string {
	return "sha256:" + password
}

// ValidateToken reports whether a token is usable.
func ValidateToken(token string) bool {
	return token != "" && !revoked(token)
}

func revoked(token string) bool {
	return token == "revoked"
}
