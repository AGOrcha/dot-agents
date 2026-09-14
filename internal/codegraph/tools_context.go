package codegraph

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// The change-oriented context tools: `get_impact_radius_tool` and
// `get_review_context_tool`, plus the response vocabulary they share with the
// rest of the release surface (the node/edge projections, the estimated
// context-savings block, and the empty-result confidence markers).
//
// `get_minimal_context_tool` is deliberately absent. It reads its risk score,
// key entities and test-gap count from changes.analyze_changes, which derives
// changed-symbol attribution from `git diff --unified=0` hunk boundaries
// (changes.py:431) and maps them over the DIFF's files rather than the
// caller's `changed_files`. Those boundaries are xdiff's, not reproducible by
// go-git, so the tool is bridge-routed for the same reason
// `detect_changes_tool` is. The two tools below are genuinely file-level: they
// seed from whole changed files and never look at a hunk.

func init() {
	RegisterTool("get_impact_radius_tool", impactRadiusTool)
	RegisterTool("get_review_context_tool", reviewContextTool)
}

// ── shared response vocabulary ───────────────────────────────────────────────

// sanitizeNameMaxLen is graph._sanitize_name's default bound.
const sanitizeNameMaxLen = 256

// SanitizeName is graph._sanitize_name at its default limit: ASCII control
// characters are stripped and the name is capped at 256 characters.
//
// Node names come from source code, so they are attacker-influenced text that
// flows into an agent's context. Stripping controls stops a crafted name from
// forging structure in a rendered response.
func SanitizeName(s string) string { return SanitizeNameLimit(s, sanitizeNameMaxLen) }

// SanitizeNameLimit is SanitizeName with an explicit cap, which the confidence
// markers need at their own (tighter) budget.
//
// The cap counts CHARACTERS, not bytes: upstream slices a Python str, so a
// byte-based truncation would cut a different amount of any non-ASCII
// identifier and could split a rune.
func SanitizeNameLimit(s string, maxLen int) string {
	if maxLen < 0 {
		maxLen = 0
	}
	var b strings.Builder
	b.Grow(len(s))
	kept := 0
	for _, r := range s {
		// Tab and newline survive: upstream keeps them, and the callers that
		// cannot tolerate them collapse whitespace themselves.
		if r != '\t' && r != '\n' && r < 0x20 {
			continue
		}
		if kept == maxLen {
			break
		}
		b.WriteRune(r)
		kept++
	}
	return b.String()
}

// NormalizeFilePath is parser.normalize_file_path: a file path in
// forward-slash form, which is graph identity and therefore must be
// separator-stable across operating systems.
func NormalizeFilePath(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// NodeToDict is graph.node_to_dict, the node projection every tool payload
// shares.
//
// `parent_name` is the one field that needs care: the column is nullable and
// upstream emits JSON null for a node with no parent, while
// graphstore.GraphNode flattens NULL to "". Emitting "" here would differ from
// the release on every top-level symbol in every payload, so the empty string
// is mapped back to null.
func NodeToDict(n graphstore.GraphNode) map[string]any {
	return map[string]any{
		"id":             n.ID,
		"kind":           n.Kind,
		"name":           SanitizeName(n.Name),
		"qualified_name": SanitizeName(n.QualifiedName),
		"file_path":      n.FilePath,
		"line_start":     n.LineStart,
		"line_end":       n.LineEnd,
		"language":       n.Language,
		"parent_name":    nullableSanitized(n.ParentName),
		"is_test":        n.IsTest,
	}
}

// nullableSanitized renders a nullable text column: null when unset, sanitized
// otherwise. Upstream's `_sanitize_name(x) if x else x` returns the falsy value
// untouched, which for a NULL column is None.
func nullableSanitized(s string) any {
	if s == "" {
		return nil
	}
	return SanitizeName(s)
}

// edgeTargetListKeys are the two `extra` lists edge_to_dict republishes.
var edgeTargetListKeys = [2]string{"ambiguous_targets", "unresolved_targets"}

// edgeTargetListCap bounds each republished target list.
const edgeTargetListCap = 20

// EdgeToDict is graph.edge_to_dict.
//
// `confidence` / `confidence_tier` are passed through verbatim. Both columns
// carry the release's dataclass defaults (1.0 / "EXTRACTED") as SQL DEFAULTs,
// so a client-side fallback would add nothing and would mask a genuine 0.0.
//
// When `extra` carries an ambiguous- or unresolved-target list, the list is
// republished sanitized and capped, alongside its untruncated count and a
// truncation flag — the same honest-bounding contract the `*_total` fields use
// elsewhere.
func EdgeToDict(e graphstore.GraphEdge) map[string]any {
	result := map[string]any{
		"id":              e.ID,
		"kind":            e.Kind,
		"source":          SanitizeName(e.SourceQualified),
		"target":          SanitizeName(e.TargetQualified),
		"file_path":       e.FilePath,
		"line":            e.Line,
		"confidence":      e.Confidence,
		"confidence_tier": e.ConfidenceTier,
	}
	for _, key := range edgeTargetListKeys {
		raw, ok := e.Extra[key].([]any)
		if !ok {
			continue
		}
		// The cap is applied BEFORE the string filter, so a list whose first
		// 20 entries are not all strings yields fewer than 20 — and the count
		// below still reports the full length.
		head := raw
		if len(head) > edgeTargetListCap {
			head = head[:edgeTargetListCap]
		}
		targets := make([]any, 0, len(head))
		for _, entry := range head {
			if text, isString := entry.(string); isString {
				targets = append(targets, SanitizeName(text))
			}
		}
		result[key] = targets

		resolution := strings.TrimSuffix(key, "_targets")
		count, counted := extraInt(e.Extra[resolution+"_target_count"])
		if !counted {
			count = len(raw)
		}
		result[resolution+"_target_count"] = count
		result[resolution+"_targets_truncated"] =
			truthy(e.Extra[resolution+"_targets_truncated"]) || count > len(targets)
	}
	return result
}

// ── estimated context savings (context_savings.py) ───────────────────────────

// charsPerToken is the deliberately conservative character/token ratio. The
// release labels every number derived from it an ESTIMATE for exactly this
// reason: it is a character count, not a model tokenizer.
const charsPerToken = 4

// EstimateTokens is context_savings.estimate_tokens.
//
// A string is measured directly; anything else is measured as the byte length
// of its Python-canonical JSON rendering. That rendering — and not
// encoding/json — is the contract: Python writes floats through repr (1.0
// serialises as "1.0", not "1"), escapes every non-ASCII rune, and leaves
// `<`, `>` and `&` alone, so swapping in encoding/json would silently shift
// every saved_tokens value in every payload.
func EstimateTokens(value any) int {
	if value == nil {
		return 0
	}
	text, isString := value.(string)
	if !isString {
		text = pyJSON(value)
	}
	if text == "" {
		return 0
	}
	return max(1, (len(text)+charsPerToken-1)/charsPerToken)
}

// EstimateFileTokens is context_savings.estimate_file_tokens: the baseline a
// caller would have paid by reading the changed files outright, measured from
// file SIZES so the estimate costs no I/O beyond a stat. Files that do not
// exist contribute nothing, which is why a change set of untracked or deleted
// paths produces no savings block at all.
func EstimateFileTokens(root string, files []string) int {
	total := 0
	for _, name := range files {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		total += max(1, int((info.Size()+charsPerToken-1)/charsPerToken))
	}
	return total
}

// AttachContextSavings is context_savings.attach_context_savings with an
// explicit baseline: it adds the `context_savings` block to result in place.
//
// The returned-token side of the ratio is EstimateTokens(result) as result
// stands right now, so this MUST be the last mutation of a payload — a key
// added afterwards is not counted and the block silently overstates the
// saving. Nothing is attached when the baseline is zero, because there is no
// baseline to have saved against; a zero SAVING, by contrast, is reported
// honestly as saved_tokens 0.
func AttachContextSavings(result map[string]any, originalTokens int) {
	if originalTokens <= 0 {
		return
	}
	returned := EstimateTokens(result)
	saved := max(0, originalTokens-returned)
	result["context_savings"] = map[string]any{
		"estimated":     true,
		"saved_tokens":  saved,
		"saved_percent": int(math.Round(float64(saved) / float64(originalTokens) * 100)),
	}
}

// pyJSON renders value exactly as
// json.dumps(value, default=str, ensure_ascii=True, separators=(",",":"),
// sort_keys=True) does. Only its LENGTH is consumed, but the length is only
// right if the bytes are.
func pyJSON(value any) string {
	var b strings.Builder
	writePyJSON(&b, value)
	return b.String()
}

func writePyJSON(b *strings.Builder, value any) {
	switch v := value.(type) {
	case nil:
		b.WriteString("null")
		return
	case bool:
		b.WriteString(strconv.FormatBool(v))
		return
	case string:
		writePyString(b, v)
		return
	case float64:
		b.WriteString(pyFloatRepr(v))
		return
	case float32:
		b.WriteString(pyFloatRepr(float64(v)))
		return
	case int:
		b.WriteString(strconv.Itoa(v))
		return
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
		return
	case map[string]any:
		writePyObject(b, v)
		return
	case []any:
		writePyArray(b, len(v), func(i int) any { return v[i] })
		return
	case []string:
		writePyArray(b, len(v), func(i int) any { return v[i] })
		return
	case []map[string]any:
		writePyArray(b, len(v), func(i int) any { return v[i] })
		return
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			b.WriteString("null")
			return
		}
		writePyJSON(b, rv.Elem().Interface())
	case reflect.Slice, reflect.Array:
		writePyArray(b, rv.Len(), func(i int) any { return rv.Index(i).Interface() })
	case reflect.Map:
		entries := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			entries[reflectMapKey(iter.Key())] = iter.Value().Interface()
		}
		writePyObject(b, entries)
	case reflect.Struct:
		// A typed struct reaches a payload through the transport, which
		// renders it by its json tags — the hint suggestions are the live
		// case. Walking the fields keeps each one's Go TYPE, so an int field
		// still spells as an int; a round-trip through encoding/json would
		// flatten every number to a float and re-spell it as "3.0".
		writePyObject(b, pyStructEntries(rv))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b.WriteString(strconv.FormatInt(rv.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		b.WriteString(strconv.FormatUint(rv.Uint(), 10))
	default:
		// Upstream passes default=str, so an otherwise unserialisable value
		// becomes its string form rather than an encoding failure.
		writePyString(b, fmt.Sprint(value))
	}
}

func reflectMapKey(key reflect.Value) string {
	if key.Kind() == reflect.String {
		return key.String()
	}
	return fmt.Sprint(key.Interface())
}

// pyStructEntries projects an exported struct onto its json field names,
// honouring `-`, `omitempty` and anonymous embedding the way encoding/json
// does, while leaving each value's Go type intact for the renderer.
func pyStructEntries(rv reflect.Value) map[string]any {
	entries := map[string]any{}
	structType := rv.Type()
	for i := range structType.NumField() {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" && options == "" {
			continue
		}
		value := rv.Field(i)
		if field.Anonymous && name == "" && field.Type.Kind() == reflect.Struct {
			// An embedded struct's fields are promoted, not nested.
			for key, promoted := range pyStructEntries(value) {
				entries[key] = promoted
			}
			continue
		}
		if name == "" {
			name = field.Name
		}
		if strings.Contains(options, "omitempty") && value.IsZero() {
			continue
		}
		entries[name] = value.Interface()
	}
	return entries
}

