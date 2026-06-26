# Step 6: Converge — ratify and seed the spec

The forks have been routed: empirical verdicts from step 4, the ranked must-decide list
from step 5, the already-settled citations, the owner-decisions. Now the owner ratifies,
and the decisions are written into the canonical spec.

## Owner ratification

- Present, in one place: the empirical verdicts (each with its fidelity audit), the
  ranked judgment-call list (each with a recommended default), the already-settled
  citations, and the owner-decision items awaiting a pick.
- The owner ratifies. For empirical forks the verdict is usually accepted as-is (it is
  evidence, not opinion); for judgment calls the owner accepts the default or overrides
  it; for owner-decisions the owner picks.
- A `[PROPOSED]` decision becomes ratified here, or is sent back as a still-open fork.

## Seed or refine the spec

Write the ratified decisions into `.agents/workflow/specs/<id>/design.md` per the
`workflow-artifact-model` rule. The spec owns:

- **What & why** — the problem statement and goals.
- **Decisions + rationale** — each ratified fork: what was chosen and why the
  alternatives were rejected. For empirical forks, the rationale cites the audited
  experiment; for judgment calls, the decisive trade-off.
- **Open questions** — forks not yet resolved (kicked to a later cycle).
- **Done criteria** — verifiable, not vague. The execution half's verification traces
  back to these.
- **Deferred items** — the forks tagged deferred in step 3, explicitly out of scope.

The spec does NOT carry file paths, function names, or task breakdowns — those belong in
the plan, written downstream by the execution half. A spec that starts accumulating them
has become a plan; keep it focused on requirements and decisions.

## Evidence is recorded, not inlined

The prototype modules and the fidelity/cross-brain audits are the **evidence** behind the
decisions. Reference them from the spec (and carry them into `.agents/history/<id>/` when
the plan archives); do not paste experiment code into the spec. A decision in the spec
should be traceable to its audited evidence in one hop.

## Hand off to the execution half

Once the spec is ratified, the ideation cycle's job is done. The spec is the contract;
the execution half (`isp` / `orchestrator-session-start`) plans and implements against it.
Ideation produced the spec/proposal; execution implements it. Do not start writing
product code here — that is `isp`'s turn.

## Dispatch the authoring

As in the empirical pass, the driver orchestrates and steers; dispatch the actual spec
drafting to a subagent with the ratified decisions and evidence pointers, then review.
The driver's leverage is in the framing and the gates, not in typing the spec.
