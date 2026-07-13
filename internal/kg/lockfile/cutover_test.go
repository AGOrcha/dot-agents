package lockfile

import (
	"reflect"
	"testing"
	"time"
)

const (
	cutConsumer = "compliance-register"
	cutView     = "controls_with_changed_function_evidence"
	cutDependee = "crg"
)

func cutNow() time.Time { return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC) }

// registerReadyView seeds a ready view on the consumer depending on cutDependee.
func registerReadyView(t *testing.T) *Lockfile {
	t.Helper()
	lf := New()
	lf.RegisterView(cutConsumer, cutView, "sha256:v0",
		[]ViewDependency{{Adapter: cutDependee, SchemaDigest: "sha256:d0", Version: "1.0.0"}}, cutNow())
	if s, ok := lf.ViewStatusOf(cutConsumer, cutView); !ok || s != StatusReady {
		t.Fatalf("registered view status = %v (ok=%v), want ready", s, ok)
	}
	return lf
}

// TestCutoverCompatiblePath is the §10.3 compatible cycle:
// ready → pending-recompat-check → pending-rebuild → ready.
func TestCutoverCompatiblePath(t *testing.T) {
	lf := registerReadyView(t)

	affected := lf.MarkDependeeBumped(cutDependee, cutNow())
	if !reflect.DeepEqual(affected, []string{cutConsumer + "/" + cutView}) {
		t.Fatalf("MarkDependeeBumped affected = %v", affected)
	}
	assertStatus(t, lf, StatusPendingRecompatCheck)

	newDeps := []ViewDependency{{Adapter: cutDependee, SchemaDigest: "sha256:d1", Version: "1.1.0"}}
	if err := lf.ResolveRecompat(cutConsumer, cutView, true, newDeps, cutNow()); err != nil {
		t.Fatalf("ResolveRecompat: %v", err)
	}
	assertStatus(t, lf, StatusPendingRebuild)

	if err := lf.MarkViewRebuilt(cutConsumer, cutView, "sha256:v1", cutNow()); err != nil {
		t.Fatalf("MarkViewRebuilt: %v", err)
	}
	assertStatus(t, lf, StatusReady)

	// Compatible cutover blocks nothing.
	if b := lf.ActivationBlockers(cutDependee); len(b) != 0 {
		t.Fatalf("ActivationBlockers = %v, want none", b)
	}
	// The recorded dependency digest advanced to the new schema.
	v, _ := lf.findView(cutConsumer, cutView)
	if v.DependsOn[0].SchemaDigest != "sha256:d1" {
		t.Fatalf("DependsOn digest = %q, want sha256:d1", v.DependsOn[0].SchemaDigest)
	}
}

// TestCutoverIncompatibleBlocksActivation is the §10.3 incompatible path:
// ready → pending-recompat-check → dsl-update-required, which blocks the
// dependee's (re)activation (O1: no ack opt-out).
func TestCutoverIncompatibleBlocksActivation(t *testing.T) {
	lf := registerReadyView(t)
	lf.MarkDependeeBumped(cutDependee, cutNow())

	if err := lf.ResolveRecompat(cutConsumer, cutView, false, nil, cutNow()); err != nil {
		t.Fatalf("ResolveRecompat: %v", err)
	}
	assertStatus(t, lf, StatusDSLUpdateRequired)

	blockers := lf.ActivationBlockers(cutDependee)
	if !reflect.DeepEqual(blockers, []string{cutConsumer + "/" + cutView}) {
		t.Fatalf("ActivationBlockers = %v, want the dependent view", blockers)
	}
	// An unrelated adapter is not blocked.
	if b := lf.ActivationBlockers("other"); len(b) != 0 {
		t.Fatalf("ActivationBlockers(other) = %v, want none", b)
	}
}

// TestCutoverTransitionGuards covers the state-precondition errors and the
// no-op cases (a non-ready/non-dependent view is not affected).
func TestCutoverTransitionGuards(t *testing.T) {
	lf := registerReadyView(t)

	// ResolveRecompat is only valid from pending-recompat-check.
	if err := lf.ResolveRecompat(cutConsumer, cutView, true, nil, cutNow()); err == nil {
		t.Fatal("ResolveRecompat from ready: want error")
	}
	// MarkViewRebuilt is only valid from pending-rebuild.
	if err := lf.MarkViewRebuilt(cutConsumer, cutView, "x", cutNow()); err == nil {
		t.Fatal("MarkViewRebuilt from ready: want error")
	}
	// Unknown view errors on every transition.
	if err := lf.ResolveRecompat("nope", "nope", true, nil, cutNow()); err == nil {
		t.Fatal("ResolveRecompat unknown view: want error")
	}
	if err := lf.MarkViewRebuilt("nope", "nope", "x", cutNow()); err == nil {
		t.Fatal("MarkViewRebuilt unknown view: want error")
	}
	if _, ok := lf.ViewStatusOf("nope", "nope"); ok {
		t.Fatal("ViewStatusOf unknown view: want ok=false")
	}

	// A bump of an unrelated dependee leaves the view ready (not affected).
	if a := lf.MarkDependeeBumped("unrelated", cutNow()); len(a) != 0 {
		t.Fatalf("MarkDependeeBumped(unrelated) = %v, want none", a)
	}
	assertStatus(t, lf, StatusReady)
}

func assertStatus(t *testing.T, lf *Lockfile, want ViewStatus) {
	t.Helper()
	got, ok := lf.ViewStatusOf(cutConsumer, cutView)
	if !ok || got != want {
		t.Fatalf("view status = %v (ok=%v), want %v", got, ok, want)
	}
}
