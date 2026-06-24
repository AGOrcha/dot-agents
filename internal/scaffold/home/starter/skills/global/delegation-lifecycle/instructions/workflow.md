# Workflow: Delegation Lifecycle

Use this skill when delegating a task to a sub-agent with a bounded write scope and integrating the result back into the canonical plan.

## 0. Pre-fanout gate (parent / orchestrator) — CANONICAL

This is the **single canonical pre-fanout gate**. Every other skill (orchestrator-session-start preflight / gotchas / workflow / eligible-orientation) points here rather than re-stating it. The orchestrator **MUST** clear all four checks below before any `da workflow fanout`; a failed check **MUST** block the fanout. The whole gate exists because the same wasted-spawn / stale-bundle / missed-caller pattern recurred across the run despite the lessons being written (`.agents/workflow/specs/agent-ops-hardening/design.md` §3.5) — so it is now a hard gate, not advice.

### 0a. Task status vs. shipped PRs — MUST cross-check the forge

`da workflow eligible` reports TASKS.yaml `status`, which drifts behind merged PRs after parallel batches (`[[validate-bundle-against-head]]`). For each task you intend to fanout:

```bash
gh pr list --state merged --search "<task-id>" --json number,title,mergedAt --limit 5
git log --oneline origin/master | grep -iE "(<task-id>|<task-keyword>)" | head -5
```

If the work already shipped, you **MUST NOT** fanout. Run closeout instead (archives artifacts AND auto-advances; do not also call `workflow advance`):

```bash
da workflow delegation closeout --plan <plan-id> --task <task-id> --decision accept
```

### 0b. write_scope MUST exist on HEAD

Bundle write_scope decays as the tree moves under it (`[[validate-bundle-against-head]]`). Before fanout:

1. You **MUST** confirm every file in the proposed `--write-scope` exists on current HEAD.
2. If the task notes assert a premise ("dedup these duplicates", "X has no caller"), you **MUST** re-confirm the premise still holds on `origin/master` — not against a stale local ref (`[[stale-local-master-ref]]`).

### 0c. Caller walk — write_scope MUST include cross-file callers

Author the scope against the actual import/caller graph, not the plan's static file list (`[[bundle-scope-via-code-graph]]`). You **MUST** enumerate callers of every symbol the change moves or alters and fold missed-caller files into write_scope upfront:

```bash
# code-graph first: file_summary per scope file, then callers/tests per symbol
#   mcp__code-review-graph__query_graph_tool  (file_summary | callers_of | tests_for)
# then the reliable textual fallback for unexported Go names (word-boundary anchored):
grep -rln "<symbol>\b" <relevant-dirs>/ --include="*.go"
```

`callers_of` underreports for cobra `RunE` lambdas and test files using type aliases — so the grep pass is mandatory, not optional, for unexported symbols.

### 0d. Coverage-delta forecast — an asserting test outside write_scope MUST fail the gate

A change that alters or deletes a symbol breaks every `*_test.go` that asserts on it. If an asserting test caller falls **outside** the proposed write_scope, the worker cannot fix it within scope and will be forced into a fold-back (the t13 chain burned 3 spawns exactly this way — `[[bundle-scope-via-code-graph]]`). So:

1. For every **changed or deleted** symbol in the scope, list its `*_test.go` callers (`query_graph_tool tests_for` for exported names; the `grep -rln '<symbol>\b' --include='*_test.go'` pass for unexported).
2. Compute the delta: `{test files that assert on the change}  −  {test files already inside write_scope}`.
3. If the delta is **non-empty**, you **MUST** either expand write_scope to include those test files, or **REFUSE the fanout** and re-shape the task. Do NOT fanout a scope that breaks an out-of-scope test — that is a guaranteed fold-back.

This is the canonical statement of the coverage-delta / "write_scope MUST include the tests a change breaks" rule. The delegation brief-template bullet (§ "Brief-template defaults") and the orchestrator AGENT.md both reference it; it is authored only here.

### 0e. No overlapping active delegation — MUST NOT re-fanout

```bash
ls .agents/active/delegation-bundles/ 2>/dev/null
ls .agents/active/delegation/ 2>/dev/null
```

If a bundle for this task already exists, you **MUST NOT** re-fanout — skip to bundle-to-execution with the existing bundle path (a second bundle for one task creates a conflict closeout cannot resolve).

## 1. Fanout

Run from the parent agent to create the delegation contract and bundle in one step:

