# Proposal: read task/coordination state from a master source (git-native shared SOT)

- type: design input / spec amendment
- status: draft (for review)
- date: 2026-07-11
- extends: `.agents/workflow/specs/work-tracking-storage-abstraction/design.md`
  (adds a `git-ref` WorkStore backend between `local` and `kg`)
- prompted by: the recurring task-state-commit + worktree pain (owner ask,
  2026-07-11) — "read from master source"

## Problem (grounded)

Workflow/coordination state (`PLAN.yaml`, `TASKS.yaml`, loop state under
`.agents/workflow/**`) is read and written as **working-tree files on whatever
branch/worktree the agent happens to be on**:

- `loadCanonicalTasks` = `os.ReadFile(plansBaseDir(projectPath)/<id>/TASKS.yaml)`
  and `saveCanonicalTasks` writes the same working-tree path
  (`commands/workflow/plan_task.go:508-536`). No git ref is consulted.
- The code itself flags this as temporary: *"INTERIM band-aid: the strategic fix
  is the WorkStore/backend storage abstraction … file-locking the existing YAML
  read-modify-write holds the line until that cutover lands"* (`plan_task.go:562-565`).
- Interprocess safety is partial: only `plan_task.go` takes `agentslock.AcquireFileLock`
  (`:573`); `delegation.go`/`contract.go`/`eligible_accounting.go` are still
  unlocked (see `workflow-store-concurrency-safe-writes.md`).

This is exactly the failure `work-tracking-storage-abstraction/design.md §1`
diagnoses: **"Git's working tree is the wrong substrate for coordination state."**
A wave-engine run had workers advance status inside their **own worktree's**
`TASKS.yaml` copy; it never propagated to the main repo; the scout kept reading
main's stale copy and **re-dispatched → 5 duplicate PRs for `p1c`**. Status
fragments across per-worktree YAML, the lagging canonical YAML, PR state, and the
scout's stale snapshot. Two consequences the owner keeps hitting:

1. **Divergence** — task state lives on feature branches / in worktrees and never
   reconciles to one authority; "what's the status?" depends on where you read.
2. **Commit entanglement** — because state must be committed so the
   fresh-clone/worktree loop doesn't discard it (`plan_task.go:2162-2164`,
   `2238-2240`), `da workflow commit` stages the **whole** `.agents/workflow/`
   store (the "45 paths, 3 mine" bug — `obs-da-workflow-commit-scope-safety.md`),
   and task-state commits get tangled into code-branch history. The
   `worker-bundle-authoring` plan's `commit-1-task-pathset`/`commit-2-cli-scoped-mode`
   tasks exist only to scope that entanglement.

## The idea: a master source for coordination state

Decouple the **coordination-state plane** from the **code branch/worktree**.
Read and write status from a single **canonical git source** — a fixed ref, not
the per-worktree working copy. This is `work-tracking-storage-abstraction`'s
D1/D2 ("two planes; status reads resolve against the backend, not the per-worktree
YAML") implemented in **pure git, with no daemon and no external service** — the
missing rung between the spec's `local` backend (per-worktree files, no shared
SOT) and its `kg` backend (needs the `da service` daemon + graph store).

### Backend shape: `git-ref` WorkStore

- **State ref (the "master source").** A dedicated ref carries only the
  coordination plane — e.g. `refs/agents/state` (or a conventional `agents-state`
  branch), configurable. It is **orthogonal to the code branch**: worktrees on
  `feature/x`, `feature/y`, and detached HEADs all resolve status against the
  same ref.
- **Read path.** `loadCanonicalTasks` (behind the `WorkStore` facade, D3) resolves
  against the ref, not the working tree: `git cat-file`/`show <state-ref>:<path>`,
  or a single shared linked worktree of the state ref that all agents read. This
  is D2 ("status reads resolve against the backend") with the ref AS the backend.
