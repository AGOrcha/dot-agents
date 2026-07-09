package eval

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/harness"
	"github.com/AGOrcha/dot-agents/internal/eval/runner"
	"github.com/AGOrcha/dot-agents/internal/eval/scoringbridge"
	"github.com/AGOrcha/dot-agents/internal/eval/store"
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// scoredRun builds a fully-populated harness.EvalRun + store.Result for the
// happy-path renderers.
func scoredRun() (harness.EvalRun, store.Result) {
	run := harness.EvalRun{
		Spec:       validSpec(),
		RunID:      "eval-render-1",
		BaseCommit: zeroCommit,
		Run: runner.Result{
			ExitCode:  0,
			Duration:  time.Second,
			Telemetry: runner.AgentTelemetry{Harness: "fake-harness", Model: "test-model"},
		},
		Verify: &verifier.VerifyResult{Passed: true, Phase: verifier.PhaseTest, ExitCode: 0},
		Score: scoringbridge.Result{
			Score: scoring.Score{Value: 0.812, Band: "good", Scored: true, RubricVersion: "9.9.9"},
		},
	}
	res := store.Result{
		RunDir:       "/runs/eval-render-1",
		TaskspecPath: "/runs/eval-render-1/taskspec.yaml",
		EvalRunPath:  "/runs/eval-render-1/eval-run.yaml",
		RecordPath:   "/runs/eval-render-1/iteration-log/iter-1.yaml",
		ScorePath:    "/runs/eval-render-1/iteration-log/iter-1.score.yaml",
	}
	return run, res
}

func TestRenderRunText(t *testing.T) {
	run, res := scoredRun()
	var buf bytes.Buffer
	if err := renderRun(&buf, run, res, false); err != nil {
		t.Fatalf("renderRun: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Eval run eval-render-1",
		"kg-go-impl-fixture",
		"fake-harness/test-model",
		"pass",
		"0.812",
		"band good",
		"rubric 9.9.9",
		"eval-run.yaml",
		"iter-1.score.yaml",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text render missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderRunJSON(t *testing.T) {
	run, res := scoredRun()
	var buf bytes.Buffer
	if err := renderRun(&buf, run, res, true); err != nil {
		t.Fatalf("renderRun json: %v", err)
	}
	var got runJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode json: %v\n%s", err, buf.String())
	}
	if got.RunID != "eval-render-1" || !got.Score.Scored || got.Score.Value != 0.812 {
		t.Errorf("json envelope = %+v", got)
	}
	if got.Verify.Phase != string(verifier.PhaseTest) || !got.Verify.Passed {
		t.Errorf("json verify = %+v", got.Verify)
	}
	if got.Sidecars.EvalRun != res.EvalRunPath {
		t.Errorf("json sidecar eval_run = %q, want %q", got.Sidecars.EvalRun, res.EvalRunPath)
	}
}

// The degraded shapes: nil verify, unscored score, unknown harness, no model.
func TestRenderRunTextDegraded(t *testing.T) {
	run := harness.EvalRun{
		Spec:   &evalcore.TaskSpec{TaskID: "t", Language: evalcore.LanguageGo, Difficulty: evalcore.DifficultyHard},
		RunID:  "eval-degraded",
		Run:    runner.Result{ExitCode: 2},
		Verify: nil,
		Score:  scoringbridge.Result{Score: scoring.Score{Scored: false, Band: scoring.BandUnscored}},
	}
	var buf bytes.Buffer
	if err := renderRun(&buf, run, store.Result{}, false); err != nil {
		t.Fatalf("renderRun: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"(unknown)", "(not run)", "band " + scoring.BandUnscored} {
		if !strings.Contains(out, want) {
			t.Errorf("degraded render missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "0.000") {
		t.Errorf("unscored run should render a dash, not a numeric score:\n%s", out)
	}
}

// nil verify must also be tolerated by the JSON path.
func TestRenderRunJSONNilVerify(t *testing.T) {
	run := harness.EvalRun{
		Spec:   validSpec(),
		RunID:  "eval-nilverify",
		Run:    runner.Result{Telemetry: runner.AgentTelemetry{Harness: "h"}},
		Verify: nil,
		Score:  scoringbridge.Result{Score: scoring.Score{Scored: true, Value: 0.5, Band: "fair"}},
	}
	var buf bytes.Buffer
	if err := renderRun(&buf, run, store.Result{}, true); err != nil {
		t.Fatalf("renderRun json: %v", err)
	}
	var got runJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Verify.Passed {
		t.Error("nil verify should marshal to passed=false")
	}
	// harness-only agent (no model) renders as the bare harness.
	if got.Agent.Harness != "h" || got.Agent.Model != "" {
		t.Errorf("agent = %+v", got.Agent)
	}
}
