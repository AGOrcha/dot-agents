# Global-shipped skills resolve only project-scoped — clean-clone → `da install` → zero-skips gap

**Date:** 2026-07-09
**Scope:** project-local (per `proposal-routing.md` — targets dot-agents' own skill resolution + home-config/install path; would not matter if this repo left dot-agents management)
**Status:** proposal — surfaced live by `p6-payout-backfill` (payout migration); belongs to **P7** (loop-discipline-stop-hooks broader-consumer sweep) per that task's contract "Out of Scope."

## Problem

The discipline skills `isp`, `loop-worker`, `iteration-close`, `delegation-lifecycle` (and `agent-handoff`) ship as **global** in the starter templates (`internal/scaffold/home/starter/...`), but on an operator machine they resolve only **scoped to the dot-agents project** — they live in the project's skill scope, not in `~/.agents/skills/global/`. So a **clean clone of a downstream consumer → `da install`/`da refresh` CANNOT resolve them**: the gate hooks + skill wiring for `isp`/`loop-worker` fail to materialize, and loop-discipline adoption silently breaks for that consumer.

Surfaced live: `p6-payout-backfill` could not get payout to resolve the 5 required discipline skills from a clean install. Worked around with `da skills promote` scoped to `~/.agents/skills/payout/` — non-invasive, but a **per-project workaround, not the fix**. `plan-wave-picker` and `provider-consumer-pair` exhibit the same class of gap (still unfixed).

## Root cause

The skills are authored to ship global, but on this machine they were installed/promoted **project-scoped** (dot-agents), and there is **no clean-clone → global-resolution path**:
- the home-config sync path that would materialize starter-shipped global skills into `~/.agents/skills/global/` on install is absent; and
- the declared **packages artifact resolver** (which would let any consumer resolve a shipped-global skill by reference without a per-project promote) is **unimplemented**.

So "ships global" is a template property that never becomes a resolvable global install on a fresh machine/consumer.

## Proposed

1. **Reproducibility gate:** `clean clone → da install → da doctor` (or the readiness check) reports **zero skill-resolution skips** for the starter-shipped global skills. Add it to CI so the gap can't reopen.
2. **Fix path (either):**
   - (a) a **home-config sync/install path** that materializes starter-shipped global skills into `~/.agents/skills/global/` on `da install`/`refresh`; or
   - (b) implement the **packages artifact resolver** so consumers resolve shipped-global skills by reference (no per-project promote).
3. **Interim (documented):** the per-project `da skills promote --scope <project>` workaround, exactly as `p6-payout-backfill` did for payout — recorded so P7 doesn't mistake it for the fix.

## Relationship

- Surfaced by `p6-payout-backfill` (plan `loop-discipline-stop-hooks`); evidence in `.agents/history/loop-discipline-stop-hooks/payout-migration-readback.md`.
- **P7 owns the broader downstream-consumer sweep** (per the p6 contract) — this gap must be closed before P7 can claim clean adoption across consumers, since every consumer hits it on a clean install.
- Sibling of the workflow-store persistence gap in `.agents/proposals/pr-merge-auto-reconcile.md` (both are "local machine state that never becomes shareable/reproducible").
