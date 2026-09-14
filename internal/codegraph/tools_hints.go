package codegraph

import (
	"fmt"
	"sync"
)

// The release attaches a `_hints` block to a subset of its tool responses:
// which tool to call next, related identifiers, and warnings extracted from
// the payload. The block is SESSION-STATEFUL — a next-step suggestion is
// dropped once that tool has been called in the connection — so it cannot be
// reproduced by a static table. A replacement that emitted fixed hints would
// match every single-call fixture and still be wrong for a real client, which
// is why the session is modelled here and pinned by the
// session-sequence fixture.

// hintsMaxPerCategory bounds each hint list.
const hintsMaxPerCategory = 3

// hintsMaxToolsHistory / hintsMaxNodesTracked bound the session's memory, so
// a long-lived connection cannot grow without limit.
const (
	hintsMaxToolsHistory = 100
	hintsMaxNodesTracked = 1000
)

// hintSuggestion is one next-step entry.
type hintSuggestion struct {
	Tool       string `json:"tool"`
	Suggestion string `json:"suggestion"`
}

// hintWorkflow is the release's tool-adjacency table. Keys are the release's
// HINT names, which omit the `_tool` suffix the MCP tool names carry.
var hintWorkflow = map[string][]hintSuggestion{
	"list_flows": {
		{Tool: "get_flow", Suggestion: "Drill into a specific flow for step-by-step details"},
		{Tool: "get_affected_flows", Suggestion: "Check which flows are affected by recent changes"},
		{Tool: "get_architecture_overview", Suggestion: "See the high-level architecture"},
	},
	"get_flow": {
		{Tool: "query_graph", Suggestion: "Inspect callers/callees of a step in this flow"},
		{Tool: "get_affected_flows", Suggestion: "Check if changes affect this flow"},
		{Tool: "list_flows", Suggestion: "Browse other execution flows"},
	},
	"get_affected_flows": {
		{Tool: "detect_changes", Suggestion: "Get risk-scored change analysis"},
		{Tool: "get_flow", Suggestion: "Inspect a specific affected flow"},
		{Tool: "get_review_context", Suggestion: "Build a full review context for the changes"},
	},
	"list_communities": {
		{Tool: "get_community", Suggestion: "Inspect a specific community's members"},
		{Tool: "get_architecture_overview", Suggestion: "See cross-community coupling and warnings"},
		{Tool: "list_flows", Suggestion: "See execution flows across communities"},
	},
	"get_community": {
		{Tool: "query_graph", Suggestion: "Explore callers/callees of community members"},
		{Tool: "list_communities", Suggestion: "Browse other communities"},
		{Tool: "get_architecture_overview", Suggestion: "See how this community fits the architecture"},
	},
	"get_architecture_overview": {
		{Tool: "list_communities", Suggestion: "Drill into individual communities"},
		{Tool: "detect_changes", Suggestion: "See how recent changes affect the architecture"},
		{Tool: "list_flows", Suggestion: "Explore execution flows"},
	},
	"detect_changes": {
		{Tool: "get_review_context", Suggestion: "Build a full review context with source snippets"},
		{Tool: "get_affected_flows", Suggestion: "See which execution flows are affected"},
		{Tool: "get_impact_radius", Suggestion: "Expand the blast radius analysis"},
		{Tool: "refactor", Suggestion: "Look for refactoring opportunities in changed code"},
	},
	"refactor": {
		{Tool: "query_graph", Suggestion: "Verify call sites before applying a rename"},
		{Tool: "detect_changes", Suggestion: "Check risk of the refactored code"},
		{Tool: "semantic_search_nodes", Suggestion: "Find related symbols to also rename"},
	},
	"semantic_search_nodes": {
		{Tool: "query_graph", Suggestion: "Inspect callers/callees of a search result"},
		{Tool: "get_flow", Suggestion: "See the execution flow through a matched node"},
		{Tool: "get_impact_radius", Suggestion: "Check the blast radius from matched nodes"},
	},
}

// hintIntentTools classifies a session's recent calls into an intent.
var hintIntentTools = map[string]map[string]bool{
	"reviewing": {
		"detect_changes": true, "get_review_context": true,
		"get_affected_flows": true, "get_impact_radius": true,
	},
	"debugging": {
		"query_graph": true, "get_flow": true, "semantic_search_nodes": true,
	},
	"refactoring": {
		"refactor": true, "find_dead_code": true, "suggest_refactorings": true,
	},
	"exploring": {
		"list_communities": true, "get_architecture_overview": true,
		"list_flows": true, "list_graph_stats": true,
	},
}

