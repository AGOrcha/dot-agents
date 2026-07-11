# omp-platform findings — verify (unit) stage (D14 managed-gitignore-autofill)

da↔omp friction observed during unit verification. Terse: command + what happened.

- **`da workflow resolve-prompt` works cleanly under omp, but base/kind layers
  are unresolved.** `.../bin/da --json workflow resolve-prompt --kind verifier
  --slug unit` returned `matched:true` yet only the repo-local
  `verifiers/unit.project.md` `exists:true`; `verifiers/verifier.base.md` and
  `verifiers/unit.md` (the role contract + kind spec the overlay says it
  "composes after") resolved to `""`/`exists:false`. The overlay is meaningful
  only atop those layers — a verifier following the resolved prompt alone gets
  the command matrix but NOT the base result-schema/evidence-taxonomy contract.
  da should either ship those base layers or have resolve-prompt warn when an
  overlay's declared predecessors are absent.

- **Mutation-verify vs "ONE writer per worktree" (RULE 3) is unspecified.**
  Proper mutation testing requires the verifier to temporarily edit code in the
  SLICE worktree (revert a hunk, run the test, restore via `git checkout --`).
  CONVENTIONS grants write ownership only to the impl stage, so the strongest
  verification technique technically violates the single-writer rule. Worked
  fine here (impl idle, restored to clean HEAD `2bba968`), but da has no
  sanctioned "scratch mutation" mode — a `da verify mutate`/ephemeral-overlay
  seam, or an explicit carve-out in the swarm protocol, would remove the
  ambiguity.

- **Cross-package coverage is invisible to per-package `go tool cover`.** The new
  `platform.CollectManagedOutputs` / `(*copilot).ManagedOutputs` report 0.0% when
  covering `./internal/platform/` alone, because their only tests live in the
  `commands` package (black-box, `package commands` importing platform).
  Accurate numbers needed `-coverpkg=./internal/platform/,./commands/,./internal/links/`
  across all three packages (then 100%/100%). A D12 per-file gate that greps
  per-package `-func` output would falsely flag these as uncovered — the gate
  must use `-coverpkg` union coverage, or the verifier will chase phantom gaps.

- **`-count=1` scoped race run is fast and clean under omp.** The tasked
  `go test -race -count=1 ./internal/links/... ./internal/platform/...
  ./commands/...` ran in ~72s, 16 pkgs ok, no embedded-shell quirks; subshell
  `(cd "$SLICE" && …)` per RULE 3 avoided cwd leakage. No friction in the test
  path itself.
