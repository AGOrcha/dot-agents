package workflow

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// blocked-on:<ref> parameterized task state (design.md §3.4).
//
// A task paused on an external blocker carries the literal status
// `blocked-on:<ref>`, where `<ref>` is part of the state — not a separate
// field. The four supported ref kinds and their auto-resume predicates are
// defined in §3.4 / §3.4.5:
//
//   - task:<plan>/<task>  — wait for a named task to reach `completed`
//   - secret:<NAME>       — wait for a GH/repo secret to exist
//   - decision:<id>       — wait for a maintainer decision to land
//   - condition:<pred>    — generic predicate via a pluggable evaluator
//
// This file owns the parse/format of the state string, the predicate
// evaluator registry, and the §3.4.4 eligibility-decay annotation. Network
// access (gh secret list) is reached only through the BlockerEnv seam so
// tests never touch the network.

// blockedOnPrefix is the status prefix that marks a parameterized
// blocked-on:<ref> task state.
const blockedOnPrefix = "blocked-on:"

// Blocker ref kinds (the token before the first ':' inside <ref>).
const (
	blockerKindTask      = "task"
	blockerKindSecret    = "secret"
	blockerKindDecision  = "decision"
	blockerKindCondition = "condition"
)

// defaultBlockerStaleDays is the §3.4.4 decay threshold: a task blocked for
// longer than this without its predicate evaluating true is surfaced with a
// blocker_stale_since annotation. Projects may override via .agentsrc.json.
const defaultBlockerStaleDays = 7

// BlockerRef is the parsed form of the `<ref>` portion of a
// `blocked-on:<ref>` status. Kind is one of the blockerKind* constants and
// Arg is the remainder after the first ':' (e.g. for
// `blocked-on:task:p1/t2` → Kind="task", Arg="p1/t2").
type BlockerRef struct {
	Kind string
	Arg  string
}

// String renders the ref back to its canonical `<kind>:<arg>` form. It is the
// inverse of parseBlockerRef and is stable (round-trips).
func (r BlockerRef) String() string {
	return r.Kind + ":" + r.Arg
}

// formatBlockedOnStatus builds the persistable status string for a ref. It is
// the inverse of ParseBlockedOnStatus.
func formatBlockedOnStatus(ref BlockerRef) string {
	return blockedOnPrefix + ref.String()
}

// IsBlockedOnStatus reports whether a persisted status string is a
// parameterized blocked-on:<ref> state.
func IsBlockedOnStatus(status string) bool {
	return strings.HasPrefix(status, blockedOnPrefix)
}

// validBlockerKinds is the recognized set of ref kinds, used for hinting and
// validation.
var validBlockerKinds = map[string]bool{
	blockerKindTask:      true,
	blockerKindSecret:    true,
	blockerKindDecision:  true,
	blockerKindCondition: true,
}

// blockerKindList renders the recognized kinds in a stable order for error
// hints.
func blockerKindList() string {
	order := []string{blockerKindTask, blockerKindSecret, blockerKindDecision, blockerKindCondition}
	quoted := make([]string, len(order))
	for i, k := range order {
		quoted[i] = "`" + k + "`"
	}
	return strings.Join(quoted, ", ")
}

// parseBlockerRef parses the `<ref>` body (already stripped of the
// blocked-on: prefix) into a BlockerRef. It rejects an empty ref, a missing
// kind/arg separator, an unknown kind, or an empty arg.
func parseBlockerRef(ref string) (BlockerRef, error) {
	if ref == "" {
		return BlockerRef{}, fmt.Errorf("empty blocker ref: expected `<kind>:<arg>` with kind one of %s", blockerKindList())
	}
	kind, arg, found := strings.Cut(ref, ":")
	if !found || arg == "" {
		return BlockerRef{}, fmt.Errorf("malformed blocker ref %q: expected `<kind>:<arg>` (kind one of %s)", ref, blockerKindList())
	}
	if !validBlockerKinds[kind] {
		return BlockerRef{}, fmt.Errorf("unknown blocker kind %q: valid kinds are %s", kind, blockerKindList())
	}
	return BlockerRef{Kind: kind, Arg: arg}, nil
}

