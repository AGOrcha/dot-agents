# `da workflow fold-back update` clobbers a proposal-routed fold-back's proposal body

## Pattern

Running `da workflow fold-back update --plan <p> --slug <s> --observation "<new text>"`
on a **proposal-routed** fold-back (one created with `--propose`, `routed_to:
proposal:obs-<slug>.md`) rewrites BOTH the staging artifact AND the destination proposal
file `~/.agents/proposals/obs-<slug>.md` — the proposal body is REPLACED with the new
observation, destroying the original design content. The proposal ends up circular
("routed to proposal <this file>") and the real content is lost.

## Root cause

`fold-back update` re-runs the same routing that `create` does, re-materializing the
destination from the (now-updated) observation. For a proposal route the destination is a
free-standing file (frequently in an untracked `~/.agents` tree), so the overwrite is not
recoverable from the repo. Sibling of `workflow-task-update-replaces-notes` (same
replace-not-append clobber family).

## Rule

- To mark a **proposal-routed** fold-back consumed/archived: **do NOT run `fold-back
  update`.** Just `git mv` the staging artifact to
  `.agents/history/<plan>/fold-backs/[<task>/]<slug>.yaml` (PLURAL `fold-backs/`) and, if a
  resolution marker is wanted, hand-edit the ARCHIVED copy (it has left the CLI-managed
  active queue). The proposal file is the durable record — never touch it via the CLI.
- `fold-back update` is only safe for **task_note / plan_summary** routed fold-backs (it
  refines an inline note), and even then it REPLACES, not appends — reconstruct original +
  addition.
- Recovery when clobbered: the pre-update observation is in git on the staging artifact's
  add-commit (`git log --all --diff-filter=A -- .agents/active/fold-back/<slug>.yaml`, then
  `git show <c>:<path>`); restore the proposal body from there.

## How to apply

Archiving proposal-routed fold-backs = `git mv` only. Reserve `fold-back update` for
note-routed records, reading current text first. `git diff` / inspect the proposal file
before committing any fold-back archival batch.
