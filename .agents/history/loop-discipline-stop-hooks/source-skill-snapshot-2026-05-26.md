# Source Skill Snapshot — 2026-05-26

**Use these SHAs as the source-of-truth for P3 / P3b / P4 copy ops.**

P3 (starter promotion), P3b (companion discipline skills scaffolding), and P4
(sentinel wiring) all copy from `~/.agents/skills/dot-agents/{iteration-close,
isp, loop-worker, delegation-lifecycle}/` and `~/.agents/skills/global/
agent-handoff/`. If a maintainer edits one of those trees mid-flight, the
starter copy silently drifts. This snapshot pins the baseline. Any of those
three tasks that copy from a different hash than recorded below must be
re-validated before merge.

## Snapshot metadata

- Captured: 2026-05-26
- Source repo: `~/.agents` (HEAD = `22d3bc8e29a65f8bd01c0bf689514e80d78db81b`)
- Capture method per tree:
  - **git tree SHA** — `git -C ~/.agents rev-parse HEAD:<path>` (committed state)
  - **worktree content sha256** — sorted manifest of `sha256(file)  path`
    for every regular file under the tree, then sha256 of the manifest
    (reflects on-disk bytes including any uncommitted edits)
- Working-tree dirtiness at capture: `~/.agents` has uncommitted changes;
  only `skills/dot-agents/loop-worker/instructions/gotchas.md` is dirty among
  the five target trees (1 insertion, 1 deletion vs committed tree). The
  other four trees are clean — their git tree SHA and worktree content hash
  describe identical bytes. P3/P3b/P4 should copy the **worktree** state
  (filesystem bytes) and verify against the worktree content sha256, not the
  git tree SHA, because the starter promotion reads from disk.

## Recorded SHAs

### 1. `~/.agents/skills/dot-agents/iteration-close/`

- git tree SHA: `a8b817bf38360854154dd10b2df50f7b0a76f812`
- worktree content sha256: `563335201b27c52974e3133521a371e6e567d197af3253604bf6aef4e3306795`
- sample mtime: `SKILL.md` = `2026-05-03T23:46:19`
- dirty vs HEAD: no

### 2. `~/.agents/skills/dot-agents/isp/`

- git tree SHA: `b2611e60430156ed668ff830cd03d3a0a62f6b42`
- worktree content sha256: `3aaae36f66c6b839b28ea1521ed153abef6e7b33f725e792fab49c5dea9d4c9a`
- sample mtime: `SKILL.md` = `2026-04-21T10:47:54`
- dirty vs HEAD: no

### 3. `~/.agents/skills/dot-agents/loop-worker/`

- git tree SHA: `76ecb7cf8f54add033f1374255c4931d6ae90a9f`
- worktree content sha256: `0c19a8c923c5e9c438d5491605f92e0801a779f895446ad81cddbe6a5ec4e594`
- sample mtime: `SKILL.md` = `2026-05-21T23:56:52`
- dirty vs HEAD: **yes** — `instructions/gotchas.md` has 1 insertion / 1 deletion
  vs the committed tree. P3 should treat the worktree content sha256 as
  authoritative.

### 4. `~/.agents/skills/dot-agents/delegation-lifecycle/`

- git tree SHA: `2a8cb51373ed94ac757414bacd1484dd70fcf775`
- worktree content sha256: `40492a21f656ed89dec408176cde578e80f0ffe8daf0852c1c72f4cdcbef4057`
- sample mtime: `SKILL.md` = `2026-05-21T23:56:52`
- dirty vs HEAD: no

### 5. `~/.agents/skills/global/agent-handoff/`

- git tree SHA: `c981df1c23d7af979d2d5e4e454c67dc10be6247`
- worktree content sha256: `460eb71b76c74943c684dd2626761342f23c963d798fd2107eceb4a6d855a335`
- sample mtime: `SKILL.md` = `2026-03-23T19:33:11`
- dirty vs HEAD: no

## Re-verification recipe

To confirm a tree has not drifted before P3 / P3b / P4 copy:

```sh
# git tree SHA (committed state)
git -C ~/.agents rev-parse HEAD:skills/dot-agents/iteration-close

# worktree content sha256 (on-disk bytes)
cd ~/.agents && find skills/dot-agents/iteration-close -type f -not -path '*/.*' \
  | sort \
  | while read f; do
      h=$(shasum -a 256 "$f" | awk '{print $1}')
      echo "$h  $f"
    done \
  | shasum -a 256 | awk '{print $1}'
```

If either value differs from the table above, pause and reconcile with the
maintainer before continuing the copy.
