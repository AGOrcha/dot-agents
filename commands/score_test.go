package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/NikashPrakash/dot-agents/internal/scoring"
)

// An empty iter-log dir produces a friendly "no iterations" message rather
// than an error — the most common state when the command is first wired in.
func TestScoreRunEmptyIterLog(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := runScoreRun(&buf, scoreRunOpts{iterLogDir: dir, repoDir: dir, noWrite: true}); err != nil {
		t.Fatalf("runScoreRun: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "no iterations found") {
		t.Errorf("expected empty-log notice, got:\n%s", got)
	}
}

// CLI smoke: write a fixture iter-N.score.yaml, then drive `da score iteration N`
// against it and assert the breakdown rendering names every rubric signal.
// This is the smoke the persist task wired toward: a known sidecar, the
// command, an explainable rendering.
func TestScoreIterationRendersBreakdown(t *testing.T) {
	dir := t.TempDir()
	r := scoring.DefaultRubric()
	set := scoring.SignalSet{
		Iteration: 42,
		Verifier:  scoring.PresentSignal(1.0, "all gates green"),
		Scope:     scoring.PresentSignal(0.5, "partial scope"),
	}
	rec := scoring.IterationRecord{
		Iteration: 42,
		Verifiers: []scoring.VerifierRecord{
			{LinkedTraces: []scoring.LinkedTrace{{TraceRef: "t/a", OutcomeRef: "commit:beef"}}},
		},
	}
	if _, err := scoring.WriteIterationScoreWithRecord(dir, r.Score(set), rec); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var buf bytes.Buffer
	if err := runScoreIteration(&buf, dir, 42); err != nil {
		t.Fatalf("runScoreIteration: %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Iteration 42") {
		t.Errorf("missing iteration header in:\n%s", got)
	}
	if !strings.Contains(got, "rubric "+r.Version) {
		t.Errorf("missing rubric version in:\n%s", got)
	}
	if !strings.Contains(got, "linked_traces_to_outcomes: yes") {
		t.Errorf("derived marker not rendered:\n%s", got)
	}
	for _, sig := range r.Signals {
		if !strings.Contains(got, string(sig.ID)) {
			t.Errorf("breakdown missing signal %q in:\n%s", sig.ID, got)
		}
	}
	// The verifier signal was present at sub-score 1.0; its detail string must
	// reach the rendered breakdown — that is the explainability promise.
	if !strings.Contains(got, "all gates green") {
		t.Errorf("verifier detail dropped:\n%s", got)
	}
}

// Iteration without a sidecar errors with a hint to run `da score run`.
func TestScoreIterationMissingSidecar(t *testing.T) {
	dir := t.TempDir()
	err := runScoreIteration(&bytes.Buffer{}, dir, 99)
	if err == nil {
		t.Fatal("expected error for missing sidecar, got nil")
	}
	if !strings.Contains(err.Error(), "no score sidecar") || !strings.Contains(err.Error(), "da score run") {
		t.Errorf("error message should point at the fix: %v", err)
	}
}

// Session smoke: write a session-<id>.score.yaml, drive `da score session <id>`,
// assert the session view renders every per-iteration ref row.
func TestScoreSessionRendersPerIteration(t *testing.T) {
	dir := t.TempDir()
	ss := scoring.SessionScore{
		SessionID:     "smoke-session-1",
		RubricVersion: "9.9.9",
		Iterations:    []int{1, 2, 3},
		Scored:        true,
		Value:         0.75,
		Band:          "good",
		PerIteration: []scoring.SessionIterRef{
			{Iteration: 1, Scored: true, Value: 1.0, Band: "excellent"},
			{Iteration: 2, Scored: true, Value: 0.5, Band: "fair"},
			{Iteration: 3, Scored: false, Value: 0, Band: scoring.BandUnscored},
		},
	}
	if _, err := scoring.WriteSessionScore(dir, ss); err != nil {
		t.Fatalf("WriteSessionScore: %v", err)
	}

	var buf bytes.Buffer
	if err := runScoreSession(&buf, dir, "smoke-session-1"); err != nil {
		t.Fatalf("runScoreSession: %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Session smoke-session-1") {
		t.Errorf("missing session header in:\n%s", got)
	}
	if !strings.Contains(got, "rubric 9.9.9") {
		t.Errorf("missing rubric version in:\n%s", got)
	}
	if !strings.Contains(got, "0.750") || !strings.Contains(got, "good") {
		t.Errorf("missing session value/band in:\n%s", got)
	}
	for _, want := range []string{"1.000", "0.500", "excellent", "fair", scoring.BandUnscored} {
		if !strings.Contains(got, want) {
			t.Errorf("per-iter row missing %q in:\n%s", want, got)
		}
	}
}

// `da score session` errors when the sidecar is missing, again with a hint.
func TestScoreSessionMissingSidecar(t *testing.T) {
	dir := t.TempDir()
	err := runScoreSession(&bytes.Buffer{}, dir, "no-such-session")
	if err == nil {
		t.Fatal("expected error for missing sidecar, got nil")
	}
	if !strings.Contains(err.Error(), "no session sidecar") || !strings.Contains(err.Error(), "da score run") {
		t.Errorf("error message should point at the fix: %v", err)
	}
}

// resolveIterLogDir prefers explicit values and falls back to the default,
// matching how every score subcommand wires its --iter-log-dir flag.
func TestResolveIterLogDirDefault(t *testing.T) {
	if got := resolveIterLogDir(""); got != defaultIterLogDir {
		t.Errorf("default = %q, want %q", got, defaultIterLogDir)
	}
	if got := resolveIterLogDir("/custom/path"); got != "/custom/path" {
		t.Errorf("explicit override = %q, want /custom/path", got)
	}
}

// truncStr shortens strings to a width with an ellipsis; ≤3 widths fall back to
// a hard crop because three dots have no room.
func TestTruncStr(t *testing.T) {
	tests := []struct {
		s     string
		width int
		want  string
	}{
		{"abc", 10, "abc"},
		{"abcdef", 6, "abcdef"},
		{"abcdef", 5, "ab..."},
		{"abcdef", 3, "abc"},
		{"abcdef", 0, "abcdef"},
	}
	for _, tt := range tests {
		got := truncStr(tt.s, tt.width)
		if got != tt.want {
			t.Errorf("truncStr(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
		}
	}
}

// --- t3-cli-readback: hook-outcome source attribution -------------------------

// writeIterScoreSidecar writes a PersistedScore as iter-N.score.yaml under dir.
// Helper for the hook-outcome readback tests so the table-driven cases focus
// on the breakdown shape rather than YAML plumbing.
func writeIterScoreSidecar(t *testing.T, dir string, ps scoring.PersistedScore) {
	t.Helper()
	data, err := yaml.Marshal(ps)
	if err != nil {
		t.Fatalf("marshal score sidecar: %v", err)
	}
	path := filepath.Join(dir, "iter-"+strconv.Itoa(ps.Iteration)+".score.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeHookOutcomesSidecar writes a raw YAML sidecar at the conventional
// iter-N.hook-outcomes.yaml path. Tests use raw YAML (not the wf struct) so
// each case can include fields outside the readback's projection — the
// readback must ignore them without panicking.
func writeHookOutcomesSidecar(t *testing.T, dir string, iter int, body string) {
	t.Helper()
	path := filepath.Join(dir, "iter-"+strconv.Itoa(iter)+".hook-outcomes.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// presentHookRow is a PersistedContribution shaped as the hook_outcomes
// signal at the remediate band — the most adversarial path through the
// renderer (sub-score 0.0 is the gate that drove the bug class this task
// surfaces). Reused across the readback test table.
func presentHookRow(detail string) scoring.PersistedContribution {
	return scoring.PersistedContribution{
		Signal:          scoring.SignalHookOutcomes,
		Label:           "Hook-gate outcomes",
		Present:         true,
		SubScore:        0.0,
		Detail:          detail,
		NominalWeight:   0.10,
		EffectiveWeight: 0.10,
		Contribution:    0.0,
	}
}

// absentHookRow is the breakdown row written when no sidecar voted. The
// renderer must NOT print a "Hook outcome sources" block when the signal
// row is absent even if a sidecar happens to exist on disk — the row's
// Present=false is the load-bearing gate.
func absentHookRow() scoring.PersistedContribution {
	return scoring.PersistedContribution{
		Signal:        scoring.SignalHookOutcomes,
		Label:         "Hook-gate outcomes",
		Present:       false,
		NominalWeight: 0.10,
	}
}

// validHookSidecar is a two-record sidecar where one record is scored
// (remediate_at_stop) and another is deferred (continuity_advice). Only
// the scored record may surface in the readback list per design D3/D6.
const validHookSidecar = `schema_version: 1
records:
  - schema_version: 1
    sentinel_id: iteration-close-r1
    skill: iteration-close
    lifecycle_point: stop
    intervention_class: remediate_at_stop
    result: remediate
    rule_id: iteration-close.R1.1
    platform: claude
    ts: "2026-05-26T00:00:00Z"
    correlation_id: c1
  - schema_version: 1
    sentinel_id: continuity-pre-compact
    skill: iteration-close
    lifecycle_point: pre_compact
    intervention_class: continuity_advice
    result: advise
    rule_id: iteration-close.R5.1
    platform: claude
    ts: "2026-05-26T00:00:01Z"
    correlation_id: c2
`

func TestRunScoreIterationRendersHookOutcomeSources(t *testing.T) {
	tests := []struct {
		name            string
		hookRow         scoring.PersistedContribution
		sidecar         string
		wantHookBlock   bool
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:          "present row + scored record renders sentinel and rule",
			hookRow:       presentHookRow("remediate: iteration-close.R1.1"),
			sidecar:       validHookSidecar,
			wantHookBlock: true,
			wantContains: []string{
				"Hook outcome sources:",
				"RULE_ID",
				"SENTINEL_ID",
				"iteration-close.R1.1",
				"iteration-close-r1",
				"stop",
				"remediate",
				"remediate_at_stop",
			},
			// The deferred continuity_advice record exists in the sidecar
			// but MUST NOT appear in the readback — it never voted.
			wantNotContains: []string{
				"continuity-pre-compact",
				"continuity_advice",
				"iteration-close.R5.1",
			},
		},
		{
			name:          "absent hook row suppresses the block even when sidecar exists",
			hookRow:       absentHookRow(),
			sidecar:       validHookSidecar,
			wantHookBlock: false,
			wantNotContains: []string{
				"Hook outcome sources:",
				"iteration-close.R1.1",
			},
		},
		{
			name:          "present row but no sidecar on disk degrades silently",
			hookRow:       presentHookRow("remediate: iteration-close.R1.1"),
			sidecar:       "", // no file written
			wantHookBlock: false,
			wantNotContains: []string{
				"Hook outcome sources:",
				"SENTINEL_ID",
			},
		},
		{
			name:          "malformed sidecar degrades silently",
			hookRow:       presentHookRow("remediate: iteration-close.R1.1"),
			sidecar:       "this: is: not: valid: yaml: at all",
			wantHookBlock: false,
			wantNotContains: []string{
				"Hook outcome sources:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupHookSourcesFixture(t, 11, tt.hookRow, tt.sidecar)
			got := runScoreIterationToString(t, dir, 11)
			assertOutputContains(t, got, tt.wantContains, tt.wantNotContains)
			// Belt-and-braces: the readback contract forbids transcript
			// content. The sidecar schema does not model any such field, so
			// asserting on a sentinel string proves the renderer is not
			// fabricating one from elsewhere.
			assertNotContains(t, got, "transcript", "readback contract violation")
		})
	}
}

// setupHookSourcesFixture writes a one-row PersistedScore sidecar at iter and,
// when sidecar is non-empty, the matching hook-outcomes sidecar. Returns the
// temp dir.
func setupHookSourcesFixture(t *testing.T, iter int, row scoring.PersistedContribution, sidecar string) string {
	t.Helper()
	dir := t.TempDir()
	ps := scoring.PersistedScore{
		Iteration:     iter,
		RubricVersion: "2.1.0",
		Scored:        true,
		Value:         0.5,
		Band:          "fair",
		Breakdown:     []scoring.PersistedContribution{row},
	}
	writeIterScoreSidecar(t, dir, ps)
	if sidecar != "" {
		writeHookOutcomesSidecar(t, dir, iter, sidecar)
	}
	return dir
}

// runScoreIterationToString invokes runScoreIteration into a buffer and
// returns the rendered string. Fails the test if the call errors.
func runScoreIterationToString(t *testing.T, dir string, iter int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := runScoreIteration(&buf, dir, iter); err != nil {
		t.Fatalf("runScoreIteration: %v", err)
	}
	return buf.String()
}

// assertOutputContains asserts every want substring is present and every
// notWant substring is absent in got.
func assertOutputContains(t *testing.T, got string, want, notWant []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q:\n%s", w, got)
		}
	}
	for _, nw := range notWant {
		assertNotContains(t, got, nw, "")
	}
}

// assertNotContains fails the test if needle appears in haystack.
func assertNotContains(t *testing.T, haystack, needle, reason string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		return
	}
	if reason == "" {
		t.Errorf("output unexpectedly contains %q:\n%s", needle, haystack)
		return
	}
	t.Errorf("output leaked %q — %s:\n%s", needle, reason, haystack)
}

func TestRunScoreIterationJSONIncludesHookOutcomeSources(t *testing.T) {
	tests := []struct {
		name             string
		hookRow          scoring.PersistedContribution
		writeSidecar     bool
		wantSourcesLen   int
		wantSourcesField bool // whether hook_outcome_sources should appear in JSON
		wantSentinelID   string
		wantRuleID       string
	}{
		{
			name:             "present row emits hook_outcome_sources with scored record only",
			hookRow:          presentHookRow("remediate: iteration-close.R1.1"),
			writeSidecar:     true,
			wantSourcesLen:   1,
			wantSourcesField: true,
			wantSentinelID:   "iteration-close-r1",
			wantRuleID:       "iteration-close.R1.1",
		},
		{
			name:             "absent row omits hook_outcome_sources field",
			hookRow:          absentHookRow(),
			writeSidecar:     true,
			wantSourcesField: false,
		},
		{
			name:             "present row + no sidecar omits hook_outcome_sources field",
			hookRow:          presentHookRow("remediate: iteration-close.R1.1"),
			writeSidecar:     false,
			wantSourcesField: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sidecar := ""
			if tt.writeSidecar {
				sidecar = validHookSidecar
			}
			dir := setupHookSourcesFixture(t, 22, tt.hookRow, sidecar)
			withJSONFlag(t)

			rawJSON := runScoreIterationToString(t, dir, 22)
			payload := decodeJSONPayload(t, rawJSON)
			assertHookSourcesField(t, payload, rawJSON, tt.wantSourcesField, tt.wantSourcesLen, tt.wantSentinelID, tt.wantRuleID)

			// Negative-path inside the JSON case: the disallowed transcript
			// field name must not appear at any depth, and the deferred
			// continuity record must not have leaked through the filter.
			assertNotContains(t, rawJSON, "transcript", "readback contract violation")
			assertNotContains(t, rawJSON, "continuity", "deferred-record filter regression")
		})
	}
}

// withJSONFlag flips Flags.JSON on for the duration of the test and restores
// the prior value on cleanup.
func withJSONFlag(t *testing.T) {
	t.Helper()
	prev := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prev })
}

// decodeJSONPayload parses raw into a generic map. Fails the test on error.
func decodeJSONPayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode JSON: %v\nraw:\n%s", err, raw)
	}
	return payload
}

