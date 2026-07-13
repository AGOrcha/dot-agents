// Package views declares the compliance-register adapter's cross-adapter
// materialized views (graph-backend-adapter-contract §8.3). It is the single
// declaration site for the compliance register's cross-namespace dependency on
// the CRG adapter: the view's reads_from set (here, the `crg` namespace), the
// cross-namespace edge that links a compliance evidence note to a CRG symbol,
// and the DSL query that joins them all live here.
//
// This package lives under the compliance-register adapter (not under crg)
// because the dependent owns the cross-adapter coordination (§2.3, §10.3): the
// consumer's instance is where both adapters are co-installed, so the consumer's
// view declares — and its lockfile tracks — the dependency. CRG never knows its
// dependents.
//
// # Adaptation to the shipped schemas
//
// The §8.3 worked example is illustrative against a fuller hypothetical schema:
// it names a `function` note type, a `references` evidence→function edge, and a
// `f.last_changed > e.collected_at` field-to-field comparison. The SHIPPED CRG
// adapter (t4) models code symbols as the `symbol` note type, and the shipped
// compliance schema (t2) has no evidence→symbol link, so this view adapts the
// example to what the two adapters actually declare:
//
//   - `symbol` is used in place of `function` (the shipped CRG note type);
//   - a `references` cross edge (evidence → symbol) is declared here, since it
//     spans the two namespaces and appears in neither adapter's own schema.
//
// ⚠️  KNOWN-INCORRECT APPROXIMATION (BUG-2) — PENDING OWNER DECISION  ⚠️
//
// The spec view means "evidence references a function that CHANGED SINCE the
// evidence was collected" (design.md §8.3: `f.last_changed > e.collected_at`).
// This implementation filters `s.stale.reason = 'source'`, which is a CURRENT
// staleness marker, NOT a "changed since collected" predicate. It is therefore
// semantically WRONG at the edges:
//
//   - FALSE POSITIVE: evidence collected AFTER the symbol changed (the symbol is
//     source-stale but the evidence already reflects the change) still matches.
//   - FALSE NEGATIVE: once the stale tag clears (the symbol is re-reviewed) the
//     evidence stays old, but the view no longer flags it.
//
// The correct predicate is NOT expressible in the v1 DSL: it requires comparing
// two note fields (`s.stale.fired_at > e.collected_at`), and §5.1 forbids
// field-to-field comparison (WHERE compares a field to a param/literal only; the
// sole field-side function is STARTS_WITH). A materialized view runs one query
// with no per-row params, so a per-evidence cutoff param is not available
// either. This needs a v1.5 temporal field-to-field comparison.
//
// ESCALATED as an owner decision (see PR / merge-back): (a) DEFER this view
// until the v1.5 DSL lands the temporal comparison, or (b) extend the DSL now.
// Until that is decided, the source-stale filter remains as a LOUDLY-MARKED
// interim placeholder — it must NOT be treated as the correct contract behavior.
// ApproximationNotice carries this caveat for any programmatic surface.
package views

