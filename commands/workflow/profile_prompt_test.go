package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

func TestProfileMapKey(t *testing.T) {
	if k, err := profileMapKey(profileKindVerifier); err != nil || k != "verifier_profiles" {
		t.Fatalf("verifier: got %q, %v", k, err)
	}
	if k, err := profileMapKey(profileKindReviewer); err != nil || k != "reviewer_profiles" {
		t.Fatalf("reviewer: got %q, %v", k, err)
	}
	if _, err := profileMapKey("nonsense"); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestDecodeProfilePromptFiles(t *testing.T) {
	raw := map[string]any{
		"verifier_profiles": map[string]any{
			"cli-runner": map[string]any{
				"prompt_files": []any{"verifiers/verifier.base.md", "verifiers/cli-runner.md", " "},
			},
			"no-files": map[string]any{"label": "x"},
		},
	}
	// matched + entries (blank dropped, order preserved)
	got, matched := decodeProfilePromptFiles(raw, "verifier_profiles", "cli-runner")
	if !matched {
		t.Fatal("cli-runner should be matched")
	}
	want := []string{"verifiers/verifier.base.md", "verifiers/cli-runner.md"}
	if len(got) != len(want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// profile exists, no prompt_files
	if g, matched := decodeProfilePromptFiles(raw, "verifier_profiles", "no-files"); !matched || len(g) != 0 {
		t.Fatalf("no-files: got %#v matched=%t, want matched empty", g, matched)
	}
	// missing slug
	if _, matched := decodeProfilePromptFiles(raw, "verifier_profiles", "ghost"); matched {
		t.Fatal("ghost slug must be unmatched")
	}
	// missing map
	if _, matched := decodeProfilePromptFiles(raw, "reviewer_profiles", "x"); matched {
		t.Fatal("absent reviewer_profiles map must be unmatched")
	}
}

func TestResolvePromptRef(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	mkfile := func(root, rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// repo-local committed overlay (full .agents/... ref) + a logical name in both scopes
	mkfile(project, ".agents/prompts/verifiers/cli-runner.project.md")
	mkfile(project, ".agents/prompts/verifiers/both.md")
	mkfile(home, "prompts/verifiers/both.md")
	mkfile(home, "prompts/verifiers/verifier.base.md")
	absent := "verifiers/ghost.md"

	cases := []struct {
		name       string
		entry      string
		wantScope  string
		wantExists bool
	}{
		{"full repo ref", ".agents/prompts/verifiers/cli-runner.project.md", "repo-local", true},
		{"logical repo wins over shared", "verifiers/both.md", "repo-local", true},
		{"logical shared-home", "verifiers/verifier.base.md", "shared-home", true},
		{"unresolved", absent, "unresolved", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := resolvePromptRef(project, home, tc.entry)
			if e.Ref != tc.entry {
				t.Fatalf("ref = %q, want %q", e.Ref, tc.entry)
			}
			if e.Scope != tc.wantScope {
				t.Fatalf("scope = %q, want %q", e.Scope, tc.wantScope)
			}
			if e.Exists != tc.wantExists {
				t.Fatalf("exists = %t, want %t", e.Exists, tc.wantExists)
			}
		})
	}
}

// snapshotWithProfiles installs an appTypeSnapshot seam returning a Snapshot whose
// effective config carries the given verifier_profiles / reviewer_profiles JSON.
func snapshotWithProfiles(t *testing.T, verifierProfiles, reviewerProfiles string) {
	t.Helper()
	orig := appTypeSnapshot
	t.Cleanup(func() { appTypeSnapshot = orig })
	extra := map[string]json.RawMessage{}
	if verifierProfiles != "" {
		extra["verifier_profiles"] = json.RawMessage(verifierProfiles)
	}
	if reviewerProfiles != "" {
		extra["reviewer_profiles"] = json.RawMessage(reviewerProfiles)
	}
	appTypeSnapshot = func(string) (*config.Snapshot, error) {
		return &config.Snapshot{Effective: config.AgentsRC{Version: 1, ExtraFields: extra}}, nil
	}
}

func TestComposeProfilePrompt_VerifierBaseFirst(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	write := func(root, rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(home, "prompts/verifiers/verifier.base.md")
	write(home, "prompts/verifiers/cli-runner.md")
	write(project, ".agents/prompts/verifiers/cli-runner.project.md")

	snapshotWithProfiles(t, `{"cli-runner":{"prompt_files":["verifiers/verifier.base.md","verifiers/cli-runner.md","verifiers/cli-runner.project.md"]}}`, "")

	view, err := composeProfilePrompt(project, home, profileKindVerifier, "cli-runner")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Matched || len(view.Entries) != 3 {
		t.Fatalf("matched=%t entries=%d, want matched 3", view.Matched, len(view.Entries))
	}
	// base-first order preserved; scopes resolved per file (base/per-type shared, overlay repo)
	wantScope := []string{"shared-home", "shared-home", "repo-local"}
	for i, e := range view.Entries {
		if !e.Exists {
			t.Fatalf("entry %d (%s) unresolved", i, e.Ref)
		}
		if e.Scope != wantScope[i] {
			t.Fatalf("entry %d scope = %q, want %q", i, e.Scope, wantScope[i])
		}
	}
}

// TestComposeProfilePrompt_Override proves a higher scope changing the merged
// profile's prompt_files list changes the effective composition — the same
// override path execution_profile rides (verifier/reviewer_profiles are
// scope-merged config).
func TestComposeProfilePrompt_Override(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	// base list
	snapshotWithProfiles(t, `{"unit":{"prompt_files":["verifiers/verifier.base.md","verifiers/unit.md"]}}`, "")
	base, err := composeProfilePrompt(project, home, profileKindVerifier, "unit")
	if err != nil || len(base.Entries) != 2 {
		t.Fatalf("base compose: %v entries=%d", err, len(base.Entries))
	}
	// higher scope replaced the array (CategoryScalar wholesale replace) — add an overlay
	snapshotWithProfiles(t, `{"unit":{"prompt_files":["verifiers/verifier.base.md","verifiers/unit.md","verifiers/unit.project.md"]}}`, "")
	over, err := composeProfilePrompt(project, home, profileKindVerifier, "unit")
	if err != nil {
		t.Fatal(err)
	}
	if len(over.Entries) != 3 || over.Entries[2].Ref != "verifiers/unit.project.md" {
		t.Fatalf("override should add a third entry, got %#v", over.Entries)
	}
}

func TestComposeProfilePrompt_Reviewer(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	snapshotWithProfiles(t, "", `{"architecture-standards":{"prompt_files":["reviewers/reviewer.base.md","reviewers/architecture-standards.md"]}}`)
	view, err := composeProfilePrompt(project, home, profileKindReviewer, "architecture-standards")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Matched || len(view.Entries) != 2 {
		t.Fatalf("reviewer compose: matched=%t entries=%d", view.Matched, len(view.Entries))
	}
}

func TestComposeProfilePrompt_Unmatched(t *testing.T) {
	snapshotWithProfiles(t, `{"unit":{"prompt_files":["x"]}}`, "")
	view, err := composeProfilePrompt(t.TempDir(), t.TempDir(), profileKindVerifier, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if view.Matched || len(view.Entries) != 0 {
		t.Fatalf("ghost should be unmatched with no entries, got %#v", view)
	}
}

func TestComposeProfilePrompt_BadKind(t *testing.T) {
	if _, err := composeProfilePrompt(t.TempDir(), t.TempDir(), "bogus", "x"); err == nil {
		t.Fatal("expected error for bad kind")
	}
}
