---
id: obs-gitmodifiedfiles-leading-space-misslice
kind: observation
status: unreviewed
created: 2026-07-21
area: commands/workflow/state.go (parseGitModifiedFiles / gitModifiedFiles ~986-1000)
severity: low
surfaced_by: H2 perf dedupe (PR #485) — extracting parseGitModifiedFiles exposed the quirk; preserved byte-identically there (a perf PR must not change behavior), filed here for a separate fix
---

# `git status --short` filename mis-slice on any leading-space (unstaged) status code

## Observation

`parseGitModifiedFiles` (extracted verbatim from the original `gitModifiedFiles` in #485)
computes each modified path by applying `strings.TrimSpace(line)` to EACH line and then
slicing `line[3:]`. Porcelain-v1 short lines are fixed-width `XY<space>PATH` — two status
columns plus a one-space separator, where a SPACE is itself a significant status value
(`X` empty = unstaged). For any worktree-modified-but-unstaged entry the line begins with a
space (` M path`). The per-line `TrimSpace` eats that leading space, so `line[3:]` slices
one byte too late and drops the first character of the filename:

```
" M foo.go"  --TrimSpace-->  "M foo.go"  --[3:]-->  "oo.go"   (should be "foo.go")
```

- This hits **every** leading-space entry (` M`, ` D`, ` A`, ` R`, …), not just the first
  — it is per-line, not an aggregate-output trim.
- Staged (`M `, `A `) and untracked (`??`) lines have a non-space first column, so they
  slice correctly — which is why existing tests (which use those) never caught it.
- Impact: `status`/`orient`/`checkpoint` record a filename missing its first character for
  unstaged entries. Record fidelity only (no control-flow effect) → low severity, but it
  affects the common unstaged-edit case.

## Root cause

`strings.TrimSpace(line)` before a FIXED-COLUMN slice (`line[3:]`) breaks the porcelain
contract that the first two columns are significant. Pre-existing on master; #485 only
relocated the logic into `parseGitModifiedFiles` and preserved it exactly (equivalence
tests compare against the unchanged `gitModifiedFiles`, so the quirk is invisible to them).

## Suggested fix (separate reviewed change — NOT applied in the perf PR)

1. Do not `TrimSpace` the line before the column slice. Split raw output on `\n`, drop only
   an empty trailing line, and slice each non-empty line at index 3 (`XY ` = 3 bytes); or
   parse `git status --porcelain=v1 -z` and split on NUL for unambiguous framing (also
   handles paths with spaces / renames `R  old -> new`).
2. Add a table test with leading-space first columns (` M path`, ` D path`, `MM path`,
   `R  old -> new`) asserting exact paths.

## Evidence

- `commands/workflow/state.go` `parseGitModifiedFiles` (post-#485) / `gitModifiedFiles`
  (pre-#485): per-line `strings.TrimSpace` then `line[3:]`.
- Surfaced in the H2 merge-back (PR #485) notes: quirk preserved, not fixed, to keep the
  perf change behavior-identical.