// hintIntentOrder makes the "highest score wins" tie-break deterministic. The
// release relies on Python's dict insertion order here, so the declaration
// order of _INTENT_TOOLS is part of the observable behaviour.
var hintIntentOrder = []string{"reviewing", "debugging", "refactoring", "exploring"}

// SessionState is one MCP connection's in-memory history.
//
// It is per-connection, not per-process: two concurrent clients must not see
// each other's suppressed suggestions.
type SessionState struct {
	mu           sync.Mutex
	toolsCalled  []string
	nodesQueried map[string]bool
	filesTouched map[string]bool
	intent       string
}

// NewSessionState returns an empty session.
func NewSessionState() *SessionState {
	return &SessionState{
		nodesQueried: map[string]bool{},
		filesTouched: map[string]bool{},
	}
}

// Session returns this engine's hint session, creating it on first use.
func (e *Engine) Session() *SessionState {
	e.sessionOnce.Do(func() { e.session = NewSessionState() })
	return e.session
}

// ResetSession clears the hint session. `da kg serve` builds a fresh engine
// per connection; this exists so a test can assert the session-dependent
// suppression from a known starting point.
func (e *Engine) ResetSession() {
	e.sessionOnce.Do(func() {
		// Intentionally empty: this only burns the once so the Session()
		// accessor stops constructing a session, letting the assignment
		// below install the replacement without a later Do overwriting it.
	})
	e.session = NewSessionState()
}

func (s *SessionState) recordToolCall(name string) {
	s.toolsCalled = append(s.toolsCalled, name)
	if len(s.toolsCalled) > hintsMaxToolsHistory {
		s.toolsCalled = s.toolsCalled[len(s.toolsCalled)-hintsMaxToolsHistory:]
	}
}

func (s *SessionState) recordNodes(ids []string) {
	for _, id := range ids {
		if len(s.nodesQueried) >= hintsMaxNodesTracked {
			return
		}
		s.nodesQueried[id] = true
	}
}

func (s *SessionState) recordFiles(paths []string) {
	for _, path := range paths {
		s.filesTouched[path] = true
	}
}

// inferIntent classifies the last ten calls.
func (s *SessionState) inferIntent() string {
	if len(s.toolsCalled) == 0 {
		return "exploring"
	}
	recent := s.toolsCalled
	if len(recent) > 10 {
		recent = recent[len(recent)-10:]
	}
	scores := map[string]int{}
	for _, tool := range recent {
		for intent, tools := range hintIntentTools {
			if tools[tool] {
				scores[intent]++
			}
		}
	}
	best, bestScore := "exploring", 0
	for _, intent := range hintIntentOrder {
		if scores[intent] > bestScore {
			best, bestScore = intent, scores[intent]
		}
	}
	if bestScore == 0 {
		return "exploring"
	}
	return best
}

// AttachHints adds the release's `_hints` block to a result.
//
// hintName is the release's hint name for the tool (no `_tool` suffix).
// Handlers call this at exactly the point the release calls generate_hints —
// which is deliberately AFTER the early-return paths, so a not-found result
// carries no hints, exactly as the release's does.
func (e *Engine) AttachHints(hintName string, result map[string]any) map[string]any {
	if result == nil {
		return result
	}
	result["_hints"] = e.Session().generateHints(hintName, result)
	return result
}

// generateHints builds the block and advances the session.
func (s *SessionState) generateHints(hintName string, result map[string]any) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.recordToolCall(hintName)
	s.intent = s.inferIntent()

	nextSteps := s.buildNextSteps(hintName)
	warnings := extractHintWarnings(result)
	// `related` is built BEFORE the result is tracked, so this call's own
	// files can still be suggested.
	related := s.buildRelated(result)
	s.trackResult(result)

	return map[string]any{
		"next_steps": capSuggestions(nextSteps),
		"related":    capStrings(related),
		"warnings":   capStrings(warnings),
	}
}

// buildNextSteps drops suggestions for tools already called in the session —
// including the current one, because it was just recorded.
func (s *SessionState) buildNextSteps(hintName string) []hintSuggestion {
	called := make(map[string]bool, len(s.toolsCalled))
	for _, tool := range s.toolsCalled {
		called[tool] = true
	}
	out := make([]hintSuggestion, 0, len(hintWorkflow[hintName]))
	for _, candidate := range hintWorkflow[hintName] {
		if !called[candidate.Tool] {
			out = append(out, candidate)
		}
	}
	return out
}

