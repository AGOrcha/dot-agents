package eval

import (
	"testing"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
)

// TestVerifiersHasTypeScript proves the init() registration wired the
// TypeScript verifier into the language map, so `da eval run --language
// typescript` resolves a verifier.
func TestVerifiersHasTypeScript(t *testing.T) {
	if _, ok := verifiers()[evalcore.LanguageTypeScript]; !ok {
		t.Fatal("verifiers() must include the typescript verifier")
	}
}
