package crgrelease

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// PydanticVersion is the validator version whose diagnostics the pinned
// release surfaces to MCP clients. It appears verbatim in every validation
// error's documentation URL, so reproducing the release's error semantics
// means reproducing this too.
const PydanticVersion = "2.13"

// msgInvalidString is the validator's diagnostic for a non-string value where
// a string is required. Clients match the wording verbatim, so every site
// that rejects a non-string — a bare string parameter, an explicit null on
// one, or a list element — must report exactly this text.
const msgInvalidString = "Input should be a valid string"

// ToolError is a tools/call failure expressed the way the pinned release
// expresses it: the MCP result is NOT a JSON-RPC error, it is a successful
// response carrying `isError: true` and one text block holding the message.
type ToolError struct {
	Message string
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// UnknownToolError is the pinned release's answer to a tools/call naming a
// tool it does not publish.
func UnknownToolError(name string) *ToolError {
	return &ToolError{Message: fmt.Sprintf("Unknown tool: %s", pyRepr(name))}
}

// validationIssue is one field-level validation failure.
type validationIssue struct {
	// loc is the error location: a parameter name, or `name.index` for a
	// rejected list element.
	loc      string
	message  string
	kind     string
	value    any
	valueSet bool
}

// Args is a validated, defaulted argument set for one tool call.
//
// Binding applies the release's semantics in the release's order: unknown
// arguments are rejected, missing required arguments are reported, supplied
// values are coerced under the validator's lax rules, and every omitted
// parameter takes its published default. Handlers therefore never re-implement
// a default or re-check a type.
type Args struct {
	tool   string
	values map[string]any
}

// Tool returns the tool these arguments were bound for.
func (a Args) Tool() string { return a.tool }

// Raw returns the fully defaulted argument map. It is the payload forwarded to
// the retained Python bridge when a call is routed there, so a bridge-routed
// call sees exactly the values a native handler would.
func (a Args) Raw() map[string]any {
	out := make(map[string]any, len(a.values))
	for key, value := range a.values {
		out[key] = value
	}
	return out
}

// String returns a string parameter's effective value. An optional parameter
// left unset returns "".
func (a Args) String(name string) string {
	value, _ := a.values[name].(string)
	return value
}

// OptString returns a string parameter's value and whether it was set to a
// non-null value. Tools that branch on "argument supplied at all" (a flow
// selected by id versus by name, an explicit diff base versus an auto-resolved
// one) need the second result.
func (a Args) OptString(name string) (string, bool) {
	value, ok := a.values[name].(string)
	return value, ok
}

// Int returns an integer parameter's effective value.
func (a Args) Int(name string) int {
	value, _ := a.values[name].(int64)
	return int(value)
}

// OptInt returns an integer parameter's value and whether it was set.
func (a Args) OptInt(name string) (int, bool) {
	value, ok := a.values[name].(int64)
	return int(value), ok
}

// Bool returns a boolean parameter's effective value.
func (a Args) Bool(name string) bool {
	value, _ := a.values[name].(bool)
	return value
}

// OptBool returns a boolean parameter's value and whether it was set. The
// tri-state `recurse_submodules` parameter, whose unset case defers to an
// environment variable, is why this exists.
func (a Args) OptBool(name string) (bool, bool) {
	value, ok := a.values[name].(bool)
	return value, ok
}

// StringSlice returns a list parameter's value and whether it was set. An
// unset list is not an empty list: for the change-oriented tools an unset
// `changed_files` means "auto-detect from git", while an empty list means
// "nothing changed".
func (a Args) StringSlice(name string) ([]string, bool) {
	value, ok := a.values[name].([]string)
	return value, ok
}

// Bind validates and defaults one tool call's arguments.
//
// raw may be nil or empty, which the release treats as "no arguments".
func (t Tool) Bind(raw json.RawMessage) (Args, *ToolError) {
	supplied, order, err := decodeArguments(raw)
	if err != nil {
		// A non-object arguments payload never reaches a field validator;
		// the release reports it as a single top-level failure.
		return Args{}, &ToolError{Message: renderIssues(t.Name, []validationIssue{{
			loc:     "arguments",
			message: "Input should be a valid dictionary",
			kind:    "dict_type",
		}})}
	}

	args := Args{tool: t.Name, values: make(map[string]any, len(t.Params))}
	issues, fatal := t.bindDeclared(&args, supplied, order)
	if fatal != nil {
		return Args{}, fatal
	}
	issues = append(issues, t.unknownArgumentIssues(supplied, order)...)
	if len(issues) > 0 {
		return Args{}, &ToolError{Message: renderIssues(t.Name, issues)}
	}
	return args, nil
}

// bindDeclared validates every parameter the tool declares, writing accepted
// values into args. Declared parameters are walked in declaration order —
// that is the order the release's validator reports field errors in. The
// second result is non-nil only for a contract bug (a published default that
// violates its own schema), which aborts binding outright.
func (t Tool) bindDeclared(
	args *Args, supplied map[string]json.RawMessage, order []string,
) ([]validationIssue, *ToolError) {
	required := make(map[string]bool, len(t.Required))
	for _, name := range t.Required {
		required[name] = true
	}
	argsObject := newArgsObject(supplied, order)

	var issues []validationIssue
	for _, name := range t.order {
		param := t.Params[name]
		value, present := supplied[name]
		if !present {
			issue, fatal := t.bindOmitted(args, name, param, required[name], argsObject)
			if fatal != nil {
				return nil, fatal
			}
			if issue != nil {
				issues = append(issues, *issue)
			}
			continue
		}
		if issue := bindSupplied(args, name, param, value); issue != nil {
			issues = append(issues, *issue)
		}
	}
	return issues, nil
}

// newArgsObject rebuilds the whole arguments object in the order the client
// sent it, which is the reported input for a missing-argument failure.
func newArgsObject(supplied map[string]json.RawMessage, order []string) orderedMap {
	argsObject := orderedMap{keys: order, values: map[string]any{}}
	for _, name := range order {
		argsObject.values[name] = decodeAny(supplied[name])
	}
	return argsObject
}

// bindOmitted applies the release's semantics for a declared parameter the
// client did not send: a required one is reported missing, and anything else
// takes its published default when it has a non-null one.
func (t Tool) bindOmitted(
	args *Args, name string, param Param, required bool, argsObject orderedMap,
) (*validationIssue, *ToolError) {
	if required {
		return &validationIssue{
			loc:      name,
			message:  "Missing required argument",
			kind:     "missing_argument",
			value:    argsObject,
			valueSet: true,
		}, nil
	}
	if !param.HasDefault || param.Default == nil {
		return nil, nil
	}
	coerced, issue := coerce(param, param.Default)
	if issue != nil {
		// A default that does not satisfy its own schema is a contract bug,
		// not a client error.
		return nil, &ToolError{Message: fmt.Sprintf(
			"crgrelease: %s default for %s violates its schema", t.Name, name)}
	}
	args.values[name] = coerced
	return nil, nil
}

// bindSupplied validates one argument the client did send, writing the
// coerced value into args when the validator accepts it.
func bindSupplied(args *Args, name string, param Param, value json.RawMessage) *validationIssue {
	decoded := decodeAny(value)
	if decoded == nil {
		if param.Optional {
			// Explicit null on an optional parameter is the release's way
			// of saying "unset"; leave the value absent.
			return nil
		}
		issue := nullIssue(name, param)
		return &issue
	}
	coerced, issue := coerce(param, decoded)
	if issue != nil {
		issue.loc = name + issue.loc
		return issue
	}
	args.values[name] = coerced
	return nil
}

// unknownArgumentIssues reports arguments the tool does not declare, in the
// order they were sent. The schema closes the argument object, so an
// unrecognised key is a hard failure rather than an ignored extra; the
// release emits these issues after every declared-parameter failure.
func (t Tool) unknownArgumentIssues(
	supplied map[string]json.RawMessage, order []string,
) []validationIssue {
	var issues []validationIssue
	for _, name := range order {
		if _, known := t.Params[name]; known {
			continue
		}
		issues = append(issues, validationIssue{
			loc:      name,
			message:  "Unexpected keyword argument",
			kind:     "unexpected_keyword_argument",
			value:    decodeAny(supplied[name]),
			valueSet: true,
		})
	}
	return issues
}

// orderedMap preserves a JSON object's key order. The release's diagnostics
// echo the whole arguments object for a missing-argument failure, and Python
// dicts render in insertion order, so the echo is only reproducible if the
// client's key order survives decoding.
type orderedMap struct {
	keys   []string
	values map[string]any
}

// decodeArguments decodes a tools/call arguments object, preserving key
// order. A nil, empty or null payload is the release's "no arguments".
func decodeArguments(raw json.RawMessage) (map[string]json.RawMessage, []string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]json.RawMessage{}, nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	token, err := decoder.Token()
	if err != nil {
		return nil, nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, nil, fmt.Errorf("crgrelease: arguments is not an object")
	}
	values := map[string]json.RawMessage{}
	var order []string
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, nil, fmt.Errorf("crgrelease: non-string argument key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, err
		}
		if _, seen := values[key]; !seen {
			order = append(order, key)
		}
		values[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, nil, err
	}
	return values, order, nil
}