func writePyArray(b *strings.Builder, n int, at func(int) any) {
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		writePyJSON(b, at(i))
	}
	b.WriteByte(']')
}

func writePyObject(b *strings.Builder, entries map[string]any) {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	// sort_keys=True. Python compares str by code point, which is byte order
	// for UTF-8, so a plain string sort agrees.
	sort.Strings(keys)
	b.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		writePyString(b, key)
		b.WriteByte(':')
		writePyJSON(b, entries[key])
	}
	b.WriteByte('}')
}

// pyStringEscapes are the short escape forms Python emits; every other
// character outside printable ASCII becomes a \uXXXX sequence.
var pyStringEscapes = map[rune]string{
	'"':  `\"`,
	'\\': `\\`,
	'\n': `\n`,
	'\r': `\r`,
	'\t': `\t`,
	'\b': `\b`,
	'\f': `\f`,
}

func writePyString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		if escape, ok := pyStringEscapes[r]; ok {
			b.WriteString(escape)
			continue
		}
		// ensure_ascii keeps only printable ASCII literal. Note that `/`,
		// `<`, `>` and `&` are NOT escaped — encoding/json escapes the last
		// three, which is one of the reasons this encoder is hand-rolled.
		if r >= 0x20 && r <= 0x7e {
			b.WriteRune(r)
			continue
		}
		if r > 0xffff {
			high, low := utf16.EncodeRune(r)
			fmt.Fprintf(b, `\u%04x\u%04x`, high, low)
			continue
		}
		fmt.Fprintf(b, `\u%04x`, r)
	}
	b.WriteByte('"')
}

// pyFloatRepr renders a float the way Python's repr does, which is what
// json.dumps uses for floats.
//
// The shape is shortest-round-trip digits, rendered fixed-point with a
// mandatory ".0" on an integral value, switching to exponent form when the
// decimal point would fall outside (-4, 16]. Go's own 'g' formatting differs
// on both counts — it prints 1.0 as "1" and switches to exponents at a
// different threshold — so the digits are re-laid-out here.
func pyFloatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}

	sign, digits, exp10 := shortestDecimal(f)
	// decpt is the position of the decimal point relative to the digit
	// string, i.e. how many digits precede it.
	decpt := exp10 + 1

	if decpt <= -4 || decpt > 16 {
		mantissa := digits[:1]
		if len(digits) > 1 {
			mantissa += "." + digits[1:]
		}
		exponent := decpt - 1
		expSign := "+"
		if exponent < 0 {
			expSign = "-"
			exponent = -exponent
		}
		return fmt.Sprintf("%s%se%s%02d", sign, mantissa, expSign, exponent)
	}

	switch {
	case decpt <= 0:
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= len(digits):
		return sign + digits + strings.Repeat("0", decpt-len(digits)) + ".0"
	default:
		return sign + digits[:decpt] + "." + digits[decpt:]
	}
}

// shortestDecimal splits f into its sign, its shortest round-trip significant
// digits, and the base-10 exponent of the first digit.
func shortestDecimal(f float64) (sign, digits string, exp10 int) {
	formatted := strconv.FormatFloat(f, 'e', -1, 64)
	if strings.HasPrefix(formatted, "-") {
		sign = "-"
		formatted = formatted[1:]
	}
	mantissa, exponent, _ := strings.Cut(formatted, "e")
	digits = strings.Replace(mantissa, ".", "", 1)
	exp10, _ = strconv.Atoi(exponent)
	return sign, digits, exp10
}

// ── empty-result confidence markers (uncertainty.py) ─────────────────────────

// confidenceMaxChars is the hard budget for a confidence marker. Compact
// context is the point of the whole surface, so an advisory sentence that
// grows past this is a regression rather than a feature.
const confidenceMaxChars = 140

// updateHint is the remedy every staleness marker ends with.
const updateHint = "run `code-review-graph update`"

// ImpactGapPattern is the synthetic pattern name the impact tools use against
// the language-gap table, which is otherwise keyed by query_graph patterns.
const ImpactGapPattern = "impact_radius"

// languageGap is one verified static-analysis blind spot, scoped to the
// queries it actually affects. The scoping is what keeps the note honest: a
// call-resolution caveat belongs on callers_of and on the impact radius, and
// never on file_summary, whose empty result has nothing to do with calls.
type languageGap struct {
	languages []string
	patterns  []string
	note      string
}

