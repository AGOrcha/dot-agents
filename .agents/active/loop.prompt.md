# Automated Work Looper Prompt

Copy the prompt below into Codex as: `/loop 1hr <prompt>`

---

## Prompt

```
For the specs in progress: docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md and docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md

## Iteration Start
1. Read `.agents/active/loop-state.md` for prior iteration context (skip if missing)
2. Run `git status --short` to see current dirty state — if there are uncommitted changes from a prior iteration, review and commit them first
3. Skim the two driving specs only if loop-state.md doesn't already summarize the current position
4. Identify the current scenario tags before selecting work. Reuse existing tags from `## Scenario Coverage` when possible, or add precise new ones such as `clean-repo`, `dirty-repo`, `no-canonical-plan`, `blocked-plan-set`, `kg-pre-postprocess`, `kg-postprocess-complete`, `retry-recovered`, or `write-command-path`

## Wave Selection
5. Use the plan-wave-picker skill (`.agents/skills/plan-wave-picker/`) to select the next wave from `.agents/active/*.plan.md`
   - Priority order: in-progress waves with unchecked items > waves with all dependencies complete > new waves
   - Skip plans tagged as blocked, waiting on external input, or listed in the skip-list section of loop-state.md
   - A plan's Status header (e.g., "Status: Completed") is authoritative — unchecked `- [ ]` items on a completed plan are stale, not real work
   - Prefer implementation waves over architectural/research waves
   - If no actionable wave exists, write that finding to loop-state.md and stop

## Implementation (ONE item per iteration)
6. Pick the next single unchecked item from the selected wave's plan
7. Implement the code change — keep scope tight, touch minimal files
8. Run focused tests first: `go test ./<changed-packages>`
9. Run regression: `go test ./...`
10. If tests pass, commit immediately:
   - Format: `<area>: <what changed>`
   - Example: `kg: add deterministic query for entity lookup`
11. If tests fail, fix the failure before moving on — do not leave red tests uncommitted

## Exercising Workflow Commands (after each implementation commit)
After committing implementation work, exercise relevant `go run ./cmd/dot-agents` commands as a live integration test. This is real product testing — treat the results seriously.

Analysis-oriented trace goals:
- Prefer commands or repo states that add new evidence, not just repeated happy-path confirmation
- If the current iteration only hits `[ok]` paths, try to include one safe expected empty-state, warning-state, or other non-destructive edge case
- Favor uncovered scenario/command combinations over duplicate runs when choosing what to exercise

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

Pick the commands most relevant to the wave you just worked on. Run at least one read-only command per iteration.

