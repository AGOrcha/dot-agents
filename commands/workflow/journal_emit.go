package workflow

import (
	"fmt"
	"sort"

	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

// journal_emit.go wires the session-handoff journal (internal/journal) into the
// state-mutating `da workflow` runners. Each journaled workflow command (the
// registry in internal/journal/schema.go) appends one typed event after its
// state mutation completes, so a session killed mid-flight can be reconstructed
// from durable file state instead of a re-grounding burst (spec R1).
//
// Emission is BEST-EFFORT and NON-FATAL: the command's real work has already
// succeeded by the time we journal, so a journal error is surfaced as a warning
// and swallowed — it must never turn a successful mutation into a failed command.
// A blank repoPath (the project could not be resolved) skips emission entirely.

// journalEmit is the append seam over journal.Emit, overridable in tests so they
// can capture the typed envelope (or inject an append failure) without touching
// the real per-repo state dir.
var journalEmit = journal.Emit

// journalActor reports the agent role stamped on workflow journal events. The CLI
// has no runtime role probe yet (loop-worker / orchestrator are process concepts
// it cannot currently observe), so every workflow command records ActorMain.
// Centralized here so a future actor probe lands in exactly one place.
func journalActor() journal.Actor { return journal.ActorMain }

// emitWorkflowEvent runs build (NewEvent / NewFailedEvent) and appends the result
// best-effort. A build or append error is warned, never returned. repoPath=="" —
// the project was not resolved, so there is no journal to write to — is a no-op.
func emitWorkflowEvent(repoPath, command string, build func() (journal.Envelope, error)) {
	if repoPath == "" {
		return
	}
	ev, err := build()
	if err == nil {
		err = journalEmit(repoPath, ev)
	}
	if err != nil {
		ui.Warn(fmt.Sprintf("journal: %s: %v", command, err))
	}
}

// emitWorkflowSuccess appends a success event for a journaled command from its
// typed input + observed payloads (observed may be nil to omit it).
func emitWorkflowSuccess(repoPath, command string, input, observed any) {
	emitWorkflowEvent(repoPath, command, func() (journal.Envelope, error) {
		return journal.NewEvent(command, journalActor(), input, observed)
	})
}

// emitWorkflowFailure appends a failed event (input only — observed is dropped
// per R1) for a command whose state mutation did not complete.
func emitWorkflowFailure(repoPath, command string, input any) {
	emitWorkflowEvent(repoPath, command, func() (journal.Envelope, error) {
		return journal.NewFailedEvent(command, journalActor(), input)
	})
}

// journalTier1 is the deferred tail a Tier-1 runner registers: on success it
// records the durable delta, on failure (the mutation did not complete) it
// records an input-only failed event. ok is flipped true by the runner only once
// every state mutation has landed, so an output/encoding error after the mutation
// still records success (the state DID change) rather than a false failure.
func journalTier1(repoPath, command string, input, observed any, ok bool) {
	if ok {
		emitWorkflowSuccess(repoPath, command, input, observed)
		return
	}
	emitWorkflowFailure(repoPath, command, input)
}

// emitWorkflowDelta appends a Tier-2 delta event recording only the fields that
// actually changed (changed: field name → new value). A no-op (nothing changed)
// journals nothing — Tier-2 records the delta, never an empty mutation (spec R6).
// plan/task scope the record; pass "" when not applicable (e.g. prefs).
func emitWorkflowDelta(repoPath, command, plan, task string, changed map[string]string) {
	if repoPath == "" || len(changed) == 0 {
		return
	}
	cf, err := journal.NewChangedFields(changed)
	if err != nil {
		ui.Warn(fmt.Sprintf("journal: %s: %v", command, err))
		return
	}
	emitWorkflowSuccess(repoPath, command,
		&journal.DeltaInput{Plan: plan, Task: task, ChangedFields: cf},
		&journal.DeltaObserved{FieldsReplaced: sortedKeys(changed)})
}

// sortedKeys returns the map keys in deterministic order so fields_replaced is
// stable across runs.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