// Patterns answered from CALLS edges, where an unresolved dynamic call site
// can hide a real answer. references_to is deliberately excluded: it reads
// REFERENCES edges, which is exactly where an unresolved handoff does land.
var gapCallPatterns = []string{"callers_of", "callees_of", "tests_for", ImpactGapPattern}

// Patterns answered from IMPORTS_FROM edges.
var gapImportPatterns = []string{"imports_of", "importers_of", ImpactGapPattern}

var gapJSFamily = []string{"javascript", "typescript", "tsx"}

// languageGaps is ordered: the first entry matching (language, pattern) wins.
var languageGaps = []languageGap{
	{
		languages: []string{"php"},
		patterns:  gapImportPatterns,
		note: "php include/require is not indexed as an import edge, so " +
			"importers can be missing here (#819)",
	},
	{
		languages: []string{"php"},
		patterns:  gapCallPatterns,
		note: "php container-resolved and constructor-injected calls are not " +
			"statically traced, so callers can be missing (#850, #851)",
	},
	{
		languages: gapJSFamily,
		patterns:  []string{"handlers_of", "endpoints_for"},
		note: "js/ts route registration is not indexed and endpoint edges are " +
			"spring-only, so handlers can be missing",
	},
	{
		languages: gapJSFamily,
		patterns:  gapImportPatterns,
		note: "npm-aliased import specifiers are not resolved, so importers " +
			"can be missing here (#343)",
	},
	{
		languages: gapJSFamily,
		patterns:  gapCallPatterns,
		note: "js/ts callbacks land on REFERENCES not CALLS, and obj[name]() " +
			"is unresolved, so callers can be missing",
	},
	{
		languages: []string{"java"},
		patterns:  gapCallPatterns,
		note: "java aop advice and reflective invocation are not statically " +
			"traced, so callers can be missing here (#592)",
	},
	{
		languages: []string{"go"},
		patterns:  []string{"inheritors_of"},
		note: "go interface satisfaction is structural, never declared, so " +
			"implementers can be missing from inheritors_of",
	},
	{
		languages: []string{"csharp"},
		patterns:  gapCallPatterns,
		note: "c# di-container registrations are not statically traced, so " +
			"interface-typed callers can be missing here",
	},
	{
		languages: []string{"python"},
		patterns:  gapCallPatterns,
		note: "python getattr dispatch and decorator-based registration are " +
			"not statically traced, so callers can be missing here",
	},
}

// LanguageGapNote is uncertainty.gap_note: the first verified gap note for
// language under pattern, or "" when the language has no known gap there.
func LanguageGapNote(language, pattern string) string {
	if language == "" {
		return ""
	}
	normalized := strings.ToLower(strings.TrimSpace(language))
	for _, gap := range languageGaps {
		if containsString(gap.languages, normalized) && containsString(gap.patterns, pattern) {
			return gap.note
		}
	}
	return ""
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// ConfidenceNote is uncertainty._bounded, the final gate every marker passes
// through: nothing leaves over the character budget.
func ConfidenceNote(note string) string { return confidenceFragment(note, confidenceMaxChars) }

// clipConfidence truncates to limit characters, marking the cut so a reader
// can tell a clipped sentence from a short one.
func clipConfidence(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:max(1, limit-1)]) + "~"
}

// cleanConfidence makes attacker-influenced source text safe to embed in a
// single line. SanitizeNameLimit keeps tabs and newlines, which would let a
// crafted node name forge extra advisory lines, so whitespace runs are
// collapsed here as well.
func cleanConfidence(text string) string {
	return strings.Join(strings.Fields(SanitizeNameLimit(text, confidenceMaxChars)), " ")
}

// confidenceFragment is sanitized text guaranteed to fit limit characters.
func confidenceFragment(text string, limit int) string {
	return clipConfidence(cleanConfidence(text), max(1, limit))
}

// InterpolatedNote is uncertainty._interpolated: prefix + suffix are fixed
// text, and the interpolated value gets whatever budget they leave.
func InterpolatedNote(prefix, value, suffix string) string {
	budget := confidenceMaxChars - len([]rune(prefix)) - len([]rune(suffix))
	return prefix + confidenceFragment(value, budget) + suffix
}

// InterpolatedTargetNote is InterpolatedNote for a query target, which is a
// qualified name.
func InterpolatedTargetNote(prefix, target, suffix string) string {
	budget := confidenceMaxChars - len([]rune(prefix)) - len([]rune(suffix))
	return prefix + targetFragment(target, budget) + suffix
}

// targetFragment fits a qualified name into limit, keeping the half that
// identifies it. A left-anchored clip would throw away the symbol and keep a
// directory prefix — the least useful half — so the bare symbol is tried
// before clipping. The split runs on the RAW target because sanitising first
// can cut the "::" off a very long path.
func targetFragment(target string, limit int) string {
	cleaned := cleanConfidence(target)
	if len([]rune(cleaned)) <= limit {
		return cleaned
	}
	if index := strings.LastIndex(target, "::"); index >= 0 {
		symbol := cleanConfidence(target[index+2:])
		if length := len([]rune(symbol)); length > 0 && length <= limit {
			return symbol
		}
	}
	return clipConfidence(cleaned, max(1, limit))
}

// NotIndexedNote says the graph never saw target, so the zero proves nothing.
func NotIndexedNote(target string) string {
	return InterpolatedTargetNote(
		"target not indexed: no node matching '",
		target,
		"', so this 0 is not evidence that none exist",
	)
}

// UnresolvedStaleNote says the target is missing from a graph that predates
// HEAD. A stale graph explains the miss AND has a remedy, so it outranks the
// flat "not indexed" wording, which reads like a permanent limitation.
func UnresolvedStaleNote(target string) string {
	return InterpolatedTargetNote(
		"graph is stale: no node matching '",
		target,
		"'; the graph predates HEAD, so run `code-review-graph update` first",
	)
}

// GraphStaleness is uncertainty._staleness: it returns a staleness note ("" if
// the graph is not known to be stale) and whether currency was actually
// VERIFIED.
//
// Two independent signals can prove staleness: the build commit versus the
// checked-out commit, and a file's mtime versus the build timestamp. The
// second matters because a commit match says nothing about uncommitted edits.
// The second return is only true when a check ran and passed, so a caller
// never confuses "checked and fresh" with "could not check" — that distinction
// is the difference between the confident and the hedged marker wording.
//
// filePath may be empty, meaning "commit check only".
func GraphStaleness(store graphstore.CodeGraphReader, root, filePath string) (string, bool) {
	if store == nil {
		return "", false
	}
	storedSHA, err := store.GetMetadata("git_head_sha")
	if err != nil {
		storedSHA = ""
	}
	liveSHA := ""
	if storedSHA != "" {
		liveSHA = headCommit(root)
	}
	if storedSHA != "" && liveSHA != "" && liveSHA != storedSHA {
		return ConfidenceNote(
			"graph is stale: built at an older commit than HEAD, so this " +
				"0 may be out of date; " + updateHint,
		), false
	}
	commitVerified := storedSHA != "" && liveSHA != ""

	builtAtRaw, err := store.GetMetadata("last_updated")
	if err != nil {
		builtAtRaw = ""
	}
	if builtAtRaw == "" || filePath == "" {
		return "", commitVerified && filePath == ""
	}

	// Graphs store absolute or repo-relative paths depending on how they were
	// built, so a relative one is anchored at the root rather than the CWD.
	path := filePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	// The stamp is upstream's `time.strftime("%Y-%m-%dT%H:%M:%S")`: naive
	// LOCAL time with no offset, which is why parseGraphTimestamp tries that
	// layout before RFC3339. Reading a naive stamp as UTC would be wrong by
	// the whole local offset — a far nastier failure than the sub-second
	// window below, because it does not self-correct and would report a
	// freshly built graph as hours stale, or a stale one as current.
	builtAt, err := parseGraphTimestamp(builtAtRaw)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	if info.ModTime().After(builtAt) {
		return InterpolatedNote(
			"graph is stale: ",
			filepath.Base(path),
			" changed after the last build; "+updateHint,
		), false
	}
	return "", commitVerified
}

