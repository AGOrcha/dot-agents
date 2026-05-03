# Binary Rename — `dot-agents` → `da` (UV-style abbreviation)

**Status:** active
**Created:** 2026-05-03
**Owner:** dot-agents
**Spec/contract source:** ad-hoc analysis from the 2026-05-03 prep session (rename-scope categorization in `loop-state.md` Next Iteration Playbook side-note + the user's WIP diff on `.goreleaser.yaml`, `.github/workflows/auto-release.yml`, `.agentsrc.json`).
**Related ADRs (produced by this plan):** ADR-0006, ADR-0007 (conditional)
**Foundation ADR:** [ADR-0001](../../../../docs/adr/0001-adopt-architecture-decision-records.md)
**Peer plans:** `self-review-iteration-close-wiring`, `typescript-port` — write scopes do not overlap; can run in parallel.

---

## 1. Why this plan exists

The user has begun renaming the binary from `dot-agents` to `da` (UV-style:
short, fast to type, signals "different tool than the project name"). Three
files in the WIP have it underway: `.goreleaser.yaml` ships `binary: da`,
`.github/workflows/auto-release.yml` builds and smoke-tests `./bin/da`, and
`.agentsrc.json` was modified.

But the rename is partial. ~150–200 files across the repo plus the user's
`~/.agents/` and `~/.claude/CLAUDE.md` reference `dot-agents <command>` in
documentation, plans, specs, skills, and tests. Without a structured sweep,
the rename will leave stale references that confuse users and mislead
agents — exactly the resource-graduation drift pattern the architecture
note's §6.5 audit identified.

This plan is **isolated for the most part**: write scopes are docs, build
configs, and the binary entrypoint. It does not touch the Go source for
behavior (`commands/`, `internal/`, `ports/typescript/src/`). Concurrent
ISP execution alongside `self-review-iteration-close-wiring` is safe;
the only narrow overlap is the architecture note (this plan's t3 sweeps
binary refs in §4 / §6.5; the self-review plan's t4 cross-references
§1.6 of the same file). Coordinate via task ordering, not via
write-scope conflicts.

## 2. Plan-level contract (hard test + common false positive)

> **Hard test:** After t6 completes, every user-facing reference to the
> binary in this repo and in `~/.agents/` + `~/.claude/CLAUDE.md` reads
> `da` (or carries an explicit compat-shim note for the deprecation
> window). Running `da --version`, `da init --yes`, `da workflow status`,
> `da kg health`, and `da review` works identically to running the same
> commands as `dot-agents` did pre-rename. New PRs that introduce a
> fresh `dot-agents` invocation in a user-facing position are caught
> (by convention checklist or future lint).
>
> **Common false positive:** the shim ships and CI passes because every
> test still uses `dot-agents` (which the shim handles), and the
> documentation sweep claims "done" while skills/instructions still
> show `dot-agents <cmd>`. Verification must include a sample of
> user-reading flows (run `/agent-start` and `/orchestrator-session-start`
> in a clean session, confirm the agent's output uses `da`).

## 3. Methodology lens — applied at plan level

### 3.1 Four-question lens (annimaniac)

| Question | Today | Post-plan |
|---|---|---|
| What can AI **see**? | references to `dot-agents` in docs and skills emit stale guidance to agents | references read `da` consistently; agents emit current invocations |
| What can AI **do**? | `dot-agents <cmd>` works (legacy binary); after t2 + shim, both names work | both names work during deprecation; only `da` works post-t7 |
| Who can **extend**? | unchanged — anyone editing docs/skills | unchanged, but a convention note in CONTRIBUTING flags new `dot-agents` references for review |
| How has the **org** changed? | mid-rename inconsistency in WIP | binary name is `da`; docs/agents/users converged |

### 3.2 Resource graduation matrix view

This plan does not move a resource graduation tier — it's a global
naming refactor, not a tier promotion. Worth noting because **this
makes it a good candidate for a "validate the rename pipeline before
committing to it" approach**: ship the shim (t2), verify it works in
a session, then sweep docs (t3-t5) at a comfortable pace.

### 3.3 Anti-scope discipline

Renames target **only the user-facing binary name and CLI invocations**:

**STAYS unchanged:**
- Go module path `github.com/NikashPrakash/dot-agents` (~92 files)
- Source dir `cmd/dot-agents/` (~28 build/script refs)
- Project name "dot-agents" in headings, repo refs, marketing
- Directory names: `.agents/`, `~/.agents/`, `~/.agents/skills/dot-agents/`, `~/.agents/rules/dot-agents/`
- Go package names

**SACRED — do not touch:**
- `.agents/history/` — historical record. Renaming there falsifies what was actually run when those plans were live.

**RENAMES:**
- The binary output name (already started by user)
- All `dot-agents <cmd>` invocations in docs, plans, specs, skills, rules,
  research, tests, CI workflows, top-level CLAUDE.md
- The TS port binary (decision in t6: `da-ts` vs `dot-agents-ts`)

## 4. Task graph

```
t1-decide-shim-or-cutover  (produces ADR-0006)
     │
     ├──────► t2-ship-shim-or-cutover-binary
     │              │
     │              └──► t6-ts-port-binary-decision  (may produce ADR-0007)
     │
     ├──────► t3-sweep-priority-docs       (parallel; can split docs/ into batches)
     ├──────► t4-sweep-plans-specs-research (parallel)
     └──────► t5-sweep-skills-rules-claude-md (parallel)
                    │
                    └──► (joins t2, t6) ──► t7-drop-dot-agents-shim  (calendar-gated)
```

`t3`, `t4`, `t5` are independent (non-overlapping write scope) and run in
parallel. `t6` is a smaller decision that depends on `t2`. `t7` is
calendar-gated — only run after the deprecation window in ADR-0006 has
elapsed (recommended 6-12 months).

## 5. ADRs produced

| ADR | Title | Produced by | When |
|---|---|---|---|
| 0006 | Binary rename strategy: shim vs cutover (recommendation: hybrid) | t1 | this plan, immediately |
| 0007 | TS port binary naming (`da-ts` vs `dot-agents-ts`) | t6 | only if non-trivial; can fold into ADR-0006 if trivial |

## 6. Out of scope

- **Renaming `~/.agents/skills/dot-agents/` or `~/.agents/rules/dot-agents/` namespace dirs.** That namespace is the project name, not the binary name. Keep.
- **Renaming the source dir `cmd/dot-agents/`.** Stays. Goreleaser's `main: ./cmd/dot-agents` line stays.
- **Renaming the Go module path.** Stays.
- **The repo name on GitHub.** Stays.
- **Backfilling ADRs for prior decisions** (managed compounding terminology, async peer review, etc.). That's a separate retro task — not part of this rename.
- **Telemetry to measure deprecation usage.** If we had it, t7 would consult it before dropping the shim. We don't, so t7 is calendar-gated only.

## 7. Coordination with peer plans

Three active plans run concurrently:

- **`self-review-iteration-close-wiring`** — touches `~/.agents/skills/dot-agents/{self-review,iteration-close}/`, `commands/workflow/iter_log.go` (read), `agent-context-resolution-architecture.md` §1.6, `docs/adr/000{2,3,4,5}-*.md` (new). **Overlap risk:** small overlap with this plan's t3 (sweeps binary refs in architecture note) and t5 (sweeps skills). **Mitigation:** if both plans are in flight, sequence t3/t5 of the rename plan AFTER the self-review plan's t2/t3 commit; or do the rename sweep on the new content right after each self-review task lands.

- **`typescript-port`** — touches `ports/typescript/`, `docs/TYPESCRIPT_PORT_GAP.md` (new), `docs/typescript-port-boundary.json` (new), CI workflow under `.github/workflows/typescript-port.yml`. **Overlap risk:** small overlap with this plan's t6 (TS port binary naming) and t3 (docs/TYPESCRIPT_PORT_BOUNDARY.md). **Mitigation:** t6 of the rename plan and tp1/tp2 of the typescript-port plan touch the same boundary doc — sequence so the rename happens before or after the gap audit, not concurrently.

For ISP execution: the orchestrator should pick whichever plan has the
most evidence-confidence and run it to completion (or to a natural break)
before pivoting. Don't fan out across all three plans in one batch.

## 8. Verification gates per task

Each task in `TASKS.yaml` declares its own hard-test + false-positive
in the `notes:` block. The end-to-end gate is t6 (or t7 if shim path):
clean grep across docs + a real session run that observes agent output
using `da`.

## 9. Closeout signals

- ADR-0006 accepted, ADR-0007 accepted (if produced).
- `da` binary works; `dot-agents` binary still works during deprecation
  (if shim path) and prints the deprecation notice.
- `grep -rE "dot-agents [a-z]" docs/ .agents/workflow/ research/ ~/.agents/skills ~/.agents/rules ~/.claude/CLAUDE.md`
  returns only entries inside `.agents/history/` or explicit
  compat-shim references.
- `dot-agents workflow plan archive --plan binary-rename-da-sweep`
  (or `da workflow plan archive` post-rename — both work via shim).
- t7 archived as not-applicable if cutover path was chosen, OR
  scheduled for a future session if shim path was chosen.

---

## 10. ISP execution notes

### Suggested ISP turn sequence

```
turn 1 → t1-decide-shim-or-cutover         (direct; produces ADR-0006)
turn 2 → t2-ship-shim-or-cutover-binary    (direct; build/CI; smoke-tested)
turn 3 → fanout: { t3, t4, t5 }            (parallel; non-overlapping write scope)
turn 4 → t6-ts-port-binary-decision        (direct)
turn 5 → archive (PLAN.yaml status → completed; t7 deferred to future)
turn 6 (months later) → t7-drop-dot-agents-shim  (calendar-gated; potentially scheduled agent)
```

### Pre-flight requirements

- The user's WIP changes to `.goreleaser.yaml`, `.github/workflows/auto-release.yml`, `.agentsrc.json` are uncommitted at plan start. t2 absorbs these into the shim-or-cutover commit so they don't get lost.
- The `binary-rename-da-sweep` plan should NOT block the
  `self-review-iteration-close-wiring` plan from making progress; both
  are independent. Pick whichever one is higher value at any given
  ISP turn.

### Evidence sidecars consulted by every task

- The user's WIP diff (`.goreleaser.yaml`, `.github/workflows/auto-release.yml`, `.agentsrc.json`) — what's already changed.
- `agent-context-resolution-architecture.md` §6.5 — the audit pattern that motivated treating drift as a load-bearing problem.
- `docs/adr/0001-adopt-architecture-decision-records.md` — ADR conventions for ADR-0006 + ADR-0007.
- The rename-scope analysis in `.agents/active/loop-state.md` "Next Iteration Playbook" side-note.