```bash
da workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --write-scope "<scope-prefix-1>/,<scope-prefix-2>/" \
  --owner "<worker-name>" \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --feedback-goal "<concrete question evidence must answer>" \
  --prompt-file .agents/prompts/loop-worker.project.md \
  --context-file <path/to/spec-or-context-doc> \
  --selection-reason "<why this task now>"
```

Useful additional flags:
- `--scenario-tag <tag>` — repeatable; surfaces in the verifier scoring matrix
- `--regression-artifact <path>` — repeatable; e.g. a testing matrix the verifier should cross-check
- `--validation-queue <path>` — higher-layer validation queue the worker must drain
- `--verifier-sequence <id1,id2>` — override the staged verifier chain when the project's `app_type_verifier_map` default doesn't fit
- `--verifier-retry-max <n>` — cap how many auto-fix iterations the verifier may run before escalation
- `--require-negative-coverage` — force verifier to demand a regression test for the bug being fixed
- `--skip-evidence-check` — only when intentionally proceeding without a scope sidecar

Expected effects of fanout:
- creates `.agents/active/delegation/<task-id>.yaml` (the contract)
- creates `.agents/active/delegation-bundles/<delegation-id>.yaml` (the bundle the worker reads)
- validates that the plan and task exist
- rejects overlapping active write scopes
- advances the task to `in_progress`

For direct (non-delegated) orchestrator work that still needs a contract for the audit trail, use:

```bash
da workflow contract create --plan <plan-id> --task <task-id> --direct --write-scope <...>
```

### Brief-template defaults (every fanout bundle)

Bundle every delegation with these six defaults unless the task explicitly overrides them. They encode lessons that recurred across the run (`.agents/workflow/specs/agent-ops-hardening/design.md` §3.10); each links the owning lesson rather than re-authoring it.

