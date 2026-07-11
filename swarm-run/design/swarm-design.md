# dot-agents inner-loop swarm — design

## What this is
An artifact-driven omp **swarm** (DAG of omp subagents, file-coordinated) that drives the
dot-agents inner loop using da's **execution profiles** to structure impl→verify→review→gate.
It triple-purposes as live evidence for (1) `omp-platform-handling` (da↔omp platform support),
(2) `git-ref-work-backend` D9/D10 (coordination state on `refs/agents/state`), and (3) it
delivers real work (the `managed-gitignore-autofill` D14/R8 slice first).

## Phased scope (NOT haphazard)
- **Phase 1 (this run): one proving slice** — `managed-gitignore-autofill/wire-managed-gitignore-autofill`
  (app_type `go-cli`, deps=∅). Runs the FULL go-cli profile pipeline end-to-end, coordinating on
  `refs/agents/state`, producing ONE owner-held PR + a findings bundle. Proves the mechanism.
- **Phase 2 (after proof): generalize** to the 29 ready tasks (PlansBreakdownDoc) as a pipeline
  that picks the next ready task, respects the 9 write_scope conflict pairs, one board-writer,
  and each task's own app_type profile. Not launched until Phase 1 is green + reviewed.

## Two planes (git-ref backend, D9/D10)
- **Coordination plane** = `refs/agents/state` (parallel lineage, NEVER merged to master — D10).
  A shared linked worktree at `.agents/worktrees/_state`. All swarm coordination — signals,
  verify/review results, findings, merge-back — lives under `.agents/worktrees/_state/active/**`
  and is committed to `refs/agents/state` (pushed for durability). This is the D9 git-ref backend
  operated manually (da's WorkStore git-ref backend is unbuilt); the run dogfoods it. `.agents/active/**`
  semantics (active scratch → archived to history) are honored here.
- **Code plane** = per-slice git worktree `.agents/worktrees/<slice>` off `origin/master`
  (VCS-stamp-valid, reachable). Impl/build/test happen here. Code branches stay pristine of
  coordination churn (RULE — decouple-coordination-commits).

## Workspace + sandbox
- `workspace = ~/proj-docs/dot-agents` (repo root) — so both planes' worktrees (`.agents/worktrees/*`,
  gitignored) are reachable by subagents (sandboxed to workspace root) and VCS-stamp-valid.
- Coordination via files ONLY (no inter-agent data passing). Signals are small parseable files;
  detail goes in structured artifacts. Every agent's real deliverable is a FILE (RULE 0 — summaries
  are discarded).

## go-cli execution-profile → swarm stages (faithful mapping)
Resolved from the repo's `.agentsrc.json` (da ships empty defaults): go-cli =
verifier_sequence `[unit, cli-runner]`, lens_set `[architecture-standards, acceptance-invariants,
adversarial, cross-harness-adversarial]`, lens_concurrency `gated`, gate = built-in `default`
(`event.pr.open ∧ signal.ci.rollup==green ∧ gate.quality.sonar ∧ metric.new_code_issues==0`).

Waves (from `waits_for`):
1. **readiness** — RULE 9 canonical-ref check: `git fetch origin master`; reconcile the D14 task
   vs `config-distribution-model/design.md §15` + verify `EnsureManagedGitignore` is truly unwired
   (vs config-v2-coherence "§6.1 .gitignore auto-fill" — is it a DIFFERENT mechanism?). Create the
   slice worktree off origin/master, symlink `.venv`. Emit `readiness.md` (READY|BLOCKED + notes +
   prior-attempt patch pointer).
2. **impl** — implement the 5-item checklist (TASKS.yaml notes) in the slice worktree; `go build
   -o bin/da ./cmd/da` (installed 0.4.2 lacks features); focused tests; commit (NO AI trailer).
   Emit `impl.md` (files, sha, self-test).