// nullIssue is the failure for an explicit null on a non-optional parameter.
func nullIssue(name string, param Param) validationIssue {
	issue := validationIssue{loc: name, value: nil, valueSet: true}
	switch primaryType(param) {
	case "integer":
		issue.message, issue.kind = "Input should be a valid integer", "int_type"
	case "number":
		issue.message, issue.kind = "Input should be a valid number", "float_type"
	case "boolean":
		issue.message, issue.kind = "Input should be a valid boolean", "bool_type"
	case "array":
		issue.message, issue.kind = "Input should be a valid list", "list_type"
	default:
		issue.message, issue.kind = msgInvalidString, "string_type"
	}
	return issue
}

func primaryType(param Param) string {
	if len(param.Types) == 0 {
		return "string"
	}
	return param.Types[0]
}

// coerce applies the release validator's lax coercion rules for one parameter.
//
// The rules are not JSON-Schema's: a numeric string becomes an integer, a
// bool counts as an integer, an integral float becomes an integer, but a
// fractional float and a non-string value for a string parameter are
// rejected. Each rejection carries the validator's own error kind because
// clients surface those strings.
func coerce(param Param, value any) (any, *validationIssue) {
	switch primaryType(param) {
	case "string":
		text, ok := value.(string)
		if !ok {
			return nil, &validationIssue{
				message:  msgInvalidString,
				kind:     "string_type",
				value:    value,
				valueSet: true,
			}
		}
		return text, nil
	case "integer":
		return coerceInt(value)
	case "number":
		switch typed := value.(type) {
		case float64:
			return typed, nil
		case int64:
			return float64(typed), nil
		case bool:
			if typed {
				return float64(1), nil
			}
			return float64(0), nil
		case string:
			parsed, err := strconv.ParseFloat(typed, 64)
			if err != nil {
				return nil, &validationIssue{
					message:  "Input should be a valid number, unable to parse string as a number",
					kind:     "float_parsing",
					value:    value,
					valueSet: true,
				}
			}
			return parsed, nil
		}
		return nil, &validationIssue{
			message:  "Input should be a valid number",
			kind:     "float_type",
			value:    value,
			valueSet: true,
		}
	case "boolean":
		return coerceBool(value)
	case "array":
		items, ok := value.([]any)
		if !ok {
			return nil, &validationIssue{
				message:  "Input should be a valid list",
				kind:     "list_type",
				value:    value,
				valueSet: true,
			}
		}
		out := make([]string, 0, len(items))
		for index, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, &validationIssue{
					loc:      "." + strconv.Itoa(index),
					message:  msgInvalidString,
					kind:     "string_type",
					value:    item,
					valueSet: true,
				}
			}
			out = append(out, text)
		}
		return out, nil
	}
	return value, nil
}

