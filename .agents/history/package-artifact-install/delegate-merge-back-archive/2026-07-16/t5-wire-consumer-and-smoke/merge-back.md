---
schema_version: 1
task_id: t5-wire-consumer-and-smoke
parent_plan_id: package-artifact-install
title: Wire dot-agents onto da-agc and add the tree content-install smoke test
summary: 'Wired the real AGorcha/da-agc git source + extends + packages[] (release-docs-refresh skill, platform-dirs-change-analyst + promise-gap-analyst agents) into dot-agents own .agentsrc.json. Manually proved the full mechanism end-to-end against the LIVE da-agc repo in an isolated sandbox (fetch -> CAS materialize -> kind:artifact lock + artifact-content digest -> per-project CAS-direct projection -> config verify, all green). Added hermetic, offline CI coverage: internal/config/resolver_test.go drives the real gitArtifactFetcher+MaterializeToStore+VerifyStoreContentDigest chain against an in-memory fixture shaped exactly like da-agc tree; scripts/verify.sh extends the git-source-smoke fixture (DC2) with a full content-install lane (materialize/lock/project, frozen no-op, tamper-fail+self-heal, deleted-lock re-fetch). Left .agentsrc.lock untouched due to a discovered local-vs-sourced name collision on this machine (see Integration Notes). Landed as 13d0a2c5 on worktree-agent-ab8c70c7c3768d707 (base feat/package-artifact-install).'
files_changed:
    - .agentsrc.json
    - internal/config/resolver_test.go
    - scripts/verify.sh
verification_result:
    status: pass
    summary: 'go test ./internal/config/... ./commands/... ./internal/... all green (no regressions). bash scripts/verify.sh: 110 passed, 0 failed, including the new content-install lane (materialize+lock+project, frozen no-op byte-identity, tampered-CAS verify failure + self-heal, deleted-lock forced re-fetch). gofmt/go vet/shellcheck clean on all touched files. Manually drove the REAL binary against the LIVE AGorcha/da-agc repo in an isolated sandbox (separate from this repo checkout) and confirmed fetch -> materialize -> lock -> project -> config verify all pass end-to-end.'