- **Write path.** A status transition commits the changed state file(s) to the
  state ref via an **atomic compare-and-swap** (`git update-ref <ref> <new> <old>`;
  retry-on-mismatch) — the interprocess-safe RMW the current `agentslock` only
  half-covers, now serialized at the ref instead of the file. Team sharing = push/
  fetch that one ref to `origin`; offline = the local ref is the degenerate SOT
  (mirrors the spec's `local`).
- **Conflict granularity.** To avoid line-level `TASKS.yaml` merge conflicts when
  two workers transition **different** tasks, either (a) split status onto
  per-task state files under the ref, or (b) keep the CAS-retry loop (re-read,
  re-apply the single-task transition, re-push). Per-task files are the cleaner
  target and align with D5's per-task lease/claim.

### What it fixes

- **Kills the re-dispatch bug** (the p1c 5-duplicate-PRs failure) with **no
  daemon/KG**: the scout's eligibility query reads the state ref, so a worker's
  `pending → awaiting_review` on its own worktree is immediately visible to
  everyone — D5's "structural re-dispatch fix," git-native.
- **Dissolves the commit-scope pain.** Coordination state is no longer committed
  into the **code** branch at all — it lives on its own ref. The whole-store-vs-
  task-scoped-commit problem (`commit-1`/`commit-2`, the "45 paths" obs) largely
  evaporates: code commits stop carrying `.agents/workflow/**`, and state commits
  stop carrying code. The two planes get separate lineages.
- **One authority.** "What's the status" is always "what the state ref says,"
  regardless of which worktree/branch you're standing in.

## Tradeoffs / open questions

- **Projection vs §3B.** The spec's §3B principle is "the file-system projection
  IS the agent's interface — zero new semantics; agents just read/edit files." A
  `git-ref` backend must therefore still **project the ref into a readable path**
  (a shared linked worktree of the state ref, or a read-through checkout into
  `.agents/workflow/`) so an agent keeps reading a plain file. That projection +
  write-capture is lighter than the D4 daemon but is NOT zero — decide whether a
  tiny reconciler (or a git hook / `da workflow` write-through) owns it.
- **Ref contention** under high fan-out → CAS-retry storms; per-task state files
  (above) plus short critical sections keep it bounded. This is strictly better
  than today's unlocked multi-writer RMW.
- **Positioning vs `kg`.** `git-ref` is the **no-infrastructure shared SOT**: the
  natural upgrade from `local` for teams that want cross-worktree atomic status
  without standing up the `kg`/`cloudflare-do` daemon. It rides the same
  `WorkStore` interface (D3) and the same D8 scope declaration
  (`work_tracking.backend = local | git-ref | kg | cloudflare-do | jira | linear`),
  so a team can graduate `local → git-ref → kg` without changing the agent-facing
  file interface.

## Near-term minimal shim (before the full WorkStore lands)

A high-leverage subset that kills the divergence bug without the interface:
add a `work_tracking.read_from` option so `loadCanonicalTasks` (and the scout's
eligibility read) resolve `TASKS.yaml` from **`origin/<default-branch>` or the
state ref** instead of the per-worktree working copy, while writes still land as
today (committed on merge). Even read-side-only, this stops the scout from
re-dispatching in-flight work — the single worst symptom — for the cost of one
`git show`-backed read path.

## Recommendation

1. Amend `work-tracking-storage-abstraction/design.md` to add `git-ref` as a
   first-class `WorkStore` backend (D3) + config value (D7 / D8 ladder), with the
   state-ref + CAS + per-task-file design above.
2. Fold the `worker-bundle-authoring` **commit-scope** thread (`commit-1-task-pathset`,
   `commit-2-cli-scoped-mode`) into this: with a state ref, task-state commits are
   ref-writes, not code-branch scoping — re-scope or retire those tasks accordingly.
3. Land the near-term read-from-master shim first (small, unblocks the re-dispatch
   pain immediately); treat the full `git-ref` WorkStore as the durable follow-on
   that converges with the storage-abstraction spec.

Relationship: this does **not** compete with the `kg`-as-SOT end-state — it is the
git-native intermediate backend on the same `WorkStore` seam, and the read-from-master
shim is worth doing regardless because it fixes today's worst symptom cheaply.
