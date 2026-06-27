# Step 6: Converge — ratify the decision

The forks have been routed: empirical verdicts from step 4, the ranked must-decide list
from step 5, the already-settled citations, the owner-decisions. Now the owner ratifies.

`ideation-cycle` produces a **ratified decision + a per-fork evidence sidecar** and RETURNS
them. It does **not** type the spec file in either mode — `spec-scaffold` (or a delegated
spec-drafting equivalent) writes the prose. Where the return goes (see
`instructions/composition.md`):

- **Dispatched from `kg-ideate`** — return the ratified decision + evidence sidecar pointer to
  Phase 2 (`spec-scaffold`), which writes the spec prose. `ideation-cycle` resolved the fork;
  it does not author the spec.
- **Standalone** — hand the ratified decision + sidecar to a **delegated spec-drafting step**
  (a `spec-scaffold`-equivalent) that writes the prose; see "What the spec must carry" below
  for what to brief it with. `ideation-cycle` still does not type the spec file itself.

## Owner ratification

- Present, in one place: the empirical verdicts (each with its fidelity audit), the
  ranked judgment-call list (each with a recommended default), the already-settled
  citations, and the owner-decision items awaiting a pick.
- The owner ratifies. For empirical forks the verdict is usually accepted as-is (it is
  evidence, not opinion); for judgment calls the owner accepts the default or overrides
  it; for owner-decisions the owner picks.
- A `[PROPOSED]` decision becomes ratified here, or is sent back as a still-open fork.

## Fold the result HONESTLY — narrow + caveated, unsettled axes named

A spine run rarely settles the whole fork cleanly; it settles a *part* of it. Ratify what the
evidence actually supports, no more:

- **Fold narrow + caveated when the result is narrow.** If the verdict held for one task
  family, one regime, or one condition, the ratified decision says exactly that — the scope is
  part of the decision, not a footnote dropped on the way to a clean headline. The
  depth-degradation arc's durable findings were the *small* ones that survived the audit (relay
  loss → structured hand-backs; the infra nesting ceiling), never the headline claim.
- **Record the unsettled axes explicitly.** Every axis the run could NOT decide — the
  confound it couldn't separate, the regime it couldn't reach, the mechanism left open — is
  written down as an open question / unsettled axis on the decision, not silently dropped. A
  documented **null** ("couldn't induce / couldn't reach the regime / generalizes to one family
  only") is a first-class outcome that ratifies as such.
- **Iterate or extract.** Each gate reveals the next flaw; either loop until the fork is sound,
  or **extract the actionable subset** that IS settled and mark the rest explicitly unsettled.
  Chasing mechanistic closure past diminishing returns is optional; the honest scoping is not.

## What the spec must carry (brief the spec-drafting step with this)

The spec-drafting step (`spec-scaffold` dispatched, or a delegated equivalent standalone)
writes `.agents/workflow/specs/<id>/design.md` per the `workflow-artifact-model` rule. Brief it
— via a **structured/pointer hand-back, not retold prose** (relay discipline) — with the
ratified decisions + the evidence sidecar pointers. The spec it writes owns:

- **What & why** — the problem statement and goals.
- **Decisions + rationale** — each ratified fork: what was chosen and why the
  alternatives were rejected. For empirical forks, the rationale cites the audited
  experiment; for judgment calls, the decisive trade-off.
- **Open questions / unsettled axes** — forks not yet resolved, AND the axes a spine run
  couldn't decide (the confound, the unreached regime, the open mechanism) — recorded
  explicitly, never dropped to manufacture a clean answer.
- **Done criteria** — verifiable, not vague. The execution half's verification traces
  back to these.
- **Deferred items** — the forks tagged deferred in step 3, explicitly out of scope.

The spec does NOT carry file paths, function names, or task breakdowns — those belong in
the plan, written downstream by the execution half. A spec that starts accumulating them
has become a plan; keep it focused on requirements and decisions.

## Evidence is a per-fork sidecar, linked from the decision

Each hard fork's evidence — the pre-registration, the evidence artifact (prototype dir / eval
matrix / ingested corpus + KG query), the negative-control result, the power/regime check, and
both the GATE 1 (design) and GATE 2 (conclusion) cross-brain audit verdicts — is its **own
sidecar artifact**, LINKED from the spec's decision entry. It is NOT inlined into the spec body and NOT buried in transient task
notes. This anticipates the lineage schema (the decision `derives_from` its evidence
sidecar). Reference the sidecar from the spec decision (and carry it into
`.agents/history/<id>/` when the plan archives); a decision should be traceable to its
audited evidence in one hop. In dispatched mode, return the sidecar pointer so
`spec-scaffold` links it from the decision it folds in.

## Hand off

- **Dispatched mode:** hand the ratified decision + evidence back to `spec-scaffold`; its
  spec, plan, and the eventual handoff to `[[isp]]` continue from there. `ideation-cycle`
  is done once the fork is resolved.
- **Standalone mode:** once the spec-drafting step has written it and it is ratified, it is the
  contract; the execution half (`[[isp]]` / `[[orchestrator-session-start]]`) plans and
  implements against it.

Either way: do not start writing product code here — that is `isp`'s turn. And do not type the
spec file here — that is `spec-scaffold`'s. `ideation-cycle` resolves forks and returns
decisions + evidence; it does not author the spec and does not implement.

## Dispatch the authoring

The driver orchestrates and steers; it dispatches the actual spec drafting to `spec-scaffold`
(or an equivalent subagent) with the ratified decisions and evidence sidecar pointers — handed
back structured/pointer-based, never retold prose — then reviews. The driver's leverage is in
the framing and the gates, not in typing the spec.
