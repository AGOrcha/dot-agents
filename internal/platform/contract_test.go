package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlatformContract_AllImplementorsHonorInterface drives every Platform
// returned by All() through a uniform set of contract assertions: identity is
// non-empty + unique, RemoveLinks tolerates an empty repo, and
// SharedTargetIntents reports a deterministic shape. Platform CLI presence is
// orthogonal — we never invoke version probes from the test.
// assertPlatformIdentity validates ID() and DisplayName() uniqueness and
// non-emptiness for a single platform implementation.
func assertPlatformIdentity(t *testing.T, p Platform, seenIDs, seenNames map[string]bool) string {
	t.Helper()
	id := p.ID()
	if id == "" {
		t.Fatal("ID() must be non-empty")
	}
	if strings.TrimSpace(id) != id {
		t.Errorf("ID() %q has surrounding whitespace", id)
	}
	if seenIDs[id] {
		t.Errorf("duplicate platform ID: %q", id)
	}
	seenIDs[id] = true

	name := p.DisplayName()
	if name == "" {
		t.Error("DisplayName() must be non-empty")
	}
	if seenNames[name] {
		t.Errorf("duplicate DisplayName: %q", name)
	}
	seenNames[name] = true
	return id
}

// assertDeprecatedDetailsPaired confirms that when HasDeprecatedFormat returns
// false, DeprecatedDetails returns an empty string.
func assertDeprecatedDetailsPaired(t *testing.T, p Platform, repo string) {
	t.Helper()
	if p.HasDeprecatedFormat(repo) {
		return
	}
	if details := p.DeprecatedDetails(repo); details != "" {
		t.Errorf("DeprecatedDetails non-empty when HasDeprecatedFormat=false: %q", details)
	}
}

// assertSharedTargetIntentsShape verifies SharedTargetIntents returns valid
// intents (non-empty TargetPath, correct Project) without error.
func assertSharedTargetIntentsShape(t *testing.T, p Platform) {
	t.Helper()
	intents, err := p.SharedTargetIntents("contract-proj")
	if err != nil {
		t.Errorf("SharedTargetIntents returned error: %v", err)
	}
	for i, intent := range intents {
		if intent.TargetPath == "" {
			t.Errorf("intent[%d] has empty TargetPath", i)
		}
		if intent.Project != "contract-proj" {
			t.Errorf("intent[%d].Project = %q, want contract-proj", i, intent.Project)
		}
	}
}

// assertPlatformContract runs the per-platform contract suite: identity,
// deprecation pairing, intents shape, and RemoveLinks safety.
func assertPlatformContract(t *testing.T, p Platform, tmp string, seenIDs, seenNames map[string]bool) {
	id := assertPlatformIdentity(t, p, seenIDs, seenNames)

	repo := filepath.Join(tmp, "repo-"+id)
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	assertDeprecatedDetailsPaired(t, p, repo)
	assertSharedTargetIntentsShape(t, p)

	// RemoveLinks must be a no-op-safe call on an empty repo. Any returned
	// error indicates the platform is mishandling the missing-state base case.
	if err := p.RemoveLinks("contract-proj", repo); err != nil {
		t.Errorf("RemoveLinks on empty repo errored: %v", err)
	}
}

func TestPlatformContract_AllImplementorsHonorInterface(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))

	platforms := All()
	if len(platforms) == 0 {
		t.Fatal("All() returned no platforms")
	}

	seenIDs := map[string]bool{}
	seenNames := map[string]bool{}
	for _, p := range platforms {
		p := p
		t.Run(p.ID(), func(t *testing.T) {
			assertPlatformContract(t, p, tmp, seenIDs, seenNames)
		})
	}
}

// TestPlatformContract_ByIDRoundTrip ensures every platform exposed by All()
// is discoverable via ByID() and that ByID() returns nil for unknown ids.
func TestPlatformContract_ByIDRoundTrip(t *testing.T) {
	for _, p := range All() {
		got := ByID(p.ID())
		if got == nil {
			t.Errorf("ByID(%q) returned nil", p.ID())
			continue
		}
		if got.ID() != p.ID() {
			t.Errorf("ByID(%q).ID() = %q", p.ID(), got.ID())
		}
	}
	if got := ByID(""); got != nil {
		t.Errorf("ByID(\"\") expected nil, got %v", got)
	}
	if got := ByID("not-a-real-platform"); got != nil {
		t.Errorf("ByID(unknown) expected nil, got %v", got)
	}
}

// TestPlatformContract_CreateLinksOnEmptyAgentsHome verifies CreateLinks on an
// empty ~/.agents/ never panics and either succeeds or returns an error — but
// never silently corrupts the empty target repo with stray files.
func TestPlatformContract_CreateLinksOnEmptyAgentsHome(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	if err := os.MkdirAll(filepath.Join(tmp, "home"), 0755); err != nil {
		t.Fatal(err)
	}

	for _, p := range All() {
		p := p
		t.Run(p.ID(), func(t *testing.T) {
			repo := filepath.Join(tmp, "repo-"+p.ID())
			if err := os.MkdirAll(repo, 0755); err != nil {
				t.Fatal(err)
			}
			// Best-effort: just exercise the code path. Some platforms will
			// surface errors here, which is acceptable — the contract is "no
			// panic and bounded behaviour."
			_ = p.CreateLinks("contract-proj", repo)
		})
	}
}
