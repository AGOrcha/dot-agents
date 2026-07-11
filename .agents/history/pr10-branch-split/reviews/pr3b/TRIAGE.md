# pr3b (PR#16) — 3-lens review triage

3 lenses run (architecture, test+behavior, adversarial). **No critical / no
ship-blockers** in any lens. Verdicts: arch *approve with nits*, test *approve
with nits*, adversarial *acceptable-with-findings*.

| # | Finding | Lens(es) | Sev | Decision |
|---|---------|----------|-----|----------|
| 1 | MCP `handleGetImpactRadius` no upper `depth` clamp → unbounded CRG Python subprocess (DoS) | adv | HIGH | **FIX** (clamp + MaxResults) |
| 2 | `crg.go` hand-rolled `%q`-into-Python for `changed_files` (fragile, not exploitable today) | adv | MED | **FIX** (`json.Marshal`) |
| 3 | SQLite cross-process `SQLITE_BUSY` after 5s under concurrent writers | adv | MED | **FIX** (document in code comment) |
| 4 | Draft-plan surfacing adds a JSON-contract field (`draft_plans`) | arch+test+adv | doc | **PR#16 body note** (no code) |
| 5 | `migrations.go` is idempotent DDL, not versioned (latent at first ALTER) | arch+adv | decision | **FIX** (document deliberate contract in code) |
| 6 | `DiscoverCRGBin` can't distinguish "CRG absent" vs "CRG broken on PATH" (no liveness probe) | test | nit | **Track** as follow-up (out of PR scope; CI installs working CRG) |

Fixes 1,2,3,5 landed on `pr3b/workflow` as a review-feedback commit. #4 → PR
description. #6 → tracked follow-up (separate, not pr10-branch-split scope).
