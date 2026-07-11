# Design corrections (post proving-run, user-directed)

## C1 — Fold-back MUST auto-return to the executor (loop, not terminal leaf)
Authority: `.agents/workflow/specs/layered-pr-fanout/design.md` §3.1 state machine —
`awaiting_agent_review → in_progress (lens reject / verify retry needed)`; §2.8 keeps the slot on
awaiting_agent_review BECAUSE "lens dispatch can bounce work back". Also the verifier/lens SPLIT
(§2.4): verifier-green unblocks downstream ELIGIBILITY (→ awaiting_agent_review); lenses gate the
MERGE transition (→ awaiting_owner_review), NOT one end-gate.
- Swarm gap: swarm-extension DAG is ACYCLIC (detectCycles rejects loops) → my ready_gate FOLD-BACK
  was a terminal leaf. WRONG.
- Fix: `mode: pipeline`, `target_count = N` bounded fix-iterations. Iteration k stage-0 reads
  iteration k-1's gate verdict; on FOLD-BACK the executor re-enters with the reject feedback
  (COORD/review-*.md) and re-does impl→verify→review→gate; on READY later iterations no-op. Bound N
  (slot/iteration budget) so a persistently-failing slice stops + surfaces (not infinite).
- Honor the verifier(eligibility) vs lens(merge) split + the awaiting_review sub-status model in
  Phase 2 (currently flattened into one gate).

## C2 — Managed-output inventory: authoritative source + single-source (BLOCKER 1)
Authority: `docs/PLATFORM_DIRS_DOCS.md` line 254 (impl-audit) — Copilot managed repo outputs =
`.github/copilot-instructions.md`, `.github/agents/*.agent.md`, `.agents/skills/`, `.vscode/mcp.json`,
`.claude/settings.local.json`, `.github/hooks/*.json`. Impl OMITTED `.agents/skills/` (verified: the
one gap). Storage policy line 196 + skills matrix line 232 confirm `.agents/skills/` is a real
projected copilot output.
- Fix: add `.agents/skills/`; DERIVE ManagedOutputs from ONE source shared with CountLinks/
  SharedTargetIntents (they triplicated + drifted day one — RULE 5), cross-checked against
  PLATFORM_DIRS_DOCS as the authoritative doc.
- BLOCKER 2 (hardening): neverIgnored filters exact literals only — make pattern-aware or test that
  pattern outputs (*.lock) can't defeat the .agentsrc.lock contract.

## C3 — Spec role, honest
Used: config-distribution-model execution profile (verifier_sequence + lens_set + verifier→lens
order). UNDER-USED: layered-pr-fanout (loop, sub-statuses, eligibility-vs-merge split) +
graph-backend-adapter-contract (base/lineage resolution for layered fanout). Phase 2 mirrors the
FULL loop contract, not just the profile stage list.
