# KG Phase 6: Shared-Memory Research

Spec: `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — Phase 6
Status: Sixth phase (research, not implementation)
Depends on: KG Phases 1-5 stable

## Goal

Evaluate stronger verification models and DKG-like shared-memory approaches. Establish explicit boundaries between graph truth, workflow truth, and coordination truth. This phase produces research artifacts and an RFC, not production code.

## Important

This phase is research-only. It produces evaluation documents and an RFC. No production code should be written until the RFC is reviewed and approved.

## Research Questions

1. **Verification models**: What level of provenance verification is needed beyond source_refs?
   - Content hashing for tamper detection?
   - Signature chains for multi-author graphs?
   - Merkle-tree style integrity for partial graph sync?

2. **DKG-like shared memory**: How can multiple agents/users share graph state?
   - Conflict resolution strategies (CRDT, OT, last-write-wins with tombstones)
   - Partition tolerance — what happens when graphs diverge and reconverge?
   - Trust model — do all writers have equal authority?

3. **Truth boundaries**: Where does each system own truth?
   - Graph truth: curated knowledge, provenance, cross-links
   - Workflow truth: plan state, task status, proposals, checkpoints
   - Coordination truth: delegation contracts, merge-back, intent markers
   - Session truth: CLAUDE.md, ephemeral context, conversation state

## Deliverables

### Step 1: Evaluation of verification models

- [ ] Research document: `docs/research/kg-verification-models.md`
  - Survey existing approaches (content-addressable storage, git-based verification, Merkle DAGs)
  - Evaluate fit for local-first markdown graph
  - Recommend: which model, what tradeoffs, implementation complexity estimate
  - Consider: does this need to be in the KG layer or can git provide sufficient verification?

### Step 2: Evaluation of shared-memory approaches

- [ ] Research document: `docs/research/kg-shared-memory-evaluation.md`
  - Survey approaches: CRDTs for markdown, git-based collaboration, custom sync protocols
  - Evaluate: conflict rate expectations, resolution complexity, user experience
  - Consider: is multi-machine sync a real requirement or can git push/pull suffice?
  - Recommend: approach, scope, and whether this belongs in KG or in a separate layer

### Step 3: Truth boundary documentation

- [ ] Document: `docs/research/kg-truth-boundaries.md`
  - Define ownership for each truth domain
  - Map interactions between domains (workflow reads graph, graph imports workflow artifacts)
  - Identify invariants that must hold across boundaries
  - Flag risks where truth domains could conflict

### Step 4: RFC for shared-memory layer

- [ ] RFC: `docs/rfcs/kg-shared-memory-rfc.md`
  - Based on research findings
  - Propose: scope of shared-memory layer, integration points, phased approach
  - Define: what stays in KG, what stays in dot-agents, what needs a new layer
  - Include: acceptance criteria, blocking risks, explicit non-goals
  - Require review from stakeholders before any implementation

### Step 5: Prototype evaluation (optional)

- [ ] If research supports a specific approach, build a minimal prototype:
  - Content hashing for note integrity
  - Simple graph diff/merge for two diverged graphs
  - Evaluate prototype against real graph data
  - Document findings in `docs/research/kg-shared-memory-prototype-results.md`
- [ ] Prototype code lives in `research/prototypes/kg-shared-memory/` — not in production paths

## Files Created

- `docs/research/kg-verification-models.md`
- `docs/research/kg-shared-memory-evaluation.md`
- `docs/research/kg-truth-boundaries.md`
- `docs/rfcs/kg-shared-memory-rfc.md`
- `research/prototypes/kg-shared-memory/` (optional)

## Acceptance Criteria

This phase is complete when:
- Verification model tradeoffs are documented with a clear recommendation
- Shared-memory approaches are evaluated with a recommendation
- Truth boundaries are explicitly documented
- An RFC exists that can be reviewed before any implementation starts
- The local-first graph core (Phases 1-5) is validated as the right foundation for whatever comes next

## Key Constraint

No production code. Research artifacts and RFC only. The point is to make an informed decision, not to ship a premature shared-memory system.
