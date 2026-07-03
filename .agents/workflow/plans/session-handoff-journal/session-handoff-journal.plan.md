# Session-Handoff Journal Plan

- plan-id: `session-handoff-journal`
- spec: [`../../specs/session-handoff-journal/design.md`](../../specs/session-handoff-journal/design.md)
  (ratified 2026-06-22, openQ=0)
- depends on: `config-v2-migration/p4h-agentslock-interprocess-lock` (completed —
  supplies the interprocess advisory lock the appender reuses)
- sequencing: build **after the config-v2 0.4.0 cut** (spec §Sequencing); the lock
  model is settled and `p4h` is merged, so the dependency is satisfied.

## Summary

An append-only event log (the "journal") that makes cross-session state
crash-survivable. Every state-mutating `da` command deterministically appends one
typed event (envelope + `input` + `observed`); a `PreCompact` hook appends a
deterministic live-state snapshot; the agent appends short reasoned deltas at an
adaptive cadence. Recovery **replays and re-verifies** — it never trusts remembered
prose and never injects a stale claim as fact. The journal is the episodic-memory
seam of the knowledge-architecture graph-views direction.

## Reading order

1. The **spec** is the contract — problem, decisions (D1–D13), requirements
   (R1–R9), done criteria, and scope all live there.
2. This plan describes **how** the spec is implemented and **in what order**.
3. `TASKS.yaml` is the live work queue; `PLAN.yaml` is its canonical header.

## Key implementation decisions (the HOW, derived from the spec)

- **Where it lives (D9).** Off the git-tracked tree under
  `config.AgentsStateDir()/journal/<repo-fingerprint>/` — `events.log` (NDJSON,
  one envelope per line), `snapshot.json`, `reasoned.log`. The XDG state dir is
  already the project's convention for non-tracked machine state.
- **File format + durability (D9, R1).** Command/reasoned entries are one JSON
  object per line, appended via `O_APPEND` in a single bounded write held under the
  **p4h interprocess advisory lock** (extracted as `agentslock.AcquireFileLock`).
  Full-file writes (the deterministic snapshot, rare full compaction at boundaries)
  use `fsops.WriteFileAtomic` (temp-then-rename) so a mid-write kill never produces
  false authority.
- **Package layout.** New `internal/journal` owns envelope, identity, append,
  schema registry, snapshot, and the recovery view. Commands call `journal.Emit`;
  the CLI surface is `da workflow journal {append,snapshot,recover,show,prune}`.
- **Journaled set (D3, D4).** Tier-1 unconditional transitions, Tier-2
  changed-fields-only, KG (counts/ids/decisions, not bodies), and review
  approve/reject. **Config is excluded** — `da refresh` → `.agentsrc.lock` is
  redundant with the snapshot's lock re-read (`inputs_digest` + per-layer
  `resolved_sha`). Hook-sentinel/outcome and score are also excluded.
- **Recovery is a verified view (D7, D8, D10).** On `agent-start`/orient, load the
  candidate journal + snapshot + reasoned deltas, run each item's cheap re-verify
  probe, and inject only `verified`/`changed`/`missing` facts plus explicit deltas.
  Candidates are keyed by composite repo identity; mismatches are quarantined;
  trust is the recency of the reasoned write relative to the snapshot timestamp.
- **Re-verify robustness (R7).** Prefer store/service-backed probes (`gh`/remote
  today; `da` over the KG store once KG-as-SoT lands) that survive a locked or
  missing working tree; local `git`/`da`-reading-files is fallback only.
- **Canonical vs in-PR locus (R8).** Every tracked item carries its locus —
  `canonical:{ref}` vs `in_open_pr:{pr,status}` — so recovery never conflates
  in-PR work with done-on-master or fresh-eligible.

## Open questions

None. The spec is ratified with openQ=0; the decisions above are mechanical
derivations of D1–D13 / R1–R9 onto the existing codebase (XDG state dir,
`agentslock`, `fsops`, the command runners, the starter hooks/skills).

## Task graph (depends-on)

```
config-v2-migration/p4h ─▶ p0 ─▶ p1 ─▶ p2 ─┬─▶ p3a ─┐
                                            ├─▶ p3b ─┤
                                            ├─▶ p4 ──▶ p5 ─▶ p6 ─┬─▶ p7 ─┐
                                            └────────────────────┘       ├─▶ p9
                                                                  p6 ────▶ p8 ┘
```

Write side (p3a/p3b) is R9's non-optional must-build piece and can proceed in
parallel with the snapshot/recovery libs (p4/p5). The command surface (p6) gates
the hooks (p7) and the skill readback (p8); p9 is the cross-task closeout smoke.

## Verification

Each task carries Go unit tests beside its implementation; concurrency is proven by
a parallel-`Emit` test through the extracted lock. `p9` (`tests/test-session-handoff-journal.sh`)
ties the five spec Done criteria into one end-to-end smoke. Run focused packages
first (`go test ./internal/journal/... ./internal/agentslock/... ./commands/...`),
then `go test ./...` and `./scripts/verify.sh`.
