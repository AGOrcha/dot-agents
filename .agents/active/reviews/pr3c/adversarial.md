# Adversarial Review — pr3c/kg (PR#18)

_Lens 3 of 3 (adversarial / red-team), general-purpose (codex Bash-blocked in
this context), read-only. Range: `pr3b-rebased..HEAD`._

## Verdict
**acceptable-with-findings** — headline bug fix correct & complete; the
path-traversal exposure is real but pre-existing code relocated by the
extraction (pr3c now owns it), not introduced by pr3c.

## Critical / security findings

1. **Note-ID path traversal via inbox frontmatter (`commands/kg/kg.go:1064,
   1081,1129`).** `ingestSource` re-parses `src.ID` from the file's own YAML
   frontmatter (`parseRawSourceFrontmatter`, kg.go:1064 — a non-empty
   malicious value is kept verbatim). It flows unsanitized into
   `buildSourceNote` (`note.ID = "src-"+src.ID` → `createGraphNote` →
   `filepath.Join(dir, note.ID+".md")`, kg.go:967) and `ingestDecisionNotes`
   (`dec-%s-%d`, kg.go:1129). `slugify()` (strips `/` and `.`) **is applied to
   entity IDs (line 1098) and `kg add` (line 1277) but NOT to `src.ID` on the
   ingest path.** A crafted inbox file with `id: ../../../../tmp/pwn` writes
   outside KG_HOME (0644). Requires attacker-authored file in `raw/inbox/` (or
   a poisoned upstream source). **Fix: wrap `src.ID` with `slugify()` in
   `parseRawSourceFrontmatter` (1 line) + regression test.** pr3c owns kg.go.

## High

2. **compact is not crash-atomic (`query_lint_maintain.go:1065-1076`).**
   `archiveCompactedNote` does `os.Rename(src,dst)` then `removeIndexEntry`.
   Rename is atomic (body preserved); if the process dies between rename and
   index rewrite, the note is in `_archived/` but still listed in `index.md`
   (stale, **recoverable, not lost**; order is safe). Not data-loss; document
   as a known consistency window.

## Medium / hardening

3. **`removeIndexEntry` full-file rewrite race
   (`query_lint_maintain.go:1080-1093`).** Read-modify-`os.WriteFile` (not
   atomic temp+rename). Concurrent compact+ingest can lose one writer's index
   entry (last-writer-wins, no locking). Same pattern in lint-report (line
   798) and graph-health writes. Pre-existing; acceptable for a single-user
   CLI; note as a known limitation.

4. **`markNoteStale`/`persistReweavedNote` body-loss — VERIFIED FIXED.** Traced
   every note-rewrite sink: `f06ab87` is complete. `persistReweavedNote` now
   reads+parses the on-disk body and passes it through. `markNoteStale`
   already preserved body. `archiveCompactedNote` uses `os.Rename` (whole
   file). No remaining frontmatter-rewrite-drops-body sites.

## Hidden assumptions (validated)
- Bridge/SQL/exec clean — no `exec.Command`, no Sprintf-SQL, no raw
  `db.Query` in any kg file; all delegate to graphstore typed methods /
  `NewCRGBridge` (pr3b-owned).
- `sync_code_warm_link` "links" are sqlite rows, **not** filesystem symlinks —
  no `os.Symlink`/`EvalSymlinks`/`RemoveAll`; no symlink TOCTOU.
- Refactor `d39c6cb` semantically faithful — `for s := range steps { if
  !t.Run(...){return} }` preserves abort-on-first-failure; `curationState`
  pointer-shared, sequential, no stale-state risk. (Soft `t.Error` steps still
  continue — matches original `runStep` exactly; not a regression.)
- Seam globals safe **only because** no kg test uses `t.Parallel()`; all
  swaps use `t.Cleanup`. Implicit — adding `t.Parallel()` later would
  silently corrupt cross-test seam state. Worth a `seams.go` comment.
- `commands/kg.go` shim is clean delegation; errors propagate via Cobra
  `RunE`; no swallowing/widened surface. `explain.go`/`review.go` not modified
  in pr3c.

**Recommended gate:** finding #1 (slugify `src.ID`) before merge if untrusted
inbox content is plausible.
