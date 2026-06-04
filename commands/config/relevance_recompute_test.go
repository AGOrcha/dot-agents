package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// errFakeWrite is returned by a writer stub to exercise JSON-render error paths.
var errFakeWrite = errors.New("fake write failure")

// recomputeRepoBody is a fixture .agentsrc.json whose go-cli profile lists units
// across two stages so the recompute classifier, gap miner, and proposed-layer
// builder all have data to exercise.
const recomputeRepoBody = `{
  "repo_id": "fixture",
  "execution_profile": {
    "by_app_type": {
      "go-cli": {
        "relevance": {
          "verify": {
            "core": ["unit"],
            "situational": ["cli-runner"]
          },
          "review": {
            "core": ["review-pr"],
            "situational": ["self-review"],
            "noise": ["article-extract"]
          }
        }
      }
    },
    "default_class": "situational"
  }
}`

// mustRecomputeOptions builds a runRelevanceOptions already switched to the
// recompute path (--recompute) so the shared command struct exercises the
// driver-event flow exactly as `da config relevance --recompute` does.
func mustRecomputeOptions(project string) *runRelevanceOptions {
	return &runRelevanceOptions{
		recompute: true,
		appType:   "go-cli",
		stdout:    &bytes.Buffer{},
		stderr:    &bytes.Buffer{},
		cwd:       project,
	}
}

func recomputeOut(opts *runRelevanceOptions) string {
	return opts.stdout.(*bytes.Buffer).String()
}

// seedIter writes an iter-N.yaml (v2) and, when scored, its iter-N.score.yaml
// sidecar under the project's iteration-log dir.
func seedIter(t *testing.T, project string, n int, body string) {
	t.Helper()
	dir := filepath.Join(project, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir iter dir: %v", err)
	}
	path := filepath.Join(dir, iterName(n))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func seedScore(t *testing.T, project string, n int, scored bool, value float64) {
	t.Helper()
	dir := filepath.Join(project, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir iter dir: %v", err)
	}
	body := "iteration: " + itoaTest(n) + "\n" +
		"rubric_version: 2.1.0\n" +
		"scored: " + boolStr(scored) + "\n" +
		"value: " + floatStr(value) + "\n" +
		"band: good\n"
	path := filepath.Join(dir, "iter-"+itoaTest(n)+".score.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write score %s: %v", path, err)
	}
}

func iterName(n int) string { return "iter-" + itoaTest(n) + ".yaml" }

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func floatStr(v float64) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// iterBody builds a minimal v2 iteration-log entry whose summary/verifiers carry
// the supplied citation text.
func iterBody(n int, summary string, verifierType string) string {
	return "schema_version: 2\n" +
		"iteration: " + itoaTest(n) + "\n" +
		"date: \"2026-06-04\"\n" +
		"wave: \"n/a\"\n" +
		"task_id: \"\"\n" +
		"commit: \"abc\"\n" +
		"files_changed: 1\n" +
		"lines_added: 1\n" +
		"lines_removed: 0\n" +
		"impl:\n" +
		"  item: \"x\"\n" +
		"  summary: \"" + summary + "\"\n" +
		"  scope_note: \"\"\n" +
		"  feedback_goal: \"\"\n" +
		"  retries: 0\n" +
		"  focused_tests_added: 0\n" +
		"verifiers:\n" +
		"  - type: \"" + verifierType + "\"\n" +
		"    status: pass\n" +
		"    gate_passed: true\n" +
		"    tests_added: 0\n" +
		"    result_artifact: \"\"\n" +
		"review:\n" +
		"  phase_1_decision: \"\"\n" +
		"  phase_2_decision: \"\"\n" +
		"  overall_decision: \"\"\n" +
		"  escalation_reason: \"\"\n" +
		"  reviewer_notes: \"\"\n" +
		"  decision_artifact: \"\"\n"
}

// ---------- runRecompute: required flag ----------

