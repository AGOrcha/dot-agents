package auth

// mintToken derives a session token from a user and password digest.
func mintToken(user, digest string) string {
	return user + ":" + digest
}