3. **verify_unit** → 4. **verify_cli_runner** — sequential (verifier_sequence order + one-writer).
   unit = `go test -race` on write_scope pkgs; cli-runner = dogfood `da refresh` in an ISOLATED temp
   project asserting the managed block contains `.github/hooks/*.json`, `.agentsrc.lock` tracked,
   byte-stable re-run. Each resolves its prompt via `bin/da workflow resolve-prompt --kind verifier
   --slug <unit|cli-runner>`. Emit `verify-<slug>.md` (PASS|FAIL + evidence).
5. **review lenses** (4, wave 5) — architecture-standards, acceptance-invariants, adversarial,
   cross-harness-adversarial. Each resolves `... --kind reviewer --slug <lens>`, reviews read-only
   (slice worktree + verify signals), mutation-verify + the 3 anti-patterns (RULE 7). Emit
   `review-<lens>.md` (APPROVE|REJECT + reasons). **cross_harness uses a DIFFERENT model** (RULE 7).
   NOTE (divergence, a finding): profile lens_concurrency=`gated`; the swarm has no true short-circuit,
   so lenses run PARALLEL (read-only, distinct outputs) and the ready-gate enforces "all APPROVE" —
   captured as an execution-profile↔swarm-mapping finding.
6. **ready_gate** — read all verify+review signals. If all PASS/APPROVE: open PR (`gh pr create
   --base master --head <branch>`, NO AI trailer), poll CI to green (orchestrator owns CI-watch —
   RULE 3/4), assert the `default` gate predicates, emit `READY.md` (terminal line). Else emit
   `FOLD-BACK.md` (blocking reasons). **Never merges** (owner-held — RULE 4). **Never mutates the
   board** (`da workflow advance/closeout`) — RULE 10 (races); board reconcile is a separate serialized
   step by Main after the owner merges. Finally: collate `active/findings/omp-platform-*.md` →
   `active/findings/CONSOLIDATED.md`; commit `.agents/worktrees/_state` → `refs/agents/state` + push.

## Rule adherence (11 rules — see research/experiences-lessons.md)
0 artifacts-not-summaries (file deliverables + signal files) · 1 sentinel handoff (swarm drives
stages by explicit CLI; da stop-hooks don't fire under omp anyway — no stale sentinel; if any da
cmd writes one, ready_gate clears it) · 2 NO AI trailers (in shared context + every git-writing brief)
· 3 worktree-per-slice under repo, `git -C` never `cd`, one writer, no self-armed CI waiter · 4 owner-held
merges, workers reach READY/FOLD-BACK · 5 (n/a single slice; Phase 2 uses core+adapters) · 6 classify
before clean + managed-gitignore is da's job (this IS that task) · 7 tests drive production path +
cross-harness blocking reviewer diff-model · 8 single-source-of-truth (briefs point to canonical spec,
no rest为) · 9 readiness-gate vs canonical ref before impl · 10 ONE board writer (swarm writes NO board;
Main reconciles serially post-merge) · 11 merge-back frontmatter template (ready_gate authors it).

## omp-platform findings capture (baked in)
Every agent appends observed da↔omp friction to `active/findings/omp-platform-<agent>.md`
(hooks not firing under omp, sentinel behavior, profile-dispatch-via-CLI ergonomics, embedded-shell
quirks, git-ref backend manual-operation pain). ready_gate consolidates. Feeds
`.agents/proposals/omp-platform-handling.md` + `git-ref-work-backend`.

## Safety
`guard-rm.ts` installed (`~/.omp/agent/hooks/pre/guard-rm.ts`) — blocks the empty-expansion `rm`
cwd-wipe class. Briefs still mandate explicit paths + `git -C`. Slices isolated in worktrees so one
failure can't cascade (swarm has NO failure short-circuit — downstream agents are defensive: read
upstream signal, no-op+SKIP if BLOCKED/missing).

## Invocation
`bun /Users/nikashp/Documents/oh-my-pi/packages/swarm-extension/src/cli.ts <yaml>` (bin `omp-swarm`).
State → `<workspace>/.swarm_<name>/`. Owner merges the resulting PR; Main reconciles the board.
