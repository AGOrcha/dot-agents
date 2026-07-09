# Distilled takeaways — agentic-engineering threads (2026-07-03)

Teaser-level (full bodies login-walled; deep-read pending X MCP). Each entry: the claim + the
actionable extract for dot-agents.

## 1. Loss functions for agent loops (@elvissun)
- **Claim:** "point it at a task and walk away" is the wrong mental model for `/goal`/loop use.
  Effective autonomous loops optimize a *loss function* — an explicit, measurable objective.
- **Extract:** an autonomous loop needs (a) a defined objective and (b) a way to measure
  progress toward it each iteration. Fire-and-forget without a loss signal drifts. → Frame the
  meta-loop's iterations around an explicit success metric, not just "keep going."

## 2. Designing ML experiments that teach you something (@iamgrigorev)
- **Claim:** high experiment throughput (~100/wk) only pays off with disciplined design.
- **Extract:** every experiment needs a hypothesis + a clean readout, or the runs are noise.
  → Reinforces the fidelity discipline for our eval harness (real inputs, negative controls,
  no hidden losses).

## 3. Multiplayer agents (@sergeykarayev)
- **Claim:** agents that don't share context force humans to be couriers; they should be
  "multiplayer" (shared state / mutual awareness).
- **Extract:** siloed per-agent context is a design smell. → Validates the shared-coordination
  direction (orchestrator + workers + session-handoff journal); the "real fix is a shared
  backend" over isolated-worktree status.

## 4. Token engineering, not tokenmaxxing (@thealexker)
- **Claim:** an unmonitored expensive-model run cost $200 on one prompt; treat token spend as
  something to *engineer*, not maximize.
- **Extract:** monitor + scope model spend deliberately per task. → Exactly the
  worker-model-selection discipline (Fable is expensive; scope model to task complexity).

## 5. Signal-based outbound engine on Codex (@nifinet)
- **Claim:** outbound as an agent routine — watch a source, wait for a *real* signal, decide,
  then write from that signal.
- **Extract:** a template for an agent-run outreach engine (watch → validate signal → decide →
  draft). → Directly applicable to dot-agents X outreach once the X app is live.

## 6. Onyx — a programmable runtime for agent orchestration (@realmcore_)  ← the sharpener
- **Claim:** Onyx is a VM for *programmable* agent orchestration — "a runtime that turns
  orchestration into software engineering."
- **Extract:** orchestration lives on a spectrum from *mechanical/deterministic* (fixed step
  sequences) to *fully programmable* (a VM/DSL with control flow, state, dynamic dispatch).
  dot-agents already owns the programmable end (the **Workflow JS engine**: fan-out, pipelines,
  control flow). → The lesson for `da-recipe-scripts` is a **boundary** one, not a "add a VM"
  one (see the evaluation doc).