func coerceInt(value any) (any, *validationIssue) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case float64:
		if typed != math.Trunc(typed) {
			return nil, &validationIssue{
				message:  "Input should be a valid integer, got a number with a fractional part",
				kind:     "int_from_float",
				value:    value,
				valueSet: true,
			}
		}
		return int64(typed), nil
	case bool:
		if typed {
			return int64(1), nil
		}
		return int64(0), nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return nil, &validationIssue{
				message:  "Input should be a valid integer, unable to parse string as an integer",
				kind:     "int_parsing",
				value:    value,
				valueSet: true,
			}
		}
		return parsed, nil
	}
	return nil, &validationIssue{
		message:  "Input should be a valid integer",
		kind:     "int_type",
		value:    value,
		valueSet: true,
	}
}

func coerceBool(value any) (any, *validationIssue) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case int64:
		switch typed {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
	case float64:
		switch typed {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "t", "yes", "y", "on", "1":
			return true, nil
		case "false", "f", "no", "n", "off", "0":
			return false, nil
		}
		return nil, &validationIssue{
			message:  "Input should be a valid boolean, unable to interpret input",
			kind:     "bool_parsing",
			value:    value,
			valueSet: true,
		}
	}
	return nil, &validationIssue{
		message:  "Input should be a valid boolean",
		kind:     "bool_type",
		value:    value,
		valueSet: true,
	}
}

