// Package dsl implements the v1 named-query DSL of the graph-backend adapter
// contract (graph-backend-adapter-contract §5). It is the only query interface
// for adapter named queries, materialized views, and impact-radius operations.
//
// The package has two halves:
//
//   - Parse + validate (parser.go, lexer.go, validate.go): turn a DSL string
//     into a typed *Query AST, rejecting every forbidden construct (§5.2) at
//     parse/load time. The conformance catalog (§5.5, tests T1–T39) is the
//     normative behavior; see dsl_conformance_test.go.
//   - Evaluate (eval.go): run a parsed *Query against an sdk.NamespaceView
//     (the notes+edges of one adapter namespace) and produce result rows. This
//     is what makes the hand-written dogfood runners (the t3 #168 SDK Query
//     stand-ins) expressible declaratively.
//
// Schema-aware validation (ref depth cap §5.4.1, untyped-ref rejection,
// derivation flags §7.3, variable-length bound against max_depth) keys off a
// SchemaInfo the caller supplies (compiled from a registry.Schema). The DSL
// package never imports the registry, so it stays a leaf the registry, SDK,
// and adapters can all depend on.
package dsl

// Query is the parsed, validated form of a DSL string (§5). It is produced once
// at adapter load (Parse / ParseWithSchema) and reused for every invocation;
// the AST carries everything Eval needs, so no re-parse happens per call (§5.3).
type Query struct {
	// Matches are the MATCH / OPTIONAL MATCH clauses in source order. The
	// first match binds the driving alias.
	Matches []MatchClause
	// Where are the conjuncted WHERE predicates (an empty slice means no
	// filter). v1 has no OR; predicates AND together.
	Where []Predicate
	// Returns are the RETURN items in source order.
	Returns []ReturnItem
	// aliasType maps each bound alias to the note type it ranges over (or ""
	// for an edge alias). Built during parse; used by eval and validation.
	aliasType map[string]string
	// aliasOptional records whether an alias came from OPTIONAL MATCH (§5.4.2
	// lowering keys off this).
	aliasOptional map[string]bool
}

// MatchClause is one MATCH or OPTIONAL MATCH (§5.1). v1 supports a single
// node pattern or a single edge hop between two node patterns; the optional
// flag carries left-join semantics.
type MatchClause struct {
	Optional bool
	// Nodes are the node patterns in the clause: one (bare node) or two
	// (an edge hop a-[..]->b).
	Nodes []NodePattern
	// Edge is the edge pattern joining Nodes[0]→Nodes[1] when len(Nodes)==2;
	// nil for a bare single-node MATCH.
	Edge *EdgePattern
}

// NodePattern is `(alias:type)` (§5.1). Alias may be empty only for an
// anonymous end node, but v1 requires aliases for returned/filtered nodes.
type NodePattern struct {
	Alias string
	Type  string
}

// EdgePattern is the `-[alias?:type]->` segment of a MATCH (§5.1). VarMin/
// VarMax model the variable-length form `[:type*min..max]`; when VarMax is 0
// the pattern is a single fixed hop.
type EdgePattern struct {
	Alias  string
	Type   string
	VarMin int
	VarMax int
}

// IsVarLength reports whether the edge is a variable-length pattern `*min..max`.
func (e EdgePattern) IsVarLength() bool { return e.VarMax > 0 }

// Predicate is one WHERE comparison (§5.1). The left side is always a field
// reference on a bound alias (possibly through a ref chain); the right side is
// a parameter, literal, or allowed-function call on params/literals (§5.1.1).
type Predicate struct {
	// Op is the comparison operator, one of the closed §5.1 set
	// {=, !=, <, <=, >, >=, IN} or the STARTS_WITH function form.
	Op string
	// Left is the field reference (alias + ref/field path) the predicate
	// filters on.
	Left FieldRef
	// Right is the value expression compared against Left.
	Right ValueExpr
	// Func names a WHERE-side function wrapping the whole predicate
	// (STARTS_WITH). Empty for a plain comparison.
	Func string
}

// FieldRef is a chained field access `alias.part.part...` (§5.4 ref-joins).
// Path[0] is always a bound alias; subsequent parts are ref-field hops ending
// in a scalar field or a structured `stale.<sub>` selector (§7.3).
type FieldRef struct {
	Alias string
	// Path is the field/ref selectors after the alias, in order. For
	// `c.stated_location.region` this is ["stated_location", "region"].
	Path []string
}

// IsBareAlias reports whether the ref is just `alias` with no field path
// (a bare ref id read, §5.4.1 rule 1).
func (f FieldRef) IsBareAlias() bool { return len(f.Path) == 0 }

// ValueExpr is the right-hand side of a predicate: a param ($x), a literal, or
// an allowed-function call on params/literals (§5.1.1). Exactly one of Param,
// Literal, or Call is set.
type ValueExpr struct {
	// Param is the parameter name (without the leading $) when this is a param
	// reference.
	Param string
	// Literal holds a string/number/bool literal value.
	Literal any
	// IsLiteral disambiguates a nil/zero Literal from "not a literal".
	IsLiteral bool
	// Call is a coalesce(...) call over params/literals (the only WHERE-side
	// allowed function on the value side, §5.1.1). nil when not a call.
	Call *FuncCall
}

// FuncCall is an allowed-function application (§5.1.1). Args are themselves
// ValueExprs so coalesce can nest params and literals.
type FuncCall struct {
	Name string
	Args []ValueExpr
}

// ReturnItem is one RETURN element (§5.1): a field ref, an aggregate/intrinsic
// (count(*), hop_count, min/max, coalesce), or an edge intrinsic (e.id/e.kind).
type ReturnItem struct {
	// Ref is the field reference when the item is a plain projection; its
	// Alias is empty when the item is a pure intrinsic (hop_count/count).
	Ref FieldRef
	// Func is the aggregate/intrinsic function name (count, min, max,
	// hop_count, coalesce) or empty for a plain field projection.
	Func string
	// FuncArgs are the arguments to Func (e.g. min(c.field) has one ref arg).
	FuncArgs []ReturnItem
	// Alias is the output column name. Defaults to the source text when the
	// query omits an explicit AS.
	Alias string
}