import (
	"fmt"
	"time"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/adapterkit"
	complianceregister "github.com/AGOrcha/dot-agents/internal/adapters/builtin/compliance-register"
	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	crossnamespace "github.com/AGOrcha/dot-agents/internal/kg/dsl/cross-namespace"
	"github.com/AGOrcha/dot-agents/internal/kg/lockfile"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// ControlsWithChangedFunctionEvidence is the view name (and the note type of
// its derived rows), tracking the §8.3 worked example name.
const ControlsWithChangedFunctionEvidence = "controls_with_changed_function_evidence"

// noteTypeEvidence / noteTypeSymbol are the cross edge's endpoints, declared as
// consts so the cross-edge wiring and any future view share one spelling.
const (
	noteTypeEvidence = "evidence"
	noteTypeSymbol   = "symbol"
)

// ApproximationNotice is the machine-readable caveat for the BUG-2
// known-incorrect predicate (see the package doc): the view filters a current
// source-stale marker, not the spec's "changed since the evidence was collected"
// temporal predicate, which the v1 DSL cannot express. Surfaces that present
// this view's results SHOULD relay this notice until the owner decision lands.
const ApproximationNotice = "KNOWN-INCORRECT (BUG-2): filters current source-staleness, NOT 'changed since evidence collected' (v1 DSL cannot compare two note fields); pending owner decision to defer to v1.5 or extend the DSL"

// controlsWithChangedFnEvidenceQuery joins the compliance evidence→control
// citation with the cross-namespace evidence→symbol reference.
//
// ⚠️ BUG-2 KNOWN-INCORRECT PREDICATE: `s.stale.reason = 'source'` approximates
// "the referenced symbol CHANGED SINCE the evidence was collected" with the
// CURRENT source-stale marker. This over- and under-counts (see package doc).
// The correct `s.stale.fired_at > e.collected_at` is field-to-field and is
// forbidden in v1 (§5.1) — this stays as an escalated interim placeholder, NOT
// the contract-correct query.
const controlsWithChangedFnEvidenceQuery = `
	MATCH (e:evidence)-[:cited_by]->(c:control)
	MATCH (e)-[:references]->(s:symbol)
	WHERE s.stale.reason = 'source'
	RETURN c.control_id AS control_id, e.id AS evidence_id, s.qualified_name AS function`

// referencesEdge is the cross-namespace link the view traverses: a compliance
// evidence note references a CRG symbol. It is declared here (not in either
// adapter's schema) because it spans the two namespaces (§8.3).
func referencesEdge() crossnamespace.CrossEdge {
	return crossnamespace.CrossEdge{Name: "references", From: noteTypeEvidence, To: noteTypeSymbol}
}

// ComplianceNamespace returns the compliance-register namespace contribution
// (its name + compiled DSL schema) for use as a cross-namespace view consumer.
func ComplianceNamespace() crossnamespace.Namespace {
	return crossnamespace.Namespace{Name: complianceregister.Name, Info: complianceregister.New().SchemaInfo()}
}

// CRGNamespace returns the CRG namespace contribution, translating the shipped
// CRG registry.Schema into a DSL SchemaInfo through the shared adapterkit. The
// CRG schema is a build-time-valid embed, so a translation failure is a build
// bug rather than a runtime condition — this panics on it, mirroring the
// adapters' New → mustFromYAML seam. The fallible core is namespaceFromSchema,
// exercised directly by a white-box test with a deliberately invalid schema.
func CRGNamespace() crossnamespace.Namespace {
	return mustNamespace(crg.Name, crg.New().Schema())
}

// mustNamespace is the panic seam CRGNamespace uses: it builds the namespace or
// panics on a translation failure (a build bug for the shipped embed). A
// white-box test drives it with an invalid schema to exercise the panic.
func mustNamespace(name string, s registry.Schema) crossnamespace.Namespace {
	ns, err := namespaceFromSchema(name, s)
	if err != nil {
		panic("views: " + err.Error())
	}
	return ns
}

// namespaceFromSchema translates a registry.Schema into a cross-namespace
// contribution, returning an error (rather than panicking) for an invalid
// schema so the error path is testable.
func namespaceFromSchema(name string, s registry.Schema) (crossnamespace.Namespace, error) {
	info, err := adapterkit.BuildSchemaInfo(s)
	if err != nil {
		return crossnamespace.Namespace{}, fmt.Errorf("views: build %s schema info: %w", name, err)
	}
	return crossnamespace.Namespace{Name: name, Info: info}, nil
}

// ControlsWithChangedFunctionEvidenceView compiles the §8.3 cross-adapter view
// from the live compliance and CRG schemas. The returned view reads_from the
// CRG namespace; its multi-namespace token grants {compliance-register, write}
// + {crg, read} (§8.2), and Materialize joins the two namespaces under the
// single DSL query.
func ControlsWithChangedFunctionEvidenceView() (*crossnamespace.View, error) {
	return crossnamespace.Compile(
		ControlsWithChangedFunctionEvidence,
		ComplianceNamespace(),
		[]crossnamespace.Namespace{CRGNamespace()},
		[]crossnamespace.CrossEdge{referencesEdge()},
		controlsWithChangedFnEvidenceQuery,
	)
}

// RunCutover drives the §10.3 cross-adapter cutover for view v against a bumped
// CRG schema, WIRING the mechanical CheckCompat gate into the lockfile state
// machine (this is the t5 §10.3 integration, bug-4 fix). Given a lockfile whose
// view is ready, it:
//
//  1. freezes the view to pending-recompat-check (the CRG bump);
//  2. runs the mechanical gate CheckCompat against the new CRG schema;
//  3. applies the result — compatible → pending-rebuild (and, on a subsequent
//     bootstrap, MarkViewRebuilt → ready); incompatible → dsl-update-required,
//     which ActivationBlockers reports to BLOCK CRG (re)activation until the
//     compliance adapter ships an updated query (O1: no ack opt-out).
//
// It returns the resulting view status. The view must already be registered in
// the lockfile (RegisterView) as ready.
func RunCutover(lf *lockfile.Lockfile, v *crossnamespace.View, updatedCRG crossnamespace.Namespace, now time.Time) (lockfile.ViewStatus, error) {
	lf.MarkDependeeBumped(crg.Name, now)
	compat, _ := v.CheckCompat([]crossnamespace.Namespace{updatedCRG})
	deps := []lockfile.ViewDependency{{Adapter: crg.Name}}
	if err := lf.ResolveRecompat(v.Consumer(), v.Name(), compat == crossnamespace.CompatOK, deps, now); err != nil {
		return "", err
	}
	// ResolveRecompat just succeeded, so the view is present; read its new status.
	status, _ := lf.ViewStatusOf(v.Consumer(), v.Name())
	return status, nil
}
