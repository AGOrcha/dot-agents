# CLI-runner verifier — repo project overlay (built-binary smoke slice)

Use this file as **`--prompt-file`** when the delegated role is **CLI end-to-end smoke verification only**: read **`impl-handoff.yaml`**, build the `da` binary **fresh** from the tree, run the **`scripts/verify.sh` smoke floor** plus **task-scoped** invocations of the command(s) the change touched, then emit **`.agents/active/verification/<task_id>/cli-runner.result.yaml`** validated against **`schemas/verification-result.schema.json`**.

This profile is the end-to-end half of the go-cli verifier sequence: `unit` proves the code is correct (`go test`); **`cli-runner` proves the wired-up binary actually runs**. Both are pre-merge ISP verifier-class profiles — this is **not** the post-merge PR-watch agent.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Shared stage instruction base (parent-resolved; not an `app_type` profile) | Stable evidence, scope, and sandbox discipline; no merge-back or parent closeout |
| Stage-safe repo project overlay | Paths, command matrices, guardrails |
| **This file (`verifiers/cli-runner.project.md`)** | Repo wording for **cli-runner** turns: build the binary, run the smoke floor + scoped invocations, record **`cli-runner.result.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `feedback_goal`; impl scope is **not** yours |

Do **not** implement or fix product code in this role unless the bundle explicitly widens `write_scope`. Prefer failing the verification run with a clear `summary` and `status: fail` when the binary will not build or a required invocation regresses.

## Preconditions

1. **Cold-start from** `.agents/active/verification/<task_id>/impl-handoff.yaml` (see Phase 8 impl-handoff in `docs/LOOP_ORCHESTRATION_SPEC.md`).
2. Confirm `ready_for_verification: true` before treating a green run as meaningful; if `false`, record `status: partial` or `unknown` with explanation.
3. Use **`write_scope_touched`** to choose the **task-scoped** invocations: for each touched path that maps to a CLI command (a file under `commands/<area>/…` → the `da <area> …` command), exercise that command. If the mapping is ambiguous, fall back to the smoke floor alone and say so in `summary`.

## Commands (build fresh → floor → scoped)

**Order:**

1. **Build (required, always fresh):**
   `go build -o ./bin/da ./cmd/da`
   A build failure is a terminal `status: fail` — the tree does not produce a working binary; record and stop.
2. **Smoke floor (required):** `bash scripts/verify.sh`
   The shared CLI smoke harness (locates/uses `./bin/da`, exercises `--version`/`--help`, `status`, `doctor`, `explain`, `workflow`, dry-runs, and expected-failure cases). Any failure here fails the pass even when the touched command itself works — a change must not regress a sibling command.
3. **Task-scoped invocations (required when a touched path maps to a command):** run the changed/new subcommand end-to-end against `./bin/da`.
   - **Positive:** the happy-path invocation exits `0` and emits the expected key output (assert on a stable substring or `--json` field, not the whole stream).
   - **Negative:** where the change introduces a failure mode, run the invalid invocation and assert a **non-zero** exit and a clear error (mirror `scripts/verify.sh`'s `expect_success=false` cases).

If the build or the floor fails, you may skip the scoped invocations but must set `status: fail` and explain in `summary`.

## Result artifact

**Path:** `.agents/active/verification/<task_id>/cli-runner.result.yaml`

Minimal shape (schema-enforced):

| Field | Value |
|-------|--------|
| `schema_version` | `1` |
| `task_id` | Same as bundle / impl-handoff |
| `parent_plan_id` | Canonical plan id |
| `verifier_type` | `cli-runner` |
| `status` | `pass` \| `fail` \| `partial` \| `unknown` |
| `summary` | What built/ran, key invocations + outputs asserted, failures |
| `recorded_at` | RFC3339 timestamp |
| `commands` | The build line, the `scripts/verify.sh` line, and each scoped invocation |
| `artifact_paths` | Optional: smoke log path, captured command output, if saved |

Optional: `delegation_id`, `recorded_by` when tied to fanout or automation.

## Evidence classification

Classify the verification story in prose (and optionally in `summary`): `ok`, `ok-warning`, `impl-bug`, `tool-bug`, `missing-feature`, `blocked` — align with Phase 8 taxonomy in `docs/LOOP_ORCHESTRATION_SPEC.md`. A binary that builds and smokes clean but is missing the task's intended command is `missing-feature`, not `ok`.
