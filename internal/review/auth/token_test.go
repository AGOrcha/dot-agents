package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestGenerateTokenShapeAndUniqueness(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		tok, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		if !strings.HasPrefix(tok, tokenPrefix) {
			t.Fatalf("token %q missing prefix %q", tok, tokenPrefix)
		}
		if len(tok) <= len(tokenPrefix) {
			t.Fatalf("token %q has no entropy body", tok)
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("duplicate token generated: %q", tok)
		}
		seen[tok] = struct{}{}
	}
}

func TestHashAndVerifyToken(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	hash, err := HashToken(tok)
	if err != nil {
		t.Fatalf("HashToken: %v", err)
	}
	if hash == tok {
		t.Fatal("hash must not equal plaintext")
	}

	ok, err := VerifyToken(tok, hash)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if !ok {
		t.Fatal("VerifyToken should accept the matching token")
	}

	// A different valid-shaped token must not verify.
	other, _ := GenerateToken()
	ok, err = VerifyToken(other, hash)
	if err != nil {
		t.Fatalf("VerifyToken mismatch err: %v", err)
	}
	if ok {
		t.Fatal("VerifyToken should reject a non-matching token")
	}
}

func TestHashTokenRejectsMalformed(t *testing.T) {
	tests := []string{"", "no-prefix", "Bearer something"}
	for _, in := range tests {
		if _, err := HashToken(in); !errors.Is(err, ErrTokenMalformed) {
			t.Errorf("HashToken(%q) err=%v want ErrTokenMalformed", in, err)
		}
	}
}

func TestVerifyTokenMalformedReturnsFalseNoError(t *testing.T) {
	// Build a valid hash to compare against.
	tok, _ := GenerateToken()
	hash, _ := HashToken(tok)

	tests := []string{"", "garbage", "Bearer x"}
	for _, in := range tests {
		ok, err := VerifyToken(in, hash)
		if err != nil {
			t.Errorf("VerifyToken(%q) unexpected err=%v", in, err)
		}
		if ok {
			t.Errorf("VerifyToken(%q) should be false", in)
		}
	}
}

func TestVerifyTokenCorruptHashReturnsError(t *testing.T) {
	tok, _ := GenerateToken()
	ok, err := VerifyToken(tok, "not-a-bcrypt-hash")
	if err == nil {
		t.Fatal("VerifyToken with corrupt hash should error")
	}
	if ok {
		t.Fatal("VerifyToken with corrupt hash should be false")
	}
}

func TestValidateTokenShape(t *testing.T) {
	if err := validateTokenShape(tokenPrefix + "abc"); err != nil {
		t.Errorf("valid token rejected: %v", err)
	}
	for _, bad := range []string{"", "abc", "Rvw_abc"} {
		if err := validateTokenShape(bad); !errors.Is(err, ErrTokenMalformed) {
			t.Errorf("validateTokenShape(%q)=%v want ErrTokenMalformed", bad, err)
		}
	}
}