// extractHintWarnings pulls warning signals out of a payload.
func extractHintWarnings(result map[string]any) []string {
	var warnings []string

	if gaps, ok := hintTestGapWarning(result); ok {
		warnings = append(warnings, gaps)
	}

	if risk, ok := toFloat(result["risk_score"]); ok && risk > 0.7 {
		warnings = append(warnings, fmt.Sprintf("High risk score (%.2f) — review carefully", risk))
	}

	warnings = append(warnings, hintPassthroughWarnings(result)...)
	return warnings
}

// hintTestGapWarning summarises a payload's `test_gaps`, naming at most the
// first five and falling back to the raw rendering of an entry that carries
// no `name`. The bool reports whether the payload listed any gap at all.
func hintTestGapWarning(result map[string]any) (string, bool) {
	gaps, ok := result["test_gaps"].([]any)
	if !ok || len(gaps) == 0 {
		return "", false
	}
	names := make([]string, 0, len(gaps))
	for i, gap := range gaps {
		if i >= 5 {
			break
		}
		if entry, ok := gap.(map[string]any); ok {
			if name, ok := entry["name"].(string); ok {
				names = append(names, name)
				continue
			}
		}
		names = append(names, fmt.Sprintf("%v", gap))
	}
	return "Test coverage gaps: " + joinComma(names), true
}

// hintPassthroughWarnings forwards at most the first three warnings the
// payload already carries, accepting both a plain string and a map with a
// `message` field.
func hintPassthroughWarnings(result map[string]any) []string {
	existing, ok := result["warnings"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for i, item := range existing {
		if i >= 3 {
			break
		}
		switch typed := item.(type) {
		case string:
			out = append(out, typed)
		case map[string]any:
			if message, ok := typed["message"].(string); ok {
				out = append(out, message)
			}
		}
	}
	return out
}

// buildRelated suggests impacted files the session has not touched yet.
func (s *SessionState) buildRelated(result map[string]any) []string {
	impacted, ok := result["impacted_files"].([]any)
	if !ok {
		return nil
	}
	var related []string
	seen := map[string]bool{}
	for _, item := range impacted {
		path, ok := item.(string)
		if !ok || s.filesTouched[path] || seen[path] {
			continue
		}
		related = append(related, path)
		seen[path] = true
		if len(related) >= hintsMaxPerCategory {
			break
		}
	}
	return related
}

// trackResult records the files and nodes this result exposed.
func (s *SessionState) trackResult(result map[string]any) {
	for _, key := range []string{"changed_files", "impacted_files"} {
		s.recordFiles(hintStringItems(result, key))
	}
	var nodeIDs []string
	for _, key := range []string{"results", "changed_nodes", "impacted_nodes"} {
		nodeIDs = append(nodeIDs, hintQualifiedNames(result, key)...)
	}
	s.recordNodes(nodeIDs)
}

// hintStringItems returns the string entries the payload lists under key,
// skipping any entry that is not a string.
func hintStringItems(result map[string]any, key string) []string {
	items, ok := result[key].([]any)
	if !ok {
		return nil
	}
	paths := make([]string, 0, len(items))
	for _, item := range items {
		if path, ok := item.(string); ok {
			paths = append(paths, path)
		}
	}
	return paths
}

// hintQualifiedNames returns the non-empty `qualified_name` of every map
// entry the payload lists under key.
func hintQualifiedNames(result map[string]any, key string) []string {
	items, ok := result[key].([]any)
	if !ok {
		return nil
	}
	var names []string
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if qualified, ok := entry["qualified_name"].(string); ok && qualified != "" {
			names = append(names, qualified)
		}
	}
	return names
}

// capSuggestions/capStrings bound a hint list and keep it a JSON array rather
// than null, which is what the release emits for an empty category.
func capSuggestions(items []hintSuggestion) []hintSuggestion {
	if len(items) > hintsMaxPerCategory {
		items = items[:hintsMaxPerCategory]
	}
	if items == nil {
		return []hintSuggestion{}
	}
	return items
}

func capStrings(items []string) []string {
	if len(items) > hintsMaxPerCategory {
		items = items[:hintsMaxPerCategory]
	}
	if items == nil {
		return []string{}
	}
	return items
}

func joinComma(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ", "
		}
		out += item
	}
	return out
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	}
	return 0, false
}
