package crgbehavior

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleReport is a one-task run with one of each surface outcome.
func sampleReport() Report {
	return Report{
		RepoRoot: "/repo", Head: "1111111111111111", GeneratedFrom: "origin/master",
		PinnedVersion: PinnedVersion, PinnedSchemaVersion: PinnedSchemaVersion,
		Release:     ReleaseInfo{Package: PackageName, Version: PinnedVersion, SchemaVersion: PinnedSchemaVersion},
		CorpusTasks: 2,
		Tasks: []TaskReport{{
			Commit: "1111111111111111", Subject: "seed commit",
			ChangedFiles: []string{"pkg/a.go"}, Languages: []string{"go"},
			GraphSymbols: 3, GraphEdges: 3, GraphFiles: 2, NativeSymbols: 3,
			Surfaces: []Surface{
				{Name: SurfaceChangedNodes, Status: StatusAgree, Metric: "native=2 bridge=2 row(s)"},
				{Name: SurfaceFlowMetrics, Status: StatusDiverge, Metric: "native=unimplemented bridge=1 row(s)",
					Reason: ErrNativeUnimplemented.Error(), Detail: []string{"only in BRIDGE: entry=a depth=1"}},
				{Name: SurfaceRiskIndex, Status: StatusNotExercised, Reason: "the release scored nothing"},
			},
		}},
		Coverage: []SurfaceCoverage{
			{Surface: SurfaceChangedNodes, Exercised: 1, Satisfied: true},
			{Surface: SurfaceFlowMetrics, Exercised: 1, Diverged: 1, Satisfied: true},
			{Surface: SurfaceRiskIndex, Reasons: []string{"the release scored nothing"}},
		},
	}
}

// The report is the evidence. It must name the release the run was produced
// against — pinned AND observed — before anything else, or a reader cannot tell
// which baseline the verdict refers to.
func TestRenderNamesThePinnedAndObservedRelease(t *testing.T) {
	var out strings.Builder
	sampleReport().Render(&out)
	text := out.String()
	for _, want := range []string{
		"pinned:   " + PackageName + " " + PinnedVersion,
		"observed: " + PackageName + " " + PinnedVersion,
		"graph schema v9",
		"1 of 2 pinned review task(s)",
		"ITS OWN commit",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report does not state %q:\n%s", want, text)
		}
	}
}

// Every surface outcome must be legible, a divergence must carry its diff, and
// the run must end in an explicit verdict plus a quotable sign-off claim.
func TestRenderShowsEveryOutcomeAndTheSignOff(t *testing.T) {
	var out strings.Builder
	sampleReport().Render(&out)
	text := out.String()
	for _, want := range []string{
		"AGREE  changed_nodes",
		"DIFFER flow_metrics",
		"only in BRIDGE: entry=a depth=1",
		"NOTRUN risk_index",
		"required surface coverage",
		"MISSING  risk_index",
		"GATE: FAIL",
		"SIGN-OFF: criterion 2 NOT SATISFIED",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report does not contain %q:\n%s", want, text)
		}
	}
}

// A sign-off has to say what failed, so the §11.4 decision can quote one line.
func TestSignOffNamesWhyTheRunIsNotASignOff(t *testing.T) {
	report := sampleReport()
	signOff := report.SignOff()
	for _, want := range []string{"divergent surface(s): flow_metrics", "never exercised: risk_index"} {
		if !strings.Contains(signOff, want) {
			t.Fatalf("sign-off %q omits %q", signOff, want)
		}
	}
	report.Tasks[0].Failure = "worktree build failed"
	if !strings.Contains(report.SignOff(), "could not run: 111111111111") {
		t.Fatalf("sign-off %q omits the task that could not run", report.SignOff())
	}
}

// A clean run states what it actually established: which release, how many
// tasks, and that every required surface was exercised.
func TestSignOffOnAPassingRunStatesWhatWasCompared(t *testing.T) {
	report := sampleReport()
	report.Tasks[0].Surfaces = []Surface{{Name: SurfaceChangedNodes, Status: StatusAgree}}
	report.Coverage = []SurfaceCoverage{{Surface: SurfaceChangedNodes, Exercised: 1, Satisfied: true}}
	if report.Verdict() != VerdictPass {
		t.Fatalf("verdict = %s, want PASS", report.Verdict())
	}
	signOff := report.SignOff()
	if !strings.Contains(signOff, "criterion 2 SATISFIED") ||
		!strings.Contains(signOff, "their own commits") ||
		!strings.Contains(signOff, PinnedVersion) {
		t.Fatalf("sign-off %q does not state what was established", signOff)
	}
}

