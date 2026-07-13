# Execution Handoff — Instructions

Rules for Phase 4 execution handoff:
- kg-ideate does NOT implement. It produces spec+plan and hands them to staged execution.
- **Direct vs fanout (step 15):** small, single-write-scope work runs direct (solo);
  multi-task work with independent write-scopes fans out to subagents. For fanout, write
  per-task constraints into TASKS.yaml notes and produce delegation bundles per
  `[[delegation-lifecycle]]`.
- **Handoff context (step 16):** pass briefing + spec + plan as orientation context into
  `[[orchestrator-session-start]]` and `[[isp]]`. The layered-pr-fanout flow
  (`.agents/workflow/specs/layered-pr-fanout/design.md`) is the staged-PR target.
- The orchestrator (not kg-ideate) owns the staged runtime: pick task → KG readback →
  decide fanout/direct → staged impl → verify → review → parent gate.
