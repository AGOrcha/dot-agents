package dsl

import "fmt"

// SchemaInfo is the slice of an adapter schema (§4) the DSL needs for
// schema-aware validation and ref resolution. The caller compiles it from a
// registry.Schema (see NewSchemaInfo) so the dsl package never imports the
// registry — it stays a leaf dependency.
type SchemaInfo struct {
	// NoteFields maps note-type → field name → field metadata.
	NoteFields map[string]map[string]FieldInfo
	// Edges maps edge-type → endpoints/derivation metadata.
	Edges map[string]EdgeInfo
	// MaxDepth bounds variable-length edge patterns (§4 / §5.1). A query whose
	// `*1..N` exceeds MaxDepth is rejected (T37). Zero means "unset" and the
	// check is skipped (Parse without a declared bound).
	MaxDepth int
}

// FieldInfo is one field's type metadata. RefType is the target note type for
// a `ref<type>` field (empty for scalar fields). Derivation mirrors the §7.3
// flag on ref fields.
type FieldInfo struct {
	Type       string
	RefType    string
	Derivation bool
}

// IsRef reports whether the field is a typed ref.
func (f FieldInfo) IsRef() bool { return f.RefType != "" }

// EdgeInfo is one edge type's endpoint metadata.
type EdgeInfo struct {
	From       string
	To         string
	Derivation bool
}

// refDepthCap is the §5.4.1 rule 5 maximum ref-chain depth (two hops).
const refDepthCap = 2

// NoteTypeDecl is the declarative form of a note type used to build a
// SchemaInfo (§4). It mirrors the registry's NoteType without importing it, so
// the dsl package stays a leaf. Fields lists the note's fields in declaration
// order.
type NoteTypeDecl struct {
	Name   string
	Fields []FieldDecl
}

// FieldDecl is one field declaration (§4). For a `ref<type>` field, Type is the
// raw declaration text (e.g. "ref<policy>") which NewSchemaInfo parses into a
// RefType.
type FieldDecl struct {
	Name       string
	Type       string
	Derivation bool
}

// EdgeTypeDecl is the declarative form of an edge type (§4).
type EdgeTypeDecl struct {
	Name       string
	From       string
	To         string
	Derivation bool
}

// NewSchemaInfo compiles a SchemaInfo from declarative note/edge declarations,
// parsing `ref<type>` field types into typed refs and rejecting the untyped
// `ref` form (§5.4, T11). maxDepth bounds variable-length patterns (§5.1).
func NewSchemaInfo(notes []NoteTypeDecl, edges []EdgeTypeDecl, maxDepth int) (SchemaInfo, error) {
	info := SchemaInfo{
		NoteFields: map[string]map[string]FieldInfo{},
		Edges:      map[string]EdgeInfo{},
		MaxDepth:   maxDepth,
	}
	for _, nt := range notes {
		fields, err := compileFields(nt)
		if err != nil {
			return SchemaInfo{}, err
		}
		info.NoteFields[nt.Name] = fields
	}
	for _, et := range edges {
		info.Edges[et.Name] = EdgeInfo{From: et.From, To: et.To, Derivation: et.Derivation}
	}
	return info, nil
}

// compileFields builds one note type's field map, parsing ref field types.
func compileFields(nt NoteTypeDecl) (map[string]FieldInfo, error) {
	out := make(map[string]FieldInfo, len(nt.Fields))
	for _, f := range nt.Fields {
		refType, err := parseRefType(f.Type)
		if err != nil {
			return nil, fmt.Errorf("note %q field %q: %w", nt.Name, f.Name, err)
		}
		out[f.Name] = FieldInfo{Type: f.Type, RefType: refType, Derivation: f.Derivation}
	}
	return out, nil
}

