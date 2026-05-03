# TypeScript Port — Sync Pipeline & Boundary Maintenance

**Status:** active
**Created:** 2026-05-03
**Owner:** dot-agents
**Spec/contract source:** `docs/TYPESCRIPT_PORT_BOUNDARY.md` (Phase 4 decision: option 2). This plan does NOT redo the boundary decision; it operationalizes the keep-in-sync mechanism so future Go changes do not silently drift the TS port out of contract.
**Related:**
- `docs/TYPESCRIPT_PORT_TDD_PLAN.md` — overall port strategy and MVP list
- `docs/TYPESCRIPT_PORT_BOUNDARY.md` — Phase 4 boundary decision
- `ports/typescript/README.md` — user-facing port intent
- `ports/typescript/tests/boundary.test.ts` — existing help-text snapshot lock

---

## 1. Why this plan exists

The TS port serves users where the Go binary cannot run: restricted
machines without Go toolchain access, Windows-first workflows, and
sandboxed environments where Node.js 20+ is acceptable but Go is not.
The Phase 4 decision (option 2) committed to:

- Stage 1 commands (`init`, `add`, `refresh`, `status`, `doctor`,
  `skills`, `agents`, `hooks`) — must mirror Go.
- Optional read-only workflow surfaces — allowed in TS without
  duplicating graph stores or write paths.
- Permanently Go-only — all KG, mutating workflow, and orchestration
  commands.

Today the TS port file-level matches this contract. But there is **no
sync pipeline** keeping the TS port aligned as Go evolves. Without a
mechanism, every Go-side change creates a silent drift opportunity —
and the
[crg-kg-integration phase G regression in self-review](.agents/history/crg-kg-integration/)
showed that "we'll remember to update both" doesn't survive across
sessions.

## 2. Plan-level contract (hard test + common false positive)

> **Hard test:** A Go CLI change that adds, removes, or renames a
> top-level command, or modifies a Stage 1 command's flag surface in a
> way the TS port must mirror, fails CI with a clear diff against the
> machine-readable boundary spec — until either the TS port is updated
> or the boundary spec explicitly opts out of mirroring. The current
> gap is captured in `docs/TYPESCRIPT_PORT_GAP.md`. `node ports/typescript/dist/cli.js
> --help` round-trips to the documented boundary surface with no
> surprises.
>
> **Common false positive:** the CI check passes because the boundary
> spec is so loose that any Go change satisfies it. The check must
> distinguish "covered by boundary opt-out (Go-only by design)" from
> "drift the TS port should mirror but doesn't yet."

## 3. Methodology lens — applied at plan level

### 3.1 Four-question lens (annimaniac)

| Question | Today | Post-plan |
|---|---|---|
| What can AI **see**? | Boundary in prose only; no machine-readable form; no programmatic comparison against Go | machine-readable JSON spec + CI check that surfaces drift |
| What can AI **do**? | TS port shipped manually; drift caught only by human eyeballs (and rarely) | drift fails CI; humans nudged to either update TS or opt the new Go surface into the boundary explicitly |
| Who can **extend**? | Anyone — but no contract enforces parity | unchanged; contract enforces parity automatically |
| How has the **org** changed? | TS port is a sidecar that decays silently | TS port becomes a tracked, contract-bound mirror; restricted-machine users get explicit guarantees |

### 3.2 Resource graduation matrix view

This plan moves the **TS-port-as-skill** row of `agent-context-resolution-architecture.md`
§1.5 by one tier — from "ad-hoc maintained sidecar" to "contract-bound
mirror with automatic drift detection." It's the second managed-compounding
demo in the repo (after self-review-iteration-close-wiring).

### 3.3 Architecture note connection

Today this plan does NOT need an ADR. The Phase 4 decision already
exists in `docs/TYPESCRIPT_PORT_BOUNDARY.md`. The work here is
implementation of the existing decision, not a new decision. If the
sync pipeline reveals a class of changes that need a policy answer
(e.g. "should bug-fix-only Go changes be auto-mirrored?"), that
question becomes a new ADR — drafted in flight rather than upfront.

