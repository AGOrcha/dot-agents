package sddregister

import (
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// Named trace queries (the dogfood's load-bearing path). Each is a v1 DSL
// string compiled against the adapter SchemaInfo at load and evaluated over the
// ingested NamespaceView via dsl.Eval.
const (
	// QueryTaskTrace answers the proposal→spec→plan→task trace the schema draft
	// calls out: given a task_key, walk task --belongs_to_plan--> plan
	// --plan_for_spec--> spec and return the owning plan + spec. This is the
	// canonical "which spec does this task implement, via which plan?" trace.
	QueryTaskTrace = "task_trace"

	// QueryTasksImplementingSpec answers "which tasks implement spec S?" via the
	// direct implements_spec edge — the inverse lookup the correlation loop needs.
	QueryTasksImplementingSpec = "tasks_implementing_spec"

	// QueryTasksBlockedBy answers "which tasks depend on task X?" via the
	// depends_on edge (reverse traversal) — the blocked-by trace.
	QueryTasksBlockedBy = "tasks_blocked_by"
)

// namedQuerySources is the DSL named-query catalog. Compiled at load
// (compileQueries) and run via RunNamed.
func namedQuerySources() map[string]string {
	return map[string]string{
		QueryTaskTrace: `
			MATCH (t:task)-[:belongs_to_plan]->(p:plan)
			MATCH (p)-[:plan_for_spec]->(s:spec)
			WHERE t.task_key = $task_key
			RETURN p.plan_id, s.spec_id`,
		QueryTasksImplementingSpec: `
			MATCH (t:task)-[:implements_spec]->(s:spec)
			WHERE s.spec_id = $spec_id
			RETURN t.task_key, t.title`,
		QueryTasksBlockedBy: `
			MATCH (t:task)-[:depends_on]->(dep:task)
			WHERE dep.task_key = $task_key
			RETURN t.task_key`,
	}
}

// RunNamed runs a named query by name with the given params over the ingested
// view. It returns an error if the query name is unknown.
func (a *Adapter) RunNamed(name string, view sdk.NamespaceView, params map[string]any) ([]sdk.Row, error) {
	q, ok := a.named[name]
	if !ok {
		return nil, fmt.Errorf("sdd-register: no named query %q", name)
	}
	return dsl.Eval(q, view, params)
}

// RunImpact runs the impact-radius query for the given plan id over the view,
// returning the task_keys the plan contains.
func (a *Adapter) RunImpact(view sdk.NamespaceView, planID string) ([]sdk.Row, error) {
	return dsl.Eval(a.impact, view, map[string]any{"plan_id": planID})
}

// QueryNames returns the named-query names in no particular order.
func (a *Adapter) QueryNames() []string {
	names := make([]string, 0, len(a.named))
	for n := range a.named {
		names = append(names, n)
	}
	return names
}