// renderIssues formats validation failures exactly as the pinned release does:
// a count header naming the call, then per-issue location, indented message
// with the machine-readable annotation, and the validator's documentation URL.
func renderIssues(tool string, issues []validationIssue) string {
	var b strings.Builder
	noun := "validation errors"
	if len(issues) == 1 {
		noun = "validation error"
	}
	fmt.Fprintf(&b, "%d %s for call[%s]", len(issues), noun, tool)
	for _, issue := range issues {
		fmt.Fprintf(&b, "\n%s\n  %s [type=%s", issue.loc, issue.message, issue.kind)
		if issue.valueSet {
			fmt.Fprintf(&b, ", input_value=%s, input_type=%s", pyRepr(issue.value), pyType(issue.value))
		}
		fmt.Fprintf(&b, "]\n    For further information visit https://errors.pydantic.dev/%s/v/%s",
			PydanticVersion, issue.kind)
	}
	return b.String()
}

// decodeAny decodes a JSON value into the validator's value domain, keeping
// integers distinct from floats — `limit: 5` and `limit: 5.0` produce the same
// accepted value but `limit: 5.5` must be rejected, so the distinction cannot
// be flattened to float64.
func decodeAny(raw json.RawMessage) any {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	return normalizeNumbers(value)
}

func normalizeNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := strconv.ParseInt(typed.String(), 10, 64); err == nil {
			return integer
		}
		if float, err := strconv.ParseFloat(typed.String(), 64); err == nil {
			return float
		}
		return typed.String()
	case []any:
		for index, item := range typed {
			typed[index] = normalizeNumbers(item)
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeNumbers(item)
		}
		return typed
	}
	return value
}

// pyRepr renders a value the way the validator's diagnostics do, because the
// rendering is part of the error string clients see.
func pyRepr(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case string:
		return "'" + strings.ReplaceAll(typed, "'", "\\'") + "'"
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case []any:
		return pyReprAnySlice(typed)
	case []string:
		return pyReprStringSlice(typed)
	case orderedMap:
		return pyReprOrderedMap(typed)
	case map[string]any:
		return pyReprMap(typed)
	}
	return fmt.Sprintf("%v", value)
}

// pyReprAnySlice renders a decoded JSON array as a Python list.
func pyReprAnySlice(items []any) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, pyRepr(item))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// pyReprStringSlice renders a coerced list parameter's value as a Python
// list. It is a distinct case because coercion has already narrowed the
// element type, so the value no longer travels as []any.
func pyReprStringSlice(items []string) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, pyRepr(item))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// pyReprOrderedMap renders a dict in the client's own key order, which is how
// Python renders the arguments object a missing-argument failure echoes.
func pyReprOrderedMap(value orderedMap) string {
	if len(value.keys) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(value.keys))
	for _, key := range value.keys {
		parts = append(parts, pyRepr(key)+": "+pyRepr(value.values[key]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// pyReprMap renders a dict whose key order was not recorded. Go map iteration
// is randomised, so the keys are sorted to keep the diagnostic deterministic.
func pyReprMap(value map[string]any) string {
	if len(value) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, pyRepr(key)+": "+pyRepr(value[key]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// pyType renders the validator's `input_type` annotation.
func pyType(value any) string {
	switch value.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case int64:
		return "int"
	case float64:
		return "float"
	case []any, []string:
		return "list"
	case map[string]any, orderedMap:
		return "dict"
	}
	return "object"
}
