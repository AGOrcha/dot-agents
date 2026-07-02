// Package sandbox provisions isolated working directories for R4 eval runs.
//
// Per R4 spec decision D4.2, the sandbox is filesystem isolation — not a
// security boundary against deliberately malicious code. The v1 provider
// (NewWorktreeSandbox) materializes each run as a linked git worktree of the
// source repository, checked out detached at the repo's current HEAD, under
// the eval-namespaced run root (.agents/eval/runs/<run-id>/worktree), plus a
// scratch HOME directory so the agent CLI cannot write into the operator's
// real home. Two concurrent Provision calls yield working trees that cannot
// see each other's writes, and neither can mutate the operator's checkout
// (R4 requirement R4).
//
// The detached-HEAD checkout is deliberate: gitwt's AddDetached is the
// documented path for ephemeral checkouts that need no branch, and it leaves
// no branch behind in the shared object store when the run is cleaned up.
// (Branch-mode worktrees would accumulate wt-* branches across runs because
// the gitwt seam has no branch-deletion operation — litter the plan's
// "ephemeral branch" sketch did not intend.)
//
// # Retention (R4 spec OQ6, R8)
//
// Sidecars under a run dir (taskspec.yaml, eval-run.yaml, ...) are retained
// indefinitely — they are small and drive the dashboard. Working trees are
// subject to a retention window (DefaultRetention, 7 days): PruneStale
// removes the worktree, scratch HOME, and sandbox marker of every run older
// than the window while leaving the run dir and its sidecars in place. The
// harness calls PruneStale on the next eval run, so a crashed run cannot
// leak working trees beyond the window.
//
// # Provider swap point
//
// Callers bind to the Sandbox interface only. A DockerSandbox, or the
// worktree-platform plan's managed-worktree provider, can replace the v1
// implementation without any caller change.
package sandbox
