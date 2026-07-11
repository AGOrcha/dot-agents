# dot-agents inner-loop swarm — design notes (accumulating)

Purpose: design an artifact-driven omp **swarm** (DAG of omp subagents, file-coordinated) that runs the dot-agents inner loop using da's **execution profiles** to structure verify/review — AND doubles as live evidence for the `omp-platform-handling` proposal.

## Swarm mechanism (from source, confirmed)
- YAML: `swarm.{name, workspace, mode(sequential|parallel|pipeline; default sequential), target_count(default 1), model, agents.<name>.{role, task, extra_context, reports_to[], waits_for[], model}}`.
- DAG from `waits_for` (+ `reports_to` reverse edges) → topological **waves**; same wave runs in **parallel** (`Promise.all`). Waves sequential. `target_count` loops the WHOLE DAG (pipeline = accumulative).
- Each agent = `runSubprocess` omp subagent: `cwd = workspace` (SHARED across all agents), systemPrompt = role+extra_context, task = user prompt, FULL tools (bash/python/read/write/edit/grep/fetch/browser), `enableLsp:false`, artifacts → `<workspace>/.swarm_<name>/context`.
- **NO failure short-circuit**: a failed agent is recorded (exitCode≠0 → errors[]) but downstream waves RUN ANYWAY; only external abort / fatal throw stops it. → agents MUST be defensive.
- Invoke: `bun /Users/nikashp/Documents/oh-my-pi/packages/swarm-extension/src/cli.ts <yaml>` (bin `omp-swarm`); runtime confirmed (bun 1.3.14, imports resolve). State → `<workspace>/.swarm_<name>/`.
- No inter-agent data passing — coordinate via **files in workspace** (signal files, structured outputs, tracking files).

## Design consequences
1. **Defensive agents** (no short-circuit): every agent first reads upstream signal/output files; if a required input is missing or an upstream wrote FAIL/SKIP, it does a clean no-op (writes its own BLOCKED/SKIP signal, exits 0) instead of producing garbage. A per-slice **gate agent** checks real evidence (tests green) before allowing PR/merge.
2. **Slice isolation**: each task/slice works in its OWN git worktree off `origin/master` (never the main tree). One failure can't corrupt a sibling. Use `git -C <worktree>` / explicit cwd on every command.
3. **da hooks/sentinels DON'T fire under omp** (omp has no da platform support yet; omp uses TS `tool_call`/`tool_result`, not da's shell gates + there's no omp `subagent_stop`). → the swarm must invoke da's execution-profile stages **EXPLICITLY via CLI** (verify-record, review lenses, merge-back, advance) in agent task instructions. This explicit reproduction IS the thing the omp adapter would automate.
4. **Execution-profile → swarm-stage mapping** (to finalize from ConfigProfilesDoc): per slice, impl-agent → verifier-agent(s) per app_type `topology.verifier_sequence` → reviewer-agent(s) per `lens_set` (Core/Situational/Noise) → gate/merge-back agent. Wired by `waits_for`.

## Safety (omp incident class)
- `guard-rm.ts` CONFIRMED installed at `~/.omp/agent/hooks/pre/guard-rm.ts` (4.4KB) — blocks the empty-expansion `rm` class (the payout cwd-wipe incident). Swarm agents (heavy bash/git) are protected. Still: brief agents to pass explicit paths, never destructive `rm` with empty-expandable targets.

## omp-platform-handling framing (user)
This run feeds `.agents/proposals/omp-platform-handling.md` (da↔omp platform support). **Every swarm agent appends observed da↔omp friction to a shared findings file** `<workspace>/findings/omp-platform-<agent>.md` (hooks not firing, sentinel behavior under omp subagents, event-mapping gaps, profile-dispatch-via-CLI ergonomics, embedded-shell quirks). The run = spec evidence. Baked into briefs, not retrofitted.
- omp conventions (from proposal): hooks `~/.omp/agent/hooks/{pre,post}/*.ts` (TS factory), extensions `~/.omp/agent/extensions/<name>/`, events tool_call/tool_result/context/session_*, profile-aware (`PI_CODING_AGENT_DIR`). Materialization gap: da shell-gate HOOK.yaml+gate.sh ≠ omp TS factory → adapter (native TS OR shell-gate bridge).

## Open decisions (resolve after research artifacts land)
- A) One big static DAG (all ready tasks × profile-stages) vs. pipeline mode (each iteration = pick next ready task → run profile stages). Leaning: start with the FIRST slice (managed-gitignore-autofill) as a proving DAG, then generalize.
- B) How many parallel slices in wave 1 (from PlansBreakdownDoc ready-set + write_scope conflict flags).
- C) Exact verifier_sequence + lens_set per app_type (from ConfigProfilesDoc) → number of verifier/reviewer agents per slice.
- D) Model per role (impl vs verify vs review vs gate).

## Prior work preserved
- `/tmp/da-swarm/research/prior-d14-attempt.patch` — cancelled task-orchestrator's ~180-line partial D14/R8 impl (UNREVIEWED, optional reference for that slice's impl agent).

