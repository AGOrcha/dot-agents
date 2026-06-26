// Module for the KG-as-SOT WorkStore lease/conflict prototype.
//
// Self-contained on purpose: it carries its OWN go.mod so the repo root
// `go test ./...` does NOT recurse into it. This is a proof-of-concept that
// validates the work-tracking-storage-abstraction spec's open questions
// OQ1 (atomic claim/lease) and OQ2 (per-field conflict resolution), and
// replays the wave-engine 5xp1c re-dispatch storm as a regression.
module proto/work-store-lease

go 1.26
