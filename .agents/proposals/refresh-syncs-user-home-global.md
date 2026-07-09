# `da refresh` should sync starter content into the user home / global scope

**Date:** 2026-07-09 (empirically tested; supersedes the withdrawn "clean-clone can't resolve" framing)
**Scope:** project-local (per `proposal-routing.md` — targets `da refresh`/`init`/`doctor` home-sync behavior)
**Status:** proposal — TESTED. Surfaced by `p6-payout-backfill`.

## What was tested (so this isn't hypothetical)

- **Clean install is fine.** A clean `da init -y` into a fresh HOME installs the discipline skills global: `skills/global/{isp,iteration-close,delegation-lifecycle,onboard,orchestrator-session-start,plan-wave-picker,provider-consumer-pair,…}` and `agents/global/loop-worker` (+ the reviewer agents) — all globally resolvable. The starter + init path is correct. (The first draft of this proposal claimed clean installs fail; that was wrong and is withdrawn.)
- **Refresh does not touch the user home.** On an EXISTING `~/.agents/`: `da init` refuses ("use `--force`") and `da refresh` only reconciles managed **projects** from `~/.agents/`, never the home/global scope itself. Tested: clean init → delete `skills/global/isp` → `da init -y` (refuses) + `da refresh` (projects only) → `isp` **stays missing**.

## The real gap

`da refresh` was intended (starter-dev design intent) to sync starter content — but it currently syncs **projects only, not the user home / global scope**. So a home created **before** a skill/agent was added to the starter-global set never receives it (short of `da init --force` reinit or a manual copy). New starter-global content silently never reaches existing operators.

Concrete impact: this operator machine's `~/.agents/skills/global/` was missing isp/iteration-close/delegation-lifecycle/loop-worker/plan-wave-picker/provider-consumer-pair/… (its home predates them). `p6-payout-backfill` hit this and worked around it by promoting into the payout project scope — masking the real cause, not fixing it. (This machine's drift was subsequently backfilled by hand, 2026-07-09.)

## Proposed fix (the config model already supports it)

Now that the user home is a first-class managed surface (config-architecture-impl L3: home-config machine-local split + `kind:project-set` identity registry), the **home / global scope can be refreshed the same way a project is**:

1. **`da refresh` reconciles the user home / global scope against the current starter-global set** — additive (add missing shipped-global skills/agents), back up + flag on conflict, and **never clobber user-authored global content** (this machine has legit extras like `article-extract`, `gh-fix-ci`, `playwright`, `thermo-nuclear-code-quality-review`). This realizes the original starter-sync intent.
2. **`da doctor` flags missing starter-global skills/agents** so drift is visible before it bites a downstream consumer.
3. Interim (until 1–2): `da init --force` (backs up) or a targeted manual backfill.

## Relationship

- Surfaced by `p6-payout-backfill` (plan `loop-discipline-stop-hooks`); readback at `.agents/history/loop-discipline-stop-hooks/payout-migration-readback.md`.
- Complements the config-architecture L3 home-config surface (the substrate that makes "refresh the home like a project" tractable).