// emptyImpactConfidence is uncertainty.empty_impact_confidence: the marker for
// a blast radius that came back empty.
//
// "Nothing depends on these files" and "nothing about these files is indexed"
// are indistinguishable to a reader without it, and an agent that reads the
// second as the first goes and greps the repository anyway — which is the
// multi-thousand-token fallback this one sentence exists to prevent.
func emptyImpactConfidence(
	store graphstore.CodeGraphReader,
	root string,
	changedFiles, resolvedFiles []string,
	language string,
) string {
	if len(resolvedFiles) == 0 {
		unknown := ""
		if len(changedFiles) > 0 {
			unknown = filepath.Base(NormalizeFilePath(changedFiles[0]))
		}
		return ConfidenceNote(NotIndexedNote(unknown))
	}
	if stale, current := GraphStaleness(store, root, resolvedFiles[0]); stale != "" {
		return ConfidenceNote(stale)
	} else if gap := LanguageGapNote(language, ImpactGapPattern); gap != "" {
		return ConfidenceNote(gap)
	} else if current {
		return ConfidenceNote(
			"changed files are indexed and the graph is current, so this " +
				"0 is a real absence",
		)
	}
	return ConfidenceNote(
		"changed files are indexed and nothing depends on them; graph " +
			"currency unverified",
	)
}

// ── changed-file resolution ──────────────────────────────────────────────────

// ResolveGraphFilePaths is tools/_common._resolve_graph_file_paths: it maps
// user-facing paths onto the paths actually stored in the graph.
//
// A graph may hold absolute, repo-relative or cwd-relative paths depending on
// how it was built, while tool inputs are usually relative to the repo root,
// so exact matching alone silently misses indexed files. The result is
// deduplicated and keeps first-seen order.
func ResolveGraphFilePaths(store graphstore.CodeGraphReader, root string, paths []string) []string {
	resolved := make([]string, 0, len(paths))
	if store == nil {
		return resolved
	}
	seen := make(map[string]bool, len(paths))
	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			resolved = append(resolved, path)
		}
	}

	for _, filePath := range paths {
		candidates := []string{NormalizeFilePath(filePath)}
		if filepath.IsAbs(filePath) {
			if rel, err := filepath.Rel(root, filepath.Clean(filePath)); err == nil &&
				!strings.HasPrefix(rel, "..") {
				candidates = append(candidates, NormalizeFilePath(rel))
			}
		} else {
			candidates = append(candidates, NormalizeFilePath(filepath.Join(root, filePath)))
		}

		for _, candidate := range candidates {
			if nodes, err := store.GetNodesByFile(candidate); err == nil && len(nodes) > 0 {
				add(candidate)
			}
		}
		for _, suffix := range dedupeStrings(candidates) {
			for _, matched := range filesMatchingSuffix(store, suffix) {
				add(matched)
			}
		}
	}
	return resolved
}

func dedupeStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// filesMatchingSuffix is graph.get_files_matching: the indexed file paths
// ending in suffix. Upstream runs it as a LIKE '%<suffix>' scan over the node
// table, whose DISTINCT results come back sorted, so the order is reproduced
// explicitly rather than inherited from an unstated query plan.
func filesMatchingSuffix(store graphstore.CodeGraphReader, suffix string) []string {
	files, err := store.GetAllFiles()
	if err != nil {
		return nil
	}
	pattern := NormalizeFilePath(suffix)
	matched := make([]string, 0, 1)
	for _, file := range files {
		if strings.HasSuffix(file, pattern) {
			matched = append(matched, file)
		}
	}
	sort.Strings(matched)
	return matched
}

// releaseChangeSet resolves the change set the way every change-oriented tool
// does: the diff against base, else the working tree.
//
// An ordinary git failure yields an empty set rather than an error. That is
// upstream's documented behaviour (get_changed_files logs and returns []), and
// it is what keeps these tools usable in a repository whose only commit is its
// root commit; "no changes detected" is the honest answer there.
//
// A Subversion working copy is the deliberate exception. Discovery is not
// implemented for it, so reporting "nothing changed" would be an empty answer
// stated confidently — the caller gets the capability error instead.
func releaseChangeSet(root, base string) ([]string, error) {
	changed, err := ReleaseChangedFiles(root, base)
	if errors.Is(err, ErrSubversionWorkingCopy) {
		return nil, err
	}
	if err == nil && len(changed) > 0 {
		return changed, nil
	}
	working, err := ReleaseWorkingTreeFiles(root)
	if errors.Is(err, ErrSubversionWorkingCopy) {
		return nil, err
	}
	if err != nil {
		return nil, nil
	}
	return working, nil
}

// ── impact radius (graph.get_impact_radius_sql) ──────────────────────────────

// impactMaxNodes is the release's default ceiling on returned impacted nodes.
const impactMaxNodes = 500

// Impact scoring. Each hop multiplies the best score, so strongly coupled
// nodes rank first. These are REVIEW-RISK weights and intentionally differ
// from the community-clustering affinity weights.
var impactEdgeWeights = map[string]float64{
	"CALLS":        1.0,
	"INHERITS":     0.9,
	"OVERRIDES":    0.9,
	"IMPLEMENTS":   0.9,
	"TESTED_BY":    0.7,
	"REFERENCES":   0.6,
	"DEPENDS_ON":   0.6,
	"IMPORTS_FROM": 0.5,
	"CONTAINS":     0.3,
}

const impactDefaultEdgeWeight = 0.5

// Edge traversal direction. A stored dependency edge points from the dependent
// to its dependency, so impact normally propagates AGAINST the stored edge
// (target to source) — that is "incoming". TESTED_BY is stored in the opposite
// orientation (production to test) and so propagates forward. CONTAINS is not
// traversed at all: changing a file already seeds every node in it, and
// following containment can bridge into unrelated structure through a stale
// edge.
const (
	impactDirectionIncoming = "incoming"
	impactDirectionOutgoing = "outgoing"
	impactDirectionNone     = "none"
)

var impactEdgeDirections = map[string]string{
	"CALLS":        impactDirectionIncoming,
	"INHERITS":     impactDirectionIncoming,
	"OVERRIDES":    impactDirectionIncoming,
	"IMPLEMENTS":   impactDirectionIncoming,
	"TESTED_BY":    impactDirectionOutgoing,
	"REFERENCES":   impactDirectionIncoming,
	"DEPENDS_ON":   impactDirectionIncoming,
	"IMPORTS_FROM": impactDirectionIncoming,
	"CONTAINS":     impactDirectionNone,
}

// An unknown relationship conservatively follows the dominant convention,
// source depends on target. That includes possible dependents without claiming
// that a changed node's own unclassified dependency is impacted.
const impactDefaultEdgeDirection = impactDirectionIncoming

const (
	impactDepthDecay = 0.6
	impactScoreFloor = 0.05
)

// ImpactRadiusResult is graph.get_impact_radius's return value: the blast
// radius of a change set, with the honest-bounding fields that make a
// truncated answer readable.
type ImpactRadiusResult struct {
	// ChangedNodes are the nodes in the changed files themselves.
	ChangedNodes []graphstore.GraphNode
	// ImpactedNodes are the reachable nodes, best-path score first.
	ImpactedNodes []graphstore.GraphNode
	// ImpactedFiles are the distinct files ImpactedNodes span.
	ImpactedFiles []string
	// Edges are the edges with both endpoints inside the radius.
	Edges []graphstore.GraphEdge
	// Truncated reports that ImpactedNodes was cut by the node ceiling.
	Truncated bool
	// TotalImpacted is the UNTRUNCATED impacted-node count.
	TotalImpacted int
	// ImpactScores maps an impacted node's qualified name to its best-path
	// score, rounded to 4 decimals.
	ImpactScores map[string]float64
}

