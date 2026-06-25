package dsl

import (
	"fmt"
	"strconv"
)

// edgeAliasType is the sentinel note-type for a bound edge alias. Field access
// on an edge alias is restricted to the intrinsic columns id/kind (§5.1, T30).
const edgeAliasType = "@edge"

// startsWith is the one prefix-predicate function permitted in WHERE (§5.1.1).
const startsWith = "STARTS_WITH"

// errFuncOnField is reused wherever a WHERE-side function is applied to a note
// field instead of a param/literal (§5.1.1; T24). Hoisted so the literal is
// declared once.
const errFuncOnField = "dsl: WHERE-side function may only be applied to params and literals, not note fields"

// parseVarLength parses the `*min..max` suffix of a variable-length edge (§5.1).
// An unbounded form (`*` or `*1..`) is rejected here (T36) — v1 requires an
// explicit upper bound.
func (p *parser) parseVarLength(edge *EdgePattern) error {
	p.next() // consume '*'
	if p.cur().kind != tokNumber {
		return fmt.Errorf("dsl: variable-length pattern needs an explicit bound at offset %d (no unbounded *)", p.cur().pos)
	}
	min, err := strconv.Atoi(p.next().text)
	if err != nil {
		return fmt.Errorf("dsl: invalid variable-length lower bound: %w", err)
	}
	if err := p.expectPunct("."); err != nil {
		return err
	}
	if err := p.expectPunct("."); err != nil {
		return err
	}
	if p.cur().kind != tokNumber {
		return fmt.Errorf("dsl: variable-length pattern needs an explicit upper bound at offset %d", p.cur().pos)
	}
	max, err := strconv.Atoi(p.next().text)
	if err != nil {
		return fmt.Errorf("dsl: invalid variable-length upper bound: %w", err)
	}
	edge.VarMin, edge.VarMax = min, max
	return nil
}

// parseWhere parses `WHERE pred (AND pred)*`. v1 has no OR.
func (p *parser) parseWhere() error {
	p.next() // WHERE
	for {
		pred, err := p.parsePredicate()
		if err != nil {
			return err
		}
		p.q.Where = append(p.q.Where, pred)
		if !p.isIdent("AND") {
			return nil
		}
		p.next()
	}
}

// parsePredicate parses one comparison. It dispatches the STARTS_WITH function
// form and the plain `field op value` form; the IN form is handled inside
// parseComparison.
func (p *parser) parsePredicate() (Predicate, error) {
	if p.isIdent(startsWith) {
		return p.parseStartsWith()
	}
	return p.parseComparison()
}

// parseStartsWith parses `STARTS_WITH(field, $param)` (§5.1.1, T26/T27). The
// first argument MUST be a field and the second MUST be a param.
func (p *parser) parseStartsWith() (Predicate, error) {
	p.next() // STARTS_WITH
	if err := p.expectPunct("("); err != nil {
		return Predicate{}, err
	}
	left, err := p.parseFieldRef()
	if err != nil {
		return Predicate{}, err
	}
	if err := p.expectPunct(","); err != nil {
		return Predicate{}, err
	}
	if p.cur().kind != tokParam {
		return Predicate{}, fmt.Errorf("dsl: STARTS_WITH second argument must be a $param (first is the field), at offset %d", p.cur().pos)
	}
	param := p.next().text
	if err := p.expectPunct(")"); err != nil {
		return Predicate{}, err
	}
	return Predicate{Func: startsWith, Left: left, Right: ValueExpr{Param: param}}, nil
}

// parseComparison parses `field op value` or `field IN $param`.
func (p *parser) parseComparison() (Predicate, error) {
	if p.functionLeadsPredicate() {
		return Predicate{}, fmt.Errorf("%s", errFuncOnField)
	}
	left, err := p.parseFieldRef()
	if err != nil {
		return Predicate{}, err
	}
	if p.isIdent("IN") {
		return p.parseIn(left)
	}
	op, err := p.parseOperator()
	if err != nil {
		return Predicate{}, err
	}
	right, err := p.parseValueExpr()
	if err != nil {
		return Predicate{}, err
	}
	return Predicate{Op: op, Left: left, Right: right}, nil
}

// functionLeadsPredicate reports whether the predicate begins with a function
// call applied to a field, i.e. `coalesce(alias.field, ...)` (T24) or any other
// `ident(` on the WHERE field side. Such forms are rejected (§5.1.1).
func (p *parser) functionLeadsPredicate() bool {
	t := p.cur()
	if t.kind != tokIdent || keywordSet[t.text] {
		return false
	}
	return p.toks[p.pos+1].kind == tokPunct && p.toks[p.pos+1].text == "("
}

