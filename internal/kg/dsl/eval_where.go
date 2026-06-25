package dsl

import (
	"sort"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// applyWhere filters the bindings by the WHERE predicates, honoring §5.4.2
// lowering: a predicate whose source alias is from a required MATCH filters the
// row out when the ref is NULL or fails (stays-in-WHERE); a predicate whose
// source alias is OPTIONAL preserves the source row, only nulling the joined
// alias when it fails (hoisted-to-ON). The two paths produce the row sets the
// conformance tests T13/T14 require.
func (ev *evaluator) applyWhere(rows []binding) []binding {
	if len(ev.q.Where) == 0 {
		return rows
	}
	var out []binding
	for _, row := range rows {
		if ev.rowPasses(row) {
			out = append(out, row)
		}
	}
	return out
}

// rowPasses evaluates every WHERE predicate against one binding. Optional-source
// predicates that fail null their joined alias instead of dropping the row.
// Predicate evaluation is total (every value form is parse-validated, §5.1.1),
// so this returns a plain bool rather than threading an unreachable error.
func (ev *evaluator) rowPasses(row binding) bool {
	for _, pred := range ev.q.Where {
		if ev.evalPredicate(pred, row) {
			continue
		}
		if ev.q.aliasOptional[pred.Left.Alias] {
			row[pred.Left.Alias] = nil // hoist-to-ON: preserve source, null the join
			continue
		}
		return false // required source: drop the row
	}
	return true
}

// evalPredicate evaluates one predicate against a binding, returning whether it
// holds. A NULL left value (unresolved ref / unmatched optional) fails the
// predicate (the LEFT JOIN NULL row).
func (ev *evaluator) evalPredicate(pred Predicate, row binding) bool {
	if pred.Func == startsWith {
		return ev.evalStartsWith(pred, row)
	}
	left, ok := ev.resolveFieldRef(pred.Left, row)
	if !ok {
		return false
	}
	right := ev.evalValueExpr(pred.Right)
	if pred.Op == "IN" {
		return inList(left, right)
	}
	return compare(pred.Op, left, right)
}

// evalStartsWith evaluates the STARTS_WITH(field, $param) prefix predicate.
func (ev *evaluator) evalStartsWith(pred Predicate, row binding) bool {
	left, ok := ev.resolveFieldRef(pred.Left, row)
	if !ok {
		return false
	}
	right := ev.evalValueExpr(pred.Right)
	ls, lok := left.(string)
	rs, rok := right.(string)
	if !lok || !rok {
		return false
	}
	return len(ls) >= len(rs) && ls[:len(rs)] == rs
}

// sortedAliases returns a binding's alias keys in sorted order (stable row keys
// and deterministic iteration).
func sortedAliases(b binding) []string {
	keys := make([]string, 0, len(b))
	for k := range b {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// fieldValue reads a scalar field from a note, returning (nil, false) when the
// note or field is absent.
func fieldValue(n *sdk.Note, key string) (any, bool) {
	if n == nil || n.Fields == nil {
		return nil, false
	}
	v, ok := n.Fields[key]
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}