// The JSON artifact is the machine-readable half of the evidence; the release
// and schema version have to survive into it.
func TestWriteJSONPersistsTheReleaseAndVerdictInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "gate.json")
	if err := sampleReport().WriteJSON(path); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("artifact is not valid JSON: %v", err)
	}
	if decoded["pinned_version"] != PinnedVersion {
		t.Fatalf("artifact pinned_version = %v, want %s", decoded["pinned_version"], PinnedVersion)
	}
	observed, _ := decoded["observed_release"].(map[string]any)
	if observed["version"] != PinnedVersion || observed["schema_version"].(float64) != float64(PinnedSchemaVersion) {
		t.Fatalf("artifact observed_release = %v, want the driven release and schema version", observed)
	}
	if _, ok := decoded["required_surface_coverage"]; !ok {
		t.Fatal("artifact carries no required-surface coverage, so the contract verdict is unauditable")
	}
}

// Divergences are counted by surface so a systematic gap is visible at a glance.
func TestDivergentSurfacesCountsFailuresAndDivergences(t *testing.T) {
	report := sampleReport()
	report.Tasks[0].Surfaces = append(report.Tasks[0].Surfaces,
		Surface{Name: SurfaceFlows, Status: StatusFailed, Reason: "boom"})
	got := report.DivergentSurfaces()
	if got[SurfaceFlowMetrics] != 1 || got[SurfaceFlows] != 1 {
		t.Fatalf("divergent surfaces = %v, want both the divergence and the failure counted", got)
	}
	if got[SurfaceRiskIndex] != 0 {
		t.Fatalf("a not-exercised surface was counted as divergent: %v", got)
	}
}

// grTBlockedDir returns a path that cannot be created as a directory because a
// regular file occupies one of its parents. It is the deterministic way to
// drive a write failure without depending on the process's uid or umask.
func grTBlockedDir(t *testing.T) string {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("stage blocker file: %v", err)
	}
	return filepath.Join(blocker, "nested")
}

// The corpus contract is an independent failure axis: a run whose every oracle
// agreed still is not a sign-off if it never exercised a required surface.
// Without this the gate would pass by comparing a convenient subset.
func TestGrTVerdictFailsOnAnUnsatisfiedContractAlone(t *testing.T) {
	report := Report{
		Release: ReleaseInfo{Package: PackageName, Version: PinnedVersion, SchemaVersion: PinnedSchemaVersion},
		Tasks: []TaskReport{{
			Commit:   "1111111111111111",
			Surfaces: []Surface{{Name: SurfaceChangedNodes, Status: StatusAgree}},
		}},
		Coverage: []SurfaceCoverage{
			{Surface: SurfaceChangedNodes, Exercised: 1, Satisfied: true},
			{Surface: SurfaceFlows},
		},
	}
	if report.Verdict() != VerdictFail {
		t.Fatalf("verdict = %s, want FAIL: every oracle agreed but a required surface was never exercised",
			report.Verdict())
	}
	if len(report.DivergentSurfaces()) != 0 {
		t.Fatalf("divergent surfaces = %v, want none — the failure is contract coverage",
			report.DivergentSurfaces())
	}
	if !strings.Contains(report.SignOff(), "never exercised: "+SurfaceFlows) {
		t.Fatalf("sign-off %q does not name the unexercised required surface", report.SignOff())
	}
}

// A run that compared nothing is the ABSENCE of evidence. Its sign-off must say
// so rather than read like a neutral or passing result.
func TestGrTSignOffOnARunWithNoComparisonIsNotEstablished(t *testing.T) {
	signOff := Report{}.SignOff()
	for _, want := range []string{"NOT ESTABLISHED", "no comparison", "absence of evidence"} {
		if !strings.Contains(signOff, want) {
			t.Fatalf("inconclusive sign-off %q omits %q", signOff, want)
		}
	}
	if strings.Contains(signOff, "SATISFIED") {
		t.Fatalf("inconclusive sign-off %q reads as a verdict on preserved behavior", signOff)
	}
}

