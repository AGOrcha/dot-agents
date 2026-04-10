# Implementation Results 4

Date: 2026-04-10
Task: Audit and refine the scaffolded shell workflow hooks for better parity with the Go workflow model.

## Gaps Identified

Before this slice, the shell hooks still had these notable gaps:

- `session-orient` reimplemented a partial orient view instead of preferring the Go workflow collector
- `session-capture` reimplemented checkpoint writing and next-action derivation instead of preferring `dot-agents workflow checkpoint`
- the session hooks could still fail hard rather than behaving as non-blocking hooks
- `secret-scan` could skip a real secret if the file also contained placeholder text
- shell fallback output was close to the spec but not aligned enough with the Go-side formatting and warning behavior

## Refinements Completed

- `session-orient` now prefers `dot-agents workflow orient` when `dot-agents` is available and falls back to shell rendering otherwise
- `session-capture` now prefers `dot-agents workflow checkpoint` when `dot-agents` is available and falls back to shell checkpoint writing otherwise
- both session hooks now exit successfully even if the preferred Go command fails, matching the non-blocking requirement
- the shell fallback orient output now mirrors the Go sections more closely, including:
  - summarized checkpoint fields instead of dumping raw YAML
  - warning output for missing lessons index or missing git repo
  - cleaner lesson rendering
- `secret-scan` now ignores placeholder-only matches without masking real secret matches elsewhere in the file

## Files Changed

- `internal/scaffold/hooks/global/session-orient/orient.sh`
- `internal/scaffold/hooks/global/session-capture/capture.sh`
- `internal/scaffold/hooks/global/secret-scan/scan.sh`

## Verification

Ran:

- `/bin/sh -n` syntax checks on the refined shell scripts
- direct fallback execution of `session-orient` against a temp git repo and temp `AGENTS_HOME`
- direct fallback execution of `session-capture` against a temp git repo and temp `AGENTS_HOME`
- direct positive-path execution of `secret-scan` to confirm placeholder text does not suppress a real secret warning

## Remaining Refinement Area

The main remaining shell-hook work is incremental:

- add stronger automated coverage around the scaffolded scripts if desired
- expand shell fallback parsing only when the Go workflow model grows
- consider whether future hooks should call additional Go workflow subcommands as those mature
