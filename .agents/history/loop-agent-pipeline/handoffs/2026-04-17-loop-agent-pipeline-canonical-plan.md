# Handoff: Loop Agent Pipeline Canonical Plan

**Created:** 2026-04-17
**Status:** Paused — pick up from here

---

## Where I Left Off

I was working in the `loop-agent-pipeline` spec thread and finished the second planning pass. The last concrete change was creating `plan-iter.2.md` and syncing `decisions.1.md` so it no longer treats D2.a/D3.a as open.

I stopped at the exact point where the next artifact should be the canonical workflow plan/task files, not more speculative spec prose. The explicit next step is:

1. generate canonical `PLAN.yaml` / `TASKS.yaml` directly from `plan-iter.2.md`
2. write a proper human-readable planning document that mirrors those canonical artifacts cleanly

---

## The Plan

Primary planning artifacts now in place:

- `.agents/workflow/specs/loop-agent-pipeline/plan-iter.1.md`
- `.agents/workflow/specs/loop-agent-pipeline/decisions.1.md`
- `.agents/workflow/specs/loop-agent-pipeline/plan-iter.2.md`
- `.agents/workflow/specs/loop-agent-pipeline/review.1.agent-thoughts.md`
- `.agents/workflow/specs/loop-agent-pipeline/review.1.human-thoughts.md`
- `review.1.txt`

`plan-iter.2.md` is the authoritative source for the next conversion step. Its shape is:

- inline resolution of D2.a and D3.a
- concrete task graph
- explicit per-task `write_scope`
- dependency graph
- shared hotspot / sequencing notes
- authoring notes for canonical `PLAN.yaml` / `TASKS.yaml`

High-level task graph from iter-2:

- Foundations:
  - `p1-pipeline-control`
  - `p2-impl-agent-surface`
  - `p3a-result-schema`
  - `p8-orchestrator-awareness`
  - `p9-sources-design-fork`
- Verifier/reviewer surfaces:
  - `p3b-unit-verifier`
  - `p3c-api-verifier`
  - `p3d-ui-verifier`
  - `p3e-batch-verifier`
  - `p3f-streaming-verifier`
  - `p4-review-agent`
- Integration / convergence:
  - `p5-iter-log-v2`
  - `p6-fanout-dispatch`
  - `p7-post-closeout`

The intended immediate deliverables for the next session are:

1. `.agents/workflow/plans/loop-agent-pipeline/PLAN.yaml`
2. `.agents/workflow/plans/loop-agent-pipeline/TASKS.yaml`
3. `.agents/workflow/plans/loop-agent-pipeline/loop-agent-pipeline.plan.md`

That human-readable plan doc should essentially restate the canonical plan and tasks in a clean format for repo users, rather than being another exploratory spec.

---

## What's Working

- The stalled Claude session was recovered by reading the JSONL transcript directly.
- D6 was already closed earlier and recorded in `decisions.1.md`.
- D7 is now locked to nested iter-log role blocks (`impl`, `verifiers[]`, `review`).
- D1 is now locked to a flag-first `workflow verify record` writer.
- `plan-iter.2.md` exists and is internally consistent with D1/D6/D7.
- `decisions.1.md` now points forward to canonical plan/task generation instead of another markdown planning pass.

Relevant file anchors:

- `.agents/workflow/specs/loop-agent-pipeline/plan-iter.2.md`
- `.agents/workflow/specs/loop-agent-pipeline/decisions.1.md`

---

## What's Not Working Yet

- There is still no canonical `.agents/workflow/plans/loop-agent-pipeline/` directory.
- No `PLAN.yaml` / `TASKS.yaml` exist yet for `loop-agent-pipeline`.
- No human-readable canonical plan doc exists yet under `.agents/workflow/plans/loop-agent-pipeline/`.
- No code has been implemented for this spec; this is still planning-only.
- No tests were run in this session because the changes were docs/spec only.

