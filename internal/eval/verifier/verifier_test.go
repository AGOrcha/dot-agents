package verifier

import (
	"errors"
	"testing"
)

func TestVerifyErrorError(t *testing.T) {
	cause := errors.New("go: not found")
	err := &VerifyError{Phase: PhaseTest, Cause: cause}
	got := err.Error()
	want := "verifier: test: go: not found"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestVerifyErrorUnwrap(t *testing.T) {
	cause := errors.New("context canceled")
	err := &VerifyError{Phase: PhaseValidate, Cause: cause}
	if unwrapped := err.Unwrap(); unwrapped != cause {
		t.Fatalf("Unwrap() = %v, want %v", unwrapped, cause)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is did not traverse VerifyError to its cause")
	}
}
