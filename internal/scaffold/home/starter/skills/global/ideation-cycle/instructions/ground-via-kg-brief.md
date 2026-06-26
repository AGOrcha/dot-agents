# Step 1: Ground — reuse `kg-brief`, don't reinvent it

The grounding stage of `ideation-cycle` **is** the `kg-brief` molecule. Do not author a
separate baseline scan — that would duplicate `kg-brief` and split the briefing into two
divergent shapes. The whole point of the role-split is that grounding is a single shared
stage both `kg-ideate` and `ideation-cycle` consume.

## Two invocation modes

### A. Dispatched from `kg-ideate` (the common case)

`kg-ideate` Phase 1 (`kg-brief`) has already produced the briefing block before Phase 2
triages decisions and dispatches a hard fork here. **Consume that briefing — do not re-run
grounding.** It is passed in (e.g. `--brief <path>` or the orientation context the
dispatcher hands down). The fork you are resolving is one decision inside the spec
`spec-scaffold` is authoring; the briefing is its grounding.

If the passed briefing is stale or missing the slice this fork needs, request a scoped
`kg-brief` re-run on the fork's topic rather than improvising a parallel scan.

### B. Standalone (one-off design question)

No briefing exists yet. Invoke `kg-brief` on the fork's topic first (or run its traversal
inline), producing the same structured briefing block. Then proceed to step 2 against it.
A standalone `ideation-cycle` run is grounded by exactly the same primitive a `kg-ideate`
run is — that is the contract.

## What `kg-brief` gives you (the briefing block)

`kg-brief` traverses, per its own SKILL, in this shape (consume it; do not re-derive it):

- **Prior Decisions** — KG decision-type nodes + §C proposal IDs, with rationale source.
- **Research Findings** — research-corpus §A/§B entries by relevance.
- **Contradictions** — `claim -[:contradicts]-> claim` edges (or `[adapter-absent]` /
  competing-decisions fallback when the citation adapter is absent).
- **Applicable Lessons** — `.agents/lessons/` matches.
- **Gaps** — what the KG has no prior decision on (these are candidate forks).
- **Prior Spec / Plan Overlap** — specs/plans sharing scope or terminology.
- **Impact Radius** — files/functions when the topic named code.

## Output of this step

The briefing block (reused or freshly produced via `kg-brief`), plus a short note on which
section the fork lives in:

- A fork usually originates from the **Gaps**, the **Contradictions**, or an **Open
  Question** a prior spec left. Tag where it came from so step 2 frames it against the
  right baseline row.
- Distinguish **ratified** prior decisions (constraints — don't re-open without new
  evidence) from **`[PROPOSED]`** ones (still forks — they need ratification in step 6).

## Rules

- **Reuse, never reinvent.** If you find yourself grepping proposals/specs/lessons by hand
  here, stop — that is `kg-brief`'s job. Invoke it (mode B) or consume its output (mode A).
- **Cite, don't paraphrase from memory.** Forks must trace to a briefing row, naming the
  KGNote ID / proposal ID / spec path. A fork built on a misremembered prior decision
  wastes the whole cycle.
- **A fork with no prior artifact is the strongest signal** — "we keep re-explaining this"
  with nothing written down is precisely what must graduate into a ratified decision. It
  shows up as a Gap in the briefing.
