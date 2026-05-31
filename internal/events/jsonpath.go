package events

import (
	"fmt"
	"strconv"
	"strings"
)

// JSONPath evaluates a small JSONPath subset against a value decoded from JSON
// (map[string]any / []any / scalars). Supported syntax:
//
//   - dotted traversal:      ".a.b"
//   - array index:           ".items[0]"
//   - one equality filter:   ".checks[?(@.conclusion=='FAILURE')]"
//
// A leading "$" is optional. The filter selects the first array element whose
// field equals the quoted literal. No other predicates are supported.
func JSONPath(root any, expr string) (any, error) {
	steps, err := parsePath(expr)
	if err != nil {
		return nil, err
	}
	cur := root
	for _, st := range steps {
		cur, err = st.apply(cur)
		if err != nil {
			return nil, err
		}
	}
	return cur, nil
}

// step is one traversal operation against the current value.
type step struct {
	field  string // map key for ".field" steps ("" when index/filter)
	index  *int   // array index for "[n]" steps
	filter *eqFilter
}

// eqFilter is a single equality predicate: @.<field> == <literal>.
type eqFilter struct {
	field   string
	literal string
}

func (s step) apply(cur any) (any, error) {
	switch {
	case s.filter != nil:
		return s.filter.apply(cur)
	case s.index != nil:
		return indexInto(cur, *s.index)
	default:
		return fieldInto(cur, s.field)
	}
}

func fieldInto(cur any, field string) (any, error) {
	m, ok := cur.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("events: jsonpath field %q: value is not an object", field)
	}
	v, ok := m[field]
	if !ok {
		return nil, fmt.Errorf("events: jsonpath field %q: not found", field)
	}
	return v, nil
}

func indexInto(cur any, idx int) (any, error) {
	arr, ok := cur.([]any)
	if !ok {
		return nil, fmt.Errorf("events: jsonpath index [%d]: value is not an array", idx)
	}
	if idx < 0 || idx >= len(arr) {
		return nil, fmt.Errorf("events: jsonpath index [%d]: out of range (len %d)", idx, len(arr))
	}
	return arr[idx], nil
}

func (f eqFilter) apply(cur any) (any, error) {
	arr, ok := cur.([]any)
	if !ok {
		return nil, fmt.Errorf("events: jsonpath filter: value is not an array")
	}
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		if scalarEquals(m[f.field], f.literal) {
			return el, nil
		}
	}
	return nil, fmt.Errorf("events: jsonpath filter: no element matched @.%s==%q", f.field, f.literal)
}

// scalarEquals compares a decoded JSON scalar to the filter literal by string
// form, so "FAILURE", 3 and true all match their textual representation.
func scalarEquals(v any, literal string) bool {
	switch t := v.(type) {
	case string:
		return t == literal
	case bool:
		return strconv.FormatBool(t) == literal
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64) == literal
	default:
		return false
	}
}

// parsePath tokenizes the expression into ordered steps.
func parsePath(expr string) ([]step, error) {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, "$")
	if expr == "" {
		return nil, fmt.Errorf("events: jsonpath: empty expression")
	}
	var steps []step
	for _, seg := range splitSegments(expr) {
		parsed, err := parseSegment(seg)
		if err != nil {
			return nil, err
		}
		steps = append(steps, parsed...)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("events: jsonpath: no steps in %q", expr)
	}
	return steps, nil
}

// splitSegments splits on "." that are outside bracket groups, so a filter
// literal containing a dot does not fracture the segment.
func splitSegments(expr string) []string {
	var segs []string
	var cur strings.Builder
	depth := 0
	for _, r := range expr {
		switch r {
		case '[':
			depth++
			cur.WriteRune(r)
		case ']':
			depth--
			cur.WriteRune(r)
		case '.':
			if depth == 0 {
				segs = append(segs, cur.String())
				cur.Reset()
				continue
			}
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}
	segs = append(segs, cur.String())
	return segs
}

// parseSegment turns one "name", "name[0]" or "name[?(...)]" segment into one
// or more steps (a field step optionally followed by index/filter steps).
func parseSegment(seg string) ([]step, error) {
	if seg == "" {
		return nil, nil
	}
	name, brackets, err := splitBrackets(seg)
	if err != nil {
		return nil, err
	}
	var steps []step
	if name != "" {
		steps = append(steps, step{field: name})
	}
	for _, b := range brackets {
		bs, err := parseBracket(b)
		if err != nil {
			return nil, err
		}
		steps = append(steps, bs)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("events: jsonpath: invalid segment %q", seg)
	}
	return steps, nil
}

// splitBrackets separates a leading field name from a sequence of bracket
// groups, e.g. "items[0]" -> ("items", ["0"]). An unclosed "[" is an error.
func splitBrackets(seg string) (string, []string, error) {
	idx := strings.IndexByte(seg, '[')
	if idx < 0 {
		return seg, nil, nil
	}
	name := seg[:idx]
	var groups []string
	rest := seg[idx:]
	for len(rest) > 0 {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return "", nil, fmt.Errorf("events: jsonpath: unclosed '[' in %q", seg)
		}
		groups = append(groups, rest[1:end])
		rest = rest[end+1:]
	}
	return name, groups, nil
}

// parseBracket parses a single bracket body: either an integer index or a
// "?(@.field=='literal')" equality filter.
func parseBracket(body string) (step, error) {
	if strings.HasPrefix(body, "?(") {
		f, err := parseFilter(body)
		if err != nil {
			return step{}, err
		}
		return step{filter: f}, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(body))
	if err != nil {
		return step{}, fmt.Errorf("events: jsonpath: invalid index %q", body)
	}
	return step{index: &n}, nil
}

// parseFilter parses "?(@.field=='literal')" into an eqFilter.
func parseFilter(body string) (*eqFilter, error) {
	inner := strings.TrimSuffix(strings.TrimPrefix(body, "?("), ")")
	inner = strings.TrimSpace(inner)
	inner = strings.TrimPrefix(inner, "@.")
	field, literal, ok := strings.Cut(inner, "==")
	if !ok {
		return nil, fmt.Errorf("events: jsonpath filter: expected '==' in %q", body)
	}
	field = strings.TrimSpace(field)
	literal = unquote(strings.TrimSpace(literal))
	if field == "" {
		return nil, fmt.Errorf("events: jsonpath filter: empty field in %q", body)
	}
	return &eqFilter{field: field, literal: literal}, nil
}

// unquote strips a single matching pair of single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '\'' || first == '"') && first == last {
			return s[1 : len(s)-1]
		}
	}
	return s
}
