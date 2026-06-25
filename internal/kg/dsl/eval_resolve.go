package dsl

import "github.com/AGOrcha/dot-agents/internal/adapters/sdk"

// staleKey is the note-field key under which a note carries its structured
// stale payload (§7.3). Hoisted so the literal is declared once across resolve
// and projection.
const staleKey = "stale"

// resolveFieldRef resolves a field reference against a binding, following ref
// hops (§5.4.1) and structured stale selectors (§7.3). It returns (value, true)
// for a resolved scalar, or (nil, false) when any hop is NULL (an unresolved
// ref or unmatched optional alias) — the LEFT JOIN NULL outcome.
func (ev *evaluator) resolveFieldRef(ref FieldRef, row binding) (any, bool) {
	note := row[ref.Alias]
	if note == nil {
		return nil, false
	}
	if ref.IsBareAlias() {
		return note.ID, true // bare ref id read (§5.4.1 rule 1)
	}
	return ev.resolvePath(note, ref.Path)
}

// resolvePath walks the selector path from a starting note, resolving ref hops
// and terminating at a scalar field or a stale.<sub> selector.
func (ev *evaluator) resolvePath(note *sdk.Note, path []string) (any, bool) {
	cur := note
	for i, part := range path {
		if part == staleKey {
			return staleSubfield(cur, path[i+1:])
		}
		if i == len(path)-1 {
			if part == "id" {
				return cur.ID, true // intrinsic id column
			}
			return fieldValue(cur, part)
		}
		next, ok := ev.followRef(cur, part)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// followRef resolves a ref field on cur to the referenced note. The ref field
// stores the target note's id; a missing or dangling ref yields (nil, false).
func (ev *evaluator) followRef(cur *sdk.Note, field string) (*sdk.Note, bool) {
	v, ok := fieldValue(cur, field)
	if !ok {
		return nil, false
	}
	id, ok := v.(string)
	if !ok {
		return nil, false
	}
	n, ok := ev.byID[id]
	if !ok {
		return nil, false
	}
	return &n, true
}

// staleSubfield reads a subfield of a note's structured stale payload (§7.3),
// e.g. stale.reason / stale.fired_at. A fresh note (no stale payload) yields
// (nil, false), matching the §7.3 "fresh → stale omitted" convention.
func staleSubfield(n *sdk.Note, rest []string) (any, bool) {
	if n == nil || n.Fields == nil {
		return nil, false
	}
	raw, ok := n.Fields[staleKey]
	if !ok || raw == nil {
		return nil, false
	}
	m, ok := raw.(map[string]any)
	if !ok || len(rest) == 0 {
		return nil, false
	}
	v, ok := m[rest[0]]
	return v, ok && v != nil
}

// evalValueExpr evaluates the right-hand side of a predicate: a param, a
// literal, or a coalesce(...) call over params/literals (§5.1.1).
func (ev *evaluator) evalValueExpr(e ValueExpr, _ binding) (any, error) {
	switch {
	case e.Call != nil:
		return ev.evalCoalesce(e.Call)
	case e.IsLiteral:
		return e.Literal, nil
	default:
		return ev.params[e.Param], nil // nil when the optional param is absent
	}
}

// evalCoalesce returns the first non-nil argument of a coalesce call, folding
// the WHERE-side param normalization before predicate evaluation (§5.1.1).
func (ev *evaluator) evalCoalesce(call *FuncCall) (any, error) {
	for _, arg := range call.Args {
		v, err := ev.evalValueExpr(arg, nil)
		if err != nil {
			return nil, err
		}
		if v != nil {
			return v, nil
		}
	}
	return nil, nil
}
