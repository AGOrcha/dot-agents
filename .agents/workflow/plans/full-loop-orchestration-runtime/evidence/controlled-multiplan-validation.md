# Controlled multi-plan validation — deterministic evidence

Repository-HEAD da built to `/tmp/da-full-loop`; OMP swarm DSL validated via `packages/swarm-extension` parser.

## Model routing (explicit-model-routing)
- `go test ./internal/config ./commands/workflow ./internal/scaffold/home` — green (race build also green: `./internal/config/... ./commands/workflow/... ./internal/scaffold/...`).
- `da --json workflow resolve-prompt --kind reviewer --slug cross-harness-adversarial` → `model=gpt-5.4 model_family=gpt`.
- `da --json workflow resolve-prompt --kind executor --slug default` → `model=claude-opus-4-8 model_family=claude`.
- `da config lint` → OK. `.agentsrc.json` carries `model`/`model_family` on every verifier, reviewer, executor, orchestrator profile; blocking cross-family lens is the only `gpt` family.

## Swarm definitions
- `profile-driven.swarm.yaml` → `da-per-task-profile-pipeline`, mode `pipeline`, target_count 3, 0 validation errors; 7 verifier slots + 4 routine Claude lens slots + 1 dedicated GPT cross-family lens (no numeric-index diversity binding).
- `reconcile.swarm.yaml` → `da-full-loop-wave-reconcile`, mode `sequential`, 0 errors.

## Outer driver (`bin/tests/omp-full-loop_test.sh`)
Fixture da + fake omp-swarm; four scenarios all PASS:
- **normal:** slots budget respected (available=2 → 2 tasks), write-scope-conflicting `conflicting-task` excluded from the wave, one wave launched, READY+FOLD_BACK reconciled once, second poll quiescent (no wave-2), no vendor CLI (`claude/codex/copilot/cursor`) invoked.
- **failure:** an inner pipeline exits non-zero; the wave still reconciles (manifest records exit_code=7/UNKNOWN) rather than orphaning the fanout-held slot.
- **recovery:** a stale `.driver-lock` (dead pid) plus an incomplete prior wave is recovered and reconciled before any new selection.
- **fanout-failure:** a later `da workflow fanout` refusal does not strand the earlier successful delegation; the refused task is recorded FOLD_BACK (exit 70) and the wave reconciles.

## Not covered here (launch-gated)
Controlled real OMP smoke over disposable non-overlapping tasks is the first live autonomous run; it is user-gated and executed at loop launch, not in this deterministic pass.
