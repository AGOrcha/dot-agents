FOLD-BACK

# Ready gate — wire-managed-gitignore-autofill (D14/R8)

Stage: ready_gate (`swarm-da-inner-loop-d14-ready_gate-0`). Owner-held merge; NO board mutation,
NO PR opened. Terminal verdict: **FOLD-BACK** — the gate's ALL-APPROVE precondition is not met.

## Gate matrix (read from COORD line-1 tokens)

| Stage                              | Verdict   | Gate effect |
|------------------------------------|-----------|-------------|
| readiness                          | READY     | ok          |
| impl                               | DONE      | ok          |
| verify (unit)                      | PASS      | ok          |
| verify (cli-runner)                | PASS      | ok          |
| review (architecture-standards)    | APPROVE   | ok          |
| review (acceptance-invariants)     | APPROVE   | ok          |
| review (adversarial)               | APPROVE   | ok (MEDIUM/LOW non-gating findings logged) |
| review (cross-harness-adversarial) | **REJECT**| **BLOCKS**  |

Gate rule: `impl==DONE ∧ both verify==PASS ∧ ALL 4 reviews==APPROVE`. Three reviews APPROVE, one
REJECTs with 2 BLOCKERs → precondition fails → **no push, no PR, no CI, no merge-back artifact**.
Folding back to impl.

## Blocking verdict + failing-stage detail

### review (cross-harness-adversarial) = REJECT — 2 BLOCKERs
Source: `COORD/review-cross-harness-adversarial.md`. Focused verification run on the slice
(`go test ./commands -run 'TestRunRefresh_WritesManagedGitignoreBlock|TestCollectManagedOutputs_CopilotDynamicAndStaticPlatforms'`
and `go test ./internal/links -run TestEnsureManagedGitignore_NeverIgnoresCommittedContract`) all
PASS on the slice — so these are true escape defects / coverage gaps, not red tests.

**BLOCKER 1 — Copilot's managed-output surface is incomplete; new tests would not catch the regression.**
- `Platform.SharedTargetIntents` is part of the repo-local output surface (`internal/platform/platform.go:66-71`).
- Copilot contributes shared skills there:
  `BuildSharedSkillMirrorIntents(project, filepath.Join(copilotAgentsDir, "skills"))`
  (`internal/platform/copilot.go:620-629`) — refresh can materialize `.agents/skills/*` for Copilot.
- `CountLinks` also treats `.agents/skills/` as Copilot-owned managed output (`copilot.go:659-671`).
- But the new `(*copilot).ManagedOutputs()` list OMITS `.agents/skills/` entirely (`copilot.go:642-649`).
- Escape: a real repo with canonical shared skills + Copilot enabled can have refresh project
  `.agents/skills/*` WITHOUT the managed `.gitignore` block ignoring them → violates the task
  contract to enumerate every enabled platform's generated/projected repo-local outputs.
- Duplication angle (`new_duplicated_lines_density`): the output inventory is now triplicated across
  `SharedTargetIntents` / `CountLinks` / `ManagedOutputs`, and the new list drifted on day one.
- Mutation-verify: neither new test kills it. `TestRunRefresh_...` (refresh_test.go:1390-1467) only
  asserts `.github/hooks/*.json` + `.agentsrc.local.json`; `TestCollectManagedOutputs_...`
  (refresh_test.go:1477-1499) only asserts `.github/hooks/*.json`, `.github/copilot-instructions.md`,
  `.claude/`, `.mcp.json`. Omitting `.agents/skills/` leaves both green → production escape.

**BLOCKER 2 — the `.agentsrc.lock` neverIgnored contract is not genuinely proven (only exact-literal).**
- Implementation filters only exact literals after slash-normalization:
  `if neverIgnored[strings.TrimSuffix(norm, "/")] { continue }` (`internal/links/gitignore.go:149-166`).
- Test coverage only exercises literal spellings (`.agentsrc.json`, `.agentsrc.lock`, `.agentsrc.lock/`)
  (`gitignore_test.go:81-103`) and the refresh-level test only checks those literals are absent from
  the rendered block (`refresh_test.go:1449-1454`).
- Escape: covering patterns `*.lock`, `/.agentsrc.lock`, `.agentsrc.*` still defeat the contract
  while every test stays green. The stronger review target ("the neverIgnored `.agentsrc.lock`
  contract genuinely holds") is not met — only a narrow exact-literal subset is proven.

## Corroboration from the adversarial (APPROVE) lens — same two holes, ranked non-gating there
`COORD/review-adversarial.md` independently found (as MEDIUM/LOW, non-gating under its own
`fail-only-on-BLOCKER/HIGH` rule) the SAME `neverIgnored` exact-literal bypass (`*.lock`,
`/.agentsrc.lock`, `.agentsrc.*` — proven with real `git check-ignore -v`), plus a CRLF/Windows
idempotency+byte-stability gap (bare-line marker equality never trims `\r`) and a zero-enabled-platforms
wiring gap (runRefresh early-returns before the overlay guarantee). Two independent red-team lenses
converging on BLOCKER 2 raises confidence it is a real contract weakness, not a lens-severity artifact.

## Required fixes before re-running the gate (fold back to impl)
1. **Complete Copilot's `ManagedOutputs()`** so it enumerates its FULL repo-local surface including
   the shared-skill mirror `.agents/skills/` (align with `SharedTargetIntents` + `CountLinks`).
   Prefer deriving the surface from the single owned inventory to kill the triplication/drift
   (`new_duplicated_lines_density`) rather than maintaining a third hand-written list.
2. **Add a regression test** asserting `.agents/skills/` lands in the managed block when Copilot is
   enabled with a shared skill present (must fail if the entry is dropped — kills BLOCKER 1).
3. **Harden the neverIgnored contract** (BLOCKER 2): either narrow the docstrings to "exact-literal
   only" AND downgrade the contract claim, or (preferred) make `normalizeIgnoreEntries` drop any
   entry whose git match would cover a `neverIgnored` file, with a regression test feeding
   `*.lock` / `.agentsrc.*` / `/.agentsrc.lock` and asserting the lock/manifest stay trackable.
4. **(Recommended, from adversarial lens, non-gating)** `strings.TrimRight(line, "\r")` before the
   marker compare + a CRLF-input idempotency test; a refresh-level zero-enabled-platform overlay test.

Impl commit under review: `2bba968` on `swarm/managed-gitignore-autofill` (`.agents/worktrees/d14`).
Branch is NOT pushed; no PR. Owner/orchestrator: route back to impl for the four fixes, then re-run
verify + all four review lenses (including cross-harness) before the gate can reach READY.
