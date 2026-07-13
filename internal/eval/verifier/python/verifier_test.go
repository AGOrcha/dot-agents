package pyverifier

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
)

// TestNew_Language asserts the Python adapter reports the Python language,
// promoted from the embedded engine.
func TestNew_Language(t *testing.T) {
	v := New()
	if v == nil {
		t.Fatal("New returned nil")
	}
	if got := v.Language(); got != eval.LanguagePython {
		t.Errorf("Language() = %q, want %q", got, eval.LanguagePython)
	}
}

// TestNew_SatisfiesVerifier asserts New returns a non-nil value that satisfies
// the shared verifier.Verifier contract (the run loop lives in the embedded
// engine; this adapter only supplies the language identity).
func TestNew_SatisfiesVerifier(t *testing.T) {
	v := New()
	if v == nil {
		t.Fatal("New returned nil")
	}
	if v.BaseVerifier == nil {
		t.Fatal("embedded BaseVerifier is nil")
	}
	var _ verifier.Verifier = v
}
