package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

func TestObservabilityAgentRuntimeFallsBackToUnknown(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"no agent key", map[string]any{}, "unknown"},
		{"agent without harness", map[string]any{"agent": map[string]any{}}, "unknown"},
		{"blank harness", map[string]any{"agent": map[string]any{"harness": "   "}}, "unknown"},
		{"valid harness trimmed", map[string]any{"agent": map[string]any{"harness": "  claude-code  "}}, "claude-code"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := observabilityAgentRuntime(tc.payload); got != tc.want {
				t.Fatalf("observabilityAgentRuntime = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveObservabilityProjectIDPrefersConfigThenFailsClosed(t *testing.T) {
	got, err := resolveObservabilityProjectID(&config.AgentsRC{RepoID: "  repo-x  "}, t.TempDir())
	if err != nil || got != "repo-x" {
		t.Fatalf("configured repo id = (%q, %v), want (repo-x, nil)", got, err)
	}
	// No configured id and a non-git dir (git derivation yields ""): fail closed.
	if _, err := resolveObservabilityProjectID(&config.AgentsRC{}, t.TempDir()); err == nil {
		t.Fatal("expected error when no project id can be resolved")
	}
}

func TestResolveScoreSidecar(t *testing.T) {
	repo := t.TempDir()

	got, err := resolveScoreSidecar(repo, 1, false)
	if err != nil || string(got) != "null" {
		t.Fatalf("no-score = (%s, %v), want (null, nil)", got, err)
	}

	if _, err := resolveScoreSidecar(repo, 1, true); err == nil {
		t.Fatal("expected read error for a missing score sidecar")
	}

	if err := os.MkdirAll(filepath.Dir(scoreSidecarPath(repo, 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	mismatch := "iteration: 2\nrubric_version: 3.0.0\nscored: true\nvalue: 0.9\nband: excellent\nbreakdown: []\n"
	if err := os.WriteFile(scoreSidecarPath(repo, 1), []byte(mismatch), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveScoreSidecar(repo, 1, true); err == nil {
		t.Fatal("expected error when the score sidecar iteration does not match")
	}

	valid := "iteration: 1\nrubric_version: 3.0.0\nscored: true\nvalue: 0.9\nband: excellent\nbreakdown: []\n"
	if err := os.WriteFile(scoreSidecarPath(repo, 1), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = resolveScoreSidecar(repo, 1, true)
	if err != nil || len(got) == 0 || string(got) == "null" {
		t.Fatalf("valid score = (%s, %v), want a non-empty JSON payload", got, err)
	}
}

func TestCanonicalizeObservabilityJSONRejectsNonFiniteInContainers(t *testing.T) {
	if _, err := canonicalizeObservabilityJSON([]byte(`[[1],{"k":2}]`)); err != nil {
		t.Fatalf("finite nested containers must canonicalize: %v", err)
	}
	for _, raw := range []string{`[1, 1e400]`, `{"a": 1e400}`} {
		if _, err := canonicalizeObservabilityJSON([]byte(raw)); err == nil {
			t.Fatalf("non-finite number inside %s must be rejected", raw)
		}
	}
}
