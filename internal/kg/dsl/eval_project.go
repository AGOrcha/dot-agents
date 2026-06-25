package dsl

import (
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// Aggregate function names (§5.1.1). Hoisted so each literal is declared once.
const (
	funcMin      = "min"
	funcMax      = "max"
	funcCount    = "count"
	funcCoalesce = "coalesce"
	funcHopCount = "hop_count"
)

// project turns filtered bindings into result rows by evaluating the RETURN
// items. When the RETURN list contains an aggregate (count/min/max), the whole
// result collapses to a single grouped row (v1 has no GROUP BY; aggregates
// aggregate the entire result set, matching the dogfood count(*) usage).
func (ev *evaluator) project(bindings []binding) ([]sdk.Row, error) {
	if ev.hasAggregate() {
		return ev.projectAggregate(bindings)
	}
	rows := make([]sdk.Row, 0, len(bindings))
	for _, b := range bindings {
		row, err := ev.projectRow(b)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// hasAggregate reports whether any RETURN item is an aggregate function.
func (ev *evaluator) hasAggregate() bool {
	for _, item := range ev.q.Returns {
		if item.Func == funcCount || item.Func == funcMin || item.Func == funcMax {
			return true
		}
	}
	return false
}

// projectRow evaluates the non-aggregate RETURN items for one binding.
func (ev *evaluator) projectRow(b binding) (sdk.Row, error) {
	row := sdk.Row{}
	for _, item := range ev.q.Returns {
		v, err := ev.evalReturnItem(item, b)
		if err != nil {
			return nil, err
		}
		row[item.Alias] = v
	}
	return row, nil
}

// evalReturnItem evaluates a single non-aggregate RETURN item against a binding.
func (ev *evaluator) evalReturnItem(item ReturnItem, b binding) (any, error) {
	switch item.Func {
	case funcHopCount:
		return ev.hopCount[bindingKey(b)], nil
	case funcCoalesce:
		return ev.evalReturnCoalesce(item.FuncArgs, b)
	case returnParamFunc:
		return ev.params[item.FuncArgs[0].Alias], nil
	case "":
		v, _ := ev.resolveFieldRef(item.Ref, b)
		return v, nil
	default:
		return nil, fmt.Errorf("dsl: unexpected RETURN function %q in row projection", item.Func)
	}
}

// evalReturnCoalesce returns the first non-nil value among coalesce args, each
// of which may be a field ref or a literal/param default.
func (ev *evaluator) evalReturnCoalesce(args []ReturnItem, b binding) (any, error) {
	for _, arg := range args {
		if arg.Ref.Alias != "" {
			if v, ok := ev.resolveFieldRef(arg.Ref, b); ok {
				return v, nil
			}
			continue
		}
		if arg.Alias != "" {
			return literalFromArg(arg.Alias), nil
		}
	}
	return nil, nil
}

// literalFromArg recovers a coalesce default literal from a return arg's stored
// alias text (numbers were stored as text; bare strings as their content).
func literalFromArg(text string) any {
	if n, err := parseNumber(text); err == nil {
		return n
	}
	return text
}

// projectAggregate collapses all bindings into a single grouped row of
// aggregate values (count(*), min(field), max(field)).
func (ev *evaluator) projectAggregate(bindings []binding) ([]sdk.Row, error) {
	row := sdk.Row{}
	for _, item := range ev.q.Returns {
		row[item.Alias] = ev.aggregateValue(item, bindings)
	}
	return []sdk.Row{row}, nil
}

// aggregateValue computes one aggregate over all bindings.
func (ev *evaluator) aggregateValue(item ReturnItem, bindings []binding) any {
	switch item.Func {
	case funcCount:
		return len(bindings)
	case funcMin, funcMax:
		return ev.minMax(item, bindings)
	default:
		// Non-aggregate item alongside aggregates: take its first-row value.
		if len(bindings) == 0 {
			return nil
		}
		v, _ := ev.evalReturnItem(item, bindings[0])
		return v
	}
}

// minMax computes min/max of a numeric field across bindings.
func (ev *evaluator) minMax(item ReturnItem, bindings []binding) any {
	var best float64
	seen := false
	for _, b := range bindings {
		v, ok := ev.resolveFieldRef(item.FuncArgs[0].Ref, b)
		f, fok := toFloat(v)
		if !ok || !fok {
			continue
		}
		if !seen || (item.Func == funcMin && f < best) || (item.Func == funcMax && f > best) {
			best, seen = f, true
		}
	}
	if !seen {
		return nil
	}
	return best
}
