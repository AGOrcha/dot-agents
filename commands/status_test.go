package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/platform"
)

// registryTestPlatform is a minimal Platform stub for commands-layer registry tests.
type registryTestPlatform struct {
	id         string
	collectErr error
}

func (p registryTestPlatform) ID() string                      { return p.id }
func (p registryTestPlatform) DisplayName() string             { return p.id }
func (p registryTestPlatform) IsInstalled() bool               { return true }
func (p registryTestPlatform) Version() string                 { return "" }
func (p registryTestPlatform) CreateLinks(_, _ string) error   { return nil }
func (p registryTestPlatform) RemoveLinks(_, _ string) error   { return nil }
func (p registryTestPlatform) HasDeprecatedFormat(string) bool { return false }
func (p registryTestPlatform) DeprecatedDetails(string) string { return "" }
func (p registryTestPlatform) SharedTargetIntents(string) ([]platform.ResourceIntent, error) {
	return nil, p.collectErr
}

func TestSharedTargetRegistryPlanLines_EmptyPlatforms(t *testing.T) {
	lines, err := sharedTargetRegistryPlanLines("proj", "/tmp/repo", nil)
	if err != nil {
		t.Fatalf("sharedTargetRegistryPlanLines: %v", err)
	}
	if lines != nil {
		t.Fatalf("expected nil lines, got %#v", lines)
	}
}

func TestSharedTargetRegistryPlanLines_MatchesDryRunSharedTargetPlanLines(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	plats := []platform.Platform{platform.NewCodex()}
	want, err := platform.DryRunSharedTargetPlanLines("proj", repo, plats)
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	got, err := sharedTargetRegistryPlanLines("proj", repo, plats)
	if err != nil {
		t.Fatalf("sharedTargetRegistryPlanLines: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d (%v) want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestSharedTargetRegistryPlanLines_PropagatesSharedIntentError(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	wrapped := errors.New("boom")
	_, err := sharedTargetRegistryPlanLines("proj", repo, []platform.Platform{
		registryTestPlatform{id: "bad", collectErr: wrapped},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wrapped) {
		t.Fatalf("errors.Is: got %v want %v", err, wrapped)
	}
	if !strings.Contains(err.Error(), "bad shared intents") {
		t.Fatalf("error = %q, want platform wrap", err.Error())
	}
}
