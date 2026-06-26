# Step 1: Ground in the baseline

A fork raised without the prior thinking gets re-litigated from scratch — the exact
"we keep re-explaining this" failure this skill exists to kill. Before enumerating
anything, sweep the repo's durable artifacts so each fork carries what is already known.

## Search order (cheapest signal first)

1. **Proposals** — `~/.agents/proposals/<id>.yaml` (global) and `.agents/proposals/<id>.md`
   (project-local). These hold `[PROPOSED]` decisions not yet ratified.
2. **Specs** — `.agents/workflow/specs/<id>/design.md`. The contract layer: decisions
   already made, open questions already named, deferred items already scoped out.
3. **Plans + tasks** — `.agents/workflow/plans/<id>/` (`PLAN.yaml`, `TASKS.yaml`,
   `<id>.plan.md`). Decisions that got resolved during implementation live in task notes.
4. **Lessons** — `.agents/lessons/<name>/LESSON.md` and the index at
   `.agents/lessons/index.md`. A fork may already be answered by a prior mistake.
5. **Schemas** — `schemas/*.schema.json` and the Go structs they mirror
   (`internal/config/agentsrc.go`, `graphstore.KGNote`, `WorkflowTask`). The current data
   shape constrains which forks are even live.
6. **History** — `.agents/history/<id>/` for archived plans whose decisions still bind.

Dispatch this sweep to a read-only search subagent (Explore / general-purpose) when it
spans many files — the driver keeps the conclusion, not the file dumps.

## Output of this step

A short baseline ledger, two columns:

- **Settled** — decision, where it is recorded, and whether it is ratified or `[PROPOSED]`.
- **Open** — the gap or question the artifact explicitly leaves unresolved (open
  questions in a spec, a `[DEFERRED]` item, a `[PROPOSED]` not yet ratified, a recurring
  re-explanation with no artifact at all).

The **Open** column is the raw material for step 2. Every fork you enumerate must trace
to a row here — if it does not, you are reinventing, not refining.

## Rules

- **Cite, don't paraphrase from memory.** Each baseline row names the file and the line /
  section. A fork built on a misremembered prior decision wastes the whole cycle.
- **Distinguish ratified from `[PROPOSED]`.** A `[PROPOSED]` decision is still a fork
  (it needs ratification in step 6); a ratified decision is a constraint (do not re-open
  it without new evidence).
- **A fork with no artifact is the strongest signal.** "We keep re-explaining this" with
  nothing written down is precisely what must graduate into a spec. Note it as Open.
