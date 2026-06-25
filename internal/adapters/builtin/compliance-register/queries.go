package complianceregister

import (
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// namedQuerySources is the §13.2 named-query catalog as v1 DSL strings. They are
// compiled against the adapter SchemaInfo at load (compileQueries) and run via
// RunNamed. Each is the contract-conformant form of the §13.2 example:
//
//   - regulations_satisfied_by: a control's satisfied regulations.
//   - stale_evidence_unsupported_controls: controls whose evidence was
//     environmentally invalidated (reads the stale tag, §7.3).
//   - policy_review_due_impact: controls whose derives_from policy was
//     environmentally invalidated and which are themselves derivation-stale —
//     the env→derivation propagation trace (§7.3).
func namedQuerySources() map[string]string {
	return map[string]string{
		"regulations_satisfied_by": `
			MATCH (c:control)-[:satisfies]->(reg:regulation)
			WHERE c.control_id = $control_id
			RETURN reg.id, reg.authority, reg.version`,
		"stale_evidence_unsupported_controls": `
			MATCH (e:evidence)-[:cited_by]->(c:control)
			WHERE e.stale.reason = 'environmental'
			RETURN c.control_id, c.owner.name, e.id, e.stale.fired_at`,
		"policy_review_due_impact": `
			MATCH (c:control)
			WHERE c.stale.reason = 'derivation'
			  AND c.derives_from.stale.reason = 'environmental'
			RETURN c.control_id, c.owner.name, c.derives_from.id, c.derives_from.version`,
	}
}

// RunImpact runs the impact-radius query for the given changed control ids over
// the corpus view, returning the blast-radius rows (regulations, risks,
// findings, evidence, owner). This is the load-bearing query the review
// pipeline invokes (§3b).
func (a *Adapter) RunImpact(view sdk.NamespaceView, changedIDs []string) ([]sdk.Row, error) {
	return dsl.Eval(a.impact, view, map[string]any{"changed_ids": changedIDs})
}

// RunNamed runs a named query by name with the given params over the corpus
// view. It returns an error if the query name is unknown.
func (a *Adapter) RunNamed(name string, view sdk.NamespaceView, params map[string]any) ([]sdk.Row, error) {
	q, ok := a.named[name]
	if !ok {
		return nil, errUnknownQuery(name)
	}
	return dsl.Eval(q, view, params)
}

// QueryNames returns the named-query names in no particular order.
func (a *Adapter) QueryNames() []string {
	names := make([]string, 0, len(a.named))
	for n := range a.named {
		names = append(names, n)
	}
	return names
}