// assertHookSourcesField asserts presence/absence of hook_outcome_sources in
// payload and — when present — that it contains exactly wantLen records whose
// first entry carries wantSentinelID + wantRuleID.
func assertHookSourcesField(t *testing.T, payload map[string]any, rawJSON string, wantField bool, wantLen int, wantSentinelID, wantRuleID string) {
	t.Helper()
	raw, present := payload["hook_outcome_sources"]
	if present != wantField {
		t.Fatalf("hook_outcome_sources present=%v, want %v\nraw:\n%s", present, wantField, rawJSON)
	}
	if !wantField {
		return
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("hook_outcome_sources type = %T, want []any", raw)
	}
	if len(arr) != wantLen {
		t.Fatalf("hook_outcome_sources len = %d, want %d (raw: %v)", len(arr), wantLen, arr)
	}
	rec, _ := arr[0].(map[string]any)
	if rec["sentinel_id"] != wantSentinelID {
		t.Errorf("sentinel_id = %v, want %q", rec["sentinel_id"], wantSentinelID)
	}
	if rec["rule_id"] != wantRuleID {
		t.Errorf("rule_id = %v, want %q", rec["rule_id"], wantRuleID)
	}
}

// Direct test of the loader so a future refactor of the renderer cannot mask
// a regression in the filter / sort contract.
func TestLoadHookOutcomeSourcesFilterAndOrder(t *testing.T) {
	dir := t.TempDir()
	body := `schema_version: 1
records:
  - schema_version: 1
    sentinel_id: zzz-late
    skill: loop-worker
    lifecycle_point: pre_tool_use
    intervention_class: prevent_before_action
    result: allow
    rule_id: loop-worker.R3.1
    platform: claude
    ts: "2026-05-26T00:00:02Z"
    correlation_id: c3
  - schema_version: 1
    sentinel_id: aaa-early
    skill: iteration-close
    lifecycle_point: stop
    intervention_class: remediate_at_stop
    result: remediate
    rule_id: iteration-close.R1.1
    platform: claude
    ts: "2026-05-26T00:00:00Z"
    correlation_id: c1
  - schema_version: 1
    sentinel_id: mid
    skill: loop-worker
    lifecycle_point: post_tool_use
    intervention_class: observe_tool_result
    result: advise
    rule_id: loop-worker.R9.1
    platform: claude
    ts: "2026-05-26T00:00:01Z"
    correlation_id: c2
`
	writeHookOutcomesSidecar(t, dir, 5, body)

	got := loadHookOutcomeSources(dir, 5)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (observe_tool_result must be filtered): %+v", len(got), got)
	}
	// Sort key is (rule_id, sentinel_id); iteration-close.R1.1 sorts before
	// loop-worker.R3.1 so the remediate record comes first regardless of
	// on-disk order.
	if got[0].RuleID != "iteration-close.R1.1" || got[0].SentinelID != "aaa-early" {
		t.Errorf("got[0] = %+v, want rule iteration-close.R1.1 / sentinel aaa-early", got[0])
	}
	if got[1].RuleID != "loop-worker.R3.1" || got[1].SentinelID != "zzz-late" {
		t.Errorf("got[1] = %+v, want rule loop-worker.R3.1 / sentinel zzz-late", got[1])
	}
	for _, s := range got {
		if !scoredHookInterventionClasses[s.InterventionClass] {
			t.Errorf("record with non-scored intervention_class slipped through: %+v", s)
		}
	}
}