integration_notes: |-
    Delegation contract for t5-wire-consumer-and-smoke was not present in this
    worktree's .agents/active/delegation/ (only t2-materialize-store.yaml was
    synced here), so `da workflow merge-back` could not record against it —
    exactly the stale-contract case the bundle anticipated ("commit + report —
    I close out from main"). Work is committed as 13d0a2c5 on
    worktree-agent-ab8c70c7c3768d707 (base feat/package-artifact-install);
    please close out from the main checkout.

    Two things worth knowing before wiring this further:

    1. BEFORE running a real `da install --yes` on any machine with this
       .agentsrc.json: check for a pre-existing
       ~/.agents/skills/dot-agents/release-docs-refresh (or any local-authored
       dir matching a newly-sourced package name) and retire/rename it first.
       This machine already has one (from an earlier, unrelated iteration),
       and install fails closed with a "conflicting intents" error rather than
       silently picking a winner — local-scope skill/agent discovery is a
       directory SCAN of ~/.agents/<bucket>/<project>/, not driven by
       .agentsrc.json's skills[]/agents[] arrays, so the manifest alone cannot
       suppress the collision. That's why .agentsrc.lock is untouched here:
       forcing a real install on THIS machine would have left the repo in a
       broken-lock state (pass-2 writes the lock before projection runs, so a
       failed projection still leaves fresh artifact units in the lock).
    2. For git "tree" packages there is no CAS-first fetch short-circuit:
       every install/refresh that participates in packages resolution
       re-clones each git-tree ref from upstream even when the CAS is already
       warm and verified (timed directly against the live da-agc remote:
       ~2s per re-run, confirmed via `time da install --yes` twice in a row).
       The no-write GUARANTEE holds at the lock/CAS/projection layer (byte-
       identical, confirmed) — just not at the network layer. Neither of
       these is in t5's write_scope (packages_pass2.go / resource_plan.go).
created_at: "2026-07-16T02:10:00Z"
---

## Summary

Wired the real `AGorcha/da-agc` git source into dot-agents' own `.agentsrc.json`:

```json
"sources": [
  { "type": "local" },
  { "type": "git", "id": "da-agc", "url": "git@github.com:AGOrcha/da-agc.git", "ref": "main" }
],
"extends": ["da-agc:dot-agents/base.json"],
"packages": [
  "da-agc:skill/release-docs-refresh@main",
  "da-agc:agent/platform-dirs-change-analyst@main",
  "da-agc:agent/promise-gap-analyst@main"
]
```

Confirmed the live repo's real tree shape via `gh api repos/AGOrcha/da-agc/git/trees/main` before wiring (`skill/release-docs-refresh/SKILL.md`, `agent/platform-dirs-change-analyst/AGENT.md`, `agent/promise-gap-analyst/AGENT.md`, `dot-agents/base.json` declaring `skills:["release-docs-refresh"]`, `agents:[the two names]`) — the fixtures added below mirror this exactly.

**Manual live-remote proof (DOGFOOD, done in an isolated sandbox, not this repo checkout):** built the `da` binary, pointed a scratch project's `.agentsrc.json` at the identical wiring above, and ran `da install --yes` for real against `git@github.com:AGOrcha/da-agc.git` over SSH. Result: git layer resolved and cached content-addressed by SHA (`b20340c3...`), all 3 packages materialized into the CAS (`~/.agents/cache/artifacts/{skills,agents}/<digest>/`), a `kind:artifact` lock unit + `artifact-content` digest anchor recorded per ref, and the skill/agents projected as CAS-direct symlinks into `.claude/skills/release-docs-refresh/`, `.claude/agents/platform-dirs-change-analyst/`, `.claude/agents/promise-gap-analyst/`, plus Codex `.toml` agent files (t2b multiplatform). `da config verify` reported `OK` (5 passed, 0 failed) — this exercises the H7 production digest resolver end-to-end. A timed second `install --yes` run: lock's `units`/`artifact-content` sections byte-identical (only the cosmetic `install.stamped_at` timestamp moved), CAS file mtimes unchanged (no rewrite) — but the run still took ~2s, i.e. a real git re-clone happened (see Integration Notes #2).

**Automated coverage added** (network-free, runs in CI):

- `internal/config/resolver_test.go` — `TestGitArtifactFetcher_DaAgcMirror_MaterializesAllThreeRefsAndVerifies` (positive, subtests per ref: fetch via the real `gitArtifactFetcher` against an in-memory git fixture shaped exactly like da-agc's tree, materialize via `MaterializeToStore`, verify content on disk, confirm `VerifyStoreContentDigest` reports present+matching, confirm a second materialize of the same bundle is a true no-op) and `TestVerifyStoreContentDigest_DaAgcMirror_DetectsCASTamper` (negative: tamper one ref's published CAS bytes post-materialize, confirm `VerifyStoreContentDigest` flips to matches=false while an untouched sibling ref stays clean — proves per-digest isolation).
- `scripts/verify.sh` — new "GIT-SOURCE CONTENT-INSTALL smoke" lane (extends the existing git-source-smoke fixture per spec DC2) against a hermetic local bare-repo source (the same `file://` clone path a network remote takes): first-run install materializes+locks+projects all 3 refs; assertions for (a) skill invocable at its projected path, (b) both agents present, (c) the `artifact-content` lock section anchoring all 3 refs by digest, (e) `config verify -> OK`; a second run proves (d) byte-identical no-op (units/artifact-content sections + projected file bytes unchanged); adversarial: a tampered CAS file fails `config verify`, then a normal re-install self-heals it (H16 quarantine+re-extract) and verify passes again; adversarial: deleting `.agentsrc.lock` forces a real re-fetch on the next install and re-anchors the same content.

Full `bash scripts/verify.sh`: **110 passed, 0 failed**. `go test ./internal/config/... ./commands/... ./internal/...`: all green, no regressions.

## Why `.agentsrc.lock` is untouched

Attempting the real wiring against THIS repo checkout's actual `~/.agents` (not the isolated sandbox above) hit a genuine collision: this machine already has a pre-existing, project-scoped, **locally-authored** `~/.agents/skills/dot-agents/release-docs-refresh/` directory from an earlier, unrelated iteration. `da install --yes` failed closed with:

```
shared targets: conflicting intents for .agents/skills/release-docs-refresh:
  skills.dot-agents.release-docs-refresh.agents-skills (.agents/skills/dot-agents/release-docs-refresh)
  vs skills.sourced.da-agc.release-docs-refresh.agents-skills (.agents/cache/artifacts/skills/<digest>)
```

Root cause traced to `internal/platform/resource_plan.go`'s `buildSharedMirrorIntentsForRoot`: local-authored skill/agent discovery is a **directory scan** of `~/.agents/<bucket>/<project>/` (`listScopedResourceDirs`), entirely independent of whether the name appears in `.agentsrc.json`'s `skills[]`/`agents[]` arrays. There is no manifest-level way to say "prefer the sourced package over any pre-existing local dir of the same name" — the projector correctly fails closed rather than silently picking a winner, but the migration itself (retiring the local copy) is an operator-level filesystem step outside `.agentsrc.json` and outside this task's write scope. Notably, `install --yes` DID write a partial lock before hitting this (pass-2 commits the lock before projection runs) — I reverted that partial write so the actual repo checkout's `.agentsrc.lock` stays byte-identical to what was there before this task.

## Verification

- `go test ./internal/config/... ./commands/... ./internal/...` — all green.
- `bash scripts/verify.sh` — 110/110 passed (includes every new assertion above).
- `gofmt -l internal/config/resolver_test.go` — clean.
- `go vet ./internal/config/...` — clean.
- `shellcheck scripts/verify.sh` — clean (one pre-existing, unrelated `SC2034 YELLOW unused` warning, confirmed present on `HEAD:scripts/verify.sh` before this change).
- Manual live-remote round trip against `git@github.com:AGOrcha/da-agc.git` in an isolated sandbox — described above.
