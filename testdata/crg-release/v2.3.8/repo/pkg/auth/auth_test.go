package auth

import "testing"

func TestLogin(t *testing.T) {
	if Login("a", "b") == nil {
		t.Fatal("expected session")
	}
}

func TestValidateToken(t *testing.T) {
	if ValidateToken("") {
		t.Fatal("expected invalid")
	}
}