func TestRunRecompute_RequiresAppType(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	opts := mustRecomputeOptions(project)
	opts.appType = "  "
	err := runRelevance(opts, testDeps())
	if err == nil {
		t.Fatal("expected error when --app-type is empty")
	}
	he, ok := err.(*hintError)
	if !ok {
		t.Fatalf("expected hintError, got %T", err)
	}
	if !strings.Contains(he.message, "requires --app-type") {
		t.Fatalf("unexpected message: %q", he.message)
	}
}

// ---------- runRecompute: missing manifest ----------

func TestRunRecompute_NoManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("AGENTS_HOME", filepath.Join(root, "home", ".agents"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	opts := mustRecomputeOptions(filepath.Join(root, "no-project"))
	if err := runRelevance(opts, testDeps()); err == nil {
		t.Fatal("expected error when no .agentsrc.json exists")
	}
}

// ---------- runRecompute: malformed execution_profile ----------

func TestRunRecompute_BadProfile(t *testing.T) {
	project := withRepoLayer(t, `{"repo_id":"x","execution_profile":"not-an-object"}`, "")
	opts := mustRecomputeOptions(project)
	if err := runRelevance(opts, testDeps()); err == nil {
		t.Fatal("expected error decoding a non-object execution_profile")
	}
}

// ---------- runRecompute: malformed score sidecar ----------

func TestRunRecompute_BadScoreSidecar(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	seedIter(t, project, 1, iterBody(1, "ran unit verifier", "unit"))
	dir := filepath.Join(project, ".agents", "active", "iteration-log")
	// A YAML sequence where the PersistedScore mapping is expected forces an
	// unmarshal type error rather than a silent zero-value parse.
	if err := os.WriteFile(filepath.Join(dir, "iter-1.score.yaml"), []byte("- not\n- a\n- mapping\n"), 0o644); err != nil {
		t.Fatalf("write bad score: %v", err)
	}
	opts := mustRecomputeOptions(project)
	if err := runRelevance(opts, testDeps()); err == nil {
		t.Fatal("expected error parsing a malformed score sidecar")
	}
}

// ---------- runRecompute: malformed iter log ----------

func TestRunRecompute_BadIterLog(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	seedIter(t, project, 1, "schema_version: 2\niteration: 1\nimpl: [not, a, map]\n")
	opts := mustRecomputeOptions(project)
	if err := runRelevance(opts, testDeps()); err == nil {
		t.Fatal("expected error parsing a malformed iteration log")
	}
}

// ---------- runRecompute: happy path, human render, classifies + suppresses ----------

func TestRunRecompute_HappyHuman(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	// "unit" cited in two passing iterations -> proposed core (stays core).
	seedIter(t, project, 1, iterBody(1, "implemented thing", "unit"))
	seedScore(t, project, 1, true, 0.9)
	seedIter(t, project, 2, iterBody(2, "more work", "unit"))
	seedScore(t, project, 2, true, 0.8)
	// "self-review" cited only in a low-scoring iteration -> actively suppressed
	// to noise (changed from situational). This is the suppression path that
	// requires real negative evidence, not silence (the silent-zero guard means
	// a never-cited unit would simply hold its class).
	seedIter(t, project, 3, iterBody(3, "ran self-review lens", "cli-runner"))
	seedScore(t, project, 3, true, 0.2)

	opts := mustRecomputeOptions(project)
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRecompute: %v", err)
	}
	out := recomputeOut(opts)

	for _, want := range []string{
		"Relevance recompute (app_type: go-cli)",
		"iterations      : 3",
		"inputs_digest   :",
		"proposals",
		"self-review",
		"cited-in-low-scoring",
		"-> noise",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("human output missing %q\n---\n%s", want, out)
		}
	}
}

// ---------- runRecompute: JSON render ----------