// ImpactRadius computes the blast radius of changedFiles by bounded best-score
// relaxation over the edge graph.
//
// This is NOT graphstore's own GetImpactRadius. That one is an unweighted BFS
// with provider-clamped bounds and no scores, totals or truncation flag; the
// release ranks by a decayed per-edge-kind weight and reports the untruncated
// total, and a caller that needs the release's answer needs this one.
//
// Relaxation, not path enumeration, is the point: a dense cyclic graph
// contains exponentially many paths, while keeping one best score per endpoint
// costs one pass over the edges per hop.
func ImpactRadius(
	store graphstore.Store,
	changedFiles []string,
	maxDepth, maxNodes int,
) (ImpactRadiusResult, error) {
	result := ImpactRadiusResult{
		ChangedNodes:  []graphstore.GraphNode{},
		ImpactedNodes: []graphstore.GraphNode{},
		ImpactedFiles: []string{},
		Edges:         []graphstore.GraphEdge{},
		ImpactScores:  map[string]float64{},
	}
	maxDepth = max(0, maxDepth)
	maxNodes = max(0, maxNodes)
	if store == nil || len(changedFiles) == 0 {
		return result, nil
	}

	seeds, err := impactSeedNames(store, changedFiles)
	if err != nil {
		return ImpactRadiusResult{}, err
	}
	if len(seeds) == 0 {
		return result, nil
	}

	edges, err := store.ReadAllEdges()
	if err != nil {
		return ImpactRadiusResult{}, err
	}
	best := relaxImpactScores(seeds, edges, maxDepth)

	// Only names that resolve to a real node can consume a result slot. A
	// bare cross-package call target or a bridging namespace string stays in
	// the relaxation as a connector but never surfaces.
	ranked := make([]impactCandidate, 0, len(best))
	for name, score := range best {
		if seeds[name] {
			continue
		}
		node, err := store.GetNode(name)
		if err != nil {
			return ImpactRadiusResult{}, err
		}
		if node == nil || isVerilogDeclaration(*node) {
			continue
		}
		ranked = append(ranked, impactCandidate{node: *node, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].node.QualifiedName < ranked[j].node.QualifiedName
	})

	result.TotalImpacted = len(ranked)
	result.Truncated = len(ranked) > maxNodes
	if result.Truncated {
		ranked = ranked[:maxNodes]
	}

	radius := make([]string, 0, len(seeds)+len(ranked))
	for name := range seeds {
		radius = append(radius, name)
	}
	for _, candidate := range ranked {
		result.ImpactedNodes = append(result.ImpactedNodes, candidate.node)
		result.ImpactScores[candidate.node.QualifiedName] = round4(candidate.score)
		radius = append(radius, candidate.node.QualifiedName)
	}
	result.ImpactedFiles = distinctFilePaths(result.ImpactedNodes)

	if result.ChangedNodes, err = nodesByName(store, sortedNames(seeds)); err != nil {
		return ImpactRadiusResult{}, err
	}
	// Stable radius order so the edge query, and therefore the response, does
	// not depend on map iteration order.
	sort.Strings(radius)
	if result.Edges, err = edgesAmong(store, radius); err != nil {
		return ImpactRadiusResult{}, err
	}
	return result, nil
}

type impactCandidate struct {
	node  graphstore.GraphNode
	score float64
}

// impactSeedNames is graph._impact_seed_qns: every node in the changed files,
// plus the namespaces a changed C# file declares.
//
// C# `using X.Y;` directives store their IMPORTS_FROM target as a raw
// namespace string rather than a file path, so without those bridge seeds the
// traversal can never reach importers of a changed .cs file. The namespace
// strings have no node rows, so they bridge only and never surface in results.
func impactSeedNames(store graphstore.Store, changedFiles []string) (map[string]bool, error) {
	seeds := map[string]bool{}
	for _, file := range changedFiles {
		nodes, err := store.GetNodesByFile(file)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			seeds[node.QualifiedName] = true
			if node.Kind != nodeKindFile || node.Language != "csharp" {
				continue
			}
			namespaces, _ := node.Extra["csharp_namespaces"].([]any)
			for _, entry := range namespaces {
				if name, ok := entry.(string); ok && name != "" {
					seeds[name] = true
				}
			}
		}
	}
	return seeds, nil
}

// relaxImpactScores spends maxDepth hops propagating the best score reachable
// from the seeds, and returns the best score per name (seeds included at 1.0).
//
// A candidate is kept only when it IMPROVES on the name's best score so far,
// which is what makes the pass terminate on a cyclic graph, and only when it
// clears the score floor, which stops a long weak chain from dragging
// unrelated code into the radius.
func relaxImpactScores(
	seeds map[string]bool,
	edges []graphstore.GraphEdge,
	maxDepth int,
) map[string]float64 {
	best := make(map[string]float64, len(seeds))
	frontier := make(map[string]float64, len(seeds))
	for name := range seeds {
		best[name] = 1.0
		frontier[name] = 1.0
	}

	for range maxDepth {
		if len(frontier) == 0 {
			break
		}
		next := map[string]float64{}
		for _, edge := range edges {
			direction, ok := impactEdgeDirections[edge.Kind]
			if !ok {
				direction = impactDefaultEdgeDirection
			}
			var from, to string
			switch direction {
			case impactDirectionOutgoing:
				from, to = edge.SourceQualified, edge.TargetQualified
			case impactDirectionIncoming:
				from, to = edge.TargetQualified, edge.SourceQualified
			default:
				continue
			}
			score, onFrontier := frontier[from]
			if !onFrontier {
				continue
			}
			weight, ok := impactEdgeWeights[edge.Kind]
			if !ok {
				weight = impactDefaultEdgeWeight
			}
			candidate := score * weight * impactDepthDecay
			if candidate <= impactScoreFloor || candidate <= best[to] {
				continue
			}
			if candidate > next[to] {
				next[to] = candidate
			}
		}
		if len(next) == 0 {
			break
		}
		for name, score := range next {
			best[name] = score
		}
		frontier = next
	}
	return best
}

// isVerilogDeclaration reports an RTL declaration, which is stored as a
// Function node for compatibility but is not a callable symbol and must not
// occupy an impact slot.
func isVerilogDeclaration(node graphstore.GraphNode) bool {
	_, ok := node.Extra["verilog_kind"]
	return ok
}

// nodesByName loads nodes for the given qualified names, dropping the ones
// with no row. The result is ordered by qualified name, which is the order
// upstream's batched `qualified_name IN (...)` lookup returns rows in.
func nodesByName(store graphstore.Store, names []string) ([]graphstore.GraphNode, error) {
	nodes := make([]graphstore.GraphNode, 0, len(names))
	for _, name := range names {
		node, err := store.GetNode(name)
		if err != nil {
			return nil, err
		}
		if node != nil {
			nodes = append(nodes, *node)
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].QualifiedName < nodes[j].QualifiedName
	})
	return nodes, nil
}

// edgesAmong returns the edges with both endpoints inside names, ordered by
// (source qualified name, edge id).
//
// That ordering is upstream's, but only as an emergent property of its
// source-qualified index probe order, so it is applied explicitly here rather
// than inherited from a query plan that an added index could change.
func edgesAmong(store graphstore.Store, names []string) ([]graphstore.GraphEdge, error) {
	edges, err := store.GetEdgesAmong(names)
	if err != nil {
		return nil, err
	}
	if edges == nil {
		edges = []graphstore.GraphEdge{}
	}
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].SourceQualified != edges[j].SourceQualified {
			return edges[i].SourceQualified < edges[j].SourceQualified
		}
		return edges[i].ID < edges[j].ID
	})
	return edges, nil
}