// ParseBlockedOnStatus parses a full persisted status string
// (`blocked-on:<kind>:<arg>`) into its BlockerRef. It returns an error when
// the status lacks the blocked-on: prefix or the ref body is malformed.
func ParseBlockedOnStatus(status string) (BlockerRef, error) {
	if !IsBlockedOnStatus(status) {
		return BlockerRef{}, fmt.Errorf("status %q is not a blocked-on state (missing %q prefix)", status, blockedOnPrefix)
	}
	return parseBlockerRef(strings.TrimPrefix(status, blockedOnPrefix))
}

// BlockerEnv is the seam through which predicate evaluators reach external
// state (GitHub, the canonical task store, resolved-decision records). The
// concrete production implementation shells out to `gh` and reads canonical
// YAML; tests inject a fake so no network or filesystem access is required.
type BlockerEnv interface {
	// SecretExists reports whether a repo/org secret with the given name is
	// present (production: `gh secret list`).
	SecretExists(name string) (bool, error)
	// TaskStatus returns the persisted status of plan/task (production: read
	// the canonical TASKS.yaml). The bool is false when the task is unknown.
	TaskStatus(plan, task string) (string, bool)
	// DecisionResolved reports whether maintainer decision id has landed
	// (production: a resolved-decision record exists).
	DecisionResolved(id string) (bool, error)
	// EvalCondition evaluates a registered named condition predicate. The
	// bool reports the predicate result; an error is returned for an
	// unregistered predicate or an evaluator failure.
	EvalCondition(predicate string) (bool, error)
}

// blockerPredicate evaluates whether a single blocked-on ref's blocker has
// resolved. Each kind maps to one predicate; the registry below makes the set
// pluggable per project.
type blockerPredicate func(env BlockerEnv, arg string) (bool, error)

// blockerPredicates is the pluggable registry mapping each ref kind to its
// auto-resume predicate (§3.4.5). RegisterBlockerPredicate lets a project add
// or override a kind without touching this file.
var blockerPredicates = map[string]blockerPredicate{
	blockerKindTask:      predicateTaskCompleted,
	blockerKindSecret:    predicateSecretExists,
	blockerKindDecision:  predicateDecisionResolved,
	blockerKindCondition: predicateCondition,
}

// RegisterBlockerPredicate installs or replaces the evaluator for a ref kind,
// keeping the registry pluggable per §3.4.5. It also marks the kind valid so
// parse accepts refs of that kind. A nil predicate or empty kind is rejected.
func RegisterBlockerPredicate(kind string, pred blockerPredicate) error {
	if kind == "" {
		return fmt.Errorf("cannot register blocker predicate: empty kind")
	}
	if pred == nil {
		return fmt.Errorf("cannot register blocker predicate for %q: nil predicate", kind)
	}
	blockerPredicates[kind] = pred
	validBlockerKinds[kind] = true
	return nil
}

// predicateTaskCompleted resolves a `task:<plan>/<task>` ref: the named task
// must have reached `completed`. An unknown task is treated as not-yet-true
// (not an error) so the blocked task simply keeps waiting.
func predicateTaskCompleted(env BlockerEnv, arg string) (bool, error) {
	plan, task, found := strings.Cut(arg, "/")
	if !found || plan == "" || task == "" {
		return false, fmt.Errorf("malformed task ref %q: expected `<plan>/<task>`", arg)
	}
	status, ok := env.TaskStatus(plan, task)
	if !ok {
		return false, nil
	}
	return status == TaskStatusCompleted, nil
}

// predicateSecretExists resolves a `secret:<NAME>` ref via the env seam
// (production: `gh secret list`).
func predicateSecretExists(env BlockerEnv, arg string) (bool, error) {
	return env.SecretExists(arg)
}

// predicateDecisionResolved resolves a `decision:<id>` ref: an explicit
// maintainer decision must have landed.
func predicateDecisionResolved(env BlockerEnv, arg string) (bool, error) {
	return env.DecisionResolved(arg)
}

// predicateCondition resolves a `condition:<predicate>` ref through the
// pluggable condition evaluator on the env seam.
func predicateCondition(env BlockerEnv, arg string) (bool, error) {
	return env.EvalCondition(arg)
}