func TestRunRecompute_JSON(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	seedIter(t, project, 1, iterBody(1, "did things", "unit"))
	seedScore(t, project, 1, true, 0.9)

	opts := mustRecomputeOptions(project)
	opts.jsonOut = true
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRecompute: %v", err)
	}
	var got recomputeResult
	if err := json.Unmarshal([]byte(recomputeOut(opts)), &got); err != nil {
		t.Fatalf("decode json: %v\n%s", err, recomputeOut(opts))
	}
	if got.AppType != "go-cli" {
		t.Fatalf("AppType = %q", got.AppType)
	}
	if got.IterationsScanned != 1 {
		t.Fatalf("IterationsScanned = %d", got.IterationsScanned)
	}
	if got.InputsDigest == "" {
		t.Fatal("expected a non-empty inputs_digest")
	}
	if len(got.Proposals) == 0 {
		t.Fatal("expected proposals")
	}
}

// ---------- runRecompute: --write emits proposed layer ----------

func TestRunRecompute_WriteEmitsProposedLayer(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	seedIter(t, project, 1, iterBody(1, "x", "unit"))
	seedScore(t, project, 1, true, 0.9)

	opts := mustRecomputeOptions(project)
	opts.write = true
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRecompute: %v", err)
	}
	out := recomputeOut(opts)
	if !strings.Contains(out, "PROPOSED execution_profile layer") {
		t.Fatalf("expected proposed layer banner\n%s", out)
	}
	if !strings.Contains(out, "by_app_type") {
		t.Fatalf("expected proposed layer json\n%s", out)
	}
}

func TestRunRecompute_WriteJSONCarriesProposedLayer(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	seedIter(t, project, 1, iterBody(1, "x", "unit"))
	seedScore(t, project, 1, true, 0.9)

	opts := mustRecomputeOptions(project)
	opts.write = true
	opts.jsonOut = true
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRecompute: %v", err)
	}
	var got recomputeResult
	if err := json.Unmarshal([]byte(recomputeOut(opts)), &got); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if got.ProposedLayer == nil {
		t.Fatal("expected ProposedLayer with --write")
	}
	if _, ok := got.ProposedLayer.ByAppType["go-cli"]; !ok {
		t.Fatal("proposed layer missing go-cli entry")
	}
}

// ---------- runRecompute: empty corpus (fresh repo) ----------

func TestRunRecompute_EmptyCorpus(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	opts := mustRecomputeOptions(project)
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRecompute on empty corpus: %v", err)
	}
	out := recomputeOut(opts)
	if !strings.Contains(out, "iterations      : 0") {
		t.Fatalf("expected zero iterations\n%s", out)
	}
	if !strings.Contains(out, "(none)") {
		t.Fatalf("expected (none) digest on empty corpus\n%s", out)
	}
}

// ---------- runRecompute: app_type with no units ----------

func TestRunRecompute_NoUnitsForAppType(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	opts := mustRecomputeOptions(project)
	opts.appType = "ideation" // not listed in fixture
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRecompute: %v", err)
	}
	out := recomputeOut(opts)
	if !strings.Contains(out, "lists no units to recompute") {
		t.Fatalf("expected no-units message\n%s", out)
	}
}

// ---------- runRecompute: --stage restricts ----------

func TestRunRecompute_StageRestrict(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	opts := mustRecomputeOptions(project)
	opts.stage = "verify"
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRecompute: %v", err)
	}
	out := recomputeOut(opts)
	if !strings.Contains(out, "stage           : verify") {
		t.Fatalf("expected stage header\n%s", out)
	}
	// review-stage units must not appear when restricted to verify.
	if strings.Contains(out, "review-pr") {
		t.Fatalf("review unit leaked into verify-only recompute\n%s", out)
	}
}

// ---------- buildRecomputeResult: pure classification ----------

