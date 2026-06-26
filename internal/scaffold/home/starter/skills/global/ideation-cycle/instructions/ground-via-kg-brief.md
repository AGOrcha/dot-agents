# Step 1: Ground — run `kg-brief` (reuse upstream briefing only if fresh)

`ideation-cycle` **runs `kg-brief`** as its grounding step. `kg-brief` is not something
`kg-ideate` hands down pre-baked unconditionally — it is `ideation-cycle`'s own step 1. Do
not author a separate baseline scan; that would duplicate `kg-brief` and split the briefing
into two divergent shapes. The optimization below lets you skip re-running it when an
upstream-fresh briefing already exists — but the default is: run `kg-brief`.

## Two invocation modes

### A. Dispatched from `kg-ideate` — reuse-by-artifact ONLY if fresh

`kg-ideate` Phase 1 (`kg-brief`) already produced a briefing block before Phase 2 triaged a
hard fork to you. You MAY consume that briefing **by artifact** (0 dispatch hops) — but only
after a freshness check. **A stale brief must never silently propagate.**

**Freshness check (gate before reuse).** Two conditions, BOTH must hold:

1. **`inputs_digest` match.** The briefing carries an `inputs_digest` over a **concrete,
   canonicalized, ordered input set**:
   - the **idea/proposal text** — content hash;
   - the **KG snapshot id** (or query-engine revision) the brief queried against;
   - the **named-query results** the brief consumed — each query id + its result-set hash;
   - the **applicable-lessons set** — lesson ids + each lesson file's content hash;
   - the **cited-artifact set** — every spec/proposal the brief cited, by path + content hash.

   These are ordered and canonicalized, then hashed via the config-v2 `ComputeInputsDigest`
   primitive (`sha256:…`; staleness = digest mismatch — `internal/config/staleness.go`,
   `resolver.go`). Reuse this primitive for coherence; do NOT invent a parallel staleness
   scheme. Recompute over the *current* inputs and compare.
2. **Dependency manifest unchanged.** The brief also records a **dependency manifest** — the
   specific KG nodes / decisions / lessons it actually READ to reach its conclusions. The brief
   is stale if **any manifest entry changed**. This is the operational definition of "a prior
   fork's resolution mutated shared brief state": a prior fork (earlier in this run) that
   re-ratified a decision, wrote a lesson, or changed a KG node the brief read flips that
   manifest entry → re-brief. (This catches staleness the static `inputs_digest` alone can miss
   when a dependency mutates mid-run.)

**On any mismatch (digest OR manifest) → RE-RUN `kg-brief`** scoped to the fork's topic. On a
clean match → consume the upstream briefing by artifact and proceed to step 2. `kg-brief` calls
nothing downstream (a terminal leaf), and deep multi-hop delegation is driver-orchestrated, not
recursively nested — see `instructions/composition.md` for the engineering bounds.

### B. Standalone (one-off design question) — always fresh

No upstream briefing exists. **Always run `kg-brief` fresh** on the fork's topic, producing
the structured briefing block, then proceed to step 2. A standalone `ideation-cycle` run is
grounded by exactly the same primitive a `kg-ideate` run is — that is the contract.

## What `kg-brief` gives you (the briefing block)

`kg-brief` traverses, per its own SKILL, in this shape (run it, or reuse-if-fresh; do not
re-derive it by hand):

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

- **Run or reuse-if-fresh, never reinvent.** If you find yourself grepping
  proposals/specs/lessons by hand here, stop — that is `kg-brief`'s job. Run it (mode B, or
  mode A on staleness) or consume its fresh output (mode A clean match).
- **Never reuse a stale brief.** The freshness gate (digest + shared-state mutation) is not
  optional. When in doubt, re-run `kg-brief` — it is a cheap terminal leaf, and a stale
  brief poisons every fork downstream of it.
- **Cite, don't paraphrase from memory.** Forks must trace to a briefing row, naming the
  KGNote ID / proposal ID / spec path. A fork built on a misremembered prior decision
  wastes the whole cycle.
- **A fork with no prior artifact is the strongest signal** — "we keep re-explaining this"
  with nothing written down is precisely what must graduate into a ratified decision. It
  shows up as a Gap in the briefing.
