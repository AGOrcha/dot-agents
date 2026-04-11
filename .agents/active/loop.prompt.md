# Automated Work Looper Prompt

Copy the prompt below into an agent as: `/loop 1hr <prompt>` (if `/loop` is available else just paste `<prompt>`)

---

## Prompt

```
For the specs in progress: docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md and docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md

**Where workflow state lives (read this once):**
- **Repo** `<project>/.agents/active/*.plan.md` — markdown waves/checklists; this is what `workflow orient` lists as active plans.
- **Repo** `<project>/.agents/workflow/plans/<id>/` — optional YAML canonical layer (`PLAN.yaml`, `TASKS.yaml`) for `workflow plan` / `workflow tasks` / `workflow advance`. Not every repo has created this yet; missing dir or zero canonical plans is normal.
- **`~/.agents/`** — dot-agents install registry (`config.json`), shared rules/skills, and **per-project runtime context** under `~/.agents/context/<project-name>/` (checkpoints, session logs, health snapshots). It does **not** store the markdown plan files or canonical `PLAN.yaml` trees.

## Iteration Start
1. Read `.agents/active/loop-state.md` for prior iteration context (skip if missing)
   - Start with `## Current Position`, `## Loop Health`, the last 2 entries in `## Iteration Log`, `## Next Iteration Playbook`, and the newest relevant `## CLI Observations` items
   - Only scan older coverage tables or traces when you need them to choose a genuinely new scenario or confirm whether a command/state is already covered
2. Run `go run ./cmd/dot-agents workflow orient` — same session snapshot the CLI uses: git summary, active markdown plans, canonical plan summaries (if any), checkpoint pointer, proposals, delegation/merge-back hints, workflow health. Use it **together with** loop-state; do not treat CLI output as a substitute for the iteration log when loop-state has fresher detail.
   - If orient warns about missing `.agents/workflow/` or lists zero canonical plans, continue using markdown `.agents/active/*.plan.md` via the plan-wave-picker — that is expected until YAML canonical plans exist for this repo.
3. Run `git status --short` to see current dirty state — if there are uncommitted changes from a prior iteration, review and commit them first
4. Skim the two driving specs only if loop-state.md doesn't already summarize the current position
5. Identify the current scenario tags before selecting work. Choose tags from the family buckets in `## Scenario Coverage`: workflow project state, workflow write paths, delegation lifecycle, cross-project workflow ops, KG lifecycle, KG maintenance/storage integrity, CRG/code-graph states, bridge/config states, cross-subsystem integration checks, and outcome-quality states
   - Reuse existing tags when possible; use the tag names exactly as written in `## Scenario Coverage`
   - Add a new tag only when it captures a genuinely different state transition, and add the matching coverage row before using the tag elsewhere in loop-state.md
   - Prefer paired scenarios when useful: uninitialized vs initialized, disabled vs enabled, dry-run vs apply, empty vs populated, raw vs postprocess-complete
   - For integration scenarios, choose a sub-bucket first: `bootstrap`, `mutation-and-reconciliation`, `analysis-and-readback`, or `closeout-and-evidence`
   - Good examples: `canonical-plan-present`, `workflow-advance-success`, `fanout-write-scope-conflict`, `kg-setup-complete`, `warm-layer-populated`, `crg-build-complete`, `workflow-graph-disabled`, `repo-health-stack`, `managed-file-restore-stack`, `kg-crg-postprocess-stack`, `verification-checkpoint-stack`, `ok-warning-ux-friction`
6. Before implementing, write down one short **feedback goal** for the evidence command run
   - This is the concrete question the CLI exercise must answer for the current change
   - Good examples: "Do projected shared targets now dedupe cleanly?", "Does `workflow health` expose the new planner state?", "Can `kg build` now reach postprocess without a bridge mismatch?"
   - Bad examples: "make sure nothing broke", "run something relevant", "reconfirm repo health"
   - If the only candidate question is "does `workflow health` stay clean?", pick a closer or less-covered surface unless the changed code directly affects `workflow health`

## Wave Selection
6. Use the plan-wave-picker skill (`.agents/skills/plan-wave-picker/`) to select the next wave from `.agents/active/*.plan.md`
   - Priority order: in-progress waves with unchecked items > waves with all dependencies complete > new waves
   - Skip plans tagged as blocked, waiting on external input, or listed in the skip-list section of loop-state.md
   - A plan's Status header (e.g., "Status: Completed") is authoritative — unchecked `- [ ]` items on a completed plan are stale, not real work
   - Prefer implementation waves over architectural/research waves
   - If no actionable wave exists, write that finding to loop-state.md and stop
8. Align with the canonical workflow view: run `go run ./cmd/dot-agents workflow plan`. If a plan under `.agents/workflow/plans/<id>/` matches the wave you chose (same initiative / documented link in the markdown plan), run `go run ./cmd/dot-agents workflow tasks <id>` for that ID. If there is no matching canonical plan, skip this — markdown remains the source of truth for the checklist.

## Implementation (ONE item per iteration)
9. Pick the next single unchecked item from the selected wave's plan
10. Implement the code change — keep scope tight, touch minimal files
11. Run focused tests first: `go test ./<changed-packages>`
    - **Positive scenarios**: cover intended success paths — default inputs, happy-path behavior, and outputs the change is meant to guarantee.
    - **Negative scenarios**: cover intended failure paths — invalid or malformed input, missing prerequisites, out-of-range values, authorization/validation rejections, and error returns. For each new branch or guard, add or extend tests that assert the **correct** error (or `errors.Is` / sentinel), not only that “an error happened.”
    - Prefer table-driven or parallel subtests when one function has multiple success/failure combinations; do not ship behavior that is only asserted on the happy path unless the surface cannot fail.
    - If a change is test-only refactors, still run focused tests and confirm existing negative cases still pass.
12. Run regression: `go test ./...` — must stay green for the full suite, including packages that encode negative-path behavior (flag parsing, config validation, planner conflicts, etc.).
13. If tests pass, run the relevant `go run ./cmd/dot-agents ...` commands on the tested working tree before the final commit:
   - Keep the evidence budget tight: one primary chain (1-3 commands) plus at most one secondary probe if it adds distinct evidence
   - Do not use `workflow health` or `status` as the primary evidence command in consecutive iterations unless it is the closest runnable surface, verifies a previous warning/bug, or proves a new state tag
14. Update loop-state/history/lessons while the iteration details are fresh, then stage the full iteration and inspect `git diff --cached --stat`
15. If everything is green, commit once with the implementation plus loop-state/history updates:
   - Format: `<area>: <what changed>`
   - Example: `kg: add deterministic query for entity lookup`
16. If tests fail, fix the failure before moving on — do not leave red tests uncommitted. If the only gap is missing **negative** coverage for new logic, add those tests in the same iteration before commit; treating “green on happy path only” as done when the change introduces new failure modes is not acceptable.

## Exercising Workflow Commands (after tests, before the final commit)
After validating the code change, exercise relevant `go run ./cmd/dot-agents` commands as a live integration test on the tested working tree. This is real product testing — treat the results seriously.

Analysis-oriented trace goals:
- Start from the feedback goal you wrote earlier. The command run should answer that question or prove why it cannot be answered yet.
- Prefer commands or repo states that add new evidence, not just repeated happy-path confirmation
- When possible, add both one uncovered command and one uncovered state transition in the same iteration
- If the current iteration only hits `[ok]` paths, try to include one safe expected empty-state, warning-state, or other non-destructive edge case
- Favor paired-state coverage over isolated one-offs so later analysis can compare behavior across contrasting conditions
- When a subsystem is already partially covered, prefer an integration scenario that chains 2-4 commands across subsystems instead of another isolated single-command success
- For integration scenarios, prefer the next uncovered stack type rather than repeating the same kind of chain every time
- Favor uncovered scenario/command combinations over duplicate runs when choosing what to exercise
- Default to one primary evidence chain; only add more traces when the first chain leaves the feedback goal unanswered
- A repeated command chain is acceptable only if one of these is true:
  - it is the closest runnable surface to the code you changed
  - it exercises a new scenario/state tag
  - it verifies a previously failing or warning path after a fix
  - loop-state.md explicitly lists it as the recommended next feedback path
- If a command only reconfirms an existing healthy path, say so explicitly and keep the trace short; do not treat it as meaningful new evidence
- If warnings are known baseline noise, mark them as baseline in the trace follow-on or CLI observations instead of pretending they are fresh feedback

Read-only commands (always safe to run):
- `go run ./cmd/dot-agents status` — verify project health
- `go run ./cmd/dot-agents doctor` — check installations and links
- `go run ./cmd/dot-agents workflow status` — show current workflow state
- `go run ./cmd/dot-agents workflow orient` — render session orient context
- `go run ./cmd/dot-agents workflow plan` — list canonical plans
- `go run ./cmd/dot-agents workflow tasks <plan>` — show tasks for a plan
- `go run ./cmd/dot-agents workflow health` — workflow health snapshot
- `go run ./cmd/dot-agents workflow drift` — detect cross-repo drift (read-only)
- `go run ./cmd/dot-agents kg health` — knowledge graph health
- `go run ./cmd/dot-agents kg query <intent>` — query the KG
- `go run ./cmd/dot-agents kg lint` — check graph integrity

Write commands (use when the wave you just implemented adds or changes these):
- `go run ./cmd/dot-agents workflow checkpoint` — write a checkpoint
- `go run ./cmd/dot-agents workflow advance <plan> <task> <status>` — advance a task
- `go run ./cmd/dot-agents kg ingest <source>` — ingest a source
- `go run ./cmd/dot-agents kg warm` — sync hot notes to warm layer

Approval-gated or fixture-gated commands (high-value for coverage, but only run when the wave justifies them and the safety rules allow it):
- `go run ./cmd/dot-agents workflow verify record` — writes verification history
- `go run ./cmd/dot-agents workflow prefs set-local <key> <value>` / `set-shared <key> <value>` — writes outside the repo or queues proposals
- `go run ./cmd/dot-agents review approve <id>` / `reject <id>` — requires pending proposals
- `go run ./cmd/dot-agents workflow fanout` / `workflow merge-back` — requires canonical plan/task fixtures and writes workflow artifacts
- `go run ./cmd/dot-agents workflow sweep --apply` — mutates managed-project workflow state
- `go run ./cmd/dot-agents kg setup` / `kg sync` — writes to `KG_HOME`, outside the repo

**Sandboxed destructive / mutating CLI tests (when the wave requires exercising the above):**
- **Goal:** Prove behavior of commands that overwrite managed files, rewrite manifests, or write under `~/.agents` / `KG_HOME` **without** risking your real project or global config.
- **Pattern:** Use disposable directories and env overrides — never run destructive commands against production paths as part of the loop unless the user explicitly approves that risk.
  1. `mkdir` a fresh temp root (e.g. `$(mktemp -d)` or a named scratch dir).
  2. Copy or clone the **project** into a subdirectory (preserve `.agentsrc.json` / `.agents/` if the command needs them). For a minimal fixture, a `go test` temp repo pattern is enough.
  3. Copy **`~/.agents`** into `"$TMP/agents-home"` (or create a minimal stub `config.json` + only the buckets the command touches) so **`AGENTS_HOME`** points at the copy: `export AGENTS_HOME="$TMP/agents-home"`.
  4. If the command reads/writes graph or state outside the repo, also redirect **`KG_HOME`**, **`XDG_STATE_HOME`**, or other vars the subcommand uses (check `internal/config` / command help) so nothing lands in the real home.
  5. Run the CLI with **cwd = temp project copy** (dot-agents resolves the project from the current directory). Example: `AGENTS_HOME="$TMP/agents-home" sh -c 'cd "$TMP/project-copy" && go run /absolute/path/to/dot-agents/cmd/dot-agents status'` — or install `dot-agents` and run `dot-agents status` from that directory with the same env. Adjust the `go run` path to your checkout.
  6. Log under `## CLI Traces`: note `sandbox: AGENTS_HOME=... project=...` so the trace is reproducible and clearly not your main machine state.
- **Automated tests:** Prefer the same idea inside `*_test.go` with `t.TempDir()`, `t.Setenv("AGENTS_HOME", ...)`, and a tiny git init fixture — that is the default for regression locks; manual sandboxes are for integration traces the tests do not cover yet.

Pick the commands most relevant to the wave you just worked on. Run at least one read-only command per iteration.
If the relevant command set is already well-covered, prefer the next uncovered scenario family rather than repeating another low-signal success trace.
Bias the selection toward commands that are nearest to the changed code path:
- command wiring or planner state changes: prefer `workflow health`, `workflow status`, `workflow orient`, `workflow plan`, `workflow tasks`, or the directly affected write path
- KG/CRG bridge changes: prefer `kg health`, `kg query`, `kg build/update`, `kg postprocess`, `kg flows`, or `kg bridge`
- cross-project workflow changes: prefer `workflow drift`, `workflow sweep`, `status`, and `doctor`
- only fall back to the generic `status` -> `doctor` bootstrap stack when no closer runnable surface exists
Integration checks are especially valuable when they cross subsystem boundaries. Prefer one of these sub-buckets:
- Bootstrap stacks:
  `status` -> `doctor` -> `workflow health`
  `add` -> `status` -> `doctor` -> `workflow status`
  `kg setup` -> `kg health` -> `kg queue`
- Mutation-and-reconciliation stacks:
  managed-file mutation or refresh-style operation -> `status` -> `doctor` -> managed-file inspection
  overwrite detection -> restore/regenerate -> `status` -> `doctor` -> `workflow health`
  `workflow drift` -> `workflow sweep` -> `workflow health`
- Analysis-and-readback stacks:
  `kg ingest` -> `kg health` -> `kg query`
  `kg build` or `kg update` -> `kg postprocess` -> `kg code-status` / `kg flows`
  `workflow graph health/query` -> `kg bridge health/mapping/query`
- Closeout-and-evidence stacks:
  `workflow checkpoint` -> `workflow health` -> `workflow log`
  `workflow verify record` -> `workflow checkpoint` -> `workflow log` -> `workflow health`
  `kg warm` -> `kg link add/list/remove` -> `workflow checkpoint`

### Handling command issues
When a command fails or produces unexpected output, classify it:
- **`[impl-bug]`** (your code change broke something): fix it in the same iteration, re-test, re-commit
- **`[tool-bug]`** (command was already broken before your change): document it in `.agents/active/<bug-name>.plan.md`, add to skip-list, do NOT attempt to fix it in this iteration
- **`[missing-feature]`** (command doesn't exist yet or is a stub): note it in loop-state.md under `## CLI Observations`, do not treat as a blocker
- **`[ok-empty]`** (empty-state or no-op, but expected): keep as a useful trace, not a failure
- **`[ok-warning]`** (worked, but output or UX still needs attention): note why in `## CLI Observations`
- **`[retry-recovered]`** (first result required a fix or rerun before final success): also add an `## Error Log` entry
- **`[blocked]`** (architectural, environment, or dependency blocker): record the blocker and stop escalating within the same iteration

Always log the exact command, output, scenario tags, expectation (`expected`, `unexpected`, or `informative-nonblocking`), follow-on action, and classification under `## CLI Traces` in loop-state.md.
If a command chain mixes classifications, add a `Commands:` line with per-command statuses so `## Command Coverage` can be reconciled mechanically.

## Safety Guardrails — HARD RULES
- Do NOT run `dot-agents refresh`, `dot-agents install`, or `dot-agents install --generate` — these can overwrite managed files
- Do NOT modify `.agentsrc.json` manually — only through Go command paths
- Do NOT start architectural redesigns or multi-phase refactors — if a wave item requires one, write the analysis to `.agents/active/<name>.plan.md`, add it to the skip-list, and pick the next wave
- Do NOT attempt to fix bugs in the dot-agents tool itself during implementation waves — document the bug in `.agents/active/<bug-name>.plan.md` and move on
- Do NOT run commands that write outside the repo (e.g., writing to `~/.agents`) without explicit user approval — if you **must** cover a mutating path, use a **sandbox** (temp copy of the project + `AGENTS_HOME` / `KG_HOME` pointing at temp dirs) and document it in the trace; do not mutate real `~/.agents` or the user’s live project tree in the loop
- Maximum 10 files changed per iteration — if scope grows beyond that, split the work and commit what you have

## Iteration End
17. Self-review: run `git diff` on any unstaged changes and `git diff --cached` on the staged iteration, then fix obvious issues before committing
18. If you hit a repeatable pattern, gotcha, or correction: update or create `.agents/lessons/<lesson-name>/LESSON.md` and add it to `.agents/lessons/index.md`
19. Append a structured entry to `## Iteration Log` in loop-state.md using this exact format:
    ```
    ### Iteration N — YYYY-MM-DD HH:MM
    - wave: <plan-name>
    - item: <specific checklist item text>
    - scenario_tags: [tag-1, tag-2]
    - feedback_goal: <the concrete question the CLI run was meant to answer>
    - files_changed: N
    - lines_added: N
    - lines_removed: N
    - tests_added: N
    - tests_total_pass: true/false
    - retries: N (compile/test failures fixed before final commit)
    - commit: <short hash>
    - scope_note: "on-target" | "expanded: <reason>" | "split: <reason>"
    - summary: <one-line description of what was done>
    ```
    Get file/line counts from `git diff --cached --stat` before the final commit. Prefer one commit that includes code + loop-state/history updates; avoid standalone `loop-state:` follow-up commits.
20. Append a self-assessment block to the same iteration entry:
    ```
    Self-assessment:
    - read_loop_state: yes/no
    - one_item_only: yes/no
    - committed_after_tests: yes/no
    - tests_positive_and_negative: yes/no (focused tests exercised both success and intended failure paths for the changed surface, or N/A if change cannot fail)
    - ran_cli_command: yes/no
    - exercised_new_scenario: yes/no
    - cli_produced_actionable_feedback: yes/no
    - linked_traces_to_outcomes: yes/no
    - stayed_under_10_files: yes/no
    - no_destructive_commands: yes/no
    ```
    Be honest — these are for post-hoc analysis, not grading.
21. Under `## CLI Traces` in loop-state.md, log every `go run ./cmd/dot-agents` invocation with:
    - A trace label, for example `Trace: workflow-status-clean-repo`
    - The exact command
    - Scenario tags
    - Feedback goal
    - Output summary (truncate long output, keep errors verbatim)
    - Expectation: `expected`, `unexpected`, or `informative-nonblocking`
    - Follow-on: `none`, `documented`, `fixed same iteration`, `deferred`, or similar
    - Classification: `[ok]`, `[ok-empty]`, `[ok-warning]`, `[retry-recovered]`, `[impl-bug]`, `[tool-bug]`, `[missing-feature]`, or `[blocked]`
    - If multiple commands form one integration scenario, give them a shared scenario tag and note the chain in the trace labels or follow-on text
    - If commands in the chain had different outcomes, record per-command classifications explicitly instead of collapsing them into one label
22. Update `## Command Coverage` in loop-state.md: for each command you ran, set Tested=yes, Last Iteration=N, Status=<classification>
    - Reconcile the table against the commands listed in this iteration's trace before finishing
23. Update `## Scenario Coverage` in loop-state.md: for each scenario you exercised, set Covered=yes, Last Iteration=N, and add a short note about what evidence was captured
    - Update the matching family bucket, not just the first row that seems close
    - When a scenario is one half of a useful pair, note which side was exercised and what still remains uncovered
    - Use tag names exactly as written in the table; if a new tag is needed, add the row first
    - For integration scenarios, describe the command chain, the stack sub-bucket, and whether the subsystems stayed coherent end-to-end
24. If any compile errors, test failures, CLI errors, or retry-recovered detours occurred during this iteration, append to `## Error Log`:
    ```
    ### Iteration N
    - type: test-failure | compile-error | cli-error
    - detail: <what failed>
    - resolution: <what fixed it>
    - retries: N
    ```
    - If `retries: N` is greater than 0 in the iteration log, an Error Log entry is mandatory
25. Under `## CLI Observations` in loop-state.md, note any patterns:
    - Commands that feel awkward or require too many steps
    - Output that is confusing or missing useful info
    - Features that would make the workflow smoother
    - UX friction (e.g., unnecessary prompts, unclear errors)
24. Update `## Current Position`, `## Loop Health`, `## Next Iteration Playbook`, and `## Analysis Readiness` in loop-state.md when new evidence changes what the later analysis phase can conclude
    - Rewrite these sections in place; do not append a second candidate-path block, duplicate priorities, or stale summary paragraphs

## What NOT to spend time on
- Workspace hygiene beyond what the current wave requires
- Skill transforms, imports, or promotions
- Schema or manifest changes unrelated to the current wave
- Reading entire spec documents on every iteration — use loop-state.md as your memory
```

---

## Design Notes

This prompt addresses specific failure modes observed in Codex sessions `019d7a6d` and `019d7a9d`:

| Failure Mode | Mitigation |
|---|---|
| Agents ran `refresh`/`install --generate` and clobbered files | Hard blocklist in Safety Guardrails |
| Scope drifted into skill transforms, schema work | Explicit "What NOT to spend time on" section |
| No commits — massive dirty git state | Commit after every passing test cycle |
| Burned rate limits on architectural exploration | "Write a plan and move on" rule |
| No inter-iteration handoff | `loop-state.md` read at start, written at end |
| Markdown plans only, ignored `workflow orient` / canonical YAML | Step 2 runs `workflow orient`; step 8 runs `workflow plan` / `workflow tasks` when YAML exists; storage locations called out at top of prompt |
| "workspace hygiene" too open-ended | Removed entirely; replaced with wave-scoped work |
| Repeated full spec reads burning context | Only read specs if loop-state.md is missing |
| "collected usage traces" was vague | Concrete `## CLI Traces` section with classification tags |
| No live testing of the tool being built | Explicit "Exercising Workflow Commands" step before the final commit |
| Tool bugs conflated with implementation bugs | Mandatory classification: `[impl-bug]` vs `[tool-bug]` vs `[missing-feature]` |
| No structured data for post-hoc analysis | Structured iteration log with metrics, self-assessment, command coverage table |
| Failures/retries invisible in traces | Dedicated `## Error Log` section with type, detail, resolution, retry count |
| No command coverage tracking | Running `## Command Coverage` table updated each iteration |
| Command coverage lacked scenario context | Added `## Scenario Coverage`, scenario tags, expectation field, and richer trace classifications |
| Flat scenario lists encouraged ad hoc tags | Grouped scenario families and paired-state guidance keep coverage systematic |
| Single-command traces missed end-to-end behavior | Added cross-subsystem integration scenarios for multi-command checks spanning workflow, KG, and config surfaces |
| Loop closeout created noisy follow-up `loop-state:` commits | Stage loop-state/history before the final commit and use `git diff --cached --stat` for counts |
| Coverage tables drifted from the actual traces | Require per-command classifications for mixed chains plus an explicit reconciliation pass over `## Command Coverage` |
| `## Next Iteration Playbook` became append-only and contradictory | Rewrite summary/playbook sections in place instead of appending new candidate blocks |
| Recent iterations leaned on repeated `workflow health` checks with low new signal | Added a primary-evidence budget and a rule against consecutive health-only traces unless tightly justified |
| Tests only asserted happy paths for new branching | Steps 11–12 require positive and negative scenarios; step 16 blocks commit when negative coverage is missing for new failure modes; self-assessment `tests_positive_and_negative` |
| Destructive CLI exercises risked real `~/.agents` | Sandboxed destructive CLI section: temp project + `AGENTS_HOME` (and `KG_HOME` etc.) overrides; traces must label the sandbox |
