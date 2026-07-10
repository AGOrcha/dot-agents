# pr3c (PR#18) — 3-lens review triage

3 lenses run. Verdicts: arch **approve**, test+behavior **approve**,
adversarial **acceptable-with-findings**. One critical actionable (security),
rest are doc/known-limitation.

| # | Finding | Lens | Sev | Decision |
|---|---------|------|-----|----------|
| 1 | Note-ID path traversal: `src.ID` from inbox frontmatter not `slugify()`d on ingest path (entity IDs & `kg add` are) → arbitrary file write outside KG_HOME | adv | **CRITICAL** | **FIX** (slugify src.ID + regression test) |
| 2 | `compact` not crash-atomic (rename ok, index-trim after — stale-but-recoverable window) | adv | HIGH | **FIX** (document known window in code) |
| 3 | `removeIndexEntry`/lint-report full-file rewrite race under concurrent `da kg` | adv | MED | **PR#18 body note** (pre-existing, single-user CLI) |
| 4 | `persistReweavedNote` body-loss | adv/test/arch | — | **VERIFIED FIXED** by `f06ab87` (two-site regression coverage) — no action |
| 5 | Seam globals safe only because no `t.Parallel()` (implicit) | adv | nit | **FIX** (`seams.go` caveat comment) |
| 6 | `persistReweavedNote` silent read/parse-error swallow (pre-existing repair path) | arch | nit | **FIX** (one-line intentional-silence comment) |
| 7 | `16cb85b` commit message stale post-rebase (cosmetic, history prose) | arch | nit | **Skip** (no history rewrite warranted) |

Fixes 1,2,5,6 landed on `pr3c/kg`. #3 → PR#18 body. #4 no action (fixed). #7
skipped.
