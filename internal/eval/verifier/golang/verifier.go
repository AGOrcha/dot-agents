package goverifier

import (
	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
)

// GoVerifier is the Go-language adapter for the shared verifier run engine. It
// embeds *verifier.BaseVerifier, so Language and Verify are promoted from the
// engine; the only Go-specific input is the eval.LanguageGo identity supplied at
// construction. The generic build-then-test run loop lives in the neutral core
// package (verifier.BaseVerifier) rather than being duplicated here.
type GoVerifier struct {
	*verifier.BaseVerifier
}

// Compile-time assertion.
var _ verifier.Verifier = (*GoVerifier)(nil)

// New returns a GoVerifier backed by the real process runner.
func New() *GoVerifier {
	return &GoVerifier{verifier.NewBase(eval.LanguageGo)}
}
