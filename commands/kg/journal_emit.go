package kg

import (
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

// journal_emit.go wires the session-handoff journal (internal/journal) into the
// state-mutating `da kg` runners — the sibling of commands/workflow's p3a wiring.
// Each journaled KG command (the registry in internal/journal/schema.go) appends
// one typed event after its mutation lands, so a session killed mid-flight can be
// reconstructed from durable file state instead of a re-grounding burst (spec R1).
//
// KG events record COUNTS / IDS / DECISION OUTCOMES, never note/edge bodies
// (spec D4): the content-delta commands carry per-change counts plus the affected
// note/link ids, and the decision commands carry an outcome plus optional graph
// counts. The typed schemas (KGContentDeltaObserved / KGDecisionObserved /
// KGIngestObserved) hold no body field, so a body cannot be journaled here.
//
// Emission is BEST-EFFORT and NON-FATAL, exactly as in p3a: the command's real
// work has already succeeded by the time we journal, so a journal error is
// surfaced as a warning and swallowed — it must never turn a successful mutation
// into a failed command. A blank repoPath skips emission entirely.

// journalEmit is the append seam over journal.Emit, overridable in tests so they
// can capture the typed envelope (or inject an append failure) without touching
// the real per-repo state dir.
var journalEmit = journal.Emit

// journalActor reports the agent role stamped on KG journal events. The CLI has
// no runtime role probe yet, so every KG command records ActorMain (mirrors the
// p3a workflow wiring); a future actor probe lands in exactly one place.
func journalActor() journal.Actor { return journal.ActorMain }

// emitKGEvent runs build (NewEvent / NewFailedEvent) and appends the result
// best-effort. A build or append error is warned, never returned. repoPath==""
// — the repo could not be resolved, so there is no journal to write to — is a
// no-op.
func emitKGEvent(repoPath, command string, build func() (journal.Envelope, error)) {
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

// emitKGSuccess appends a success event for a journaled command from its typed
// input + observed payloads (observed may be nil to omit it).
func emitKGSuccess(repoPath, command string, input, observed any) {
	emitKGEvent(repoPath, command, func() (journal.Envelope, error) {
		return journal.NewEvent(command, journalActor(), input, observed)
	})
}

// emitKGFailure appends a failed event (input only — observed is dropped per R1)
// for a command whose mutation did not complete.
func emitKGFailure(repoPath, command string, input any) {
	emitKGEvent(repoPath, command, func() (journal.Envelope, error) {
		return journal.NewFailedEvent(command, journalActor(), input)
	})
}

// journalKG is the deferred tail a KG runner registers: on success it records
// the durable delta (counts / ids / decision outcome — never bodies, D4); on
// failure (the mutation did not complete) it records an input-only failed event.
// ok is flipped true by the runner only once every state mutation has landed, so
// a rendering/output error after the mutation still records success (the state
// DID change) rather than a false failure — mirroring p3a's journalTier1.
func journalKG(repoPath, command string, input, observed any, ok bool) {
	if ok {
		emitKGSuccess(repoPath, command, input, observed)
		return
	}
	emitKGFailure(repoPath, command, input)
}
