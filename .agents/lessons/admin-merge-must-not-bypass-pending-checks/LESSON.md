# Lesson: `--admin` merges must never bypass PENDING checks (parse `gh pr checks` correctly)

## Symptom

Twice this session (#486, #494) a PR was `gh pr merge --admin`-merged while its
coverage-gate / OS `-race` jobs were still **pending**. Both happened to pass,
but the merge decision did not actually wait for them — a latent way to land a
red gate on master.

## Root cause

Hand-parsing `gh pr checks` and reading STATUS from the wrong column.
`gh pr checks` is TSV: **col1 = NAME, col2 = STATUS** (`pass`/`pending`/`fail`),
col3 = elapsed, col4 = URL. The merge gate computed pending as:

```sh
pend=$(gh pr checks $pr | awk -F'\t' 'NF>1{print $1}' | grep -ciE pending)
```

`$1` is the NAME ("Test on windows-latest"), which never contains "pending", so
`pend` was always 0 → the "all green?" gate falsely passed. `--admin` then
bypassed branch protection's required-checks gate, so nothing else stopped the
merge past pending. Buggy parse + `--admin` compounded.

The tell: the *display* line used `print $1"\t"$2` and correctly showed
`pending`, but the *merge-condition* used a different, wrong parse. Two parses
diverged.

## Fix

1. **Prefer `gh pr merge --auto --merge`** — GitHub merges only once checks
   pass; zero hand-parsing. BUT it needs repo auto-merge enabled;
   `AGOrcha/dot-agents` has it **disabled** (`GraphQL: Auto merge is not allowed
   ... enablePullRequestAutoMerge`), so it is unavailable here.
2. **When `--auto` is unavailable, poll with the STATUS column ($2)** and gate
   the merge on **zero `fail` AND zero `pending`**, both read from `$2`:
   ```sh
   fail=$(gh pr checks $pr | awk -F'\t' 'NF>1 && $2=="fail"{print $1}')
   pend=$(gh pr checks $pr | awk -F'\t' 'NF>1 && $2=="pending"{print $1}')
   # merge only when both are empty
   ```
3. **`--admin` is legitimate only for the review-approval gate** (the user
   directed the work; no reviewer available) — it must NEVER be used to merge
   past a pending or failing **check**. Before any `--admin` merge, print the
   full `gh pr checks` table and confirm every relevant row is `pass`; never
   trust a scalar count from a fragile parse.

## Rule of thumb

Keep ONE parse for both display and gating. If the table you print shows
`pending`, the merge condition must see it too — status lives in column 2.