## UPDATE after ExperiencesDoc (/tmp/da-swarm/research/experiences-lessons.md — 11 RULES)
DECISIVE constraints that reshape the topology:
- **Workspace = the dot-agents repo root** (`~/proj-docs/dot-agents`). Subagents are SANDBOXED to workspace root → `/tmp/wt-*` unreachable; worktrees MUST live under it at `.agents/worktrees/<slice>` (also required for `go build` VCS stamping in the pre-push gate). Coordination artifacts under a shared in-workspace dir (readable by all agents + orchestrator) — NEVER inside a slice's isolated worktree (isolated status "never reaches" the reader — caused a 5× re-dispatch).
- **ONE board writer** (RULE 10): da 0.4.2 `delegation.go`/`contract.go`/`eligible_accounting.go` have NO file lock → concurrent `da workflow advance/closeout/task update` SILENTLY lose updates. So: workers/verifiers/reviewers produce ARTIFACTS only; they NEVER call `da workflow` board-mutating commands. A single serialized terminal "board/reconcile" node (or Main, post-run) does advance/closeout/merge-back. Fan-out waves gated so no two `da workflow` writes overlap. `eligible`/`next` are advisory (draft plans silently skipped; activate first).
- **`git -C <abs>` never `cd`** (pwd leaks across bash calls → commits on wrong branch). One active writer per worktree. Workers do NOT self-arm CI waiters — the orchestrator/gate polls CI.
- **`--dry-run` is NOT side-effect-free in 0.4.2** (plan update/start-task/foldback dry-run mutate). Never use for safe preview.
- **Verify commands against shipped 0.4.2 `--help`**, not repo HEAD (`da run` shipped 0.5.0; #332 still open).

## Execution-profile → swarm-stage mapping (go-cli, from ExperiencesDoc swarm-hooks)
Per go-cli slice: impl-agent → verifier-agent(s) per `topology.verifier_sequence` → reviewer-agent(s) per `lens_set = [architecture-standards, acceptance-invariants, adversarial, cross-harness-adversarial]`, `lens_concurrency = gated` → READY-gate agent (real-evidence check) → [owner merge] → (serial board-reconcile node). cross-harness-adversarial reviewer MUST use a DIFFERENT model than the impl agent (Rule 7). Resolve prompts via `da workflow resolve-prompt --kind <executor|verifier|reviewer> --slug <slug>`; preconditions via stage_profiles.<stage>.<slug>.precondition_policy.

## Merge-back frontmatter (RULE 11) — required schema_version=1 fields
schema_version, task_id, title, summary, files_changed ([] or block list), result block (status+summary), integration_notes, created_at (quoted RFC3339). Block scalars (|-) for text with ': '.

## D14 task reconciliation flag (RULE 6/9)
managed-gitignore-autofill may overlap config-v2-coherence "outputs exact/prune projection + .gitignore auto-fill (§6.1)". Before impl, the slice's readiness-gate MUST reconcile: is links.EnsureManagedGitignore truly unwired, or did config-v2 wire a DIFFERENT autofill (EnsureResolved projection)? Diff task notes vs canonical spec on origin/master. (This is exactly RULE 9 in action.)

## Board-mutation policy for THIS swarm (decision)
To sidestep RULE 10 entirely on the first run: the swarm produces code + PR + verification/review/merge-back ARTIFACTS only. It does NOT mutate the canonical board (`da workflow advance/closeout`). Board reconciliation is a SINGLE serialized terminal step done by Main (me) after observing PR/merge state — or a lone board-node run strictly last. Owner holds merges (RULE 4).

## UPDATE — workspace + artifact reachability (resolve sandbox contradiction)
- **workspace = `~/proj-docs/dot-agents`** (repo root). Reason: worktrees must be under the repo (VCS stamping + RULE 3 reachability), and subagents may be sandboxed to workspace root.
- RELOCATE research+design artifacts under the workspace before spawn: `.agents/active/swarm-run/{research,design}/`. Coordination dir: `.agents/active/swarm-run/coordination/`. Ignore the whole `.agents/active/swarm-run/` + `.swarm_*/` via `.git/info/exclude` (local, no commit) so the main tree stays clean during the run. Clean up after.
- Worktrees: `.agents/worktrees/<slice>` (off origin/master, `git -C`).

## UPDATE after RunReviewDoc — BUILD da FROM SOURCE
- Installed `da` is 0.4.2 and LACKS `da run`, `da eval`, `da review users/audit` (live "unknown command"). Repo HEAD (917908d4) has them.
- → swarm impl/verifier agents MUST `go build -o <worktree>/bin/da ./cmd/da` in their worktree and use THAT binary for any da feature beyond 0.4.2, and to test their own changes. Do NOT trust the installed 0.4.2 for repo-HEAD features.
- `da review approve/reject` ignore --dry-run (apply for real). `da review` only sees ~/.agents/proposals/*.yaml pending; fold-back obs-*.md is a separate track.

## PRINCIPLE (user, mid-run) — shared writes go through da; code writes stay in the worktree
- CODE plane → each slice writes ONLY in its own worktree (one writer, isolated).
- SHARED repo-root state (da board: TASKS/PLAN.yaml, verification records, merge-back, contracts,
  hook-outcomes) → go THROUGH the `da` binary (cross-process-safe writes), NEVER raw file writes.
- NUANCE (RULE 10): da's lock is PARTIAL — only advance/task-add/task-update use withTasksLock;
  fanout/closeout/contract-create/plan-create/eligible_accounting write UNLOCKED (race under
  concurrency). So "through da" is fully safe only for the locked subset OR when serialized.
- DEFINITIVE mechanism = the git-ref backend's `git update-ref` CAS (D9) — atomic, inherently
  cross-process-safe. This is WHY coordination belongs on refs/agents/state (atomic ref updates),
  not raw-RMW files. Interim = da file-lock; tracked fix = workflow-store-concurrency-safe-writes.
- THIS run (sequential, 9 one-agent waves): swarm writes only its OWN coordination signals (raw .md
  on the ref); da-BOARD writes deferred to Main's serialized reconcile (verify-record/merge-back/
  closeout through da = the locked path). No raw writes hit da's board.
- PHASE 2 + CONVENTIONS update: verify/gate stages call `da workflow verify-record`/`merge-back`
  DIRECTLY through da (safe when sequential, or locked-subset/single-board-writer under parallel).
  Also a git-ref-work-backend finding: update-ref CAS is the shared-write safety primitive.