// EvaluateBlocker reports whether the blocker named by status has resolved
// (its auto-resume predicate is true). A status that is not a blocked-on
// state, a malformed ref, or a kind with no registered predicate returns an
// error; the caller leaves the task untouched on error.
func EvaluateBlocker(env BlockerEnv, status string) (bool, error) {
	ref, err := ParseBlockedOnStatus(status)
	if err != nil {
		return false, err
	}
	pred, ok := blockerPredicates[ref.Kind]
	if !ok {
		return false, fmt.Errorf("no predicate registered for blocker kind %q", ref.Kind)
	}
	return pred(env, ref.Arg)
}

// resolveTarget is the status a resumed task should enter when its blocker
// clears. The §3.4.5 implicit default is in_progress; the original block call
// may override it via --resume-as (in_progress or pending per §3.4.2).
const (
	resumeAsDefault    = TaskStatusInProgress
	resumeAsAlternate  = TaskStatusPending
	resumeAsFlagInProg = TaskStatusInProgress
	resumeAsFlagPend   = TaskStatusPending
)

// validResumeTargets is the set of statuses a --resume-as override may name
// (§3.4.2 exit edges to in_progress or pending).
var validResumeTargets = map[string]bool{
	resumeAsFlagInProg: true,
	resumeAsFlagPend:   true,
}

// NormalizeResumeAs validates an optional --resume-as override and returns the
// status a resumed task should enter. An empty override yields the implicit
// default (in_progress, §3.4.5). An override outside {in_progress, pending} is
// rejected.
func NormalizeResumeAs(override string) (string, error) {
	if override == "" {
		return resumeAsDefault, nil
	}
	if !validResumeTargets[override] {
		return "", fmt.Errorf(
			"invalid --resume-as %q: blocked tasks resume to %q (default) or %q",
			override, resumeAsDefault, resumeAsAlternate,
		)
	}
	return override, nil
}

// BlockerDecay carries the §3.4.4 staleness verdict for a blocked task.
type BlockerDecay struct {
	// Stale reports whether the task has been blocked past the threshold.
	Stale bool
	// Since is the RFC3339 timestamp the task entered the blocked state,
	// echoed back for the blocker_stale_since annotation. Empty when Stale
	// is false.
	Since string
	// Age is how long the task has been blocked, rounded to whole seconds.
	Age time.Duration
}

// blockerStaleThresholdDays returns the decay threshold in days, honoring a
// per-project override when positive and falling back to the §3.4.4 default of
// 7. A zero or negative override is ignored so a missing/blank config value
// does not silently disable decay.
func blockerStaleThresholdDays(configuredDays int) int {
	if configuredDays > 0 {
		return configuredDays
	}
	return defaultBlockerStaleDays
}

// EvaluateBlockerDecay computes the §3.4.4 eligibility-decay verdict for a task
// that entered its blocked state at blockedSince (RFC3339). configuredDays is
// the per-project threshold from .agentsrc.json (<=0 means "use default").
// now is injected for deterministic tests. A blank or unparseable blockedSince
// yields a non-stale verdict (we cannot prove staleness without an entry time).
func EvaluateBlockerDecay(blockedSince string, configuredDays int, now time.Time) (BlockerDecay, error) {
	if blockedSince == "" {
		return BlockerDecay{}, nil
	}
	entered, err := time.Parse(time.RFC3339, blockedSince)
	if err != nil {
		return BlockerDecay{}, fmt.Errorf("invalid blocked-since timestamp %q: %w", blockedSince, err)
	}
	age := now.Sub(entered).Round(time.Second)
	threshold := time.Duration(blockerStaleThresholdDays(configuredDays)) * 24 * time.Hour
	if age < threshold {
		return BlockerDecay{Stale: false, Age: age}, nil
	}
	return BlockerDecay{Stale: true, Since: blockedSince, Age: age}, nil
}

// blockerStaleAnnotation renders the `blocker_stale_since=<ts>` annotation
// surfaced in `da workflow eligible` output (§3.4.4) for a stale verdict.
// It returns an empty string for a non-stale verdict so callers can append it
// unconditionally.
func blockerStaleAnnotation(d BlockerDecay) string {
	if !d.Stale {
		return ""
	}
	return "blocker_stale_since=" + d.Since
}

// registeredBlockerKinds returns the recognized ref kinds in sorted order, for
// diagnostic and help output. It reflects any kinds added via
// RegisterBlockerPredicate.
func registeredBlockerKinds() []string {
	out := make([]string, 0, len(validBlockerKinds))
	for k := range validBlockerKinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
