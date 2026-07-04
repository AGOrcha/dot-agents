package eval

import (
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
	tsverifier "github.com/AGOrcha/dot-agents/internal/eval/verifier/typescript"
)

// init registers the TypeScript verifier factory so verifiers() includes it.
// Sibling language verifiers self-register the same way from their own files,
// keeping run.go language-agnostic and the per-language deliveries disjoint.
func init() {
	registerVerifier(func() verifier.Verifier { return tsverifier.New() })
}
