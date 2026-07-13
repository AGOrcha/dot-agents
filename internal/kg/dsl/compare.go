package dsl

// compare evaluates a binary comparison between two resolved values under the
// closed §5.1 operator set. Numbers compare numerically (ints and float64
// interoperate, since JSON decodes all numbers to float64); strings and bools
// compare for (in)equality only. A type mismatch yields false rather than an
// error — the predicate simply does not hold.
func compare(op string, left, right any) bool {
	if lf, rf, ok := asFloats(left, right); ok {
		return compareNumbers(op, lf, rf)
	}
	return compareNonNumeric(op, left, right)
}

// compareNumbers applies an operator to two float64 operands.
func compareNumbers(op string, l, r float64) bool {
	switch op {
	case "=":
		return l == r
	case "!=":
		return l != r
	case "<":
		return l < r
	case "<=":
		return l <= r
	case ">":
		return l > r
	case ">=":
		return l >= r
	default:
		return false
	}
}

// compareNonNumeric handles equality on strings/bools. It is reached only after
// compare() has ruled out a both-numeric pair, so equality is plain Go `==` on
// the (non-numeric) operands; ordering operators on non-numbers do not hold.
func compareNonNumeric(op string, left, right any) bool {
	switch op {
	case "=":
		return left == right
	case "!=":
		return left != right
	default:
		return false
	}
}

// asFloats coerces two values to float64 when both are numeric, reporting ok.
func asFloats(left, right any) (float64, float64, bool) {
	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	return lf, rf, lok && rok
}

// toFloat converts an int/float64 to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

// inList reports whether left appears in the right-hand list param (§5.1 IN).
// The list is a []string or []any of ids.
func inList(left, right any) bool {
	switch list := right.(type) {
	case []string:
		for _, s := range list {
			if s == left {
				return true
			}
		}
	case []any:
		for _, v := range list {
			if v == left {
				return true
			}
		}
	}
	return false
}