func TestBuildRecomputeResult_Classification(t *testing.T) {
	profile := &cfg.ExecutionProfile{
		ByAppType: map[string]cfg.AppTypeProfile{
			"go-cli": {
				Relevance: map[string]cfg.RelevanceClasses{
					"review": {
						Core:        []string{"keeper"},
						Situational: []string{"riser"},
						Noise:       []string{"sinker"},
					},
				},
			},
		},
		DefaultClass: "situational",
	}
	corpus := scoredCorpus{
		digest: "d",
		entries: []corpusEntry{
			// keeper cited twice passing -> core
			{iteration: 1, haystack: "keeper riser", scored: true, value: 0.9},
			{iteration: 2, haystack: "keeper", scored: true, value: 0.8},
			// sinker cited only in a low-scoring iteration -> noise (stays)
			{iteration: 3, haystack: "sinker", scored: true, value: 0.1},
			// unscored iteration: cites riser but does not vote
			{iteration: 4, haystack: "riser", scored: false, value: 0},
		},
	}
	opts := &runRelevanceOptions{recompute: true, appType: "go-cli"}
	res := buildRecomputeResult(opts, profile, corpus)

	byUnit := map[string]unitProposal{}
	for _, p := range res.Proposals {
		byUnit[p.Unit] = p
	}
	if got := byUnit["keeper"].ProposedClass; got != "core" {
		t.Fatalf("keeper proposed %q, want core", got)
	}
	// riser cited once passing (iter 1 only votes) -> situational, not core
	if got := byUnit["riser"].ProposedClass; got != "situational" {
		t.Fatalf("riser proposed %q, want situational", got)
	}
	if byUnit["riser"].PassingCitations != 1 {
		t.Fatalf("riser passing = %d, want 1", byUnit["riser"].PassingCitations)
	}
	if got := byUnit["sinker"].ProposedClass; got != "noise" {
		t.Fatalf("sinker proposed %q, want noise", got)
	}
	if byUnit["keeper"].Changed {
		t.Fatal("keeper should be unchanged (core -> core)")
	}
}

// ---------- proposedClass: table ----------

func TestProposedClass(t *testing.T) {
	cases := []struct {
		name    string
		current string
		sig     unitSignal
		want    string
	}{
		// Silent-zero guard: no evidence holds the current class (never demotes).
		{"never cited holds core", "core", unitSignal{}, "core"},
		{"never cited holds situational", "situational", unitSignal{}, "situational"},
		{"never cited holds noise", "noise", unitSignal{}, "noise"},
		{"low scoring -> noise", "situational", unitSignal{lowScoring: 3, passing: 1}, "noise"},
		{"two passing -> core", "situational", unitSignal{passing: 2}, "core"},
		{"one passing -> situational", "core", unitSignal{passing: 1}, "situational"},
		{"noise rehabilitated thinly -> situational", "noise", unitSignal{passing: 1}, "situational"},
		{"noise rehabilitated strongly -> core", "noise", unitSignal{passing: 2}, "core"},
		{"tie favors passing", "situational", unitSignal{passing: 1, lowScoring: 1}, "situational"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := proposedClass(tc.current, tc.sig); got != tc.want {
				t.Fatalf("proposedClass(%q,%+v) = %q, want %q", tc.current, tc.sig, got, tc.want)
			}
		})
	}
}

// ---------- unitSignal.label: table ----------

func TestUnitSignalLabel(t *testing.T) {
	cases := []struct {
		sig  unitSignal
		want string
	}{
		{unitSignal{}, signalNeverCited},
		{unitSignal{passing: 2}, signalCitedInPassing},
		{unitSignal{lowScoring: 2}, signalCitedInLowScoring},
		{unitSignal{passing: 1, lowScoring: 2}, signalCitedInLowScoring},
		{unitSignal{passing: 2, lowScoring: 1}, signalCitedInPassing},
	}
	for _, tc := range cases {
		if got := tc.sig.label(); got != tc.want {
			t.Fatalf("label(%+v) = %q, want %q", tc.sig, got, tc.want)
		}
	}
}

// ---------- citesUnit / token boundary ----------

func TestCitesUnit(t *testing.T) {
	cases := []struct {
		hay    string
		needle string
		want   bool
	}{
		{"ran the unit verifier", "unit", true},
		{"reviewunit ran", "unit", false},        // suffix attached
		{"the units passed", "unit", false},      // plural -> different token
		{"review-pr opened", "review-pr", true},  // hyphen is in-token
		{"review pr opened", "review-pr", false}, // space splits
		{"loop-worker; loop-worker", "loop-worker", true},
		{"", "unit", false},
		{"unit", "unit", true},
		{"x-unit-y", "unit", false}, // hyphen-flanked, still attached
	}
	for _, tc := range cases {
		if got := citesUnit(tc.hay, tc.needle); got != tc.want {
			t.Fatalf("citesUnit(%q,%q) = %v, want %v", tc.hay, tc.needle, got, tc.want)
		}
	}
}

