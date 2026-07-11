# omp-platform findings — impl stage (D14 managed-gitignore-autofill)

da↔omp friction observed during implementation. Terse: command + what happened.

- **Snapshot-tag collision across worktree + main for identical files.** The
  worktree `.gitignore` was byte-identical to the main repo `.gitignore`, so the
  harness edit tool's `[path#TAG]` resolved the ambiguous filename+tag to the
  cwd-root (main) file, not the intended `.agents/worktrees/d14/.gitignore`. The
  edit silently landed on the WRONG tree. Only `git check-ignore -v` reporting
  the pre-edit line numbers surfaced it. Under a swarm where the code worktree
  is nested inside the coordination repo (both real git trees with overlapping
  content), file-identity by content-tag is unsafe — the tool needs to key on
  the absolute path when the same relative name exists in multiple worktrees.
  Workaround: revert main via `git checkout --`, rewrite the worktree file with
  a full-content write (explicit path, no tag).
- **`da config explain` is the only clean way to materialize `.agentsrc.lock`
  in a bare worktree.** There is no `da lock`/`da config resolve <path>` that
  just writes the lock for an arbitrary dir; `explain` happens to drive
  `EnsureResolved` (auto-lock) as a side effect. A dedicated lock-materialize
  verb would be less of a "run the inspector for its side effect" hack. (Output
  was deterministic: digests byte-identical to the on-disk lock, only timestamps
  differed.)
- **`da add`/`da init` are interactive by default; `-y` is required in the
  embedded shell** (no TTY). `da add <path>` without `-y` prints
  `Proceed? [y/N]` then `Add cancelled.` and writes no config — easy to mistake
  for success. A da-native non-interactive/JSON mode env (`DA_YES=1`) would be
  safer for orchestrated dogfooding.
- **No `da enable <platform>` CLI verb.** Platform enablement lives only in
  `~/.agents/config.json` `agents{}` (or defaults to enabled when unset). To
  dogfood copilot-only I had to rely on the "unset ⇒ enabled" default; there is
  no CLI to toggle a single platform for a temp project, so a scripted dogfood
  cannot pin the enabled set without hand-editing config.json.
- **Managed-.gitignore wiring itself worked cleanly under omp.** Built `bin/da`,
  `da refresh -y` on a temp git project: block written+sorted+deduped, hooks
  glob inside, `.agentsrc.lock`/`.agentsrc.json` filtered out (neverIgnored),
  user content preserved, byte-stable across a second refresh. No embedded-shell
  quirks in the refresh path.
