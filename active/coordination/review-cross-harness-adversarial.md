REJECT

# Cross-harness adversarial review — wire-managed-gitignore-autofill (D14/R8)

Resolved prompt: `.agents/prompts/reviewers/cross-harness-adversarial.project.md` via `./bin/da --json workflow resolve-prompt --kind reviewer --slug cross-harness-adversarial`.
Scope: READ-ONLY review of the D14 slice diff plus targeted source/test reads.
Focused verification run: `go test ./commands -run 'TestRunRefresh_WritesManagedGitignoreBlock|TestCollectManagedOutputs_CopilotDynamicAndStaticPlatforms'` and `go test ./internal/links -run TestEnsureManagedGitignore_NeverIgnoresCommittedContract` both pass on the slice, so the findings below are true escape defects / coverage gaps, not red tests.

Verdict: **REJECT** (lens: cross-harness-adversarial).

## BLOCKER 1 — Copilot’s managed-output surface is incomplete, and the new tests would not catch the regression

`da refresh` now writes the managed block through the real production path (`commands/refresh.go:247-298`), which is good. But the newly introduced collector is already wrong for Copilot:

- `Platform.SharedTargetIntents` is explicitly part of the repo-local output surface (`internal/platform/platform.go:66-71`).
- Copilot contributes shared skills there: `BuildSharedSkillMirrorIntents(project, filepath.Join(copilotAgentsDir, "skills"))` (`internal/platform/copilot.go:620-629`), i.e. refresh can materialize `.agents/skills/*` for Copilot.
- `CountLinks` also treats `.agents/skills/` as Copilot-owned managed output (`internal/platform/copilot.go:659-671`).
- But the new `ManagedOutputs()` list omits `.agents/skills/` entirely (`internal/platform/copilot.go:642-649`).

So a real repo with canonical shared skills and Copilot enabled can have refresh project `.agents/skills/*` without the managed `.gitignore` block ignoring them. That violates the task contract to enumerate each enabled platform’s generated/projected repo-local outputs.

This is also the concrete `new_duplicated_lines_density` problem: the output inventory is now duplicated across `SharedTargetIntents`/`CountLinks`/`ManagedOutputs`, and the new list drifted on day one.

### Mutation-verify assessment

The new tests do **not** kill this bug:

- `TestRunRefresh_WritesManagedGitignoreBlock` (`commands/refresh_test.go:1390-1467`) only asserts `.github/hooks/*.json` and `.agentsrc.local.json`, not Copilot’s `.agents/skills/` shared-skill output.
- `TestCollectManagedOutputs_CopilotDynamicAndStaticPlatforms` (`commands/refresh_test.go:1477-1499`) only asserts `.github/hooks/*.json`, `.github/copilot-instructions.md`, `.claude/`, and `.mcp.json`.

So reverting/omitting Copilot’s `.agents/skills/` ignore surface still leaves the new tests green. That is a production escape, not just a theoretical coverage nit.

## BLOCKER 2 — the claimed `.agentsrc.lock` neverIgnored contract is still not genuinely proven

The review ask specifically called out whether the `.agentsrc.lock` contract "genuinely holds." It does not, at least not with the strength claimed by the code/comments/tests:

- The implementation filters only exact literal spellings after slash-normalization: `if neverIgnored[strings.TrimSuffix(norm, "/")] { continue }` (`internal/links/gitignore.go:149-166`).
- The existing/new test coverage only exercises literal entries (`.agentsrc.json`, `.agentsrc.lock`, `.agentsrc.lock/`) (`internal/links/gitignore_test.go:81-103`) and the new refresh-level test only checks those same literals are absent from the rendered block (`commands/refresh_test.go:1449-1454`).

That means covering patterns such as `*.lock`, `/.agentsrc.lock`, or `.agentsrc.*` still defeat the contract while every new test stays green. I cannot sign off on the stronger review target (“the neverIgnored .agentsrc.lock contract genuinely holds”) when the implementation and tests only prove a narrow exact-literal subset.

## What *is* solid

To avoid false negatives: the top-level refresh wiring itself is real, not hand-waved. If `refreshOneProject` stopped calling `ensureManagedGitignoreForRefresh`, `TestRunRefresh_WritesManagedGitignoreBlock` would fail immediately on missing markers (`commands/refresh_test.go:1429-1435`). So the production path is exercised; the blockers are the incomplete surface and overstated contract coverage above.
