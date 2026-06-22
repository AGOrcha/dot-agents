package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

func TestPreconditionPolicyFromConfig(t *testing.T) {
	cases := []struct {
		name string
		in   config.ResolvedPreconditionPolicy
		want PreconditionPolicy
	}{
		{
			name: "named policy with predicates converts field-for-field",
			in: config.ResolvedPreconditionPolicy{
				Name: "strict",
				Predicates: []config.ResolvedPredicate{
					{Signal: "event.pr.open"},
					{Signal: "gate.quality.sonar", Args: map[string]string{"equals": "pass"}},
				},
			},
			want: PreconditionPolicy{
				Name: "strict",
				Predicates: []Predicate{
					{Signal: "event.pr.open"},
					{Signal: "gate.quality.sonar", Args: map[string]string{"equals": "pass"}},
				},
			},
		},
		{
			name: "default (empty predicates) stays empty so evaluatePolicy falls back",
			in:   config.ResolvedPreconditionPolicy{Name: "default"},
			want: PreconditionPolicy{Name: "default"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := preconditionPolicyFromConfig(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("preconditionPolicyFromConfig = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// withResolver swaps resolvePreconditionPolicy for the duration of a test.
func withResolver(t *testing.T, fn func(projectPath, appType string) (PreconditionPolicy, error)) {
	t.Helper()
	prev := resolvePreconditionPolicy
	resolvePreconditionPolicy = fn
	t.Cleanup(func() { resolvePreconditionPolicy = prev })
}

func TestDispatchVerifierTransition(t *testing.T) {
	strict := PreconditionPolicy{Name: "strict", Predicates: []Predicate{{Signal: "event.pr.open"}}}

	t.Run("policy met → verifier decision", func(t *testing.T) {
		withResolver(t, func(_, appType string) (PreconditionPolicy, error) {
			if appType != "go-cli" {
				t.Errorf("appType = %q, want go-cli", appType)
			}
			return strict, nil
		})
		dec, err := dispatchVerifierTransition("proj", "go-cli", "t1", TaskStatusInProgress,
			SignalSnapshot{"event.pr.open": "true"})
		if err != nil {
			t.Fatalf("dispatchVerifierTransition: %v", err)
		}
		if dec.Owner != ownerVerifier || dec.To != TaskStatusAwaitingAgentReview {
			t.Errorf("decision = %+v, want verifier → awaiting_agent_review", dec)
		}
	})

	t.Run("policy unmet → gate-closed error", func(t *testing.T) {
		withResolver(t, func(_, _ string) (PreconditionPolicy, error) { return strict, nil })
		_, err := dispatchVerifierTransition("proj", "go-cli", "t1", TaskStatusInProgress, SignalSnapshot{})
		if err == nil {
			t.Fatal("expected gate-closed error, got nil")
		}
	})

	t.Run("resolver error propagates", func(t *testing.T) {
		boom := errors.New("lock read failed")
		withResolver(t, func(_, _ string) (PreconditionPolicy, error) { return PreconditionPolicy{}, boom })
		_, err := dispatchVerifierTransition("proj", "go-cli", "t1", TaskStatusInProgress, SignalSnapshot{})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want wrap of %v", err, boom)
		}
	})
}

// TestResolvePreconditionPolicyDefaultVar exercises the production accessor var
// end-to-end against a flat project with no precondition wiring: it must convert
// the config default into an empty-predicate workflow policy.
func TestResolvePreconditionPolicyDefaultVar(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, config.AgentsRCFile), []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Isolate the user-local layer so a developer ~/.agents config cannot leak a
	// precondition policy into this default-path assertion.
	t.Setenv("AGENTS_HOME", t.TempDir())
	policy, err := resolvePreconditionPolicy(repo, "go-cli")
	if err != nil {
		t.Fatalf("resolvePreconditionPolicy: %v", err)
	}
	if policy.Name != "default" || len(policy.Predicates) != 0 {
		t.Errorf("policy = %+v, want default with no predicates", policy)
	}
}
