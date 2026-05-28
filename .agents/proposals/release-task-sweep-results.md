# Release task sweep results

Applies the [release-gated-plans convention](release-gated-plans-convention.md) (hybrid
model: MINOR -> feature-plan tail, MAJOR -> breaking-plan tail, PATCH -> cadence train) across
active workflow plans.

- **VERSION at sweep time:** `0.3.2` -> next minor `0.4.0`, next major `1.0.0`. Release tasks
  use placeholder ids (`release-minor` / `release-major` / `release-patch`) and resolve the exact
  number from current VERSION at ready-time per the convention's version-collision handling.
- **`app_type: release`** used on every release tail. The `workflow-tasks.schema.json` does not
  enumerate `app_type` at all (it is not even a declared task property), so it imposes no
  constraint; `da workflow eligible/status` parse every edited file cleanly. No fallback needed.
- **Surveyed against `origin/master`** (worktree base). Note: `config-v2-migration` and
  `platform-driven-diagnostics` named in the task brief / convention do **not** exist on
  `origin/master` — they were not present to edit (see "Notes / discrepancies" below).

## Classification table

| Plan | Delivered surface | Classification | Release tail added | Target-version note |
|---|---|---|---|---|
| graph-backend-adapter-contract | New adapter contract/DSL (additive) **+ t6 decommissions Python bridge & removes crg-bridge from `da` core** | **MAJOR** (ambiguous) | y (`release-major`) | next major from VERSION (1.0.0 today) |
| loop-discipline-stop-hooks | New sentinel CLI, hooks+skills in starter, platform mapper ext | MINOR | y (`release-minor`) | next minor (0.4.0 today) |
| orchestration-companion-stop-hooks | New companion terminal gates + starter skills | MINOR | y (`release-minor`) | next minor (0.4.0 today) |
| r1-5-hook-enforcement-telemetry | Persisted hook outcomes + new scoring signals + CLI readback | MINOR | y (`release-minor`) | next minor (0.4.0 today) |
| r2-observability-dashboard | New dashboard (React/TS + Go API) + serving binary | MINOR | y (`release-minor`) | next minor (0.4.0 today) |
| r3-background-worker-service | New `da service` command + HTTP surface | MINOR | y (`release-minor`) | next minor (0.4.0 today) |
| r4-code-task-generation-eval | New `da eval` command + multi-language harness | MINOR | y (`release-minor`) | next minor (0.4.0 today) |
| r5-review-labeling-access | New review UI, admin CLI, RBAC, R1 feedback signal | MINOR | y (`release-minor`) | next minor (0.4.0 today) |
| worktree-platform | New managed-worktree capability (gitwt + skills integration) | MINOR (ambiguous) | y (`release-minor`) | next minor (0.4.0 today) |
| pr10-branch-split | Branch-split logistics / retire stale branches — no product surface | patch-train (N/A) | n | feeds patch train; release logistics only |
| r1-outcome-scoring | Scoring + CLI (delivered, **status: completed**) | MINOR-delivered | n (see note) | folds into imminent 0.4.0 via PR #145 |
| **release-patch-train** (new) | Standing cadence vehicle for the patch digit | PATCH train | y (`release-patch`, recurring) | next patch from VERSION at each tick |

### Do-not-touch internal-only plans (feed the patch train; no tail added)

`root-command-decomposition`, `seam-interface-di-migration`, `production-code-helper-extraction`,
`coverage-95-staged`, `coverage-gate-per-file`, `sonarqube-pr10`, `cross-platform-test-skips-audit`,
`refresh-skill-relink`, `shared-target-projection-wiring`, `workflow-client-commands`,
`workflow-commit-command`. Left untouched per instructions.

## Patch-train placement

Created a new tiny plan `release-patch-train/` (PLAN.yaml + TASKS.yaml) rather than appending to an
existing maintenance plan. Justification: every maintenance plan is on the do-not-touch list and is
a *bounded, completable* code plan; a recurring release vehicle must outlive any single plan's
completion. A standalone plan with a single recurring `release-patch` task (depends_on: empty,
reset-to-pending after each release) is the clean home. Its notes carry the scope guard (only
patch-eligible work; anything user-visible -> MINOR tail; anything breaking -> MAJOR tail).

## Notes / discrepancies (for maintainer ruling)

1. **graph-backend-adapter-contract MAJOR is the one genuinely ambiguous call.** t1-t5 are purely
   additive (a MINOR on their own); only the final gated `t6-bridge-decommission` (remove Python
   subprocess bridge, drop crg-bridge from core, flip `da` core namespace) makes the plan breaking.
   t6 is fenced behind a multi-week §11.4 parity gate with a "keep the bridge if any consumer still
   reads it" anti-scope. Classified MAJOR because the decision rule keys on *any* removed/incompatible
   consumed contract. If you prefer, split t6 into its own breaking plan and revert this to a MINOR
   tail — flagged in the task notes.
2. **worktree-platform** is mostly internal (`internal/gitwt/`) but ships a new managed-worktree
   capability consumed via the orchestration skills, changing how delegated work is isolated/committed
   (observable). Classified MINOR; if you judge it pure internal plumbing, drop the tail and let it
   feed the patch train. Flagged in the task notes.
3. **r1-outcome-scoring** is `status: completed` and its work folds into the imminent **0.4.0**
   (PR #145 / the de-facto 0.4.0 release task per the convention's "Immediate application"). No tail
   added to a completed plan; its release is already accounted for by 0.4.0. Surfaced here for a ruling.
4. **config-v2-migration not present on origin/master.** The brief asked to add a tail to it (mid-flight,
   p1 in_progress, deps incl p1/p4, note 0.4.0-vs-0.5.0 fold). The plan directory does not exist on the
   worktree base, so no edit was possible. Add the tail when/if that plan lands on master.
5. **platform-driven-diagnostics not present on origin/master** either (named in the convention's
   immediate-application list as folding into 0.4.0). No action possible here.

## Verification

`da workflow eligible` and `da workflow eligible --json` both exit 0 with no stderr after the edits;
`da workflow status --json` enumerates `release-patch-train` as a recognized plan. All TASKS.yaml are
parsed by the Go YAML loader these commands use (a parse error would fail them). Block scalars (`|-`)
used on every notes field per `[[schema-usage]]` colon-space rule.
