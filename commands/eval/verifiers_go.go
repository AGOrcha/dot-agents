package eval

import (
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
	goverifier "github.com/AGOrcha/dot-agents/internal/eval/verifier/golang"
)

// init registers the Go verifier factory so verifiers() includes it. Sibling
// language verifiers self-register the same way from their own files, keeping
// run.go language-agnostic and the per-language deliveries disjoint.
func init() {
	registerVerifier(func() verifier.Verifier { return goverifier.New() })
}
