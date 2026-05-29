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

func TestHashTokenIsArgon2idPHC(t *testing.T) {
	tok, _ := GenerateToken()
	hash, err := HashToken(tok)
	if err != nil {
		t.Fatalf("HashToken: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=") {
		t.Fatalf("hash %q is not an argon2id PHC string", hash)
	}
	if !strings.Contains(hash, "m=65536,t=1,p=4") {
		t.Fatalf("hash %q missing expected params", hash)
	}
}

func TestHashTokenSaltIsUnique(t *testing.T) {
	tok, _ := GenerateToken()
	h1, _ := HashToken(tok)
	h2, _ := HashToken(tok)
	if h1 == h2 {
		t.Fatal("hashing the same token twice must yield distinct salts/hashes")
	}
	// Both must still verify against the same plaintext.
	for _, h := range []string{h1, h2} {
		ok, err := VerifyToken(tok, h)
		if err != nil || !ok {
			t.Fatalf("VerifyToken(%q) ok=%v err=%v", h, ok, err)
		}
	}
}

func TestVerifyTokenCorruptHashReturnsError(t *testing.T) {
	tok, _ := GenerateToken()
	cases := []string{
		"not-a-hash",
		"$argon2id$v=19$m=65536,t=1,p=4$badsalt",     // too few fields
		"$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$a2V5",       // wrong algorithm
		"$argon2id$v=99$m=65536,t=1,p=4$c2FsdA$a2V5", // wrong version
		"$argon2id$vX$m=65536,t=1,p=4$c2FsdA$a2V5",   // unparsable version
		"$argon2id$v=19$mX$c2FsdA$a2V5",              // unparsable params
		"$argon2id$v=19$m=65536,t=1,p=4$!!!$a2V5",    // bad base64 salt
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$!!!",  // bad base64 key
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$",     // empty key
	}
	for _, h := range cases {
		ok, err := VerifyToken(tok, h)
		if err == nil {
			t.Errorf("VerifyToken with corrupt hash %q should error", h)
		}
		if ok {
			t.Errorf("VerifyToken with corrupt hash %q should be false", h)
		}
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