func TestUnitCitationSignal_EmptyNeedle(t *testing.T) {
	corpus := scoredCorpus{entries: []corpusEntry{{haystack: "anything", scored: true, value: 1}}}
	if got := unitCitationSignal("   ", corpus); got.total() != 0 {
		t.Fatalf("empty needle should not match, got %+v", got)
	}
}

// ---------- gaps ----------

func TestCorpusGaps(t *testing.T) {
	corpus := scoredCorpus{
		entries: []corpusEntry{
			// gaps are mined from the structured unitTokens, never the free-text
			// haystack — so a prose slug in haystack must not surface.
			{
				haystack:   "prose mentions some-task-id that is not a unit",
				unitTokens: []string{"cli-runner", "new-helper", ""},
				scored:     true,
				value:      0.9,
			},
			{unitTokens: []string{"new-helper"}, scored: true, value: 0.5},
		},
	}
	listed := map[string]bool{"cli-runner": true}
	gaps := corpusGaps(corpus, listed)
	if !containsStr(gaps, "new-helper") {
		t.Fatalf("expected new-helper gap, got %v", gaps)
	}
	if containsStr(gaps, "cli-runner") {
		t.Fatalf("listed unit should not appear as a gap, got %v", gaps)
	}
	if containsStr(gaps, "some-task-id") {
		t.Fatalf("free-text prose slug must not be mined as a gap, got %v", gaps)
	}
	if containsStr(gaps, "") {
		t.Fatalf("empty token must not be a gap, got %v", gaps)
	}
}

func TestStructuredUnitTokens(t *testing.T) {
	rec := scoring.IterationRecord{
		Verifiers: []scoring.VerifierRecord{
			{Type: "Unit"},
			{Type: "  "}, // blank -> dropped
			{Type: "cli-runner"},
		},
		Review: scoring.ReviewBlock{FailedGates: []string{"Lens-Gate", ""}},
	}
	got := structuredUnitTokens(rec)
	if !containsStr(got, "unit") || !containsStr(got, "cli-runner") || !containsStr(got, "lens-gate") {
		t.Fatalf("expected lower-cased verifier+gate tokens, got %v", got)
	}
	if containsStr(got, "") {
		t.Fatalf("blank tokens should be dropped, got %v", got)
	}
}

// ---------- proposedLayer ----------

func TestProposedLayer(t *testing.T) {
	proposals := []unitProposal{
		{Unit: "a", Stage: "review", ProposedClass: "core"},
		{Unit: "b", Stage: "review", ProposedClass: "noise"},
		{Unit: "c", Stage: "review", ProposedClass: "situational"},
		{Unit: "d", Stage: "verify", ProposedClass: "core"},
	}
	layer := proposedLayer("go-cli", proposals, "situational")
	if layer.DefaultClass != "situational" {
		t.Fatalf("DefaultClass = %q", layer.DefaultClass)
	}
	prof := layer.ByAppType["go-cli"]
	if !containsStr(prof.Relevance["review"].Core, "a") {
		t.Fatal("a should be core in review")
	}
	if !containsStr(prof.Relevance["review"].Noise, "b") {
		t.Fatal("b should be noise in review")
	}
	if !containsStr(prof.Relevance["review"].Situational, "c") {
		t.Fatal("c should be situational in review")
	}
	if !containsStr(prof.Relevance["verify"].Core, "d") {
		t.Fatal("d should be core in verify")
	}
}

// ---------- stageUnits / recomputeStages ----------