// distinctFilePaths returns the distinct file paths of nodes, in first-seen
// order.
//
// Upstream builds this from a Python set, whose iteration order is hash-seed
// dependent and therefore not reproducible even by the release against
// itself. First-seen order over the score-ranked node list is deterministic
// and puts the most impacted file first, which is the useful reading.
func distinctFilePaths(nodes []graphstore.GraphNode) []string {
	files := make([]string, 0, len(nodes))
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if !seen[node.FilePath] {
			seen[node.FilePath] = true
			files = append(files, node.FilePath)
		}
	}
	return files
}

func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// round4 reproduces Python's round(x, 4). Go's math.Round breaks ties away
// from zero while Python rounds the exact binary value half-to-even, which is
// what strconv's correctly-rounded decimal formatting does.
func round4(f float64) float64 {
	// FormatFloat's own output always parses back — including "NaN", "+Inf"
	// and "-Inf" — so the error is structurally impossible here.
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', 4, 64), 64)
	return rounded
}

// ── Tool: get_impact_radius ──────────────────────────────────────────────────

// impactRadiusTool answers `get_impact_radius_tool`: which symbols, and
// therefore which files, a change set can affect.
func impactRadiusTool(e *Engine, args crgrelease.Args) (any, error) {
	maxDepth := args.Int("max_depth")
	changedFiles, explicit := args.StringSlice("changed_files")
	if !explicit {
		var err error
		if changedFiles, err = releaseChangeSet(e.root, args.String("base")); err != nil {
			return nil, err
		}
	}
	if len(changedFiles) == 0 {
		return map[string]any{
			"status":         statusOK,
			"summary":        "No changed files detected.",
			"changed_nodes":  []map[string]any{},
			"impacted_nodes": []map[string]any{},
			"impacted_files": []string{},
			"truncated":      false,
			"total_impacted": 0,
		}, nil
	}

	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	// The baseline is what reading the changed files outright would have
	// cost, so it is measured from the CALLER's paths, not the resolved ones.
	originalTokens := EstimateFileTokens(e.root, changedFiles)
	resolved := ResolveGraphFilePaths(store, e.root, changedFiles)
	impact, err := ImpactRadius(store, resolved, maxDepth, impactMaxNodes)
	if err != nil {
		return nil, err
	}

	summary := strings.Join(impactSummaryLines(changedFiles, impact, maxDepth), "\n")
	nodesOmitted := max(0, impact.TotalImpacted-len(impact.ImpactedNodes))

	// An empty radius is ambiguous, so it is the one case that earns a
	// confidence marker.
	confidence := ""
	if len(impact.ImpactedNodes) == 0 {
		confidence = emptyImpactConfidence(
			store, e.root, changedFiles, resolved, firstLanguage(impact.ChangedNodes))
	}

	var response map[string]any
	if args.String("detail_level") == detailMinimal {
		response = map[string]any{
			"status":              statusOK,
			"summary":             summary,
			"risk":                impactRiskBand(len(impact.ImpactedNodes)),
			"impacted_file_count": len(impact.ImpactedFiles),
			"key_entities":        nodeNames(impact.ImpactedNodes, 5),
			"truncated":           impact.Truncated,
			"nodes_omitted":       nodesOmitted,
		}
	} else {
		response = map[string]any{
			"status":         statusOK,
			"summary":        summary,
			"changed_files":  changedFiles,
			"changed_nodes":  nodeDicts(impact.ChangedNodes, nil),
			"impacted_nodes": nodeDicts(impact.ImpactedNodes, impact.ImpactScores),
			"impacted_files": impact.ImpactedFiles,
			"edges":          edgeDicts(impact.Edges),
			"truncated":      impact.Truncated,
			"total_impacted": impact.TotalImpacted,
			"nodes_omitted":  nodesOmitted,
		}
	}
	if confidence != "" {
		response["confidence"] = confidence
	}
	AttachContextSavings(response, originalTokens)
	return response, nil
}

// impactSummaryLines renders the blast-radius summary, naming the truncation
// explicitly when it happened.
func impactSummaryLines(changedFiles []string, impact ImpactRadiusResult, maxDepth int) []string {
	lines := []string{
		fmt.Sprintf("Blast radius for %d changed file(s):", len(changedFiles)),
		fmt.Sprintf("  - %d nodes directly changed", len(impact.ChangedNodes)),
		fmt.Sprintf("  - %d nodes impacted (within %d hops)", len(impact.ImpactedNodes), maxDepth),
		fmt.Sprintf("  - %d additional files affected", len(impact.ImpactedFiles)),
	}
	if impact.Truncated {
		lines = append(lines, fmt.Sprintf(
			"  - Results truncated: showing %d of %d impacted nodes",
			len(impact.ImpactedNodes), impact.TotalImpacted))
	}
	return lines
}

// detailMinimal is the compact output mode every detail_level-aware tool
// accepts.
const detailMinimal = "minimal"

// impactRiskBand is the release's blast-radius-size risk band.
func impactRiskBand(impactedCount int) string {
	switch {
	case impactedCount > 20:
		return "high"
	case impactedCount > 5:
		return "medium"
	default:
		return "low"
	}
}

// firstLanguage is the first non-empty language among nodes, which is what the
// confidence marker consults the language-gap table with.
func firstLanguage(nodes []graphstore.GraphNode) string {
	for _, node := range nodes {
		if node.Language != "" {
			return node.Language
		}
	}
	return ""
}

// nodeNames lists at most limit node names.
func nodeNames(nodes []graphstore.GraphNode, limit int) []string {
	if len(nodes) < limit {
		limit = len(nodes)
	}
	names := make([]string, 0, limit)
	for _, node := range nodes[:limit] {
		names = append(names, node.Name)
	}
	return names
}

// nodeDicts projects nodes, attaching each one's impact score when scores are
// supplied and the node has one.
func nodeDicts(nodes []graphstore.GraphNode, scores map[string]float64) []map[string]any {
	dicts := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		dict := NodeToDict(node)
		if score, ok := scores[node.QualifiedName]; ok {
			dict["impact_score"] = score
		}
		dicts = append(dicts, dict)
	}
	return dicts
}

func edgeDicts(edges []graphstore.GraphEdge) []map[string]any {
	dicts := make([]map[string]any, 0, len(edges))
	for _, edge := range edges {
		dicts = append(dicts, EdgeToDict(edge))
	}
	return dicts
}

// ── Tool: get_review_context ─────────────────────────────────────────────────

// Hard ceilings for the review context. It walks the full impact radius of a
// change set, so on a whole-repo diff every list below is proportional to the
// REPOSITORY rather than to the change. The numbers come from measured cost
// per row against a 5.6k-node graph: a node dict is ~60 tokens, a source line
// ~10.
const (
	reviewCtxMaxNodes         = 100
	reviewCtxMaxEdges         = 150
	reviewCtxMaxFiles         = 200
	reviewCtxMaxLinesPerFile  = 500
	reviewCtxSourceLineBudget = 800
	// reviewCtxFallbackLines bounds the "no changed node in this file" source
	// fallback, which shows the head of the file instead.
	reviewCtxFallbackLines = 50
	// reviewCtxContextBefore / reviewCtxContextAfter pad each changed node's
	// snippet window.
	reviewCtxContextBefore = 3
	reviewCtxContextAfter  = 2
)

