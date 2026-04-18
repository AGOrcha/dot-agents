# Review-agent — repo project overlay (human gate)

Use this file as **`--prompt-file`** when the delegated role is **review-only**: consume verifier artifacts and **`impl-handoff.yaml`**, decide whether the slice is safe to merge, and persist a **CLI-owned** decision — do **not** hand-author `review-decision.yaml` except in emergencies; prefer **`dot-agents workflow verify record --kind review`** so enums, escalation rules, and the verification log stay aligned with **`schemas/verification-decision.schema.json`**.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Global `~/.agents/profiles/loop-worker.md` | Habits: evidence, scope, closeout cadence (review agents still follow human-judgment steps) |
| Repo project overlay (e.g. `.agents/active/active.loop.md`) | Paths, matrices, guardrails |
| **This file (`review-agent.project.md`)** | Repo wording for **review** turns: what “phase 1 / phase 2” mean here, which gates exist, when to **escalate** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `write_scope`, `feedback_goal` |

Do **not** fold implementation or verifier execution into this prompt unless the bundle explicitly assigns it.

## Decision artifact (CLI-owned)

The command writes or replaces:

`.agents/active/verification/<task_id>/review-decision.yaml`

| Input flag | Maps to |
|------------|---------|
| `--phase1-decision` / `--phase2-decision` | `phase_1_decision` / `phase_2_decision` (`accept` \| `reject` \| `escalate`) |
| (derived) `--overall-decision` optional | `overall_decision` — pessimistic merge: any **reject** → **reject**; else any **escalate** → **escalate**; else **accept** |
| `--failed-gate` (repeatable) | `failed_gates[]` — verifier or gate slugs that failed |
| `--escalation-reason` | **Required** when overall is **escalate** |
| `--reviewer-notes` | Optional free-form notes |
| `--task` | Task id under `.agents/active/delegation/<task>.yaml` (omit when exactly one active delegation is readable) |

The same invocation appends one **lean** line to **`verification-log.jsonl`** (`kind: review`, `status` derived: accept→`pass`, reject→`fail`, escalate→`partial`) with `artifacts` pointing at `review-decision.yaml`.

Iteration logs pick up review fields on `workflow checkpoint --log-to-iter N --role review` by re-reading that YAML.

## Examples

```bash
dot-agents workflow verify record --kind review \
  --task my-task-id \
  --phase1-decision accept --phase2-decision accept \
  --summary "Scoped change matches bundle; ship it"

dot-agents workflow verify record --kind review \
  --phase1-decision reject --phase2-decision accept \
  --failed-gate unit --failed-gate api \
  --summary "Unit and API gates still red on scoped surface"

dot-agents workflow verify record --kind review \
  --phase1-decision escalate --phase2-decision accept \
  --escalation-reason "Security-sensitive diff needs staff review" \
  --summary "Defer merge pending human security pass"
```
