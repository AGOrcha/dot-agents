# Const extraction can trigger Sonar CPD on tabular data

## Pattern

When fixing a Sonar `go:S1192` duplicated-literal warning by extracting consts, the resulting code can trigger **Sonar Copy/Paste Detector (CPD)** on what looks like a clean refactor — specifically when the dup'd literals were originally arranged in a tabular layout (each row mostly the same shape with one differing string).

Symptom: S1192 disappears (consts extracted), but new dup density appears on the same file. Sonar's CPD treats repeated `{name, label, fn, fn, fn}` row entries as duplicated blocks even though only the first column varies.

## Root cause

Tabular literal data — like `commands/explain.go`'s tree-rendering row table — is INTRINSICALLY repetitive because each row shares the same structural shape. Extracting the differing string into a const just renames "duplicated literal" into "duplicated block." Sonar's two analyzers (linter for S1192 vs CPD for block duplication) flag the same code differently.

Encountered on PR #117 (master-sonar-hygiene) during commit `4314c108`: the initial const extraction passed S1192 but tripped CPD until the row table itself was refactored to data-driven form (one loop over a `[]struct{...}` rather than 13 copy-pasted rows).

## Rule

When fixing S1192 dup literals:
1. First check whether the literal participates in a tabular layout (≥3 rows of similar shape). If yes, extracting just the literal isn't enough — the whole table needs to become data-driven.
2. Convert to: `for _, row := range []struct{Name, Label string; Render func()}{ {...}, {...}, ... }` — one loop over a typed slice. The repeated `{...}` per row no longer reads as duplicated code blocks to CPD.
3. Run BOTH the linter (`golangci-lint run`) and a fresh Sonar PR analysis after the refactor; the linter catches S1192, but only Sonar's CPD catches block-level dup.

## How to apply

For any new S1192 fix:
1. Read 20 lines around each occurrence — is the literal in a row-like context?
2. If row-like → data-driven refactor (not just const extraction).
3. If standalone → const extraction is fine.
4. Push and let Sonar's PR analysis confirm both gates pass.

## Cross-references

- `[[no-lazy-allowlist-tech-debt]]` — don't allowlist the CPD warning; the data-driven refactor is the right fix
- `[[worker-owns-pr-readiness-loop]]` — the loop should ratchet through const-extraction AND its possible follow-on CPD warning before returning READY
