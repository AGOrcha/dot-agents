package lifecycle

import (
	"errors"
	"testing"
)

// TestApplyDepsToGlobals_ZeroValueIsNoOp pins the no-op-safe contract
// documented on applyDepsToGlobals: invoking with a zero-value Deps
// must leave every package var untouched (Version/Commit/Describe
// only copy on non-empty, ErrorWithHints only on non-nil).
func TestApplyDepsToGlobals_ZeroValueIsNoOp(t *testing.T) {
	priorV, priorC, priorD := Version, Commit, Describe
	priorEWH := ErrorWithHintsFn
	t.Cleanup(func() {
		Version, Commit, Describe = priorV, priorC, priorD
		ErrorWithHintsFn = priorEWH
	})

	applyDepsToGlobals(Deps{})

	if Version != priorV || Commit != priorC || Describe != priorD {
		t.Errorf("zero Deps must not mutate Version/Commit/Describe; got %q/%q/%q want %q/%q/%q",
			Version, Commit, Describe, priorV, priorC, priorD)
	}
}

// TestApplyDepsToGlobals_FlagsFnTakesPrecedence covers the FlagsFn !=
// nil branch documented in deps.go: the closure wins over the snapshot.
func TestApplyDepsToGlobals_FlagsFnTakesPrecedence(t *testing.T) {
	priorFlags := Flags
	t.Cleanup(func() { Flags = priorFlags })

	staleFlags := GlobalFlags{Verbose: false, DryRun: true}
	liveFlags := GlobalFlags{Verbose: true, DryRun: false}
	applyDepsToGlobals(Deps{
		Flags:   staleFlags,
		FlagsFn: func() GlobalFlags { return liveFlags },
	})
	if !Flags.Verbose || Flags.DryRun {
		t.Errorf("FlagsFn must win over Flags snapshot, got Flags=%+v", Flags)
	}
}

// TestApplyDepsToGlobals_FlagsFallsBackToSnapshot covers FlagsFn == nil
// so the Flags struct copy path executes.
func TestApplyDepsToGlobals_FlagsFallsBackToSnapshot(t *testing.T) {
	priorFlags := Flags
	t.Cleanup(func() { Flags = priorFlags })

	snap := GlobalFlags{Verbose: true, Force: true}
	applyDepsToGlobals(Deps{Flags: snap})
	if !Flags.Verbose || !Flags.Force {
		t.Errorf("expected Flags snapshot copied, got %+v", Flags)
	}
}

// TestApplyDepsToGlobals_BuildInfoCopiedWhenNonEmpty covers the three
// non-empty conditional copies for Version/Commit/Describe.
func TestApplyDepsToGlobals_BuildInfoCopiedWhenNonEmpty(t *testing.T) {
	priorV, priorC, priorD := Version, Commit, Describe
	t.Cleanup(func() {
		Version, Commit, Describe = priorV, priorC, priorD
	})

	applyDepsToGlobals(Deps{
		Version:  "v1.2.3",
		Commit:   "abcd1234",
		Describe: "v1.2.3-1-gabcd",
	})
	if Version != "v1.2.3" || Commit != "abcd1234" || Describe != "v1.2.3-1-gabcd" {
		t.Errorf("build info not copied; got V=%q C=%q D=%q", Version, Commit, Describe)
	}
}

// TestApplyDepsToGlobals_ErrorWithHintsCopiedWhenNonNil covers the last
// conditional copy + the ErrorWithHintsFn override surface.
func TestApplyDepsToGlobals_ErrorWithHintsCopiedWhenNonNil(t *testing.T) {
	priorEWH := ErrorWithHintsFn
	t.Cleanup(func() { ErrorWithHintsFn = priorEWH })

	sentinel := errors.New("custom hint formatter sentinel")
	applyDepsToGlobals(Deps{
		ErrorWithHints: func(message string, hints ...string) error { return sentinel },
	})
	if got := ErrorWithHintsFn("x", "h"); !errors.Is(got, sentinel) {
		t.Errorf("ErrorWithHintsFn override not installed; got %v", got)
	}
}

// TestDefaultErrorWithHints covers the package-default formatter so the
// fallback path (no Deps override) is locked in.
func TestDefaultErrorWithHints(t *testing.T) {
	got := defaultErrorWithHints("boom", "hint1", "hint2")
	if got == nil {
		t.Fatal("defaultErrorWithHints returned nil")
	}
	if msg := got.Error(); msg == "" {
		t.Errorf("expected non-empty error message, got %q", msg)
	}
}
