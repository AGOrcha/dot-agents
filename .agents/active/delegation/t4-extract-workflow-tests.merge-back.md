# Merge-back: t4-extract-workflow-tests (stage=impl)

- **Plan:** go-test-fixture-extraction
- **Task:** t4-extract-workflow-tests
- **Stage:** impl
- **PR:** https://github.com/NikashPrakash/dot-agents/pull/44
- **Branch:** t4-extract-workflow-tests (1 commit on origin)
- **Commit:** 67fd9cb refactor(commands/workflow/test): align initWorkflowTestRepoWithCommit with execabs hardening
- **Worktree:** /Users/nikashp/Documents/dot-agents/.claude/worktrees/t4 (keep until merge)
- **Verification status:** pass

## What was delivered

Bundle scope was "refactor 8 commands/workflow/*_test.go files to use the shared internal/testutil package." After full read of internal/testutil (10 helpers: NewTempProject, WriteAgentsRC, WriteAgentManifest, WriteSkillManifest, WriteCanonicalAgent, WriteCanonicalSkill, WriteScopeFile, InitGitRepo, WritePreservationManifest, AssertExtraFieldsPreserved) and full read of all 8 in-scope files, the actual replaceable mechanical surface was tiny — most of the prior migration is already on master.

**Single mechanical change:**
- `commands/workflow/testutil_test.go::initWorkflowTestRepoWithCommit` now uses `golang.org/x/sys/execabs.Command` instead of `os/exec.Command` for the second-commit `git` run. This matches the project-wide go:S4036 hardening already adopted by `testutil.InitGitRepo`, `commands/workflow/iter_log.go`, `commands/workflow/state.go`, `commands/workflow/plan_task.go`, and `commands/workflow/delegation.go`. Behavior unchanged; a comment now explains why this cannot directly route through testutil (testutil.InitGitRepo does init+single-commit, no append-commit mode).

## Why this is the only replacement available

The 8 in-scope files (state_plan_test.go, testutil_test.go, workflow_integration_test.go, delegation_fanout_test.go, iter_log_test.go, foldback_test.go, plan_task_test.go, drift_sweep_test.go) were systematically scanned for the patterns internal/testutil covers:

| Pattern grep | Hits in scope |
|---|---|
| `exec.Command.*git` / `execabs.Command.*git` | 1 site (initWorkflowTestRepoWithCommit, now fixed) — plus 1 in plan_check_scope_test.go which is NOT in this bundle's write_scope |
| Inline `.agentsrc.json` JSON literal | 2 sites — one inside testutil.InitGitRepo's `files` map (already routed through testutil; replacing with WriteAgentsRC would be more verbose, not less), one inside `setupVerifierDispatchProject` which uses extra fields not on `config.AgentsRC` |
| Inline `GIT_AUTHOR_NAME` / `git config user.email` | only inside the initWorkflowTestRepoWithCommit block (this PR) |

Two of the 8 files (`testutil_test.go` and `workflow_integration_test.go`) already `import "github.com/NikashPrakash/dot-agents/internal/testutil"` and call `testutil.InitGitRepo` — the testutil docstrings explicitly cite these as "Replaces commands/workflow/testutil_test.go::initWorkflowTestRepo" and "the inline git block in workflow_integration_test.go::TestWorkflow_EmptyStateGraceful," confirming this work was done as part of t2-create-internal-testutil rather than a separate t4 step.

The other 6 files (state_plan, delegation_fanout, iter_log, foldback, plan_task, drift_sweep) do not directly init their own git repos or write their own `.agentsrc.json` — they consume the shared `initWorkflowTestRepo` and the `setupX` helpers in `testutil_test.go`, so there is nothing in them to point at testutil that isn't already pointed.

## Follow-up backlog (for t5 bundle author)

These would each require **extending internal/testutil** (forbidden for this bundle), so they are explicit t5 candidates:

1. **CanonicalPlan / CanonicalTaskFile / CanonicalSliceFile writers.** Seven `setupX` helpers in `testutil_test.go` (`setupTestProject`, `setupFanoutBase`, `setupFanoutSliceProject`, `setupFanoutTwoTaskProject`, `setupVerifierDispatchProject`, `setupFoldBackProject`, `setupFoldBackTwoPlanProject`) build PLAN.yaml/TASKS.yaml/SLICES.yaml by hand. A `testutil.WriteCanonicalPlan(t, repo, plan)` + `WriteCanonicalTasks(t, repo, planID, tasks)` + `WriteCanonicalSlices(t, repo, planID, slices)` trio would collapse all seven and probably help t3 (internal/platform) too. Single biggest leverage point.

2. **`AppendCommit(t, repo, files)`** for the HEAD~1-needs-to-exist pattern. Only one caller today (`initWorkflowTestRepoWithCommit`) but the boilerplate is identical to `InitGitRepo` minus the init step.

3. **AgentsRC extra-fields ergonomics.** `setupVerifierDispatchProject` writes a custom rc literal with `verifier_profiles` + `app_type_verifier_map` fields not present on `config.AgentsRC`. Routing through `testutil.WriteAgentsRC` would require building a `map[string]json.RawMessage` for `ExtraFields` — more verbose than the literal. Either promote those fields to typed members on `AgentsRC` (per schema-usage rule, atomic 6-step change) or accept the literal in-place.

## Scope discipline

- **forbidden_scope respected.** Did not touch `internal/testutil/**`, `commands/workflow/*.go` production code, `commands/**` outside workflow, `cmd/**`, or `.agents/workflow/**`.
- **No scope-creep event.** No test refactor forced a signature change in production code; the one replacement was self-contained.
- **Worktree discipline.** All git operations went through `git -C /Users/nikashp/Documents/dot-agents/.claude/worktrees/t4 …`; no `cd` into the worktree.
- **Explicit-paths-only `git add`.** Used `git add -- commands/workflow/testutil_test.go`, never `git add .`.

## Verification trace

```
$ cd /Users/nikashp/Documents/dot-agents/.claude/worktrees/t4
$ go build ./...                                              # clean (no output)
$ go vet ./...                                                # clean (no output)
$ go test ./internal/testutil -race -count=1 -timeout 60s
ok  	github.com/NikashPrakash/dot-agents/internal/testutil	1.350s
$ go test ./commands/workflow -race -count=1 -timeout 240s
ok  	github.com/NikashPrakash/dot-agents/commands/workflow	21.434s
```

PR #44 CI on push: Lint Workflows passed (5s); Test on macos-latest / ubuntu-latest / windows-latest pending at handoff time.

## Parent action

1. Review PR #44.
2. If accepted, run `da workflow advance go-test-fixture-extraction --task t4-extract-workflow-tests --status completed` (the CLI flagged the delegation-contract pathway as absent — this bundle was authored as a manual YAML bundle, not via `da workflow fanout`, so the in-progress flip needs to be done at the parent level).
3. Decide whether to author a t5 bundle that **first** extends internal/testutil with the three canonical-plan writers, **then** sweeps the 7 `setupX` helpers in this package + the analogous helpers in `internal/platform/` and `commands/`. That single addition is what would have made *this* bundle meaningfully larger.
