# Archived delegations — 2026-05-20

Archive of 20 delegation contracts that accumulated in `.agents/active/delegation/` predating the PR #37 salvage and had no matching bundle in `.agents/active/delegation-bundles/` by 2026-05-20.

Triaged by an Explore subagent (orchestrator session 2026-05-20). Each contract's status was cross-checked against canonical plan state (`da workflow orient --json`), git log evidence (commits on master, merged PRs), and proposal sign-off (`.agents/proposals/` siblings). All 20 entries below are **archive-to-history** — work landed, audit deliverable signed off, or coordination role completed.

One additional contract (`cg6b-b3-workflow-helpers.md`) was **NOT archived** — it is the audit target; see `.agents/active/delegation/cg6b-b3-workflow-helpers.AUDIT.md` for rationale.

## Roster

| Filename | Plan / Task | Evidence |
|---|---|---|
| cg4-sync-warm-link-coverage.md | coverage-gate-per-file / cg4-pr3c-comply | PR #18 merged; `sync_code_warm_link.go` ≥95% in v0.3.2; task `completed` in TASKS.yaml |
| cg6b-b1-hooks.md | coverage-gate-per-file / cg6b-ratchet-loop (B1) | PR #26 merged; 3 allowlist entries pruned; B1 complete per TASKS.yaml notes |
| cg6b-b2-workflow-schema.md | coverage-gate-per-file / cg6b-ratchet-loop (B2) | PR #35 merged; 4 allowlist entries pruned; B2 complete per TASKS.yaml notes |
| codex-hooks-gap-audit.md | — | READ-ONLY audit deliverable; sibling proposal `codex-hooks-agents-linking-gap.md` on master |
| docs-0.3.0.md | — | PR #20 merged (release/0.3.0); docs reconciled to 0.3.0 surface |
| docs-0.3.1.md | pr10-branch-split | PR #16 merged (pr3b-rebased); docs reconciled to 0.3.1 with `da workflow` surface |
| docs-0.3.2.md | pr10-branch-split | PR #18 merged (pr3c-rebased); docs reconciled to 0.3.2 with `da kg` surface |
| docs-refresh.md | — | PR #29 merged (docs-refresh); accuracy pass complete; spawned the two hooks proposals |
| fix-workflow-refs-030.md | — | PR #21 merged (fix/workflow-refs-030); 0.3.0 references to unshipped `da workflow` corrected |
| gcc1-pin-store-contract.md | graphstore-concurrency-contract / gcc1-pin-store-contract | PR #30 merged; Store interface + CONTRACT.md published; Deps bound to contract |
| gcc2-fix-windows-sqlite.md | graphstore-concurrency-contract / gcc2-path-a-impl (fix 3) | PR #34 merged; Windows SQLite single-conn deadlock fixed via pool relaxation |
| gcc2-path-a.md | graphstore-concurrency-contract / gcc2-path-a-impl | PR #34 merged; lazy ephemeral store + enforced bounds/timeout implemented |
| hooks-cli-showremove.md | — | PR #36 merged; `da hooks show`/`remove` implemented; shared base-remove helper extracted |
| hooks-wiring-verify-opencode-fit.md | — | READ-ONLY audit deliverable; sibling proposal `hooks-wiring-and-opencode-fit.md` on master |
| pr5-scaffold-rehome.md | pr10-branch-split / pr5-scaffold-rehome | PR #27 merged; scaffold embedded in binary, `src/` shell tree deleted, 5 prompts recovered |
| pr7-test-infra-docs.md | pr10-branch-split / pr7-test-infra-docs | PR #28 merged; test infra, ADRs, research, tooling ported from mega-branch |
| proj-coach-charter.md | — | Orchestration/coaching role; the streams it managed (gcc2/gcc3/cg6b/di-refactor) are now in flight or merged |
| stp-doctor-repair.md | shared-target-projection-wiring / stp-doctor-repair | PR #32 merged; doctor repair runs projection + full CreateLinks per managed project |
| stp-import-relink.md | shared-target-projection-wiring / stp-import-relink | PR #31 merged; shared-target projection wired into import relink path |
| stp1-wire-refresh.md | shared-target-projection-wiring / stp1-wire-refresh | Part of STP via PR #26 / #31 / #32; projection wiring established in refresh path |

## Why archive vs delete

These contracts predate the PR #37 salvage and most plans they reference have since been canonicalized in `.agents/workflow/plans/`. Their content is useful provenance — what was originally delegated, with what scope, by which orchestrator iteration. Deleting would lose that history. Archival under `history/archived-delegations/<date>/` keeps the record discoverable without polluting the live `active/delegation/` directory.

## Counts

- **20 archived** (this manifest)
- **1 audit target** (`cg6b-b3-workflow-helpers.md` — left in `.agents/active/delegation/` with audit note)
- **0 keep-active** (none of the 21 had in-flight follow-up work)
