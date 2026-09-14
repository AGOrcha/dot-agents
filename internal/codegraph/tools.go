package codegraph

import (
	"fmt"
	"sort"
	"time"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ToolHandler answers one tool of the pinned code-review-graph release.
//
// Args is already validated and defaulted against the release's published
// schema by internal/crgrelease, so a handler never re-checks a type, applies
// a default, or invents a bound: it reads the values it needs and produces the
// release's response payload.
type ToolHandler func(e *Engine, args crgrelease.Args) (any, error)

// toolHandlers is the native half of the release surface. A tool absent here
// is routed to the retained bridge by the MCP server, which is why the map is
// the single place that decides what "implemented natively" means.
var toolHandlers = map[string]ToolHandler{}

// RegisterTool wires a native handler. It panics on a duplicate or on a name
// the pinned release does not publish, so a typo fails at startup rather than
// silently leaving a tool on the bridge forever.
func RegisterTool(name string, handler ToolHandler) {
	if _, ok := crgrelease.Lookup(name); !ok {
		panic(fmt.Sprintf(
			"codegraph: %q is not published by code-review-graph %s", name, crgrelease.Version))
	}
	if _, exists := toolHandlers[name]; exists {
		panic(fmt.Sprintf("codegraph: duplicate native handler for %q", name))
	}
	toolHandlers[name] = handler
}

// NativeToolNames lists the tools served in-process, sorted.
func NativeToolNames() []string {
	names := make([]string, 0, len(toolHandlers))
	for name := range toolHandlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CallTool runs one release tool natively.
//
// It returns graphstore.ErrToolNotNative for a tool with no native handler,
// which asks the MCP server to route the call to the retained bridge rather
// than answer it approximately.
func (e *Engine) CallTool(name string, args crgrelease.Args) (any, error) {
	handler, ok := toolHandlers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", graphstore.ErrToolNotNative, name)
	}
	result, err := handler(e, args)
	if err != nil {
		return nil, err
	}
	return e.withProvenance(result), nil
}

// SourcesFullyNative reports whether the repository's sources are entirely
// within the native scanner's extraction coverage, naming the languages that
// are not when they are. The MCP server routes every tool call for a
// repository that is not fully covered to the bridge, because a native answer
// would be computed from a graph that is missing those files.
func (e *Engine) SourcesFullyNative() (bool, []string, error) {
	report, err := ScanCapability(e.root)
	if err != nil {
		return false, nil, err
	}
	var nonNative []string
	for _, language := range report.Languages {
		if !language.Native && language.Files > 0 {
			nonNative = append(nonNative, language.Language)
		}
	}
	return report.FullyNative, nonNative, nil
}

// ── shared response helpers ──────────────────────────────────────────────────
//
// Every tool of the release shares a small response vocabulary: the `_graph`
// provenance envelope, the error shape, and the bounding contract that makes
// a truncated list honest. They live here so all handlers spell them the same
// way — a handler that invented its own `total` semantics would be a silent
// contract divergence.

// withProvenance attaches the release's `_graph` build-metadata envelope to a
// dict-shaped result that does not already carry one.
//
// The envelope is deliberately best-effort: a missing, empty or unreadable
// graph must never fail the enclosing call, because these fields exist to tell
// a caller the answer may be stale, not to gate the answer.
func (e *Engine) withProvenance(result any) any {
	payload, ok := result.(map[string]any)
	if !ok {
		return result
	}
	if _, exists := payload["_graph"]; exists {
		return result
	}
	if provenance := e.graphProvenance(); len(provenance) > 0 {
		payload["_graph"] = provenance
	}
	return payload
}

// graphProvenance reads the three build-metadata rows the release reports.
//
// `head_matches_build` compares COMMITS only. It deliberately does not claim
// that staged, unstaged or untracked files are represented by the graph —
// asserting that would be a misleading freshness signal.
func (e *Engine) graphProvenance() map[string]any {
	store, err := e.readStore()
	if err != nil || store == nil {
		return nil
	}
	provenance := map[string]any{}
	if updatedAt, err := store.GetMetadata("last_updated"); err == nil && updatedAt != "" {
		provenance["updated_at"] = updatedAt
		if builtAt, perr := parseGraphTimestamp(updatedAt); perr == nil {
			age := int(e.now().Sub(builtAt).Seconds())
			if age < 0 {
				age = 0
			}
			provenance["age_seconds"] = age
		}
	}
	headSHA, err := store.GetMetadata("git_head_sha")
	if err != nil || headSHA == "" {
		// Without a build commit the remaining fields are meaningless; the
		// release omits the whole envelope in that case.
		if len(provenance) == 0 {
			return nil
		}
		return provenance
	}
	provenance["built_at_sha"] = headSHA
	if branch, err := store.GetMetadata("git_branch"); err == nil && branch != "" {
		provenance["built_on_branch"] = branch
	}
	if live := headCommit(e.root); live != "" {
		provenance["head_sha"] = live
		provenance["head_matches_build"] = live == headSHA
	}
	return provenance
}

// parseGraphTimestamp accepts the naive local timestamp the release writes
// (`2006-01-02T15:04:05`) as well as an RFC3339 value.
func parseGraphTimestamp(value string) (time.Time, error) {
	if parsed, err := time.ParseInLocation("2006-01-02T15:04:05", value, time.Local); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

// ErrorResponse is the release's standard error payload. It is a successful
// tool result with an `error` field, not a transport failure, so a caller sees
// the reason instead of a bare RPC error.
func ErrorResponse(message string, extra map[string]any) map[string]any {
	payload := map[string]any{
		"status":  "error",
		"error":   message,
		"summary": message,
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

// Bounded caps items at min(maxResults, hardCap) and reports the UNTRUNCATED
// total plus whether the list was cut.
//
// Reporting the real total is the contract: a caller that sees ten results and
// no total cannot tell ten from ten-thousand, which is exactly the failure the
// release's bounding rules exist to prevent.
func Bounded[T any](items []T, maxResults, hardCap int) (visible []T, total int, truncated bool) {
	total = len(items)
	limit := maxResults
	if hardCap < limit {
		limit = hardCap
	}
	if limit < 0 {
		limit = 0
	}
	if total <= limit {
		return items, total, false
	}
	return items[:limit], total, true
}

// ShownOf renders the release's ", showing N of M" summary fragment, which is
// present only when the list was actually cut.
func ShownOf(shown, total int) string {
	if shown < total {
		return fmt.Sprintf(", showing %d of %d", shown, total)
	}
	return ""
}

// ValidatePositiveInt mirrors the release's bound check, including its
// explicit rejection of a boolean — `True` would otherwise silently mean 1.
func ValidatePositiveInt(value int, name string) error {
	if value < 1 {
		return fmt.Errorf("%s must be an integer greater than or equal to 1", name)
	}
	return nil
}
