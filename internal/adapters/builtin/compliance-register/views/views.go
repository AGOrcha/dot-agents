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
//     spans the two namespaces and appears in neither adapter's own schema;
//   - "a function that has changed in CRG since the evidence was collected" is
//     expressed as the symbol carrying the §7.3 source-stale tag — exactly the
//     O5 source_mutation driver CRG fires on a content-hash change. The literal
//     `f.last_changed > e.collected_at` form is NOT expressible in the v1 DSL
//     (field-to-field comparison is forbidden, §5.1), so the stale tag is the
//     contract-conformant expression of "changed". The time-window refinement
//     ("since collected") is a §12 v1.5 budget candidate, flagged for review.
package views

import (
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/adapterkit"
	complianceregister "github.com/AGOrcha/dot-agents/internal/adapters/builtin/compliance-register"
	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	crossnamespace "github.com/AGOrcha/dot-agents/internal/kg/dsl/cross-namespace"
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

// controlsWithChangedFnEvidenceQuery joins the compliance evidence→control
// citation with the cross-namespace evidence→symbol reference, keeping only
// controls whose cited evidence references a CRG symbol that has changed
// (carries the §7.3 source-stale tag). It returns the control's external id,
// the citing evidence id, and the changed symbol's qualified name.
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
