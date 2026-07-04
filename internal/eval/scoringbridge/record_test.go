package scoringbridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/scoring"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// runVariants covers the emit-shape permutations: identity/tokens present or
// not, build ran or not, and the three test-command outcomes.
func runVariants(t *testing.T) map[string]EvalRun {
	t.Helper()
	full := testRun(t)

	noAgent := testRun(t)
	noAgent.Agent = AgentTelemetry{}

	noBuild := testRun(t)
	noBuild.Verify = VerifyResult{TestRan: true, TestPassed: false}

	neverRan := testRun(t)
	neverRan.Verify = VerifyResult{}

	return map[string]EvalRun{
		"full passing run":            full,
		"no agent identity or tokens": noAgent,
		"no build cmd, failing test":  noBuild,
		"verification never ran":      neverRan,
	}
}

// TestEmitRecordMatchesProductionParser pins the translation invariant:
// toIterationRecord must equal what scoring.ParseIterationRecord — the
// production schema-aware parser — yields from the marshaled bytes. If the
// two ever diverge, the persisted record and the scored record would describe
// different runs.
func TestEmitRecordMatchesProductionParser(t *testing.T) {
	for name, run := range runVariants(t) {
		t.Run(name, func(t *testing.T) {
			emit := buildEmitRecord(run)
			data, err := yaml.Marshal(emit)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			parsed, err := scoring.ParseIterationRecord(data)
			if err != nil {
				t.Fatalf("ParseIterationRecord() = %v, want nil", err)
			}
			if got := emit.toIterationRecord(); !reflect.DeepEqual(parsed, got) {
				t.Errorf("translation diverges from production parse:\n got %+v\nwant %+v", got, parsed)
			}
		})
	}
}

// TestEmittedRecordValidatesAgainstIterLogSchema validates every emitted
// variant against the canonical workflow iter-log JSON schema — the same
// schema_version-2 contract the orchestration CLI writes, so eval entries are
// indistinguishable in shape from production entries (spec D4.4/R5).
func TestEmittedRecordValidatesAgainstIterLogSchema(t *testing.T) {
	sch := compileIterLogSchema(t)
	for name, run := range runVariants(t) {
		t.Run(name, func(t *testing.T) {
			assertEmittedRecordValid(t, sch, run)
		})
	}
}

// assertEmittedRecordValid marshals one emit variant to YAML, round-trips it to
// JSON, and validates it against the compiled iter-log schema. Extracted from
// the table loop so the error handling stays flat (each check is one step, not
// nested inside for -> subtest closure).
func assertEmittedRecordValid(t *testing.T, sch *jsonschema.Schema, run EvalRun) {
	t.Helper()
	data, err := yaml.Marshal(buildEmitRecord(run))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("re-parse emitted YAML: %v", err)
	}
	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("to JSON: %v", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonBytes))
	if err != nil {
		t.Fatalf("jsonschema.UnmarshalJSON: %v", err)
	}
	if err := sch.Validate(inst); err != nil {
		t.Errorf("emitted record violates workflow-iter-log schema: %v\nrecord:\n%s", err, data)
	}
}

// compileIterLogSchema compiles the repo's canonical iter-log schema.
func compileIterLogSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	const schemaURL = "./schemas/workflow-iter-log.schema.json"
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "workflow-iter-log.schema.json"))
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaURL, doc); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	sch, err := c.Compile(schemaURL)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return sch
}

func TestVerifierBlocks(t *testing.T) {
	truth, falsehood := true, false
	cases := []struct {
		name string
		in   VerifyResult
		want []emitVerifier
	}{
		{
			name: "build and test pass",
			in:   VerifyResult{BuildRan: true, BuildPassed: true, TestRan: true, TestPassed: true},
			want: []emitVerifier{
				{Type: "build", Status: "pass", GatePassed: true},
				{Type: "test", Status: "pass", GatePassed: true, TestsTotalPass: &truth},
			},
		},
		{
			name: "build fails, test fails",
			in:   VerifyResult{BuildRan: true, TestRan: true},
			want: []emitVerifier{
				{Type: "build", Status: "fail"},
				{Type: "test", Status: "fail", TestsTotalPass: &falsehood},
			},
		},
		{
			name: "no build cmd, test passes",
			in:   VerifyResult{TestRan: true, TestPassed: true},
			want: []emitVerifier{
				{Type: "test", Status: "pass", GatePassed: true, TestsTotalPass: &truth},
			},
		},
		{
			name: "nothing ran",
			in:   VerifyResult{},
			want: []emitVerifier{
				{Type: "test", Status: "unknown"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := verifierBlocks(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("verifierBlocks() = %d entries, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i].Type != tc.want[i].Type || got[i].Status != tc.want[i].Status ||
					got[i].GatePassed != tc.want[i].GatePassed ||
					!reflect.DeepEqual(optionalBool(got[i].TestsTotalPass), optionalBool(tc.want[i].TestsTotalPass)) {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestBuildEmitRecordFields(t *testing.T) {
	run := testRun(t)
	emit := buildEmitRecord(run)

	if emit.SchemaVersion != 2 || emit.Iteration != evalIteration {
		t.Errorf("header = {v%d iter %d}, want {v2 iter %d}", emit.SchemaVersion, emit.Iteration, evalIteration)
	}
	if emit.Wave != run.RunID {
		t.Errorf("Wave = %q, want the run id %q", emit.Wave, run.RunID)
	}
	if emit.TaskID != run.Spec.TaskID {
		t.Errorf("TaskID = %q, want %q", emit.TaskID, run.Spec.TaskID)
	}
	if emit.Commit != run.BaseCommit {
		t.Errorf("Commit = %q, want the base commit %q", emit.Commit, run.BaseCommit)
	}
	if emit.Date != "2026-07-02" || emit.CheckpointAt != "2026-07-02T10:30:00Z" {
		t.Errorf("timestamps = (%q, %q), want the fixed FinishedAt rendering", emit.Date, emit.CheckpointAt)
	}
	if emit.Agent == nil || emit.Agent.Model != run.Agent.Model {
		t.Errorf("Agent = %+v, want the runner identity (spec R9)", emit.Agent)
	}
	if emit.SessionTokens == nil || emit.SessionTokens.CacheHitRate != 0.8 {
		t.Errorf("SessionTokens = %+v, want the captured telemetry", emit.SessionTokens)
	}
}

func TestAgentBlockOmittedWhenIdentityEmpty(t *testing.T) {
	if got := agentBlock(AgentTelemetry{Retries: 2}); got != nil {
		t.Errorf("agentBlock() = %+v, want nil when no identity was captured", got)
	}
}

func TestRunSummaryNamesTaskAndDifficulty(t *testing.T) {
	run := testRun(t)
	got := runSummary(run)
	want := "eval run kg-go-impl-001-x7: go/medium task kg-go-impl-001 scored via the R1 bridge"
	if got != want {
		t.Errorf("runSummary() = %q, want %q", got, want)
	}
}
