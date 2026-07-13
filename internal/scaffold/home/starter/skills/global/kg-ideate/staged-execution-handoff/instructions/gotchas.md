# Gotchas: staged-execution-handoff

Common failure points:

## This Molecule Never Implements

- "kg-ideate produces no code" — step 16 hands the briefing + spec + plan to the
  orchestrator/staged runtime and stops. Starting to write code, files, or even a
  scaffold "to save a round trip" here duplicates the orchestrator's own job
  (`orchestrator-session-start`/`isp`) and skips its pick-task/KG-readback/decide gate.

## Nested and Compound-Only — Never Invoked Standalone

- Unlike `spec-scaffold`/`plan-scaffold`, this molecule stays nested under `kg-ideate/`
  and is only ever dispatched as this compound's own Phase 4 step. It assumes a real
  spec + plan already exist from Phases 2–3; there is no documented fallback for being
  invoked with only one of the two, or with neither.

## Step 15's Fanout Decision Inherits Phase 3's Write-Scope Quality

- The direct-vs-fanout call depends on each task's write_scope being genuinely
  disjoint. If Phase 3 derived write-scopes by guessing rather than from the Phase 1
  impact radius (see `plan-scaffold`'s `write-scope-derive.md`), that weakness carries
  straight into this step and can produce a fanout bundle with colliding scopes instead
  of a clean per-task split.
