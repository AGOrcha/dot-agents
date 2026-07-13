package pyverifier

import (
	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
)

// PyVerifier is the Python-language adapter for the shared verifier run engine.
// It embeds *verifier.BaseVerifier, so Language and Verify are promoted from the
// engine; the only Python-specific input is the eval.LanguagePython identity
// supplied at construction. The Python toolchain commands (py_compile / pytest /
// unittest) come from the TaskSpec authored by the Python generator, so no
// Python-specific run logic lives here.
type PyVerifier struct {
	*verifier.BaseVerifier
}

// Compile-time assertion.
var _ verifier.Verifier = (*PyVerifier)(nil)

// New returns a PyVerifier backed by the real process runner.
func New() *PyVerifier {
	return &PyVerifier{verifier.NewBase(eval.LanguagePython)}
}
