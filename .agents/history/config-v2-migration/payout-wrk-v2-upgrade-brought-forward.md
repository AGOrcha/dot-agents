# Payout upgrade — brought forward for 0.4.1

Brought-forward note for `config-v2-migration` task `p8-payout-wrk-v2-upgrade`. The
live migration/readback runs on the Windows box against `NikashPrakash/payout-wrk`;
this repo owns the readiness picture and the readback record only.

## What "payout upgrade for 0.4.1" means

Target repo: `NikashPrakash/payout-wrk` (org `PayoutPos`, has submodules, Windows
machine). It currently ships a **v1 `.agentsrc.json`** and has a **git source
configured**. The upgrade is two proofs plus one diagnosis:

- **(a) Migrate v1 -> v2 via `da config migrate`** (PR #138). The command copies the
  ORIGINAL manifest to `.agentsrc.json.v1.bak` BEFORE writing the v2 file (always
  recoverable), folds the deprecated keys (`verifier_profiles` / `reviewer_profiles`
  / `app_type_verifier_map`) into the unified `stage_profiles` / `execution_profile`
  model, bumps `version` to `2`, and is idempotent (clean v2 = no-op, no backup).
  Validate with `--dry-run` first.
- **(b) Verify a fresh install-from-git-source resolves correctly** for payout-wrk's
  configured git source: clean clone (incl. submodules) + `da install` / `da refresh`
  -> local source resolves, units lock + layer stack resolve with no error.
- **(c) Triage the Windows "failed to get the local" error** the maintainer hit. Do
  this BEFORE (a)/(b) — it may block install-from-git.

## "failed to get the local" — triage (candidate root causes)

The literal string is not in the codebase; it is a runtime symptom. Most plausible
origin is the **local-source resolve** step, which emits `local source: ...` errors
(`internal/config/local_source.go`: `open %s`, `git init %s`, `empty root`). The
"local" source is the git-backed `~/.agents` repo (§7A); install-from-git reaches it
via the install path.

Windows-specific suspect: `commands/internal/lifecycle/install.go` `CloneGitSource`
shells out to a **real git binary** — `exec.LookPath("git")` then `git clone --depth 1`.
Unlike `internal/config/fetcher.go` (`gitCloneShallow`, in-process go-git), this path
carries a git-binary/PATH dependency.

Candidates to confirm on the Windows box:
1. `git` not on PATH for the process invoking `da` (LookPath fails) -> clone/local
   resolve fails.
2. Submodule handling in the shallow `git clone --depth 1` (payout-wrk has submodules).
3. `~/.agents` local source dir not initialized / not openable (`local source: open`).

Likely durable fix if (1)/(2) is the cause: the existing config-v2-migration fold-back
**`fb:consolidate-clone-on-gogit`** — migrate `CloneGitSource` to in-process go-git
(`gogit.PlainCloneContext`, reuse fetcher ref-normalization), removing the
git-binary/PATH dependency and the argv flag-injection class. Land it first IF
diagnosis confirms it blocks.

## Dependency picture (maintainer: assume no deps unless a REAL blocker)

- **#124 MERGED** (2026-06-24) — the earlier work-plan "p6-payout-backfill depends on
  #124" dep is **satisfied**. (That `p6-payout-backfill` task in
  `loop-discipline-stop-hooks` is a DIFFERENT scope — hook-discipline backfill to the
  local `/Users/nikashp/Documents/payout` — not this payout-wrk v1->v2 upgrade.)
- **REAL BLOCKER: PR #138 is OPEN** (`feat/config-migrate-command`, task
  `p7b-v1-auto-migrator`, status `awaiting_owner_review`). `da config migrate` does
  not exist on master yet, so step (a) cannot run until #138 merges. Encoded as
  `p8.depends_on: p7b-v1-auto-migrator`.
- **CANDIDATE BLOCKER (diagnose): the Windows local-fetch error** — step (c). If it
  blocks install-from-git, resolve before (b).
- Requires Windows access to the payout-wrk box for live execution.

So: NOT dep-free. One confirmed blocker (#138 merge) and one candidate blocker (the
Windows local-fetch bug) stand between "brought forward" and "executable."

## Readiness checklist for the live run

- [ ] **PR #138 merged** to master (provides `da config migrate`). BLOCKER for (a).
- [ ] dot-agents at the #138-bearing version installed on the Windows box (`da version`).
- [ ] **Windows local-fetch bug diagnosed** (step c): reproduce, identify root cause
      from the candidates above. If git-PATH/clone is the cause, land
      `fb:consolidate-clone-on-gogit` (or confirm it is a non-blocker / env fix).
- [ ] Windows access to the `payout-wrk` checkout (org `PayoutPos`), submodules
      initialized (`git submodule update --init --recursive`).
- [ ] payout-wrk on a clean branch with the v1 `.agentsrc.json` committed (so the
      `.v1.bak` + v2 diff is reviewable; rollback = restore from backup).
- [ ] `da config migrate --dry-run` previews the expected fold + planned `.v1.bak`
      path before applying.
- [ ] After apply: v2 `.agentsrc.json` validates; `.agentsrc.json.v1.bak` present and
      byte-identical to the pre-migration original; re-run is a no-op.
- [ ] Fresh clone of the git source + `da install`/`da refresh` resolves local source,
      units lock, and the full layer stack without error (step b).
- [ ] Readback written to
      `.agents/history/config-v2-migration/payout-wrk-v2-upgrade-readback.md`
      (pre-migration shape, migrate invocation + backup path, v2 delta, fresh-install
      resolution, Windows diagnosis outcome).