// reviewContextTool answers `get_review_context_tool`: an impact analysis plus
// the source and guidance a reviewer needs, bounded so it fits a client's
// context window.
func reviewContextTool(e *Engine, args crgrelease.Args) (any, error) {
	maxResults := args.Int("max_results")
	maxFiles := args.Int("max_files")
	maxLinesPerFile := args.Int("max_lines_per_file")
	// Validation order is observable: upstream reports the first offending
	// bound, and it raises BEFORE opening the store, so a bad bound is a
	// transport error with no payload rather than an in-band error dict.
	if err := ValidatePositiveInt(maxResults, "max_results"); err != nil {
		return nil, err
	}
	if err := ValidatePositiveInt(maxFiles, "max_files"); err != nil {
		return nil, err
	}
	if err := ValidatePositiveInt(maxLinesPerFile, "max_lines_per_file"); err != nil {
		return nil, err
	}

	changedFiles, explicit := args.StringSlice("changed_files")
	if !explicit {
		var err error
		if changedFiles, err = releaseChangeSet(e.root, args.String("base")); err != nil {
			return nil, err
		}
	}
	if len(changedFiles) == 0 {
		return map[string]any{
			"status":  statusOK,
			"summary": "No changes detected. Nothing to review.",
			"context": map[string]any{},
		}, nil
	}

	store, err := e.readStore()
	if err != nil {
		return nil, err
	}
	originalTokens := EstimateFileTokens(e.root, changedFiles)
	resolved := ResolveGraphFilePaths(store, e.root, changedFiles)
	impact, err := ImpactRadius(store, resolved, args.Int("max_depth"), impactMaxNodes)
	if err != nil {
		return nil, err
	}

	if args.String("detail_level") == detailMinimal {
		return minimalReviewContext(changedFiles, impact, originalTokens), nil
	}
	return standardReviewContext(
		e.root, changedFiles, impact, args.Bool("include_source"),
		maxResults, maxFiles, maxLinesPerFile, originalTokens,
	), nil
}

// minimalReviewContext is the token-efficient summary: risk band, counts, the
// top changed entities and a test-gap count, with no subgraph or source.
func minimalReviewContext(
	changedFiles []string,
	impact ImpactRadiusResult,
	originalTokens int,
) map[string]any {
	risk := impactRiskBand(len(impact.ImpactedNodes))
	result := map[string]any{
		"status": statusOK,
		"summary": strings.Join([]string{
			fmt.Sprintf("Review context for %d changed file(s):", len(changedFiles)),
			fmt.Sprintf("  - Risk: %s", risk),
			fmt.Sprintf("  - %d impacted nodes in %d files",
				len(impact.ImpactedNodes), len(impact.ImpactedFiles)),
		}, "\n"),
		"risk":                risk,
		"changed_file_count":  len(changedFiles),
		"impacted_file_count": len(impact.ImpactedFiles),
		"key_entities":        nodeNames(impact.ChangedNodes, 5),
		"test_gaps":           len(untestedChangedFunctions(impact, false)),
		"next_tool_suggestions": []string{
			"detect_changes", "get_affected_flows", "get_impact_radius",
		},
	}
	AttachContextSavings(result, originalTokens)
	return result
}

// standardReviewContext is the full review payload: a bounded subgraph, source
// snippets under a shared line budget, and review guidance.
func standardReviewContext(
	root string,
	changedFiles []string,
	impact ImpactRadiusResult,
	includeSource bool,
	maxResults, maxFiles, maxLinesPerFile, originalTokens int,
) map[string]any {
	// Every list below scales with the change set, so each is bounded and
	// reports its untruncated total.
	shownFiles, filesTotal, filesCut := Bounded(changedFiles, maxFiles, reviewCtxMaxFiles)
	impactedFiles, impactedFilesTotal, impactedFilesCut :=
		Bounded(impact.ImpactedFiles, maxFiles, reviewCtxMaxFiles)
	changedNodes, changedNodesTotal, changedNodesCut :=
		Bounded(impact.ChangedNodes, maxResults, reviewCtxMaxNodes)
	impactedNodes, impactedNodesTotal, impactedNodesCut :=
		Bounded(impact.ImpactedNodes, maxResults, reviewCtxMaxNodes)
	edges, edgesTotal, edgesCut := Bounded(impact.Edges, maxResults, reviewCtxMaxEdges)

	guidance := reviewGuidance(impact)
	context := map[string]any{
		"changed_files":        shownFiles,
		"changed_files_total":  filesTotal,
		"impacted_files":       nonNilStrings(impactedFiles),
		"impacted_files_total": impactedFilesTotal,
		"graph": map[string]any{
			"changed_nodes":        nodeDicts(changedNodes, nil),
			"changed_nodes_total":  changedNodesTotal,
			"impacted_nodes":       nodeDicts(impactedNodes, nil),
			"impacted_nodes_total": impactedNodesTotal,
			"edges":                edgeDicts(edges),
			"edges_total":          edgesTotal,
		},
		"truncated": filesCut || impactedFilesCut || changedNodesCut ||
			impactedNodesCut || edgesCut,
		"review_guidance": guidance,
	}

	if includeSource {
		attachSourceSnippets(context, root, shownFiles, impact.ChangedNodes, maxLinesPerFile)
	}

	return withContextSavings(map[string]any{
		"status": statusOK,
		"summary": strings.Join([]string{
			fmt.Sprintf("Review context for %d changed file(s)", filesTotal) +
				ShownOf(len(shownFiles), filesTotal) + ":",
			fmt.Sprintf("  - %d directly changed nodes", changedNodesTotal) +
				ShownOf(len(changedNodes), changedNodesTotal),
			fmt.Sprintf("  - %d impacted nodes in %d files",
				impactedNodesTotal, impactedFilesTotal) +
				ShownOf(len(impactedNodes), impactedNodesTotal),
			"",
			"Review guidance:",
			guidance,
		}, "\n"),
		"context": context,
	}, originalTokens)
}

func withContextSavings(result map[string]any, originalTokens int) map[string]any {
	AttachContextSavings(result, originalTokens)
	return result
}

func nonNilStrings(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

// attachSourceSnippets inlines the changed files' source under a SHARED line
// budget.
//
// The per-file limit alone is not enough: without a total budget, N files each
// contribute a whole file, which is how snippets came to be 109k tokens of a
// 134k-token worst case. When the budget runs out mid-list the remaining files
// are dropped and the payload says so.
func attachSourceSnippets(
	context map[string]any,
	root string,
	shownFiles []string,
	changedNodes []graphstore.GraphNode,
	maxLinesPerFile int,
) {
	snippets := map[string]any{}
	perFile := min(maxLinesPerFile, reviewCtxMaxLinesPerFile)
	budget := reviewCtxSourceLineBudget

	for _, relPath := range shownFiles {
		if budget <= 0 {
			context["source_truncated"] = true
			context["truncated"] = true
			break
		}
		fullPath := filepath.Join(root, filepath.FromSlash(relPath))
		info, err := os.Stat(fullPath)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			snippets[relPath] = "(could not read file)"
			continue
		}
		lines := splitSourceLines(string(data))
		allowed := min(perFile, budget)
		if len(lines) > allowed {
			snippets[relPath] = relevantSourceLines(lines, changedNodes, fullPath, allowed)
			budget -= allowed
			continue
		}
		snippets[relPath] = numberedLines(lines, 0, len(lines))
		budget -= len(lines)
	}
	context["source_snippets"] = snippets
}

// replaceInvalidUTF8 is Python's `bytes.decode("utf-8", "replace")`.
//
// It is NOT strings.ToValidUTF8, which emits one U+FFFD per maximal invalid
// RUN. Python applies Unicode's maximal-subpart rule: a byte that cannot
// start a sequence is one replacement, but a truncated-yet-valid prefix
// (`\xf0\x9f` of a four-byte rune) is consumed whole as ONE. The difference
// is observable, because these snippets are rendered into the payload
// verbatim and a latin-1 source file would otherwise come back a different
// length than the release reports.
func replaceInvalidUTF8(text string) string {
	if utf8.ValidString(text) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		// A literal U+FFFD decodes as RuneError with size 3, so only a
		// single-byte RuneError is an actual decoding failure.
		if r, size := utf8.DecodeRuneInString(text[i:]); r != utf8.RuneError || size > 1 {
			b.WriteString(text[i : i+size])
			i += size
			continue
		}
		b.WriteRune(utf8.RuneError)
		i += maximalSubpartLen(text[i:])
	}
	return b.String()
}