### Handling command issues
When a command fails or produces unexpected output, classify it:
- **`[impl-bug]`** (your code change broke something): fix it in the same iteration, re-test, re-commit
- **`[tool-bug]`** (command was already broken before your change): document it in `.agents/active/<bug-name>.plan.md`, add to skip-list, do NOT attempt to fix it in this iteration
- **`[missing-feature]`** (command doesn't exist yet or is a stub): note it in loop-state.md under `## CLI Observations`, do not treat as a blocker
- **`[ok-empty]`** (empty-state or no-op, but expected): keep as a useful trace, not a failure
- **`[ok-warning]`** (worked, but output or UX still needs attention): note why in `## CLI Observations`
- **`[retry-recovered]`** (first result required a fix or rerun before final success): also add an `## Error Log` entry
- **`[blocked]`** (architectural, environment, or dependency blocker): record the blocker and stop escalating within the same iteration

Always log the exact command, output, scenario tags, expectation (`expected`, `unexpected`, or `informative-nonblocking`), follow-on action, and classification under `## CLI Traces` in loop-state.md

## Safety Guardrails — HARD RULES
- Do NOT run `dot-agents refresh`, `dot-agents install`, or `dot-agents install --generate` — these can overwrite managed files
- Do NOT modify `.agentsrc.json` manually — only through Go command paths
- Do NOT start architectural redesigns or multi-phase refactors — if a wave item requires one, write the analysis to `.agents/active/<name>.plan.md`, add it to the skip-list, and pick the next wave
- Do NOT attempt to fix bugs in the dot-agents tool itself during implementation waves — document the bug in `.agents/active/<bug-name>.plan.md` and move on
- Do NOT run commands that write outside the repo (e.g., writing to ~/.agents) without explicit user approval
- Maximum 10 files changed per iteration — if scope grows beyond that, split the work and commit what you have

## Iteration End
12. Self-review: run `git diff` on any uncommitted changes, fix obvious issues, then commit
13. If you hit a repeatable pattern, gotcha, or correction: update or create `.agents/lessons/<lesson-name>/LESSON.md` and add it to `.agents/lessons/index.md`
14. Append a structured entry to `## Iteration Log` in loop-state.md using this exact format:
    ```
    ### Iteration N — YYYY-MM-DD HH:MM
    - wave: <plan-name>
    - item: <specific checklist item text>
    - scenario_tags: [tag-1, tag-2]
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
    Get file/line counts from `git diff --stat` after committing.
15. Append a self-assessment block to the same iteration entry:
    ```
    Self-assessment:
    - read_loop_state: yes/no
    - one_item_only: yes/no
    - committed_after_tests: yes/no
    - ran_cli_command: yes/no
    - exercised_new_scenario: yes/no
    - linked_traces_to_outcomes: yes/no
    - stayed_under_10_files: yes/no
    - no_destructive_commands: yes/no
    ```
    Be honest — these are for post-hoc analysis, not grading.
16. Under `## CLI Traces` in loop-state.md, log every `go run ./cmd/dot-agents` invocation with:
    - A trace label, for example `Trace: workflow-status-clean-repo`
    - The exact command
    - Scenario tags
    - Output summary (truncate long output, keep errors verbatim)
    - Expectation: `expected`, `unexpected`, or `informative-nonblocking`
    - Follow-on: `none`, `documented`, `fixed same iteration`, `deferred`, or similar
    - Classification: `[ok]`, `[ok-empty]`, `[ok-warning]`, `[retry-recovered]`, `[impl-bug]`, `[tool-bug]`, `[missing-feature]`, or `[blocked]`
17. Update `## Command Coverage` in loop-state.md: for each command you ran, set Tested=yes, Last Iteration=N, Status=<classification>
18. Update `## Scenario Coverage` in loop-state.md: for each scenario you exercised, set Covered=yes, Last Iteration=N, and add a short note about what evidence was captured
19. If any compile errors, test failures, CLI errors, or retry-recovered detours occurred during this iteration, append to `## Error Log`:
    ```
    ### Iteration N
    - type: test-failure | compile-error | cli-error
    - detail: <what failed>
    - resolution: <what fixed it>
    - retries: N
    ```
20. Under `## CLI Observations` in loop-state.md, note any patterns:
    - Commands that feel awkward or require too many steps
    - Output that is confusing or missing useful info
    - Features that would make the workflow smoother
    - UX friction (e.g., unnecessary prompts, unclear errors)
21. Update `## Current Position`, `## What's Next`, and `## Analysis Readiness` in loop-state.md when new evidence changes what the later analysis phase can conclude

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
| "workspace hygiene" too open-ended | Removed entirely; replaced with wave-scoped work |
| Repeated full spec reads burning context | Only read specs if loop-state.md is missing |
| "collected usage traces" was vague | Concrete `## CLI Traces` section with classification tags |
| No live testing of the tool being built | Explicit "Exercising Workflow Commands" step after each commit |
| Tool bugs conflated with implementation bugs | Mandatory classification: `[impl-bug]` vs `[tool-bug]` vs `[missing-feature]` |
| No structured data for post-hoc analysis | Structured iteration log with metrics, self-assessment, command coverage table |
| Failures/retries invisible in traces | Dedicated `## Error Log` section with type, detail, resolution, retry count |
| No command coverage tracking | Running `## Command Coverage` table updated each iteration |
| Command coverage lacked scenario context | Added `## Scenario Coverage`, scenario tags, expectation field, and richer trace classifications |
