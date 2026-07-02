// params.go parses and validates the query-parameter surface pinned by
// API.md §1.4 (pagination) and §3.1 (run list filters). Any invalid value is
// a §1.3 bad_request — the store is deliberately forgiving, so the 400s live
// here.
package handlers

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/dashboard/store"
)

// Pagination bounds (API.md §1.4).
const (
	defaultLimit = 50
	maxLimit     = 500
)

// paramError describes one invalid query parameter.
type paramError struct {
	name string
	want string
}

// Error renders the human-readable §1.3 message.
func (e *paramError) Error() string {
	return fmt.Sprintf("invalid %q: must be %s", e.name, e.want)
}

// intParam parses an optional integer query param, enforcing [min, max];
// want is the human-readable constraint for the error message.
func intParam(q url.Values, name string, def, min, max int, want string) (int, error) {
	raw := q.Get(name)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < min || v > max {
		return 0, &paramError{name: name, want: want}
	}
	return v, nil
}

// enumParam validates an optional enumerated query param against allowed.
func enumParam(q url.Values, name, def string, allowed []string) (string, error) {
	raw := q.Get(name)
	if raw == "" {
		return def, nil
	}
	if slices.Contains(allowed, raw) {
		return raw, nil
	}
	return "", &paramError{name: name, want: "one of " + strings.Join(allowed, "|")}
}

// parsePage parses the §1.4 limit/offset pair shared by the list endpoints:
// limit defaults to 50 and must be 1–500 (limit=0 is rejected), offset
// defaults to 0 and must be >= 0.
func parsePage(q url.Values) (limit, offset int, err error) {
	limit, err = intParam(q, "limit", defaultLimit, 1, maxLimit, "an integer between 1 and 500")
	if err != nil {
		return 0, 0, err
	}
	offset, err = intParam(q, "offset", 0, 0, math.MaxInt, "an integer >= 0")
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

// parseRunFilter validates the §3.1 run-list query params into the store
// filter. Unknown params are ignored; harness is a free-form exact match.
func parseRunFilter(q url.Values) (store.RunFilter, error) {
	var f store.RunFilter
	var err error
	if f.Limit, f.Offset, err = parsePage(q); err != nil {
		return store.RunFilter{}, err
	}
	if f.Sort, err = enumParam(q, "sort", "last_update",
		[]string{"last_update", "score", "iteration_count", "session_id"}); err != nil {
		return store.RunFilter{}, err
	}
	if f.Order, err = enumParam(q, "order", "desc", []string{"asc", "desc"}); err != nil {
		return store.RunFilter{}, err
	}
	if f.Band, err = enumParam(q, "band", "",
		[]string{"excellent", "good", "fair", "poor", "unscored"}); err != nil {
		return store.RunFilter{}, err
	}
	f.Harness = q.Get("harness")
	return f, nil
}

// pageOf slices the ascending iteration list per the validated limit/offset.
// An out-of-range offset yields an empty page with status 200 (§1.4), never
// an error — and never a JSON null.
func pageOf(its []store.IterationSummary, limit, offset int) []store.IterationSummary {
	if offset >= len(its) {
		return []store.IterationSummary{}
	}
	end := offset + limit
	if end > len(its) {
		end = len(its)
	}
	return its[offset:end]
}
