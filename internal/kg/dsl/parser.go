package dsl

import (
	"fmt"
)

// allowedOps is the closed WHERE operator set (§5.1, normative). `<>` and any
// other operator are rejected (T39).
var allowedOps = map[string]bool{
	"=": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true,
}

// allowedReturnFuncs is the §5.1.1 function set permitted in RETURN.
var allowedReturnFuncs = map[string]bool{
	"count": true, "min": true, "max": true, "coalesce": true,
}

// parser is a recursive-descent parser over the lexer's token slice. It is the
// front half of the contract: it accepts exactly the v1 grammar (§5.1) and
// rejects every forbidden construct (§5.2) with a precise error.
type parser struct {
	toks []token
	pos  int
	q    *Query
}

// Parse turns a DSL string into a validated *Query without schema-aware checks
// (ref depth cap, untyped-ref rejection, max_depth bound). Use ParseWithSchema
// when an adapter SchemaInfo is available so the §5.4 / §7.3 schema rules also
// run. Parse alone enforces every grammar-level rule (§5.1, §5.2).
func Parse(src string) (*Query, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{
		toks: toks,
		q: &Query{
			aliasType:     map[string]string{},
			aliasOptional: map[string]bool{},
		},
	}
	if err := p.parseQuery(); err != nil {
		return nil, err
	}
	return p.q, nil
}

func (p *parser) cur() token  { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) isIdent(word string) bool {
	t := p.cur()
	return t.kind == tokIdent && t.text == word
}

func (p *parser) isPunct(s string) bool {
	t := p.cur()
	return (t.kind == tokPunct || t.kind == tokOp) && t.text == s
}

// expectPunct consumes a punctuation token or returns a positioned error.
func (p *parser) expectPunct(s string) error {
	if !p.isPunct(s) {
		return fmt.Errorf("dsl: expected %q at offset %d, got %q", s, p.cur().pos, p.cur().text)
	}
	p.next()
	return nil
}

// expectIdent consumes any identifier token and returns its text.
func (p *parser) expectIdent() (string, error) {
	t := p.cur()
	if t.kind != tokIdent {
		return "", fmt.Errorf("dsl: expected identifier at offset %d, got %q", t.pos, t.text)
	}
	p.next()
	return t.text, nil
}

// parseQuery is the top-level clause loop: zero or more MATCH/OPTIONAL MATCH,
// an optional WHERE, then a required RETURN.
func (p *parser) parseQuery() error {
	for p.isIdent("MATCH") || p.isIdent("OPTIONAL") {
		if err := p.parseMatch(); err != nil {
			return err
		}
	}
	if p.isIdent("WHERE") {
		if err := p.parseWhere(); err != nil {
			return err
		}
	}
	if !p.isIdent("RETURN") {
		return fmt.Errorf("dsl: expected RETURN at offset %d, got %q", p.cur().pos, p.cur().text)
	}
	if err := p.parseReturn(); err != nil {
		return err
	}
	if p.cur().kind != tokEOF {
		return fmt.Errorf("dsl: trailing tokens after RETURN at offset %d (%q)", p.cur().pos, p.cur().text)
	}
	return nil
}

// parseMatch parses one MATCH or OPTIONAL MATCH clause and records its alias
// match contexts (§5.4.2 lowering keys off the optional flag).
func (p *parser) parseMatch() error {
	optional := false
	if p.isIdent("OPTIONAL") {
		optional = true
		p.next()
	}
	if !p.isIdent("MATCH") {
		return fmt.Errorf("dsl: expected MATCH at offset %d, got %q", p.cur().pos, p.cur().text)
	}
	p.next()

	first, err := p.parseNodePattern()
	if err != nil {
		return err
	}
	clause := MatchClause{Optional: optional, Nodes: []NodePattern{first}}
	p.bindAlias(first.Alias, first.Type, optional)

	if p.isPunct("-") {
		if err := p.parseEdgeHop(&clause, optional); err != nil {
			return err
		}
	}
	p.q.Matches = append(p.q.Matches, clause)
	return nil
}