func TestStageUnitsDedup(t *testing.T) {
	classes := cfg.RelevanceClasses{
		Core:        []string{"x", "y"},
		Situational: []string{"y", "z"}, // y listed twice
		Noise:       []string{"w"},
	}
	got := stageUnits(classes)
	if len(got) != 4 {
		t.Fatalf("expected 4 unique units, got %v", got)
	}
	// sorted
	want := []string{"w", "x", "y", "z"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stageUnits not sorted/deduped: %v", got)
		}
	}
}

func TestRecomputeStages(t *testing.T) {
	prof := cfg.AppTypeProfile{
		Relevance: map[string]cfg.RelevanceClasses{"review": {}, "verify": {}, "orchestrate": {}},
	}
	if got := recomputeStages(prof, "review"); len(got) != 1 || got[0] != "review" {
		t.Fatalf("stage restrict failed: %v", got)
	}
	all := recomputeStages(prof, "")
	want := []string{"orchestrate", "review", "verify"}
	if len(all) != len(want) {
		t.Fatalf("expected 3 sorted stages, got %v", all)
	}
	for i := range want {
		if all[i] != want[i] {
			t.Fatalf("stages not sorted: %v", all)
		}
	}
}

// ---------- citationHaystack ----------

func TestCitationHaystack(t *testing.T) {
	r := scoring.IterationRecord{
		Impl: scoring.ImplBlock{Summary: "Summary-Text", ScopeNote: "Scope-Text"},
		Verifiers: []scoring.VerifierRecord{
			{
				Type:             "unit",
				ResultArtifact:   "Artifact-Path",
				TestsAddedByKind: []scoring.TestAdded{{Name: "Test-Name", Kind: "positive"}},
			},
		},
		Review: scoring.ReviewBlock{
			DecisionArtifact: "decision/foo",
			FailedGates:      []string{"Lens-Gate"},
		},
	}
	hay := citationHaystack(r)
	for _, want := range []string{"summary-text", "scope-text", "unit", "test-name", "artifact-path", "lens-gate"} {
		if !strings.Contains(hay, want) {
			t.Fatalf("haystack missing %q: %q", want, hay)
		}
	}
	if hay != strings.ToLower(hay) {
		t.Fatalf("haystack should be lower-cased: %q", hay)
	}
}

// ---------- loadIterationScore: missing sidecar ----------

func TestLoadIterationScore_Missing(t *testing.T) {
	dir := t.TempDir()
	_, ok, err := loadIterationScore(dir, 7)
	if err != nil {
		t.Fatalf("missing sidecar should not error: %v", err)
	}
	if ok {
		t.Fatal("missing sidecar should report ok=false")
	}
}

// TestLoadIterationScore_ReadError exercises the non-IsNotExist read-error
// branch: the score "path" is itself a directory, so os.ReadFile fails with a
// non-NotExist error that must surface.
func TestLoadIterationScore_ReadError(t *testing.T) {
	dir := t.TempDir()
	// IterationScorePath(dir, 9) -> dir/iter-9.score.yaml; make it a directory.
	scorePath := filepath.Join(dir, "iter-9.score.yaml")
	if err := os.Mkdir(scorePath, 0o755); err != nil {
		t.Fatalf("mkdir score-as-dir: %v", err)
	}
	if _, _, err := loadIterationScore(dir, 9); err == nil {
		t.Fatal("expected a read error when the sidecar path is a directory")
	}
}

// ---------- byte / rune classifiers ----------

func TestIsUnitByte(t *testing.T) {
	for _, b := range []byte{'a', 'z', 'A', 'Z', '0', '9', '-', '_'} {
		if !isUnitByte(b) {
			t.Fatalf("isUnitByte(%q) = false, want true", b)
		}
	}
	for _, b := range []byte{' ', '.', '/', ':', '*', '\n'} {
		if isUnitByte(b) {
			t.Fatalf("isUnitByte(%q) = true, want false", b)
		}
	}
}

// ---------- dirExists ----------