// parseRefType extracts the target type from a `ref<type>` declaration. It
// returns "" for non-ref types and an error for the untyped `ref` form (§5.4,
// T11: untyped ref fields are forbidden).
func parseRefType(typeDecl string) (string, error) {
	if typeDecl == "ref" {
		return "", fmt.Errorf("untyped ref is forbidden; declare ref<type>")
	}
	const prefix = "ref<"
	if len(typeDecl) > len(prefix) && typeDecl[:len(prefix)] == prefix && typeDecl[len(typeDecl)-1] == '>' {
		return typeDecl[len(prefix) : len(typeDecl)-1], nil
	}
	return "", nil
}

// ParseWithSchema parses a DSL string and then runs the schema-aware checks
// (§5.4 ref-join rules, §5.1 variable-length bound, edge-intrinsic restriction,
// untyped-ref rejection). Grammar-level rejections (§5.2) happen in Parse; this
// adds the rules that need the adapter schema to evaluate.
func ParseWithSchema(src string, info SchemaInfo) (*Query, error) {
	q, err := Parse(src)
	if err != nil {
		return nil, err
	}
	if err := q.validateSchema(info); err != nil {
		return nil, err
	}
	return q, nil
}

// validateSchema runs every schema-aware rule over the parsed query.
func (q *Query) validateSchema(info SchemaInfo) error {
	if err := q.validateVarLength(info); err != nil {
		return err
	}
	if err := q.validateMatches(info); err != nil {
		return err
	}
	for _, pred := range q.Where {
		if err := q.validateRef(pred.Left, info); err != nil {
			return err
		}
	}
	return q.validateReturns(info)
}

// validateMatches checks every edge MATCH clause against the schema: the edge
// type must be declared, and the from/to node types of the clause's endpoints
// must match the declared edge endpoints. The check is direction-aware — the
// declared edge `from`/`to` must align with the pattern's source/end node types
// (§5.1). A wrong-direction pattern (e.g. control->finding for an edge declared
// finding->control) or an unknown edge type is rejected at load rather than
// silently returning empty data at eval time.
func (q *Query) validateMatches(info SchemaInfo) error {
	for _, m := range q.Matches {
		if m.Edge == nil {
			continue
		}
		if err := validateEdgeClause(m, info); err != nil {
			return err
		}
	}
	return nil
}

// validateEdgeClause validates one edge MATCH clause's type and endpoint types.
func validateEdgeClause(m MatchClause, info SchemaInfo) error {
	ei, ok := info.Edges[m.Edge.Type]
	if !ok {
		return fmt.Errorf("dsl: MATCH references unknown edge type %q", m.Edge.Type)
	}
	fromType, toType := m.Nodes[0].Type, m.Nodes[1].Type
	if err := edgeEndpointOK(m.Edge.Type, "from", fromType, ei.From); err != nil {
		return err
	}
	return edgeEndpointOK(m.Edge.Type, "to", toType, ei.To)
}

// edgeEndpointOK checks one endpoint's node type against the declared type. An
// untyped endpoint alias (no `:type` in the pattern, e.g. a re-referenced bound
// alias `(c)`) is permitted — its type was fixed at its binding MATCH and the
// ref/return validators enforce field access; here only an explicit mismatch is
// rejected.
func edgeEndpointOK(edgeType, role, patternType, declaredType string) error {
	if patternType == "" || patternType == declaredType {
		return nil
	}
	return fmt.Errorf("dsl: edge %q expects %s-node of type %q, but the pattern uses %q (wrong direction or type)", edgeType, role, declaredType, patternType)
}

// validateVarLength enforces the declared max_depth on every variable-length
// edge pattern (§5.1, T37) and rejects a zero/inverted bound.
func (q *Query) validateVarLength(info SchemaInfo) error {
	for _, m := range q.Matches {
		if m.Edge == nil || !m.Edge.IsVarLength() {
			continue
		}
		if m.Edge.VarMax < m.Edge.VarMin || m.Edge.VarMin < 1 {
			return fmt.Errorf("dsl: variable-length bound *%d..%d is invalid", m.Edge.VarMin, m.Edge.VarMax)
		}
		if info.MaxDepth > 0 && m.Edge.VarMax > info.MaxDepth {
			return fmt.Errorf("dsl: variable-length pattern *%d..%d exceeds declared max_depth %d", m.Edge.VarMin, m.Edge.VarMax, info.MaxDepth)
		}
	}
	return nil
}