// parseEdgeHop parses the `-[..]->(node)` tail of an edge match and appends the
// edge + end node to clause.
func (p *parser) parseEdgeHop(clause *MatchClause, optional bool) error {
	edge, err := p.parseEdgePattern()
	if err != nil {
		return err
	}
	end, err := p.parseNodePattern()
	if err != nil {
		return err
	}
	clause.Edge = &edge
	clause.Nodes = append(clause.Nodes, end)
	p.bindAlias(end.Alias, end.Type, optional)
	if edge.Alias != "" {
		// Edge aliases bind to an empty type sentinel so field validation can
		// reject non-intrinsic edge columns (T30).
		p.q.aliasType[edge.Alias] = edgeAliasType
	}
	return nil
}

// bindAlias records an alias→type binding and its match context. Empty aliases
// (anonymous nodes) are skipped.
func (p *parser) bindAlias(alias, typ string, optional bool) {
	if alias == "" {
		return
	}
	// A re-reference to an already-bound alias with no `:type` (e.g. the `(c)`
	// node in `OPTIONAL MATCH (c)-[..]`) must not clobber the original typed
	// binding; only record the type when one is supplied or the alias is new.
	if existing, ok := p.q.aliasType[alias]; ok && typ == "" {
		typ = existing
	} else {
		p.q.aliasType[alias] = typ
	}
	if _, ok := p.q.aliasOptional[alias]; !ok {
		p.q.aliasOptional[alias] = optional
	}
}

// parseNodePattern parses `(alias:type)` or `(alias)`.
func (p *parser) parseNodePattern() (NodePattern, error) {
	if err := p.expectPunct("("); err != nil {
		return NodePattern{}, err
	}
	alias, err := p.expectIdent()
	if err != nil {
		return NodePattern{}, err
	}
	var typ string
	if p.isPunct(":") {
		p.next()
		if typ, err = p.expectIdent(); err != nil {
			return NodePattern{}, err
		}
	}
	if err := p.expectPunct(")"); err != nil {
		return NodePattern{}, err
	}
	return NodePattern{Alias: alias, Type: typ}, nil
}

// parseEdgePattern parses `-[alias?:type]->` or the variable-length
// `-[:type*min..max]->` form.
func (p *parser) parseEdgePattern() (EdgePattern, error) {
	if err := p.expectPunct("-"); err != nil {
		return EdgePattern{}, err
	}
	if err := p.expectPunct("["); err != nil {
		return EdgePattern{}, err
	}
	edge, err := p.parseEdgeBody()
	if err != nil {
		return EdgePattern{}, err
	}
	if err := p.expectPunct("]"); err != nil {
		return EdgePattern{}, err
	}
	if err := p.expectArrow(); err != nil {
		return EdgePattern{}, err
	}
	return edge, nil
}

// expectArrow consumes the `->` closing the edge segment. The lexer emits `->`
// as one token; a degenerate `-` `>` pair is also accepted for robustness.
func (p *parser) expectArrow() error {
	if p.isPunct("->") {
		p.next()
		return nil
	}
	if err := p.expectPunct("-"); err != nil {
		return err
	}
	return p.expectPunct(">")
}

// parseEdgeBody parses the inside of `[...]`: an optional lowercase alias, the
// edge type, and an optional `*min..max` variable-length suffix. The two shapes
// are `[:type]` (anonymous) and `[alias:type]` (aliased, §5.1).
func (p *parser) parseEdgeBody() (EdgePattern, error) {
	var edge EdgePattern
	if p.isPunct(":") {
		// Anonymous edge: `[:type]`.
		p.next()
		typ, err := p.expectIdent()
		if err != nil {
			return edge, err
		}
		edge.Type = typ
	} else {
		if err := p.parseAliasedEdge(&edge); err != nil {
			return edge, err
		}
	}
	if p.isPunct("*") {
		if err := p.parseVarLength(&edge); err != nil {
			return edge, err
		}
	}
	return edge, nil
}

// parseAliasedEdge parses the `alias:type` body form, binding the edge alias.
func (p *parser) parseAliasedEdge(edge *EdgePattern) error {
	alias, err := p.expectIdent()
	if err != nil {
		return err
	}
	if err := p.expectPunct(":"); err != nil {
		return fmt.Errorf("dsl: edge body %q must be `:type` or `alias:type`: %w", alias, err)
	}
	edge.Alias = alias
	if edge.Type, err = p.expectIdent(); err != nil {
		return err
	}
	return nil
}