// Loader returns nil when the sidecar file does not exist — the readback is
// best-effort, never an error path. Pairs with the missing-sidecar render
// case above to lock the contract from both sides.
func TestLoadHookOutcomeSourcesMissingFileReturnsNil(t *testing.T) {
	dir := t.TempDir()
	if got := loadHookOutcomeSources(dir, 99); got != nil {
		t.Errorf("missing sidecar returned %+v, want nil", got)
	}
}

// End-to-end mini-smoke for the iteration sidecar's YAML shape: the renderer
// must read what the persister writes without an in-band conversion table.
func TestScoreIterationSidecarRoundTripsThroughCommand(t *testing.T) {
	dir := t.TempDir()
	src := scoring.PersistedScore{
		Iteration:              7,
		RubricVersion:          "test-rubric",
		Scored:                 true,
		Value:                  0.42,
		Band:                   "poor",
		LinkedTracesToOutcomes: true,
		Breakdown: []scoring.PersistedContribution{
			{Signal: "landed", Label: "Landed", Present: true, SubScore: 0.42, NominalWeight: 1.0, EffectiveWeight: 1.0, Contribution: 0.42, Detail: "marker text"},
		},
	}
	data, err := yaml.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "iter-7.score.yaml"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := runScoreIteration(&buf, dir, 7); err != nil {
		t.Fatalf("runScoreIteration: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Iteration 7", "test-rubric", "0.420", "poor", "marker text", "linked_traces_to_outcomes: yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