func TestDirExists(t *testing.T) {
	dir := t.TempDir()
	if !dirExists(dir) {
		t.Fatal("expected dirExists true for a real dir")
	}
	if dirExists(filepath.Join(dir, "nope")) {
		t.Fatal("expected dirExists false for a missing path")
	}
	// a file is not a dir
	f := filepath.Join(dir, "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if dirExists(f) {
		t.Fatal("expected dirExists false for a file path")
	}
}

// ---------- label helpers ----------

func TestRecomputeDigestLabel(t *testing.T) {
	if recomputeDigestLabel("") != "(none)" {
		t.Fatal("empty digest should label as (none)")
	}
	if recomputeDigestLabel("abc") != "abc" {
		t.Fatal("non-empty digest should pass through")
	}
}

func TestProposedClassSuffix(t *testing.T) {
	if proposedClassSuffix(unitProposal{Changed: false, ProposedClass: "noise"}) != "" {
		t.Fatal("unchanged proposal should have empty suffix")
	}
	if got := proposedClassSuffix(unitProposal{Changed: true, ProposedClass: "noise"}); got != " -> noise" {
		t.Fatalf("changed suffix = %q", got)
	}
}

// ---------- cobra command wiring ----------

// TestRelevanceCmd_RecomputeFlagRegistered asserts the recompute path is exposed
// as a flag on `da config relevance` (design §5: `--recompute [--write]`), not a
// subcommand — the surface that was reworked here.
func TestRelevanceCmd_RecomputeFlagRegistered(t *testing.T) {
	cmd := newRelevanceCmd(testDeps())
	if cmd.Flags().Lookup("recompute") == nil {
		t.Fatal("missing --recompute flag on `relevance`")
	}
	if cmd.Flags().Lookup("write") == nil {
		t.Fatal("missing --write flag on `relevance`")
	}
	// recompute must NOT have re-appeared as a subcommand.
	for _, sub := range cmd.Commands() {
		if sub.Name() == "recompute" {
			t.Fatal("recompute should be a flag, not a subcommand")
		}
	}
}

// TestRelevanceCmd_RecomputeRunEEndToEnd drives the cobra RunE closure (cwd
// resolution + dispatch into the recompute path) via `--recompute` in a real
// project dir.
func TestRelevanceCmd_RecomputeRunEEndToEnd(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	seedIter(t, project, 1, iterBody(1, "ran unit", "unit"))
	seedScore(t, project, 1, true, 0.9)

	// Run from inside the project so os.Getwd in the closure resolves there.
	t.Chdir(project)

	cmd := newRelevanceCmd(testDeps())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--recompute", "--app-type", "go-cli"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Relevance recompute (app_type: go-cli)") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

// TestRelevanceCmd_WriteWithoutRecomputeIsUsageError guards the inspector path
// from a stray --write: it only applies with --recompute, so it must be flagged
// rather than silently ignored.
func TestRelevanceCmd_WriteWithoutRecomputeIsUsageError(t *testing.T) {
	project := withRepoLayer(t, recomputeRepoBody, "")
	opts := &runRelevanceOptions{
		write:  true, // no --recompute
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		cwd:    project,
	}
	err := runRelevance(opts, testDeps())
	if err == nil {
		t.Fatal("expected a usage error for --write without --recompute")
	}
	he, ok := err.(*hintError)
	if !ok {
		t.Fatalf("expected hintError, got %T", err)
	}
	if !strings.Contains(he.message, "--write only applies with --recompute") {
		t.Fatalf("unexpected message: %q", he.message)
	}
}

// recomputeWriterFails forces renderRecompute's writeJSON branch to error so the
// --write JSON error path is covered.
type recomputeWriterFails struct{}

func (recomputeWriterFails) Write([]byte) (int, error) {
	return 0, errFakeWrite
}

func TestPrintRecomputeHuman_WriteJSONError(t *testing.T) {
	res := recomputeResult{
		AppType:       "go-cli",
		Write:         true,
		ProposedLayer: &cfg.ExecutionProfile{},
	}
	err := printRecomputeHuman(recomputeWriterFails{}, res)
	if err == nil {
		t.Fatal("expected error when the proposed-layer JSON write fails")
	}
}

// ---------- small shared test utilities ----------

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
