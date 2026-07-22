package observability

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// canonicalJSON implements the RFC 8785 JSON Canonicalization Scheme used by
// the ingest contract. JSON numbers are interpreted as IEEE-754 doubles, object
// keys use UTF-16 code-unit ordering, and strings are emitted without HTML
// escaping.
func canonicalJSON(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("JSON is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := appendCanonical(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func appendCanonical(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		appendCanonicalString(out, v)
	case json.Number:
		number, err := canonicalNumber(v)
		if err != nil {
			return err
		}
		out.WriteString(number)
	case []any:
		return appendCanonicalArray(out, v)
	case map[string]any:
		return appendCanonicalObject(out, v)
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

func appendCanonicalArray(out *bytes.Buffer, values []any) error {
	out.WriteByte('[')
	for i, item := range values {
		if i > 0 {
			out.WriteByte(',')
		}
		if err := appendCanonical(out, item); err != nil {
			return err
		}
	}
	out.WriteByte(']')
	return nil
}

func appendCanonicalObject(out *bytes.Buffer, values map[string]any) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
	out.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			out.WriteByte(',')
		}
		appendCanonicalString(out, key)
		out.WriteByte(':')
		if err := appendCanonical(out, values[key]); err != nil {
			return err
		}
	}
	out.WriteByte('}')
	return nil
}

func appendCanonicalString(out *bytes.Buffer, value string) {
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

func canonicalNumber(number json.Number) (string, error) {
	value, err := strconv.ParseFloat(string(number), 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return "", fmt.Errorf("number %q is not an RFC 8785 finite IEEE-754 value", number)
	}
	if value == 0 {
		return "0", nil
	}
	absolute := math.Abs(value)
	if absolute >= 1e-6 && absolute < 1e21 {
		return strconv.FormatFloat(value, 'f', -1, 64), nil
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	marker := bytes.LastIndexByte([]byte(scientific), 'e')
	mantissa, exponent := scientific[:marker], scientific[marker+1:]
	sign := ""
	if exponent[0] == '+' || exponent[0] == '-' {
		sign, exponent = exponent[:1], exponent[1:]
	}
	for len(exponent) > 1 && exponent[0] == '0' {
		exponent = exponent[1:]
	}
	if sign == "" {
		sign = "+"
	}
	return mantissa + "e" + sign + exponent, nil
}

func utf16Less(left, right string) bool {
	a, b := utf16.Encode([]rune(left)), utf16.Encode([]rune(right))
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func computeEventHash(event ingestEvent) (string, error) {
	material := struct {
		Kind         string          `json:"kind"`
		OccurredAt   string          `json:"occurred_at"`
		PlanID       string          `json:"plan_id"`
		TaskID       string          `json:"task_id"`
		Iteration    int             `json:"iteration"`
		Payload      json.RawMessage `json:"payload"`
		ScoreSidecar json.RawMessage `json:"score_sidecar"`
	}{
		Kind:         event.Kind,
		OccurredAt:   event.OccurredAt,
		PlanID:       event.PlanID,
		TaskID:       event.TaskID,
		Iteration:    event.Iteration,
		Payload:      event.Payload,
		ScoreSidecar: event.ScoreSidecar,
	}
	raw, err := json.Marshal(material)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
