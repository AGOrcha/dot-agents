# Archived Delegations — 2026-05-23

Stale active delegation artifacts cleaned up during session-start triage on 2026-05-23.

## Contents

- `t4-extract-workflow-tests.yaml` — bundle for task `go-test-fixture-extraction / t4-extract-workflow-tests`. Task completed and landed in PR #44 (merged before this session). Bundle and merge-back leftover in `.agents/active/` after closeout; archived here.
- `t4-extract-workflow-tests.merge-back.md` — sibling merge-back narrative for the same task.

## Not archived (intentionally kept active)

- `.agents/active/delegation/cg6b-b3-workflow-helpers.md` + `.AUDIT.md` — re-audited 2026-05-23: the 4 target files (`commands/workflow/{fs,drift,health,graph}.go`) are still in `scripts/coverage-exceptions.txt` at their pre-B3 percentages, confirming the spawn was never executed after PR #35 cleared the gate. Contract remains a valid pre-authored bundle and is queued for revival when coverage work resumes.
