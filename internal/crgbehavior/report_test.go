package crgbehavior

import (
	"bytes"
	"strings"
	"testing"
)

func renderOf(t *testing.T, r Report) string {
	t.Helper()
	var buf bytes.Buffer
	r.Render(&buf)
	return buf.String()
}

func TestRenderLocatesEveryDivergence(t *testing.T) {
	views := fixtureViews()
	views.FlowMemberships = views.FlowMemberships[:1]
	views.Communities = map[string]string{idEntry: "c1", idStep: "c2", idWidget: "c3", idLone: "c4"}
	report, err := Run(fixtureConfig(), views, agreeingImpact())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := renderOf(t, report)
	for _, want := range []string{
		"§11.4 criterion 2",       // which gate
		"commit 11111111",         // which commit
		"feat: touch a.go",        // what the review was
		"a.go",                    // which files
		"FAIL  flows",             // which query diverged, gating
		"WARN  communities",       // which query diverged, advisory
		"advisory:",               // why the advisory surface is not gating
		idStep,                    // the structural diff itself
		"1 of 1 task(s) executed", // corpus coverage
		"GATE: FAIL",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report is missing %q; a decommission decision is made on this output:\n%s", want, out)
		}
	}
}

func TestRenderMarksSkipsAndStrictMode(t *testing.T) {
	cfg := fixtureConfig(Task{Commit: "2222222222222222", Subject: "docs", ChangedFiles: []string{"absent.go"}})
	cfg.Strict = true
	report, err := Run(cfg, fixtureViews(), &fakeImpact{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := renderOf(t, report)
	if !strings.Contains(out, "SKIP  no symbol in either graph") {
		t.Fatalf("a skipped task must say why:\n%s", out)
	}
	if !strings.Contains(out, "STRICT") {
		t.Fatalf("the header must state the gating mode:\n%s", out)
	}
	if !strings.Contains(out, "0 of 1 task(s) executed") || !strings.Contains(out, "GATE: FAIL") {
		t.Fatalf("a corpus that executed nothing must not read as PASS:\n%s", out)
	}
}

func TestRenderPassAndSurfaceSkipNotes(t *testing.T) {
	report, err := Run(fixtureConfig(), fixtureViews(), agreeingImpact())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := renderOf(t, report)
	if !strings.Contains(out, "GATE: PASS") || !strings.Contains(out, "PASS  changed_nodes") {
		t.Fatalf("a clean run must render PASS:\n%s", out)
	}
	if !strings.Contains(out, "4 bridge symbols") {
		t.Fatalf("the header must state the graph the comparison ran over:\n%s", out)
	}
}

func TestRenderNamesSidesAndPrintsNoControlCharacters(t *testing.T) {
	views := fixtureViews()
	views.FlowMemberships = views.FlowMemberships[:1]
	report, err := Run(fixtureConfig(), views, agreeingImpact())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := renderOf(t, report)
	if strings.Contains(out, "\x00") {
		t.Fatalf("the flow oracle's NUL row separator must not reach the report:\n%q", out)
	}
	if strings.Contains(out, "only in a") || strings.Contains(out, "only in b") {
		t.Fatalf("the report must name the SIDES, not the oracle's a/b inputs:\n%s", out)
	}
	if !strings.Contains(out, "only in NATIVE") {
		t.Fatalf("a missing bridge row must be attributed to the native side:\n%s", out)
	}
}

func TestFailingSurfacesCountsBothTiers(t *testing.T) {
	views := fixtureViews()
	views.FlowMemberships = nil
	views.Communities = map[string]string{idEntry: "c1", idStep: "c2", idWidget: "c3", idLone: "c4"}
	report, err := Run(fixtureConfig(), views, agreeingImpact())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	failing := report.FailingSurfaces()
	if failing[SurfaceFlows] != 1 || failing[SurfaceCommunities] != 1 {
		t.Fatalf("FailingSurfaces = %v, want both the gating and advisory divergence", failing)
	}
	if strings.Count(renderOf(t, report), "(advisory)") == 0 {
		t.Fatal("the summary must mark which divergent surfaces are advisory")
	}
}