1. **Cognitive complexity ≤ 15.** New/changed functions must stay under Sonar's cognitive-complexity 15 (extract helpers). Note that `gocognit` ≠ Sonar S3776 — pass the Sonar gate, not the linter (`[[sonarcloud-gate-mechanics]]`, `[[gates-must-be-locally-reproducible]]`).
2. **write_scope MUST include the tests a change breaks.** This is the coverage-delta rule — authored once in **§ 0d** of this file. The bundle inherits it: if a change asserts-breaks a `*_test.go` outside scope, the scope was wrong, not the worker (`[[bundle-scope-via-code-graph]]`). Do not restate the rule in the brief; reference § 0d.
3. **Read-only boundary for plan/review tasks.** A task whose intent is analysis, planning, or review gets a read-only brief — no `Edit`/`Write` in the bundle's tool expectation. Route un-bounded hygiene to `general-purpose`, not `loop-worker` (`[[loop-worker-vs-general-purpose]]`).
4. **A skipped/tagged cross-platform test is UNVERIFIED until its CI shard is green.** A `t.Skip` or build-tagged (e.g. Windows) test passing locally proves nothing about the skipped platform — the worker must treat it as unverified until the matching CI shard goes green, never as covered (`[[match-ci-test-flags-locally]]`).
5. **Sanitize FS paths for Windows.** Generated paths/filenames must avoid `:`, `\`, reserved names, and case collisions; use the cross-platform fs helpers rather than hand-joining (`[[leverage-cross-platform-fs-helpers]]`).
6. **Never `--no-verify`.** The worker MUST NOT bypass pre-push/pre-commit gates. If a gate flakes, fix the gate (it is meant to be locally reproducible), do not skip it (`[[gates-must-be-locally-reproducible]]`).

## 2. Bundle handoff (orchestrator → worker)

After fanout, the success box prints `Bundle: .agents/active/delegation-bundles/<delegation_id>.yaml`. That file is the **source of truth** for the worker. Do not reconstruct the handoff from chat memory.

Before handing off, write any constraints, risks, or session-context that does NOT fit the bundle YAML fields into the matching task's `notes` field in `.agents/workflow/plans/<plan-id>/TASKS.yaml`. The worker reads `workflow tasks <plan-id>` at startup; notes there are guaranteed to be seen.

Spawn the worker with the bundle path. Patterns:

- **Native subagent (Claude Code):**
  ```
  Agent(
    description="Implement <task_id> in <plan_id>",
    subagent_type="loop-worker",
    prompt="Delegation bundle: <absolute_bundle_path>",
    mode="auto"
  )
  ```
  The `loop-worker` subagent loads its own profile — the prompt only needs the bundle path. Pick `loop-worker` ONLY when a bundle exists; for cross-cutting hygiene work without a canonical task, use `general-purpose` instead.

- **Headless loop script** (long-running batch): hand the worker the bundle path via env or arg; suitable for many-iteration runs without an interactive session.

The worker then follows `instructions/bundle-to-execution.md`.

## 3. Verifier turn(s) (between worker and parent closeout)

The verifier sequence is project-defined, not hard-coded. `.agentsrc.json` declares `verifier_profiles` (a map of profile id → profile definition) and `app_type_verifier_map` (which sequence runs for each app_type). A profile can be any verifier type the project needs: a CI-watch profile, a SAST scanner, an integration-test harness, an acceptance-suite runner, a security-review prompt overlay, a maintainer-defined custom agent — anything addressable by id.

After the worker writes `merge-back`, the workflow runtime resolves the task's `app_type` against `app_type_verifier_map` and runs each verifier in the resolved sequence. Each verifier:

1. Reads its inputs (PR ref, target files, prior verifier outputs) per its profile definition.
2. Executes its check (poll CI, run SAST, dispatch a prompt-driven review, etc.).
3. Auto-fixes mechanical issues in its domain when the profile permits (e.g. CI-watch profiles fix coverage <gate / cog complexity / dup literals; SAST profiles re-trigger after a sanitizer change; agent profiles never autofix intent).
4. Writes `.agents/active/verification/<task-id>/<profile-id>.result.yaml` with a terminal `READY` or fold-back signal.
5. Returns a structured fix-up brief to the impl worker for anything architectural/security/spec — never autofixes intent.

Common profile types you may see:
- **CI-watch** (e.g. `pr-ci`): polls `gh pr checks` + the project's quality gate, auto-fixes mechanical signals
- **SAST/security** (e.g. `snyk-watch`, `codeql-watch`): polls the security-analysis surface, brief workers on findings
- **Acceptance / integration** (e.g. `unit`, `api`, `batch`, `streaming`, `ui-e2e`): executes the project's domain test suite for the relevant app_type
- **Agent / prompt-overlay** (e.g. a custom `<project>-spec-conformance` profile): dispatches a prompt-driven check that emits structured findings

The worker exits cleanly at merge-back; the verifier sequence owns the PR-readiness loop end-to-end. The parent sees a single terminal signal per profile (and a sequence-aggregate READY) rather than a stream of mechanical-fix round trips.

If the project has **not** registered any verifier_profile for the task's app_type, the impl worker owns the readiness loop itself (fallback mode — poll `gh pr checks` + project's quality gate, auto-fix mechanical issues, do not allowlist new code).

## 4. Merge-back (worker)

Run after delegated implementation + verification finish:

```bash
da workflow merge-back \
  --task <task-id> \
  --summary "Implemented X by doing Y; tests added at Z" \
  --verification-status pass \
  --integration-notes "No conflicts, parent should accept closeout" \
  --commit-state
```

Expected effects:
- creates `.agents/active/merge-back/<task-id>.md`
- records git-diffed changed files
- marks the delegation as `merge-back-complete`
- `--commit-state` stages and commits the workflow-state mutation (iteration-close integration)

**Merge-back alone does NOT advance task status.** The parent's closeout step does.

## 5. Closeout (parent)

Run from the parent after reviewing the merge-back artifact:

```bash
da workflow delegation closeout \
  --plan <plan-id> \
  --task <task-id> \
  --decision accept   # or reject | escalate
```

Closeout BOTH advances task status to `completed` AND archives the delegation contract + bundle + merge-back. Do not also call `workflow advance` for delegated work — that path is for direct (non-delegated) iteration.

For a higher-confidence accept/reject decision, run the gate command first:

```bash
da workflow delegation gate --plan <plan-id> --task <task-id>
```

This evaluates the task-local verification evidence (verifier output + any review-lens results) into a recommended outcome the parent can rubber-stamp.

## 6. Orient / inspect delegation state

At any point:

```bash
da workflow orient
da workflow status
```

These surface the `# Delegations` section, active counts, pending intents, and merge-back queue.

## Coordination intents

If a worker needs to signal the parent mid-execution, set `pending_intent` on the delegation contract. Valid values:

- `status_request` — worker is blocked and wants direction
- `review_request` — worker wants a human/parent review pass before continuing
- `escalation_notice` — worker is surfacing scope or correctness drift
- `ack` — worker acknowledges parent guidance

The parent reads pending intents via `workflow orient` / `workflow status` and responds by editing the contract or chaining the next skill (`iteration-close`, lens-reviewer, etc.).