// validateReturns checks each RETURN field reference and the field refs nested
// inside RETURN functions (min/max).
func (q *Query) validateReturns(info SchemaInfo) error {
	for _, item := range q.Returns {
		if !item.Ref.IsBareAlias() || item.Ref.Alias != "" {
			if err := q.validateRef(item.Ref, info); err != nil {
				return err
			}
		}
		for _, arg := range item.FuncArgs {
			if arg.Ref.Alias == "" {
				continue
			}
			if err := q.validateRef(arg.Ref, info); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateRef enforces the §5.4 ref-join rules on one field reference: the
// alias is bound, edge aliases expose only id/kind, the chain stays within the
// depth cap, every hop traverses a declared typed ref, and the terminal is a
// real field (or a structured `stale.<sub>` selector, §7.3).
func (q *Query) validateRef(ref FieldRef, info SchemaInfo) error {
	typ, ok := q.aliasType[ref.Alias]
	if !ok {
		return fmt.Errorf("dsl: unbound alias %q in %s", ref.Alias, ref.describe())
	}
	if typ == edgeAliasType {
		return validateEdgeRef(ref)
	}
	return validateNoteRef(ref, typ, info)
}

// validateEdgeRef restricts edge-alias access to the intrinsic id/kind columns
// (§5.1, T30 rejects e.weight and any other edge metadata).
func validateEdgeRef(ref FieldRef) error {
	if len(ref.Path) != 1 || (ref.Path[0] != "id" && ref.Path[0] != "kind") {
		return fmt.Errorf("dsl: edge alias %q exposes only .id and .kind, not %s", ref.Alias, ref.describe())
	}
	return nil
}

// validateNoteRef walks a note-rooted ref chain, resolving each ref hop and
// enforcing the depth cap (§5.4.1 rule 5, T9/T10).
func validateNoteRef(ref FieldRef, rootType string, info SchemaInfo) error {
	if len(ref.Path) == 0 {
		return nil // bare ref id read (§5.4.1 rule 1)
	}
	if refHopDepth(ref, info, rootType) > refDepthCap {
		return fmt.Errorf("dsl: ref chain %s exceeds the depth cap of %d", ref.describe(), refDepthCap)
	}
	return walkRefChain(ref, rootType, info)
}

// walkRefChain resolves each path segment against the current note type,
// following ref hops and stopping at a scalar field or the structured stale
// selector.
func walkRefChain(ref FieldRef, curType string, info SchemaInfo) error {
	for i, part := range ref.Path {
		if part == "stale" {
			return nil // structured stale.<sub> selector (§7.3); validated as a unit
		}
		fields, ok := info.NoteFields[curType]
		if !ok {
			return fmt.Errorf("dsl: %s references unknown note type %q", ref.describe(), curType)
		}
		field, ok := fields[part]
		if !ok {
			if part == "id" && i == len(ref.Path)-1 {
				return nil // every note has an intrinsic id column
			}
			return fmt.Errorf("dsl: %s references unknown field %q on %q", ref.describe(), part, curType)
		}
		if i == len(ref.Path)-1 {
			return nil // terminal scalar field
		}
		if !field.IsRef() {
			return fmt.Errorf("dsl: %s traverses non-ref field %q on %q", ref.describe(), part, curType)
		}
		curType = field.RefType
	}
	return nil
}

// refHopDepth counts the ref hops a chain takes (each ref-typed intermediate is
// one hop; a terminal scalar or a stale selector is not a hop). A `stale`
// selector after one ref counts as depth-1 per §7.3.
func refHopDepth(ref FieldRef, info SchemaInfo, rootType string) int {
	depth, curType := 0, rootType
	for i, part := range ref.Path {
		if i == len(ref.Path)-1 || part == "stale" {
			break
		}
		field, ok := info.NoteFields[curType][part]
		if !ok || !field.IsRef() {
			break
		}
		depth++
		curType = field.RefType
	}
	return depth
}
