package tsverifier

import (
	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
)

// TSVerifier implements verifier.Verifier for TypeScript tasks. It is a thin
// adapter over the shared BaseVerifier run engine; TypeScript toolchain
// commands (tsc --noEmit / node --test / vitest) come from the TaskSpec
// authored by the TypeScript generator, so no TS-specific run logic lives here.
// The generic build-then-test run loop lives in the neutral core package
// (verifier.BaseVerifier) rather than being duplicated here.
type TSVerifier struct {
	*verifier.BaseVerifier
}

// Compile-time assertion.
var _ verifier.Verifier = (*TSVerifier)(nil)

// New returns a TSVerifier backed by the real process runner.
func New() *TSVerifier {
	return &TSVerifier{verifier.NewBase(eval.LanguageTypeScript)}
}
