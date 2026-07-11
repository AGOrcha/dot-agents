# omp-platform findings — review (acceptance-invariants) stage

da↔omp friction hit while running the acceptance-invariants review lens under the omp harness.

- **`resolve-prompt` returns only the repo-local overlay; base + lens layers report
  `exists:false`.** `"$SLICE/bin/da" --json workflow resolve-prompt --kind reviewer --slug
  acceptance-invariants` matched but `reviewers/reviewer.base.md` and `reviewers/acceptance-
  invariants.md` both resolved to `""` / `scope:"unresolved"`. The lens ran off the project overlay
  ALONE. da should either ship the base+lens defaults or surface a warning when a reviewer runs with
  only the repo overlay present — a lens with no base contract is a silent under-spec.

- **No failure short-circuit across stages; gate is a manual line-1 grep.** This lens had to
  re-read `review-architecture-standards.md` line 1 itself and branch on REJECT/SKIP. da's workflow
  has no built-in "upstream reviewer REJECTed → auto-SKIP downstream lenses" edge; every stage
  re-implements the defensive gate by hand. A first-class `workflow gate`/dependency-verdict
  primitive would remove the copy-pasted RULE-0 boilerplate.

- **`da kg build/update/postprocess` still broken (stale CRG venv shebang), per CONVENTIONS.** Did
  not attempt KG readback beyond `impact`/`code-status`; a read-only reviewer would benefit from a
  working `da kg impact` on the changed set, but the build path being broken means the graph may be
  stale for review context. da needs the CRG venv bootstrap fixed for omp worktrees.

- **Coordination is pure manual git-ref (`refs/agents/state`) + flat files.** Verdict propagation
  is line-1 tokens in Markdown that each stage greps. Works, but there is no da-native
  signal/verdict store — no `da workflow signal write/read`. The whole swarm rides on filesystem
  convention da does not model, so a malformed line 1 would silently mis-gate a downstream lens.

- **`.agentsrc.lock` is not byte-stable across identical `da refresh` runs (timestamp fields).**
  Confirmed structurally-stable but `refreshedAt`/`fetched_at`/`last_checked_at` churn on every
  refresh. For a committed contract file this guarantees a dirty worktree after any refresh —
  da should either omit per-run timestamps from the committed lock or separate volatile metadata
  from the deterministic digest record so the committed contract stays clean.
