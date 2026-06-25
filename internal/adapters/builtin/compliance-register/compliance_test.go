package complianceregister_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	complianceregister "github.com/AGOrcha/dot-agents/internal/adapters/builtin/compliance-register"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// corpusPath resolves the testdata corpus relative to the repo root.
func corpusPath(t *testing.T) string {
	t.Helper()
	// The package lives at internal/adapters/builtin/compliance-register; the
	// corpus is at <root>/testdata/compliance/corpus.json.
	return filepath.Join("..", "..", "..", "..", "testdata", "compliance", "corpus.json")
}

// loadCorpusView constructs the adapter, bootstraps the corpus through the SDK,
// and returns the adapter plus the populated namespace view.
func loadCorpusView(t *testing.T) (*complianceregister.Adapter, sdk.NamespaceView) {
	t.Helper()
	a := complianceregister.New()
	data, err := os.ReadFile(corpusPath(t))
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	s, err := a.Bootstrap(sdk.NewMemStore(), data)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	view, err := a.LoadView(s)
	if err != nil {
		t.Fatalf("load view: %v", err)
	}
	return a, view
}

// collect gathers the non-nil string values of a column across rows.
func collect(rows []sdk.Row, col string) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		if s, ok := r[col].(string); ok && s != "" {
			out[s] = true
		}
	}
	return out
}

// TestAdapterRegisters confirms the adapter registers and resolves by ref.
func TestAdapterRegisters(t *testing.T) {
	reg := registry.New()
	if err := complianceregister.Register(reg); err != nil {
		t.Fatalf("register: %v", err)
	}
	a, err := reg.Resolve("dotagents-builtin:graph/compliance-register@^1.0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if a.Name() != complianceregister.Name {
		t.Fatalf("resolved wrong adapter: %s", a.Name())
	}
}

// TestHardTestImpactRadius is the §13.2 hard test (first half): an impact_radius
// query on a control status change returns its regulations, risks, findings,
// evidence, and owner.
func TestHardTestImpactRadius(t *testing.T) {
	a, view := loadCorpusView(t)
	rows, err := a.RunImpact(view, []string{"ctl-mfa"})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	assertContains(t, "regulations", collect(rows, "reg.id"), "reg-soc2-cc6", "reg-nist-ac2")
	assertContains(t, "risks", collect(rows, "r.id"), "risk-unauth-access")
	assertContains(t, "findings", collect(rows, "f.id"), "find-mfa-gap")
	assertContains(t, tEvidence, collect(rows, "e.id"), "ev-mfa-screenshot")
	assertContains(t, "owner", collect(rows, "changed.owner.name"), "Alice Stone")
}

// TestHardTestPolicyReviewDueImpact is the §13.2 hard test (second half): when
// the policy.review_due webhook fires, controls deriving from that policy become
// derivation-stale and policy_review_due_impact surfaces them.
func TestHardTestPolicyReviewDueImpact(t *testing.T) {
	a, view := loadCorpusView(t)
	// Fire the policy.review_due webhook for pol-access only, then run the
	// named query DIRECTLY — no manual derivation step. FireEnvTrigger drives
	// the full env→derivation lifecycle (§7.3), so a real caller gets the
	// surfaced controls from the single fire. If FireEnvTrigger alone did NOT
	// propagate derivation (the hollow-lifecycle anti-pattern), this query
	// would return zero rows and the assertions below would fail.
	view, err := a.FireEnvTrigger(view, dsl.EnvTrigger{Kind: dsl.KindWebhook, Endpoint: "policy.review_due", Targets: []string{"pol-access"}, TriggerID: "wh-1"})
	if err != nil {
		t.Fatalf("fire env: %v", err)
	}
	rows, err := a.RunNamed("policy_review_due_impact", view, nil)
	if err != nil {
		t.Fatalf("named query: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("FireEnvTrigger alone must drive policy_review_due_impact (no manual derivation step) — got zero rows")
	}
	ctlIDs := collect(rows, "c.control_id")
	// ctl-mfa and ctl-rbac derive from pol-access; ctl-retention derives from
	// pol-retention (not fired) and must NOT appear.
	assertContains(t, "review-due controls", ctlIDs, idCtlMFA, "AC-3-RBAC")
	if ctlIDs["DR-1-RETENTION"] {
		t.Fatalf("ctl-retention should not be surfaced (its policy did not fire)")
	}
}

// TestStaleEvidenceQuery exercises the evidence time_after env predicate +
// stale_evidence_unsupported_controls query.
func TestStaleEvidenceQuery(t *testing.T) {
	a, view := loadCorpusView(t)
	now, _ := time.Parse("2006-01-02", "2026-06-01") // past ev-mfa-screenshot's 2026-02-01 expiry
	view, err := a.FireEnvTrigger(view, dsl.EnvTrigger{Kind: dsl.KindTimeAfter, Now: now, TriggerID: "exp-1"})
	if err != nil {
		t.Fatalf("fire time_after: %v", err)
	}
	rows, err := a.RunNamed("stale_evidence_unsupported_controls", view, nil)
	if err != nil {
		t.Fatalf("named query: %v", err)
	}
	ctls := collect(rows, "c.control_id")
	if !ctls[idCtlMFA] {
		t.Fatalf("expired ev-mfa-screenshot should surface AC-2-MFA, got %v", ctls)
	}
	if ctls["AC-3-RBAC"] {
		t.Fatalf("ev-rbac-log (expires 2027) is not expired; AC-3-RBAC must not surface")
	}
}

// TestRegulationsSatisfiedBy exercises the param-driven named query.
func TestRegulationsSatisfiedBy(t *testing.T) {
	a, view := loadCorpusView(t)
	rows, err := a.RunNamed("regulations_satisfied_by", view, map[string]any{"control_id": idCtlMFA})
	if err != nil {
		t.Fatalf("named query: %v", err)
	}
	regs := collect(rows, "reg.id")
	assertContains(t, "satisfied regs", regs, "reg-soc2-cc6", "reg-nist-ac2")
}

func assertContains(t *testing.T, label string, got map[string]bool, want ...string) {
	t.Helper()
	for _, w := range want {
		if !got[w] {
			t.Fatalf("%s: expected %q in result set, got %v", label, w, keys(got))
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
