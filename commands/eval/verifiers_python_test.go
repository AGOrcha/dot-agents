package eval

import (
	"testing"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
)

// TestVerifiersHasPython proves the init() registration wired the Python verifier
// in, so `da eval run --language python` resolves a verifier.
func TestVerifiersHasPython(t *testing.T) {
	if _, ok := verifiers()[evalcore.LanguagePython]; !ok {
		t.Fatal("verifiers() must include the python verifier")
	}
}