---

## My Current Thinking

The next move should stay disciplined:

- Do not reopen the design.
- Treat `plan-iter.2.md` as the conversion source.
- Generate the canonical workflow plan files from it directly.
- Keep the human-readable plan doc aligned with the YAML artifacts, similar to other plan directories in `.agents/workflow/plans/`.

Important planning posture:

- `p9-sources-design-fork` stays a doc-only placeholder inside this plan.
- Do not resurrect the old `p7-external-sources` implementation task.
- Keep `p8-orchestrator-awareness` as its own task instead of hiding it inside `p6-fanout-dispatch`.
- Call out `commands/workflow.go`, `commands/workflow_test.go`, `bin/tests/ralph-orchestrate`, `bin/tests/ralph-pipeline`, and `docs/LOOP_ORCHESTRATION_SPEC.md` as hotspots in task notes.

I would generate the canonical files first, then do one consistency pass against:

- `plan-iter.2.md`
- `decisions.1.md`
- existing plan conventions from `.agents/workflow/plans/global-flag-compliance/`

---

## Decisions I've Made

- **Use iter-2 as the terminal planning pass** — the next artifact should be canonical plan/task files, not more exploratory markdown.
- **D1 is flag-first** — `workflow verify record` should be the canonical writer, not `--from-decision` as the main agent path.
- **D7 uses nested role blocks** — do not flatten iter-log contributions into a generic `role_contributions[]`.
- **D2.a uses stable human-authored slugs** — no auto-id state-tracking in this plan.
- **D3.a stays in the control plane** — TDD-fresh gating belongs in `ralph-pipeline`, not agent self-reporting.
- **External sources are forked out** — main implementation plan only carries `p9-sources-design-fork` as a design-doc placeholder.

---

## Things I Tried That Didn't Work

- The first attempt to inspect the Claude transcript used `python`, which was not available in the shell path. Switched to `python3` and continued.
- Iter-1 by itself was not sufficient to author canonical plan files cleanly because it still left write-scope and integration details underspecified. That is why `plan-iter.2.md` was added.

---

## Next Time I Pick This Up

1. Create `.agents/workflow/plans/loop-agent-pipeline/`.
2. Author `PLAN.yaml` directly from `plan-iter.2.md`.
3. Author `TASKS.yaml` directly from `plan-iter.2.md`, including:
   - task ids/titles
   - statuses
   - `depends_on`
   - `write_scope`
   - `verification_required`
   - hotspot notes where applicable
4. Write `loop-agent-pipeline.plan.md` as the human-readable version of the canonical plan/tasks.
5. Sanity-check the canonical files against:
   - `.agents/workflow/specs/loop-agent-pipeline/plan-iter.2.md`
   - `.agents/workflow/specs/loop-agent-pipeline/decisions.1.md`
6. If there is time after that, decide whether to start the first implementation slice or stop after the planning artifacts land cleanly.

---

## Open Questions

- Whether `PLAN.yaml` should mark the whole plan as `draft` or `active` on first creation. My lean: `active`, since the task graph is now concrete.
- Whether the human-readable canonical plan doc should largely mirror `plan-iter.2.md` or be compressed into a shorter operator-facing summary. My lean: shorter than iter-2, but still explicit about task graph and hotspots.
- Whether to stage the unrelated planning artifacts from this session at the same time:
  - `.agents/workflow/plans/error-message-compliance/`
  - `docs/ERROR_MESSAGE_CONTRACT.md`
  I did not integrate those into the loop-agent-pipeline thread.

---

## Repo State Notes

- Branch: `feature/PA-cursor-projectsync-phase1-extract-293f`
- Working tree currently has untracked planning/spec files, including:
  - `.agents/workflow/specs/`
  - `.agents/workflow/plans/error-message-compliance/`
  - `docs/ERROR_MESSAGE_CONTRACT.md`
  - `review.1.txt`

No commit was made in this session.
