# Impl-agent — repo project overlay (implementation slice)

Use this file as **`--prompt-file`** (or alongside inline `--prompt`) when the delegated role is **implementation-only**: code changes inside `write_scope`, plus a durable handoff for verifiers. It is **not** a substitute for the global `loop-worker` profile and **not** what `bin/tests/ralph-cursor-loop` assembles (that script is **Pattern E — loop-worker** with verify / checkpoint / merge-back).

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Global `~/.agents/profiles/loop-worker.md` | Habits: scope, tests, evidence, closeout cadence |
| Repo project overlay (e.g. `.agents/active/active.loop.md`) | Paths, matrices, guardrails |
| **This file (`impl-agent.project.md`)** | Repo wording for **impl** turns: implement, commit, emit **`impl-handoff.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `write_scope`, `feedback_goal` |

Do **not** fold verifier or review duties into this prompt unless the bundle explicitly assigns them.

## Handoff artifact

Write or update:

`.agents/active/verification/<task_id>/impl-handoff.yaml`

Minimal fields (verification and pre-verifier gates consume these):

| Field | Meaning |
|-------|---------|
| `task_id` | Canonical task id (matches bundle) |
| `commit_sha` | HEAD after your impl commit(s) |
| `write_scope_touched` | Repo-relative paths you actually changed (subset of bundle `write_scope` is OK; list truthfully) |
| `ready_for_verification` | `true` only when implementation is complete and the tree is in a state verifiers should evaluate |
| `tests_unchanged_justified` | Optional bool. Set `true` **only** when you did not add/modify tests under `write_scope_touched` but policy allows it (e.g. doc-only slice); omit or `false` when tests were updated |
| `impl_notes` | Short, factual summary for verifier cold-start |

Verifiers and reviewers use `write_scope_touched` and `tests_unchanged_justified` to apply the TDD / negative-path policy without re-parsing chat prose.
