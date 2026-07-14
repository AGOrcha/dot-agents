# Negative-control analysis — generalization boundaries

What the negative controls tell us about **which synthesis themes generalize** beyond the
analytically-chosen, workflow-heavy sample — and which are scoped to the workflow loop. Convergence
and scope are computed here per rubric E2; all paths `~`-normalized (R1–R5).

## Control roster (15 seeded + 2 structural)

**15 rule-4 seeded negative controls** (`sampling-manifest.json` rule4 = "15 seeded negative controls
from unselected pool" — i.e. sessions the stratified rule-3 sampler did *not* pick, drawn to test
selection bias):

- **codex ×13** — 12 token-bearing (`cx-cost-negctrl-01..12`) + 1 non-token-bearing. Slugs span
  `~/Documents/dot-agents`, `~/Documents/payout`, `~/proj-docs/dot-agents`, and
  `scratchpad/depth-exp*`, `scratchpad/*gate*` experiment paths. Models: gpt-5.3-codex, gpt-5.4,
  gpt-5.4-mini, gpt-5.5, codex-auto-review.
- **cursor ×2** — `830f6693` (`cursor-negctrl-conversational-diagnosis`, pure SSH/git-auth chat) and
  `6ec974a3` (`cursor-negctrl-refactor-for-sonar` + `cursor-applypatch-context-failure`).

**2 structural within-set controls** (`items/omp-cc-copilot/notes.md` — deliberately *unlike* the
workflow-loop mega-sessions):

- **copilot 2+2 smoke** (`copilot-tooldef-fixed-overhead`, `copilot-trivial-smoke-outcome`).
- **D12 hop-chain batch** (20 sessions; `cc-d12-hopchain-experiment-batch`) — fresh-context
  telephone-chain trials, not loops.

## What generalizes (holds in the controls)

1. **Cost structure (T-c1) generalizes fully.** The 12 token-bearing codex controls show cache/input
   63–98% (mean ~85%), indistinguishable from the workflow-heavy corpus median 89%. Cache-read
   dominance is therefore **structural to long agent context, not an artifact of the `da workflow`
   loop or of sample selection**. This is the load-bearing generalization result: Outcomes O2/O3
   (productive-token normalization) apply corpus-wide.
   *Basis:* `cx-cost-negctrl-01..12` vs `cx-cost-permodel-01..04`, `omp-cost-totals-*`,
   `cost-cacheread-dominates-context`.

2. **Fixed context overhead (T-c4) generalizes and is task-complexity-independent.** The copilot 2+2
   smoke spent 67% of a 24,342-token context on tool definitions for a one-word answer — the overhead
   floor exists regardless of task size. Argues directly for *not* routing trivial tasks through heavy
   tool-def contexts (feeds O13).
   *Basis:* `copilot-tooldef-fixed-overhead`, `copilot-trivial-smoke-outcome`.

3. **Failure modes (T-b1, T-b3) are not workflow-complexity artifacts.** Rate-limit pressure appears
   in the non-primary-workflow controls too — scratchpad `gate`/`regate`/`bg-gate` and
   codex-auto-review control sessions carry high primary/secondary `used_percent`
   (`cx-cost-negctrl-09` codex-auto-review gate-227). Cutoffs recur even in the trivial D12 trials
   (8 cutoff / 12 complete across 20). So rate-limit and cutoff resilience (O7) is needed everywhere,
   not just in mega-sessions.
   *Basis:* `cx-cost-negctrl-09`, `cc-d12-hopchain-experiment-batch`, `cx-fail-ratelimit-01..14`.

4. **Review-craft (T-d2) generalizes beyond the harness.** Even the non-workflow cursor control
   refactors code specifically to satisfy a SonarQube rule (python:S3358) — Sonar-driven quality craft
   is present in advisory/one-off use, not only in loop-workers. Supports O5/O6 as broadly applicable.
   *Basis:* `cursor-negctrl-refactor-for-sonar`.

## What does NOT generalize (scoped to workflow use)

5. **Loop mechanism + orchestration craft (T-a1, T-a2, T-d3, T-d4) are workflow-scoped.** cursor
   `830f6693` is a pure conversational SSH/git diagnosis: **zero tools, no harness, no delegation
   bundle, no workflow readback**. It confirms non-workflow cursor sessions are advisory chat — the
   loop-worker/orchestrator, canonical-state-readback, and delegation findings **do not** carry over
   to them. The mechanism themes describe workflow sessions specifically, not "how the model behaves"
   in general.
   *Basis:* `cursor-negctrl-conversational-diagnosis` vs `cursor-loop-worker-harness`,
   `cursor-orchestrator-role-boundary`, `cursor-orchestrator-readback`.

6. **Cursor's verification/outcome *fidelity* gap (T-b5) persists in the controls.** The one control
   that hit a real tool failure (`6ec974a3`, ApplyPatch rejecting hyphen-marked context) exposes it
   **only in narration** — still no `tool_result`. So the fidelity gap is a property of the Cursor
   transcript format, invariant to workflow-vs-advisory use: cursor cannot verify tool outcomes in
   either mode (reinforces O2's hard-exclusion of cursor from outcome-verifiable axes).
   *Basis:* `cursor-applypatch-context-failure`, `cursor-gap-no-tool-results`.

## Selection-bias check

Because rule-4 controls are drawn from the *unselected* pool, their agreement with the sampled corpus
on cost/failure patterns (points 1–3) means those findings are **not an artifact of the stratified
sampler** — the sampler did not manufacture the cache-dominance or rate-limit signals. The mechanism
findings' failure to generalize (points 5–6) is the *expected* boundary, not selection bias: those
themes are defined only for workflow sessions, and the controls that break them are, by construction,
non-workflow.

## Bottom line

| theme | generalizes past workflow sample? | control evidence |
|---|---|---|
| T-c1 cache-read dominance | **yes (corpus-wide, structural)** | `cx-cost-negctrl-01..12` |
| T-c4 fixed context overhead | **yes (task-size-independent)** | `copilot-tooldef-fixed-overhead` |
| T-b1 rate-limit walls | **yes (incl. scratchpad/review sessions)** | `cx-cost-negctrl-09`, `cx-fail-ratelimit-*` |
| T-b3 cutoffs | **yes (even trivial D12 trials)** | `cc-d12-hopchain-experiment-batch` |
| T-d2 review-craft (Sonar) | **yes (advisory use too)** | `cursor-negctrl-refactor-for-sonar` |
| T-a1/T-a2/T-d3/T-d4 loop mechanism + orchestration | **no — workflow-scoped** | `cursor-negctrl-conversational-diagnosis` |
| T-b5 cursor fidelity gap | **invariant (format property, both modes)** | `cursor-applypatch-context-failure` |

Implication for downstream tasks: cost-axis and resilience outcomes (O2, O3, O4, O7, O13) are safe to
generalize corpus-wide; mechanism/orchestration projection (O1, O9) must be gated on
"is this a workflow session?" and never applied to advisory chat; and any Pareto accuracy proxy that
leans on Cursor self-reports is unsound because the fidelity gap is invariant (O11).