// maximalSubpartLen is the length of the ill-formed subsequence at the start
// of text that Python collapses into one U+FFFD: the lead byte, plus the
// continuation bytes that were legal for it, stopping at the first that was
// not.
func maximalSubpartLen(text string) int {
	lead := text[0]
	// Sequence length and the SECOND byte's legal range. The narrowed ranges
	// are what reject an overlong encoding (0xE0, 0xF0), a surrogate (0xED)
	// and a value above U+10FFFF (0xF4); a lead outside the table cannot
	// start anything, so it stands alone.
	var want int
	var lo, hi byte = 0x80, 0xbf
	switch {
	case lead >= 0xc2 && lead <= 0xdf:
		want = 2
	case lead == 0xe0:
		want, lo = 3, 0xa0
	case lead >= 0xe1 && lead <= 0xef:
		want = 3
		if lead == 0xed {
			hi = 0x9f
		}
	case lead == 0xf0:
		want, lo = 4, 0x90
	case lead >= 0xf1 && lead <= 0xf4:
		want = 4
		if lead == 0xf4 {
			hi = 0x8f
		}
	default:
		return 1
	}
	if len(text) < 2 || text[1] < lo || text[1] > hi {
		return 1
	}
	length := 2
	for length < want && length < len(text) && text[length] >= 0x80 && text[length] <= 0xbf {
		length++
	}
	return length
}

// sourceLineBreaks are the boundaries Python's str.splitlines recognises
// beyond "\r\n". Splitting on "\n" alone would mis-number every line of a
// CRLF or NEL-terminated file.
var sourceLineBreaks = []rune{
	'\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029,
}

// splitSourceLines is Python's str.splitlines: line terminators are consumed,
// "\r\n" counts once, and a trailing terminator does not produce a final empty
// line.
func splitSourceLines(text string) []string {
	// Upstream reads with errors="replace", so undecodable bytes become
	// U+FFFD rather than failing the read or shifting the line numbering.
	text = replaceInvalidUTF8(text)
	lines := []string{}
	start := 0
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if !containsRune(sourceLineBreaks, runes[i]) {
			continue
		}
		lines = append(lines, string(runes[start:i]))
		if runes[i] == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
			i++
		}
		start = i + 1
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

func containsRune(set []rune, r rune) bool {
	for _, candidate := range set {
		if candidate == r {
			return true
		}
	}
	return false
}

// numberedLines renders lines[from:to] as "<1-based number>: <text>".
func numberedLines(lines []string, from, to int) string {
	parts := make([]string, 0, to-from)
	for i := from; i < to; i++ {
		parts = append(parts, fmt.Sprintf("%d: %s", i+1, lines[i]))
	}
	return strings.Join(parts, "\n")
}

// relevantSourceLines is review._extract_relevant_lines: the windows around
// the file's changed nodes, merged where they overlap, instead of a blind head
// of the file.
//
// It is bounded by maxLines because a file where every function changed merges
// into one range covering the whole file, which would defeat the caller's
// per-file budget entirely.
func relevantSourceLines(
	lines []string,
	nodes []graphstore.GraphNode,
	filePath string,
	maxLines int,
) string {
	type window struct{ start, end int }
	windows := make([]window, 0, len(nodes))
	for _, node := range nodes {
		if node.FilePath != filePath {
			continue
		}
		windows = append(windows, window{
			start: max(0, node.LineStart-reviewCtxContextBefore),
			end:   min(len(lines), node.LineEnd+reviewCtxContextAfter),
		})
	}
	if len(windows) == 0 {
		// No changed node lands in this file: show its head rather than
		// nothing, so a reviewer still sees what the file is.
		return numberedLines(lines, 0, min(reviewCtxFallbackLines, min(maxLines, len(lines))))
	}

	sort.Slice(windows, func(i, j int) bool {
		if windows[i].start != windows[j].start {
			return windows[i].start < windows[j].start
		}
		return windows[i].end < windows[j].end
	})
	merged := []window{windows[0]}
	for _, next := range windows[1:] {
		last := &merged[len(merged)-1]
		// Adjacent ranges merge too: a one-line gap between two snippets is
		// not worth an elision marker.
		if next.start <= last.end+1 {
			last.end = max(last.end, next.end)
			continue
		}
		merged = append(merged, next)
	}

	parts := []string{}
	emitted := 0
	for _, w := range merged {
		if emitted >= maxLines {
			parts = append(parts, "... (truncated)")
			break
		}
		if len(parts) > 0 {
			parts = append(parts, "...")
		}
		// The bound is fixed BEFORE the loop. Upstream's range() is computed
		// once, so the remaining budget is read at window start; re-reading a
		// shrinking `emitted` every iteration would halve each window.
		last := min(w.end, w.start+maxLines-emitted)
		for i := w.start; i < last; i++ {
			parts = append(parts, fmt.Sprintf("%d: %s", i+1, lines[i]))
			emitted++
		}
	}
	return strings.Join(parts, "\n")
}

// reviewGuidance is review._generate_review_guidance: the handful of
// review-worthy signals the impact analysis can prove, or an explicit
// "well-contained" note when it can prove none.
func reviewGuidance(impact ImpactRadiusResult) string {
	var parts []string

	if untested := untestedChangedFunctions(impact, true); len(untested) > 0 {
		parts = append(parts, fmt.Sprintf(
			"- %d changed function(s) lack test coverage: %s",
			len(untested), strings.Join(nodeNames(untested, 5), ", ")))
	}
	if len(impact.ImpactedNodes) > 20 {
		parts = append(parts, fmt.Sprintf(
			"- Wide blast radius: %d nodes impacted. "+
				"Review callers and dependents carefully.", len(impact.ImpactedNodes)))
	}
	inheritance := 0
	for _, edge := range impact.Edges {
		if edge.Kind == "INHERITS" || edge.Kind == "IMPLEMENTS" {
			inheritance++
		}
	}
	if inheritance > 0 {
		parts = append(parts, fmt.Sprintf(
			"- %d inheritance/implementation relationship(s) affected. "+
				"Check for Liskov substitution violations.", inheritance))
	}
	if len(impact.ImpactedFiles) > 3 {
		parts = append(parts, fmt.Sprintf(
			"- Changes impact %d other files. Consider splitting into smaller PRs.",
			len(impact.ImpactedFiles)))
	}
	if len(parts) == 0 {
		parts = append(parts, "- Changes appear well-contained with minimal blast radius.")
	}
	return strings.Join(parts, "\n")
}

// untestedChangedFunctions returns the changed production functions with no
// TESTED_BY edge inside the radius.
//
// includeTestKinds distinguishes the two callers upstream has: the guidance
// pass looks at every Function node and drops the test ones afterwards, while
// the minimal payload's gap count filters test functions out up front. The
// two agree on the answer but not on the intermediate list, and the guidance
// pass is the one whose count reaches the summary text.
//
// TESTED_BY is stored production-to-test, so a changed production function
// finds its tests by matching the edge SOURCE.
func untestedChangedFunctions(impact ImpactRadiusResult, includeTestKinds bool) []graphstore.GraphNode {
	tested := map[string]bool{}
	for _, edge := range impact.Edges {
		if edge.Kind == "TESTED_BY" {
			tested[edge.SourceQualified] = true
		}
	}
	untested := make([]graphstore.GraphNode, 0, len(impact.ChangedNodes))
	for _, node := range impact.ChangedNodes {
		if node.Kind != "Function" {
			continue
		}
		if !includeTestKinds && node.IsTest {
			continue
		}
		if tested[node.QualifiedName] || node.IsTest {
			continue
		}
		untested = append(untested, node)
	}
	return untested
}
