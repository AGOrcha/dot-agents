package config

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// promptUnitProject seeds a sandboxed project whose lock pins one prompt unit,
// optionally with its cached bytes present, and returns the project path.
func promptUnitProject(t *testing.T, cached bool) string {
	t.Helper()
	project := withRepoLayer(t, `{"version": 2}`, "")
	ref := cfg.PromptUnitRef{SourceID: "team", Path: "verifiers/ts-lint.md"}
	const sha = "aaaa111122223333"
	writeLock(t, project, map[string]cfg.LockedUnit{ref.Key(): {Kind: "prompt", Digest: sha}})
	if cached {
		seedPromptCacheEntry(t, ref, sha)
	}
	return project
}

// promptCheck finds the per-prompt verify check line.
func promptCheck(t *testing.T, checks []VerifyCheck) VerifyCheck {
	t.Helper()
	check, ok := findCheck(checks, "prompt:team:verifiers/ts-lint.md")
	if !ok {
		t.Fatalf("verify report has no prompt-unit check: %#v", checks)
	}
	return check
}

// TestVerifyReportChecksPromptUnits is the item-4 contract: a pinned prompt whose
// cached bytes are present passes, a pruned one WARNS with the sync hint, and the
// warning does not flip the report's OK.
func TestVerifyReportChecksPromptUnits(t *testing.T) {
	cachedProject := promptUnitProject(t, true)
	report := buildVerifyReport(mustVerifyOptions(cachedProject, false, okProbe))
	if got := promptCheck(t, report.Checks); got.Status != verifyPass {
		t.Fatalf("cached prompt check = %#v, want pass", got)
	}

	prunedProject := promptUnitProject(t, false)
	report = buildVerifyReport(mustVerifyOptions(prunedProject, false, okProbe))
	got := promptCheck(t, report.Checks)
	if got.Status != verifyWarn {
		t.Fatalf("pruned prompt check = %#v, want warn", got)
	}
	if !strings.Contains(got.Detail, "da config sync") {
		t.Fatalf("warning must carry the sync hint, got %q", got.Detail)
	}
	if !report.OK {
		t.Fatal("a missing prompt cache entry is advisory and must not fail the report")
	}
}

// TestVerifyPromptUnitsNoneAndUnreadable covers the two edge shapes: a project
// pinning no prompt units adds no checks, and an unreadable lock degrades to a
// single advisory line rather than an error.
func TestVerifyPromptUnitsNoneAndUnreadable(t *testing.T) {
	project := withRepoLayer(t, `{"version": 2}`, "")
	if checks := verifyPromptUnits(project); len(checks) != 0 {
		t.Fatalf("a project with no prompt units must add no checks, got %#v", checks)
	}

	broken := withRepoLayer(t, `{"version": 2}`, "")
	if err := os.WriteFile(broken+"/.agentsrc.lock", []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	checks := verifyPromptUnits(broken)
	if len(checks) != 1 || checks[0].Status != verifyWarn {
		t.Fatalf("unreadable lock checks = %#v, want one warn", checks)
	}
}

// TestExplainAllListsPromptUnitsJSON is the item-5 contract on the machine
// surface: `--all --json` carries an additive prompt_units array with source,
// path, digest, and cache status.
func TestExplainAllListsPromptUnitsJSON(t *testing.T) {
	project := promptUnitProject(t, true)
	opts := mustOptions(project)
	opts.jsonOut = true
	snap := flatSnapshot(t, nil, map[string]any{"repo_id": "demo"})
	if err := emitAll(opts, snap); err != nil {
		t.Fatalf("emitAll: %v", err)
	}
	var payload struct {
		PromptUnits []PromptUnitView `json:"prompt_units"`
	}
	out := opts.stdout.(*bytes.Buffer).Bytes()
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if len(payload.PromptUnits) != 1 {
		t.Fatalf("prompt_units = %#v, want one entry", payload.PromptUnits)
	}
	got := payload.PromptUnits[0]
	if got.Ref != "team:verifiers/ts-lint.md" || got.Source != "team" || got.Path != "verifiers/ts-lint.md" {
		t.Fatalf("prompt unit = %#v", got)
	}
	if got.Status != promptStatusCached || got.Digest == "" || got.CachePath == "" {
		t.Fatalf("prompt unit = %#v, want a cached status with digest + cache path", got)
	}
}

// TestExplainAllListsPromptUnitsHuman covers the human render for both states and
// proves the section is omitted entirely when nothing is pinned.
func TestExplainAllListsPromptUnitsHuman(t *testing.T) {
	snap := flatSnapshot(t, nil, map[string]any{"repo_id": "demo"})

	cachedOut := explainAllOutput(t, promptUnitProject(t, true), snap)
	for _, want := range []string{"Source-qualified prompt units", "team:verifiers/ts-lint.md", "status : cached"} {
		if !strings.Contains(cachedOut, want) {
			t.Fatalf("human output missing %q:\n%s", want, cachedOut)
		}
	}

	missingOut := explainAllOutput(t, promptUnitProject(t, false), snap)
	if !strings.Contains(missingOut, "status : missing") || !strings.Contains(missingOut, "da config sync") {
		t.Fatalf("missing-prompt render = %s", missingOut)
	}

	noneOut := explainAllOutput(t, withRepoLayer(t, `{"version": 2}`, ""), snap)
	if strings.Contains(noneOut, "Source-qualified prompt units") {
		t.Fatal("the section must be omitted when no prompt units are pinned")
	}
}

// explainAllOutput renders `--all` (human) for a project and returns stdout.
func explainAllOutput(t *testing.T, project string, snap *cfg.Snapshot) string {
	t.Helper()
	opts := mustOptions(project)
	if err := emitAll(opts, snap); err != nil {
		t.Fatalf("emitAll: %v", err)
	}
	return opts.stdout.(*bytes.Buffer).String()
}

// TestPromptUnitViewsUnreadableLock proves explain degrades to no rows (rather
// than a second error) when the lock cannot be read.
func TestPromptUnitViewsUnreadableLock(t *testing.T) {
	broken := withRepoLayer(t, `{"version": 2}`, "")
	if err := os.WriteFile(broken+"/.agentsrc.lock", []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if views := promptUnitViews(broken); len(views) != 0 {
		t.Fatalf("views = %#v, want none for an unreadable lock", views)
	}
}
