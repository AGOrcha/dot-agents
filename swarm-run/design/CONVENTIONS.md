# Swarm agent conventions (READ FIRST — every agent)

You are one node in the **dot-agents inner-loop swarm** running the `managed-gitignore-autofill /
wire-managed-gitignore-autofill` slice (app_type **go-cli**) through da's execution profile.
Your cwd is the repo root `~/proj-docs/dot-agents`. Coordinate ONLY via files. Your return text is
DISCARDED — your deliverable is the file(s) you write.

## Paths (all relative to cwd = repo root)
- `STATE = .agents/worktrees/_state`  — coordination worktree, checked out on `refs/agents/state`
  (a PARALLEL lineage, NEVER merged to master). All coordination lives here.
- `COORD = .agents/worktrees/_state/active/coordination`  — signal files (one per stage).
- `FIND  = .agents/worktrees/_state/active/findings`       — omp-platform findings (append yours).
- `SLICE = .agents/worktrees/d14`  — the CODE worktree (off origin/master), created by `readiness`.
- Branch = `swarm/managed-gitignore-autofill`.
- Design + research you may read: `.agents/worktrees/_state/swarm-run/{design,research}/`.

## Coordination protocol (RULE 0 — artifacts, not summaries)
- Each stage reads its upstream signal file(s) from `COORD` and writes its OWN signal file there.
- Signal files are Markdown; **line 1 is a verdict token** the next stage greps:
  `READY|BLOCKED` (readiness), `DONE|SKIP` (impl), `PASS|FAIL|SKIP` (verify), `APPROVE|REJECT|SKIP`
  (review), `READY|FOLD-BACK` (gate). Put evidence/detail below line 1.
- **DEFENSIVE (the swarm has NO failure short-circuit):** if your required upstream signal is missing,
  or its verdict is a blocking one (BLOCKED / FAIL / REJECT), DO NOT do your normal work — write your
  own signal with verdict `SKIP` (reason: "gated: upstream <name>=<verdict>") and exit 0. Never
  produce work off a broken/absent upstream.

## Git discipline (RULE 3)
- Work in `SLICE` via `git -C "$SLICE" …`. NEVER `cd` into a worktree (pwd leaks across bash calls).
  For build/test that needs cwd, use a subshell: `(cd "$SLICE" && go test …)`.
- Exactly ONE writer per worktree — only the stage that owns `SLICE` writes code there.
- Do NOT arm background CI waiters; the `ready_gate` stage owns CI polling.

## da usage (build from source — installed 0.4.2 is stale)
- Installed `da` 0.4.2 LACKS repo-HEAD features (`da run`, `kg link import`, etc.). BUILD from source
  in the slice worktree and use that binary: `(cd "$SLICE" && go build -o bin/da ./cmd/da)` → `"$SLICE/bin/da"`.
- Resolve your stage's configured prompt: `"$SLICE/bin/da" --json workflow resolve-prompt --kind
  <verifier|reviewer> --slug <slug>` and follow it.
- KG blast-radius readback: `da kg impact <file> --json` and `da kg code-status` WORK. `da kg
  build/update/postprocess` are BROKEN here (stale CRG venv shebang) — do NOT call them.
- `--dry-run` is NOT side-effect-free in 0.4.2 — never rely on it.
- Do NOT run board-mutating `da workflow advance/closeout/merge-back/task update` — the 0.4.2 store
  races (RULE 10). Board reconcile is done by the human orchestrator after merge. You only PRODUCE
  artifacts.

## Attribution (RULE 2) — binds every git-writing stage
Author commits/PRs as the user. NO `Co-Authored-By: Claude …` trailer, NO "Generated with …"
footer, NO AI-authorship boilerplate. (Factual model/harness references are fine.)

## Merges (RULE 4)
NO stage merges a PR. `ready_gate` drives to a terminal READY or FOLD-BACK and leaves the merge to
the owner.

## omp-platform findings (write as you go — this run is spec evidence)
Append every da↔omp friction you hit to `FIND/omp-platform-<your-stage>.md`: da hooks/sentinels not
firing under omp, `resolve-prompt`/profile-dispatch-via-CLI ergonomics, embedded-shell quirks, the
manual git-ref (`refs/agents/state`) coordination pain, anything da would need to support omp as a
first-class platform. Terse bullets with the exact command + what happened.

## The task (single source — do NOT restate; read it)
Canonical: `.agents/workflow/plans/managed-gitignore-autofill/TASKS.yaml` task
`wire-managed-gitignore-autofill` (its `notes` = the authoritative 5-item checklist). Design source:
`.agents/workflow/specs/config-distribution-model/design.md` §15 (D14/R8). Contract code:
`internal/links/gitignore.go` (`EnsureManagedGitignore`, markers `# >>> dot-agents managed (project
outputs) >>>`, `neverIgnored = .agentsrc.json/.agentsrc.lock`). Copilot output gen:
`internal/platform/copilot.go` + `hooks.go`. Prior UNREVIEWED partial attempt (optional reference):
`.agents/worktrees/_state/swarm-run/research/prior-d14-attempt.patch`.
