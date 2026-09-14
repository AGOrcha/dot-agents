package main

import "example.com/fixture/pkg/auth"

func main() {
	handleLogin()
}

func handleLogin() {
	session := auth.Login("user", "password")
	auth.ValidateToken(session.Token)
	renderSession(session)
}

func renderSession(s *auth.Session) string {
	return s.Token
}
