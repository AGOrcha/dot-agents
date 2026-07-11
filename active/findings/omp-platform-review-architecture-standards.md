# omp-platform findings — review (architecture-standards) stage

da↔omp friction hit while running the architecture-standards review lens on the
`wire-managed-gitignore-autofill` slice. Terse bullets: exact command + what happened.

- `da --json workflow resolve-prompt --kind reviewer --slug architecture-standards` returned
  `matched:true` but with `reviewers/reviewer.base.md` and `reviewers/architecture-standards.md`
  both `exists:false` / `scope:"unresolved"` — only the repo-local `.project.md` overlay resolved.
  The base contract (role, bundle-read, findings/verdict format) and the lens definition (what the
  lens checks) are referenced by the overlay ("composes **after** …") but ship no resolvable body, so
  a cold reviewer only gets the project overlay. omp should either seed the base/lens layers or have
  `resolve-prompt` signal that a composed-after layer is missing so the reviewer knows the contract
  half is absent rather than silently reviewing off the overlay alone.

- Coordination is entirely manual file-passing on `refs/agents/state` via a detached worktree
  (`.agents/worktrees/_state/active/{coordination,findings}`). The upstream-gate protocol ("line 1 is a
  verdict token the next stage greps") is hand-rolled convention with no CLI support — I had to read
  `verify-cli-runner.md` line 1 myself and hand-write my own `review-architecture-standards.md` verdict
  token. A `da workflow signal --stage <s> --verdict <v>` / `da workflow gate --require <upstream>=PASS`
  primitive would remove the bespoke grep-line-1 contract and the "swarm has NO failure short-circuit"
  defensive-SKIP boilerplate each stage reimplements.

- `da workflow resolve-prompt` path fields came back as `~`-prefixed
  (`~/proj-docs/dot-agents/.agents/prompts/...`) — fine for display, but a consumer that feeds the
  `resolved` path straight to a reader must tilde-expand first; the embedded shell/read layer does not
  auto-expand `~` in every context. omp resolve-prompt output would be safer as an already-absolute path.

- The review is READ-ONLY against `git -C .agents/worktrees/d14 diff origin/master`; the code worktree
  (`d14`) and the coordination worktree (`_state`) are two separate git worktrees off different refs.
  Judging a slice therefore means juggling two `-C` roots (diff/read from `d14`, write signals to
  `_state`). A single `da review <slice>` that knows both roots would remove the cross-worktree `-C`
  bookkeeping each reviewer stage repeats.
