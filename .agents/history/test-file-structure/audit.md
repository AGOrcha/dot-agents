# test-file-structure — full audit (t5)

Scope: every `*_test.go` across pr3b (workflow), pr3c (kg), and
master/root (`/tmp/wt-cidrift`), classified against the convention
(mirror source file, or cohesive feature name; iteration numbers
forbidden).

## Tier 1 — conformant (no action)

- Source mirrors: `add_test.go`↔`add.go`, etc. (the bulk).
- Legitimate **feature / aspect / E2E** files (convention's feature
  clause): `doctor_repair_e2e_test.go`, `hook_normalization_roundtrip_test.go`,
  `import_pure_test.go`, `refresh_idempotency_test.go`,
  `resource_parity_test.go`, `agentsrc_mutations_test.go`,
  `lifecycle_e2e_test.go`, `delegation_fanout_test.go`,
  `workflow_integration_test.go`, `scope_evidence_test.go`,
  `plan_check_scope_test.go`, `global_flag_json_test.go`,
  `drift_sweep_test.go`, `state_plan_test.go`, `foldback_helpers_test.go`.
- Package-level / shared-helper files: `<pkg>_test.go` (e.g.
  `sync/sync_test.go`, `hooks/hooks_test.go`, `agents/agents_test.go`),
  `seams_test.go`, `wiring_test.go`, `testutil_test.go`, `coverage_test.go`
  (root: cobra-coverage harness, descriptive).

## Tier 2 — FIXED by this plan (t3/t4)

- pr3b: 15 `coverage_push{,2..10}` + `integration_harness{,2..5}` →
  dissolved (commit `ce96460`). 0 remain.
- master: 3 `ci_drift{,2,3}` → dissolved (PR #19). 0 remain.
- **Hard done-criterion met: zero iteration-numbered test files.**

## Tier 3 — tracked follow-up (soft smell, NOT a done-criterion blocker)

`<source>_extra_test.go` / `<source>_coverage_test.go`: a bucket placed
*beside* the mirror instead of *in* it. Not iteration-numbered, bounded,
descriptive — but it should fold into `<source>_test.go`. Split by
branch ownership:

| File | Mirror | Owner |
|---|---|---|
| `app_types_coverage_test.go` | `app_types_test.go` | pr3b (workflow) |
| `kg_coverage_test.go`, `extra_fault_test.go` | `kg_test.go` / feature | pr3c (kg) |
| `crg_extra_test.go`, `crg_status_extra_test.go`, `impact_extra_test.go` | crg/impact | **master** (internal/graphstore — pr3b base / merged) |
| `links_extra_test.go` | `links_test.go` | **master** (internal/links, merged) |
| `projectsync_extra_test.go`, `promote_extra_test.go`, `journal_extra_test.go` | projectsync/promote | **master** (merged) |
| `config_extra_test.go`, `agentsrc_extra_test.go`, `proposals_extra_test.go` | config/agentsrc/proposals | **master** (merged) |
| `cmd_extra_test.go` (skills) | `skills/` cmd | **master** (merged) |

Most Tier-3 files live in **already-merged code** (internal/*, skills),
outside pr3b/pr3c write-scope — folding them is a separate hygiene pass
(same split rationale as `root-command-decomposition`). The convention +
lesson now in place prevent *new* ones.

## Recommendation

- Done-criterion satisfied; the plan can close on Tier-1/2.
- Tier-3: either fold the **in-scope** files now (pr3b
  `app_types_coverage`; pr3c `kg_coverage`, `extra_fault`) as a small
  t5b, and open a `test-file-structure-wave2` follow-up for the
  master-owned remainder; or track the whole of Tier-3 as wave2.