## 4. Task graph

```
tp1-current-gap-audit
     │
     ├──────► tp2-boundary-spec
     │              │
     │              └──► tp3-sync-pipeline
     │                          │
     └──────────────────────────└──► tp4-close-mirror-gaps
```

`tp4` may be a no-op if `tp1` finds no must-mirror gaps. If gaps exceed
3 commands or 5 flag-level changes, escalate via fold-back to a
follow-up plan rather than ballooning this one.

## 5. ADRs produced

None directly. Phase 4 boundary lives in
`docs/TYPESCRIPT_PORT_BOUNDARY.md` already; this plan operationalizes
that decision rather than making new ones. If `tp3` surfaces a policy
question (e.g. "auto-mirror bug-fix-only Go changes?"), an ADR is
drafted in flight.

## 6. Out of scope

- **Plugin spec listing in `status`** (Stage 2 deferred per Phase 5 of
  the boundary doc). If `tp1` audit identifies new Stage 2 surfaces,
  they are NAMED in the gap memo but not implemented here.
- **Read-only workflow library surfaces** beyond what
  `ports/typescript/src/commands/workflow.ts` already exposes. The
  Phase 4 decision *allows* such surfaces but does not require them.
- **Publishing as standalone npm package** — Phase 6 concern, not
  triggered by this plan.
- **Renaming the binary** (the user is exploring `da` as the binary
  name). The sync pipeline's spec uses canonical command names; if the
  binary rename lands, the spec is updated alongside, but this plan
  does not block on that.

## 7. CI integration assumptions

The CI check assumes:

- `.github/workflows/` is the canonical CI surface in this repo
  (verified — existing `auto-release.yml` confirms).
- Node.js 20+ is available in CI runners (already true for existing
  TS port tests).
- The Go binary is buildable from the repo (already true for existing
  Go tests).

If the parser-fallback approach in `tp3` proves fragile (subcommand
trees, flag aliases, hidden commands), a small follow-up Go-side task
adds `dot-agents __dump-tree` (debug-only, JSON output of the cobra
tree) — that is NOT this plan's scope but is a known escape hatch.

## 8. Closeout signals

- `dot-agents workflow plan archive --plan typescript-port` (or the
  equivalent manual archive).
- `docs/TYPESCRIPT_PORT_GAP.md` exists and is current.
- `docs/typescript-port-boundary.json` exists and parses.
- CI workflow `.github/workflows/typescript-port.yml` is active and
  green on this branch.
- `tp4` either ships closure of identified gaps OR a "no gaps to close"
  merge-back if `tp1` found alignment.

---

## 9. ISP execution notes

This plan is structured for ISP. Each task in `TASKS.yaml` declares an
explicit `ISP routing` block with `mode`, `verifier`, `review`,
`anti-scope`, `bundle-context`, and `output contract`.

### Suggested ISP turn sequence

```
turn 1 → tp1-current-gap-audit       (direct; produces docs/TYPESCRIPT_PORT_GAP.md)
turn 2 → tp2-boundary-spec           (direct; produces JSON spec + cross-reference)
turn 3 → tp3-sync-pipeline           (fanout-amenable; CI tooling work)
turn 4 → tp4-close-mirror-gaps       (fanout-amenable IF gaps found; no-op otherwise)
```

### Pre-flight requirements

- The four-fold-back sweep + kg-command-surface-readiness archive
  (completed 2026-05-03) means orientation is clean.
- The TS port build (`npm ci && npm run build` from `ports/typescript/`)
  must succeed at start. If it doesn't, tp1 cannot reliably read the TS
  command surface. Verify before fanning tp1.

### Evidence sidecars consulted by every task

- `docs/TYPESCRIPT_PORT_BOUNDARY.md` — canonical Phase 4 decision.
- `ports/typescript/README.md` — user-facing intent and install path.
- `ports/typescript/tests/boundary.test.ts` — existing help-text snapshot.
- `commands/*.go` — Go side command tree.
- `ports/typescript/src/commands/*.ts` — TS side command tree.