// parseIn parses the `IN $param` tail. `IN` is permitted only with `.id` on the
// left (§5.1).
func (p *parser) parseIn(left FieldRef) (Predicate, error) {
	p.next() // IN
	if !left.isIDField() {
		return Predicate{}, fmt.Errorf("dsl: IN is permitted only with .id on the left (got %s)", left.describe())
	}
	if p.cur().kind != tokParam {
		return Predicate{}, fmt.Errorf("dsl: IN requires a $param list at offset %d", p.cur().pos)
	}
	param := p.next().text
	return Predicate{Op: "IN", Left: left, Right: ValueExpr{Param: param}}, nil
}

// parseOperator consumes a comparison operator, rejecting any outside the closed
// §5.1 set (T39 for `<>`).
func (p *parser) parseOperator() (string, error) {
	t := p.cur()
	if t.text == "<>" {
		return "", fmt.Errorf("dsl: operator '<>' is not in the v1 set; use '!=' (offset %d)", t.pos)
	}
	if (t.kind == tokPunct || t.kind == tokOp) && allowedOps[t.text] {
		p.next()
		return t.text, nil
	}
	return "", fmt.Errorf("dsl: expected a comparison operator {=,!=,<,<=,>,>=} at offset %d, got %q", t.pos, t.text)
}

// parseFieldRef parses `alias(.part)*` — a bound alias followed by zero or more
// ref/field selectors.
func (p *parser) parseFieldRef() (FieldRef, error) {
	alias, err := p.expectIdent()
	if err != nil {
		return FieldRef{}, err
	}
	ref := FieldRef{Alias: alias}
	for p.isPunct(".") {
		p.next()
		part, err := p.expectIdent()
		if err != nil {
			return FieldRef{}, err
		}
		ref.Path = append(ref.Path, part)
	}
	return ref, nil
}

// parseValueExpr parses the right-hand side of a comparison: a param, a literal,
// or an allowed-function (coalesce) call over params/literals (§5.1.1).
func (p *parser) parseValueExpr() (ValueExpr, error) {
	t := p.cur()
	switch {
	case t.kind == tokParam:
		p.next()
		return ValueExpr{Param: t.text}, nil
	case t.kind == tokString:
		p.next()
		return ValueExpr{Literal: t.text, IsLiteral: true}, nil
	case t.kind == tokNumber:
		p.next()
		n, err := parseNumber(t.text)
		if err != nil {
			return ValueExpr{}, err
		}
		return ValueExpr{Literal: n, IsLiteral: true}, nil
	case t.kind == tokIdent && p.toks[p.pos+1].text == "(":
		return p.parseValueCall()
	default:
		return ValueExpr{}, fmt.Errorf("dsl: expected a $param, literal, or coalesce(...) at offset %d, got %q", t.pos, t.text)
	}
}

// parseValueCall parses an allowed-function call on the value side. Only
// coalesce is permitted here (§5.1.1); its args must be params/literals/nested
// coalesce — never note fields (T24, T25).
func (p *parser) parseValueCall() (ValueExpr, error) {
	name := p.next().text
	if name != funcCoalesce {
		return ValueExpr{}, fmt.Errorf("dsl: function %q is not allowed in WHERE (only coalesce on params/literals)", name)
	}
	if err := p.expectPunct("("); err != nil {
		return ValueExpr{}, err
	}
	args, err := p.parseCallArgs()
	if err != nil {
		return ValueExpr{}, err
	}
	return ValueExpr{Call: &FuncCall{Name: name, Args: args}}, nil
}

// parseCallArgs parses a comma-separated argument list terminated by `)`. Each
// argument must itself be a value expression (param/literal/coalesce) — a field
// reference here is the T24/T25 rejection path.
func (p *parser) parseCallArgs() ([]ValueExpr, error) {
	var args []ValueExpr
	for {
		if p.fieldLeadsCallArg() {
			return nil, fmt.Errorf("%s", errFuncOnField)
		}
		arg, err := p.parseValueExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.isPunct(",") {
			p.next()
			continue
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		return args, nil
	}
}

// fieldLeadsCallArg reports whether the next call argument is a bare field
// reference (`alias.field`) rather than a param/literal/call — the forbidden
// field-level computation case (§5.1.1, T24).
func (p *parser) fieldLeadsCallArg() bool {
	t := p.cur()
	if t.kind != tokIdent {
		return false
	}
	// An ident followed by '(' is a nested call (allowed); an ident followed
	// by '.' or by a non-call token is a field reference (forbidden).
	return p.toks[p.pos+1].text != "("
}
