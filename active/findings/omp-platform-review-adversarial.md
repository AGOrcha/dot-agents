# omp-platform findings — review (adversarial) stage

da↔omp friction hit while running the adversarial reviewer lens on the D14/R8 slice. Terse bullets;
exact command + what happened.

- **resolve-prompt returns a partly-empty chain, no signal that the lens is missing.**
  `.agents/worktrees/d14/bin/da --json workflow resolve-prompt --kind reviewer --slug adversarial`
  → `matched:true` but 2 of 3 entries are `scope:"unresolved", exists:false` (base
  `reviewers/reviewer.base.md` AND the lens `reviewers/adversarial.md`); only the repo overlay
  `adversarial.project.md` resolves. So the "adversarial" reviewer runs with ONLY the project
  hotspot addendum — the actual red-team lens contract (the part that defines BLOCKER/HIGH, active
  probing, `sandbox_mutations`) is absent, and the CLI reports overall success anyway. omp needs
  resolve-prompt to either (a) flag `matched:true` but "no base/lens layer present" as a distinct
  degraded state, or (b) ship the base+lens defaults so a reviewer isn't silently running on an
  overlay-only prompt. A reviewer can't tell "lens intentionally overlay-only" from "lens files not
  synced for this platform."

- **No machine-readable severity/verdict contract from da for a review stage.** The lens prompt says
  "`fail` on any BLOCKER/HIGH" and verdict line `(lens: adversarial)`, but the swarm's own protocol
  wants line-1 `APPROVE|REJECT|SKIP`. da gives no schema tying reviewer severity → gate verdict, so
  each stage hand-rolls the mapping in prose. omp (running many parallel lenses) would benefit from
  da emitting a structured `{verdict, findings:[{severity}]}` the gate can grep instead of NL.

- **`da kg impact` never consulted for this review — build/postprocess broken (as CONVENTIONS warns).**
  Blast-radius readback would have been the natural adversarial input (who else calls
  `links.EnsureManagedGitignore` / `platform.CollectManagedOutputs`), but the CONVENTIONS note flags
  `da kg build/update/postprocess` as broken here (stale CRG venv shebang), so the graph may be stale.
  I fell back to `grep`/source reading. omp needs kg refresh to work under the platform, or reviewers
  can't trust impact queries and lose the token-efficient-context advantage the KG is supposed to give.

- **No sandbox seam for adversarial active-probing inside da.** The lens gates active probing on
  `sandbox_mutations`, but da/omp exposes no sandbox to the reviewer stage. I had to probe out-of-tree
  in `/tmp` (real `git check-ignore` + a hand-copied strip-logic replica) rather than exercising the
  real `internal/links` function, because CONVENTIONS forbids a non-owner writing into SLICE (only the
  `impl` stage owns the code worktree) and Go `internal/` can't be imported from an ad-hoc external
  module. Result: my strongest evidence for the CRLF idempotency hole is a faithful REPLICA, not the
  real function. omp would let adversarial reviewers get real-code evidence if it offered a read-only
  throwaway test seam (e.g. a scratch `_test.go` in a copy-on-write overlay of SLICE) instead of the
  all-or-nothing single-writer rule.
