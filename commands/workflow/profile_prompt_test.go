package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

func TestValidateProfileKind(t *testing.T) {
	for _, k := range []string{profileKindExecutor, profileKindVerifier, profileKindReviewer, profileKindOrchestrator} {
		if err := validateProfileKind(k); err != nil {
			t.Fatalf("stage %q should be valid: %v", k, err)
		}
	}
	if err := validateProfileKind("nonsense"); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestDecodeProfilePromptFiles(t *testing.T) {
	raw := map[string]any{
		"stage_profiles": map[string]any{
			"verifier": map[string]any{
				"cli-runner": map[string]any{
					// mixed legacy string + source-aware object form; blank, pathless
					// object, and non-string/non-object entries are dropped
					"prompt_files": []any{
						"verifiers/verifier.base.md",
						map[string]any{"source": "acme", "path": "verifiers/cli-runner.md", "version": "v2"},
						" ",
						map[string]any{"source": "no-path"},
						42,
					},
				},
				"no-files": map[string]any{"label": "x"},
			},
		},
	}
	// matched + entries (blank dropped, order preserved, object path extracted)
	got, matched := decodeProfilePromptFiles(raw, "verifier", "cli-runner")
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
	if g, matched := decodeProfilePromptFiles(raw, "verifier", "no-files"); !matched || len(g) != 0 {
		t.Fatalf("no-files: got %#v matched=%t, want matched empty", g, matched)
	}
	// missing slug
	if _, matched := decodeProfilePromptFiles(raw, "verifier", "ghost"); matched {
		t.Fatal("ghost slug must be unmatched")
	}
	// missing stage map
	if _, matched := decodeProfilePromptFiles(raw, "reviewer", "x"); matched {
		t.Fatal("absent reviewer stage map must be unmatched")
	}
	// no stage_profiles key at all
	if _, matched := decodeProfilePromptFiles(map[string]any{}, "verifier", "x"); matched {
		t.Fatal("absent stage_profiles must be unmatched")
	}
}

func TestDecodeProfileModelRoute(t *testing.T) {
	raw := map[string]any{
		"stage_profiles": map[string]any{
			"reviewer": map[string]any{
				"cross-harness-adversarial": map[string]any{
					"model":        " gpt-5.4 ",
					"model_family": " gpt ",
				},
			},
		},
	}
	model, family := decodeProfileModelRoute(raw, profileKindReviewer, "cross-harness-adversarial")
	if model != "gpt-5.4" || family != "gpt" {
		t.Fatalf("route = %q/%q, want gpt-5.4/gpt", model, family)
	}
	if model, family := decodeProfileModelRoute(raw, profileKindReviewer, "ghost"); model != "" || family != "" {
		t.Fatalf("missing route = %q/%q, want empty", model, family)
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
// effective config carries the given verifier / reviewer profiles (a slug->profile
// JSON object) under the unified stage_profiles map.
func snapshotWithProfiles(t *testing.T, verifierProfiles, reviewerProfiles string) {
	t.Helper()
	orig := appTypeSnapshot
	t.Cleanup(func() { appTypeSnapshot = orig })
	sp := map[string]map[string]config.StageProfile{}
	parse := func(stage, raw string) {
		if raw == "" {
			return
		}
		var m map[string]config.StageProfile
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("parse %s profiles: %v", stage, err)
		}
		sp[stage] = m
	}
	parse(profileKindVerifier, verifierProfiles)
	parse(profileKindReviewer, reviewerProfiles)
	eff := config.AgentsRC{Version: 1}
	if len(sp) > 0 {
		eff.StageProfiles = sp
	}
	appTypeSnapshot = func(string) (*config.Snapshot, error) {
		return &config.Snapshot{Effective: eff}, nil
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

	snapshotWithProfiles(t, `{"cli-runner":{"model":"claude-opus-4-8","model_family":"claude","prompt_files":["verifiers/verifier.base.md","verifiers/cli-runner.md","verifiers/cli-runner.project.md"]}}`, "")

	view, err := composeProfilePrompt(project, home, profileKindVerifier, "cli-runner")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Matched || len(view.Entries) != 3 {
		t.Fatalf("matched=%t entries=%d, want matched 3", view.Matched, len(view.Entries))
	}
	if view.Model != "claude-opus-4-8" || view.ModelFamily != "claude" {
		t.Fatalf("route = %q/%q, want claude-opus-4-8/claude", view.Model, view.ModelFamily)
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

func TestComposeProfilePrompt_SnapshotErrors(t *testing.T) {
	orig := appTypeSnapshot
	t.Cleanup(func() { appTypeSnapshot = orig })
	// non-missing error propagates
	appTypeSnapshot = func(string) (*config.Snapshot, error) {
		return nil, fmt.Errorf("boom: locked layer missing from cache")
	}
	if _, err := composeProfilePrompt(t.TempDir(), t.TempDir(), profileKindVerifier, "unit"); err == nil {
		t.Fatal("expected snapshot error to propagate")
	}
	// missing-manifest error is swallowed to an unmatched view
	appTypeSnapshot = func(string) (*config.Snapshot, error) {
		return nil, fmt.Errorf("no %s found at /x", config.AgentsRCFile)
	}
	view, err := composeProfilePrompt(t.TempDir(), t.TempDir(), profileKindVerifier, "unit")
	if err != nil || view.Matched {
		t.Fatalf("missing-manifest should yield unmatched view: matched=%t err=%v", view.Matched, err)
	}
}

func TestResolvePromptRef_AbsoluteLiteral(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "abs.md")
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := resolvePromptRef(t.TempDir(), "", abs)
	if e.Scope != "literal" || !e.Exists {
		t.Fatalf("absolute existing path should be literal+exists, got scope=%q exists=%t", e.Scope, e.Exists)
	}
}

func TestRunWorkflowResolvePrompt(t *testing.T) {
	// missing slug + bad kind error early, no project needed
	if err := runWorkflowResolvePrompt("verifier", ""); err == nil {
		t.Fatal("empty slug must error")
	}
	if err := runWorkflowResolvePrompt("bogus", "x"); err == nil {
		t.Fatal("bad kind must error")
	}
	// end-to-end render against a real repo .agentsrc.json
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"t","version":1,"sources":[{"type":"local"}],
  "verifier_profiles":{"cli-runner":{"prompt_files":["verifiers/verifier.base.md","verifiers/cli-runner.project.md"]}}
}`)
	out := captureWorkflowOutput(t, repo, func() error {
		return runWorkflowResolvePrompt(profileKindVerifier, "cli-runner")
	})
	if !strings.Contains(out, "matched : true") || !strings.Contains(out, "cli-runner") {
		t.Fatalf("resolve-prompt output missing expected content:\n%s", out)
	}
	if !strings.Contains(out, "composition (base-first)") {
		t.Fatalf("expected composition listing:\n%s", out)
	}
}

func TestResolvePromptCmd_Execute(t *testing.T) {
	// Drives the cobra command (constructor + RunE wrapper) end-to-end.
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"t","version":1,"sources":[{"type":"local"}],
  "verifier_profiles":{"cli-runner":{"prompt_files":["verifiers/verifier.base.md","verifiers/cli-runner.project.md"]}}
}`)
	out := captureWorkflowOutput(t, repo, func() error {
		cmd := newWorkflowResolvePromptCmd()
		cmd.SetArgs([]string{"--kind", "verifier", "--slug", "cli-runner"})
		return cmd.Execute()
	})
	if !strings.Contains(out, "matched : true") || !strings.Contains(out, "cli-runner") {
		t.Fatalf("resolve-prompt cmd output missing expected content:\n%s", out)
	}
}

func TestRunWorkflowResolvePrompt_JSON(t *testing.T) {
	prior := deps.Flags.JSON
	deps.Flags.JSON = func() bool { return true }
	t.Cleanup(func() { deps.Flags.JSON = prior })
	repo := setupWorkflowAppTypesProject(t, `{
  "project":"t","version":1,"sources":[{"type":"local"}],
  "reviewer_profiles":{"architecture-standards":{"prompt_files":["reviewers/reviewer.base.md"]}}
}`)
	out := captureWorkflowOutput(t, repo, func() error {
		return runWorkflowResolvePrompt(profileKindReviewer, "architecture-standards")
	})
	var v composedPromptView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("json branch should emit a valid composedPromptView: %v\n%s", err, out)
	}
	if !v.Matched || v.Kind != profileKindReviewer || v.Slug != "architecture-standards" {
		t.Fatalf("json view = %#v", v)
	}
}

func TestRenderComposedPrompt_EdgeCases(t *testing.T) {
	// unmatched
	out := captureWorkflowStdout(t, func() {
		renderComposedPrompt(composedPromptView{Kind: "verifier", Slug: "ghost", Matched: false})
	})
	if !strings.Contains(out, "no stage_profiles.verifier entry") {
		t.Fatalf("unmatched render missing notice:\n%s", out)
	}
	// matched, no prompt_files
	out = captureWorkflowStdout(t, func() {
		renderComposedPrompt(composedPromptView{Kind: "verifier", Slug: "x", Matched: true})
	})
	if !strings.Contains(out, "no prompt_files") {
		t.Fatalf("empty render missing notice:\n%s", out)
	}
}
