# Coverage-exceptions allowlisting is not a graceful escape hatch

## Pattern

Never add to `scripts/coverage-exceptions.txt` (or any "legacy debt" allowlist) as the path of least resistance when a PR fails a per-file gate. The rationale field reading "cg6 ratchet-loop raises to ≥95%" is not a plan — it's an IOU that no future session will pick up because each new session treats inherited entries as "not my work."

## Root cause

This pattern is how tech debt accumulates by design. Wave 6 had two examples: PR #83 (p0-sentinel-cli) introduced `hook_sentinel.go` at 84.91% without an allowlist entry, breaking master and blocking 6 Wave 6 PRs. The lazy fix was to allowlist it on master with "cg6 ratchet-loop raises to ≥95%" — but no cg6 task targeting that file exists. Same pattern almost repeated with `hook_outcome.go` on PR #91.

The ratchet-loop pattern only works if the loop is actually staffed. Adding entries faster than the loop drains is silent regression of the 95% gate.

## Rule

- "Not my work" is only acceptable if you can scope, plan, and **schedule** the fix back into wave execution before allowlisting. The scheduled task ID goes in the rationale (e.g. `# legacy-tail 84.91%; scheduled in plan-id/task-id targeting fileX.go for cg6 lift`).
- For a file the current PR introduces (brand-new code below threshold): never allowlist. The author owns coverage at ship time. Add tests.
- For a file an upstream PR introduced and dropped below threshold without allowlisting (pre-existing master regression that's blocking your work): the correct action is to fix the upstream regression (add coverage to that file) AND/OR open a tracked task that names the specific file, the percentage, and the wave it lands in. Only then is allowlisting acceptable as a temporary unblock.
- Before adding any allowlist entry: check whether a cg6/equivalent task already targets that file. If not, you must author one with explicit scope (file path, baseline %, target %) and link it from the rationale.

## How to apply

When the gate fails with a file below 95%:
1. Is it your new file? → Add tests. Do not allowlist.
2. Is it upstream pre-existing? → Open `da workflow plan create cg6-<short>-lift` or extend an existing cg6 plan with the file targeted; only then `# legacy-tail X%; scheduled in <plan>/<task>` in the allowlist rationale.
3. Is it STALE-ALLOWLIST (now ≥threshold)? → Remove the entry. Free hygiene win.

## Cross-references

- `[[worker-owns-pr-readiness-loop]]` — the auto-fix path is "add tests" not "edit coverage-exceptions.txt"
- `[[validate-bundle-against-head]]` — fold-back is the right move when scope expansion would require allowlisting