// A task that could not run has no comparison to report. Rendering its file,
// graph or surface lines would state facts the run never established.
func TestGrTRenderStatesAFailedTaskAndNothingElseAboutIt(t *testing.T) {
	report := sampleReport()
	report.Tasks[0].Failure = "worktree build failed"
	report.Tasks[0].Surfaces = nil
	var out strings.Builder
	report.Render(&out)
	text := out.String()
	if !strings.Contains(text, "ERROR worktree build failed") {
		t.Fatalf("report does not state the task failure:\n%s", text)
	}
	for _, absent := range []string{"files:", "graph:"} {
		if strings.Contains(text, absent) {
			t.Fatalf("a failed task still rendered %q, which the run never established:\n%s", absent, text)
		}
	}
	if !strings.Contains(text, "GATE: FAIL") {
		t.Fatalf("a run with a failed task did not render FAIL:\n%s", text)
	}
}

// "the comparison itself blew up" is its own word in the report. Printing it as
// NOTRUN would read as an absent view rather than a broken comparison.
func TestGrTRenderLabelsAFailedComparisonAsError(t *testing.T) {
	report := sampleReport()
	report.Tasks[0].Surfaces = []Surface{
		{Name: SurfaceFlows, Status: StatusFailed, Reason: "native flows: readback refused"},
	}
	var out strings.Builder
	report.Render(&out)
	text := out.String()
	for _, want := range []string{"ERROR  " + SurfaceFlows, "native flows: readback refused"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "NOTRUN "+SurfaceFlows) {
		t.Fatalf("a failed comparison rendered as not-run:\n%s", text)
	}
}

// A run judged against no required surfaces must not print an empty contract
// block — a heading with nothing under it reads as "nothing was required AND
// nothing was checked", which is the opposite of the contract's purpose.
func TestGrTRenderOmitsTheCoverageBlockWhenNoSurfaceIsRequired(t *testing.T) {
	report := sampleReport()
	report.Coverage = nil
	var out strings.Builder
	report.Render(&out)
	text := out.String()
	if strings.Contains(text, "required surface coverage") {
		t.Fatalf("an empty contract still rendered its heading:\n%s", text)
	}
	if !strings.Contains(text, "GATE: FAIL") {
		t.Fatalf("the verdict is still owed even without a contract block:\n%s", text)
	}
}

// A waived surface is a REVIEWED decision, so the report must label it WAIVED
// and print its attribution. Rendering it as MISSING or COVERED would hide
// either the waiver or the fact that nothing was compared.
func TestGrTRenderAttributesAWaivedRequiredSurface(t *testing.T) {
	waiver := RatifiedException{Surface: SurfaceFlows,
		Reason: "this corpus pins no commit with a traced flow", RatifiedBy: "t6-bridge-decommission"}
	report := sampleReport()
	report.Tasks[0].Surfaces = []Surface{{Name: SurfaceChangedNodes, Status: StatusAgree}}
	report.Coverage = []SurfaceCoverage{
		{Surface: SurfaceChangedNodes, Exercised: 1, Satisfied: true},
		{Surface: SurfaceFlows, Ratified: &waiver, Satisfied: true},
	}
	var out strings.Builder
	report.Render(&out)
	text := out.String()
	for _, want := range []string{
		"WAIVED   " + SurfaceFlows,
		"ratified by " + waiver.RatifiedBy + ": " + waiver.Reason,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "MISSING") {
		t.Fatalf("a ratified surface rendered as MISSING:\n%s", text)
	}
	if report.Verdict() != VerdictPass {
		t.Fatalf("verdict = %s, want PASS once the only unexercised surface is ratified", report.Verdict())
	}
}

// The JSON artifact is half the evidence, so a failure to persist it must reach
// the caller. Swallowing it would leave a run claiming evidence it never wrote.
func TestGrTWriteJSONSurfacesAnUnwritableArtifactPath(t *testing.T) {
	path := filepath.Join(grTBlockedDir(t), "gate.json")
	err := sampleReport().WriteJSON(path)
	if err == nil {
		t.Fatalf("WriteJSON(%s) = nil, want the write failure reported", path)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatalf("WriteJSON reported a failure but %s exists", path)
	}
}
