# Swallowed-error audits must cover preludes and void helpers, not just the headline command

## Pattern

When surfacing swallowed errors across a multi-step command pipeline (e.g.
`git add -A` → `git commit` → `git push`), fixing only the *headline* command
leaves the setup/prelude steps and error-discarding *void helper functions*
still swallowing. Audit every command in the sequence and every helper that
runs one — not just the verb named in the task title.

## Root cause

se5 (#356, "sync push/commit git-error checks") surfaced the visible failures —
the `git commit` error in `commit.go` (nothing-to-commit → `ui.Info`, else a
wrapped error) and the `git push` error in `push.go` — but left:

- `commit.go`: the unconditional pre-commit `git add -A` `.Run()` error
  discarded, so a staging failure (unwritable `~/.agents`, corrupt index, disk)
  walks straight into `git commit` as if staging succeeded. Plus a vestigial
  `git diff --cached --stat` / conditional `add -A` message-heuristic block whose
  output was thrown away (`_ = out`) while `message` was a hardcoded constant.
- `push.go`: `stageAndCommit(...)` was a **void** helper discarding BOTH
  `add -A` (`.Run()`) and the `commit` error (`commitOut, _ :=`) — so
  `runSyncPush` prompted "Push to remote?" and pushed even on a genuine commit
  failure.

The user caught it post-merge by reading the diff. A grep for the headline verb
(`commit`/`push`) missed the preludes because they use a *different* verb
(`add`) and hide inside a void helper with no `error` return to surface through.

## Rule

- Audit the WHOLE sequence: for a fixed pipeline, every step that can fail
  (stage, config, fetch, add, commit, push) must surface OR
  deliberately-and-documentedly ignore its error — not just the last/most-visible
  one.
- Void helper functions that run failable commands are swallow magnets: if a
  helper runs a command whose failure should abort the caller, it must RETURN
  `error` (change the signature + update every caller), not print-and-continue.
- Distinguish real swallows from benign display/status reads. `.Output()` on
  `rev-parse` / `status --porcelain` / `log --oneline` for a display line, or
  `rm --ignore-unmatch --quiet`, are correct as empty-on-failure UX — leave them
  (and say why in the answer). The test is: **does an ignored failure let a
  subsequent step act on false success?** If yes, surface it; if it only affects
  an informational line, it is fine.

## How to apply

- For each command in a fixed pipeline, grep for ALL the verbs
  (`add`, `commit`, `push`, `fetch`, `checkout`), not just the one in the task
  title.
- Flag every `).Run()` with no error check and every `, _ :=` / `, _ =` on
  `.Output()` / `.CombinedOutput()`, then classify each: (a) proceeds-on-false-
  success → surface via `CombinedOutput()` + a wrapped return that includes the
  captured output; (b) display/best-effort → leave, with a comment on why.
- Void helpers running commands → give them an `error` return and propagate it at
  the call site *before* any destructive/irreversible next step (a confirm
  prompt, a push, a remove).

## Cross-references

- `[[tests-must-drive-the-production-path]]` — add a test that drives the
  failing-prelude path (a real `add -A` failure surfaces), not just the happy
  path.
- `[[leverage-cross-platform-fs-helpers]]` — simulate the command failure via
  `internal/testutil` forced-error helpers, never a `runtime.GOOS` branch.
