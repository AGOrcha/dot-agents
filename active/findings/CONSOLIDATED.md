# Consolidated omp-platform findings — swarm run `da-inner-loop-d14`

Collated from `active/findings/omp-platform-*.md` (all 8 stages) + `git-ref-reconciliation-model.md`.
Slice: `managed-gitignore-autofill / wire-managed-gitignore-autofill` (app_type **go-cli**), driven
through da's execution profile under the **omp** harness. This run is spec evidence for treating omp
as a first-class da platform. Terse; each item cites the stage(s) that hit it.

---

## 1. omp-platform-handling (da ↔ omp friction)

### 1.1 `da workflow resolve-prompt` — overlay-only resolution, no degraded-state signal
Hit by EVERY verifier + reviewer stage (verify-unit, verify-cli-runner, review-{architecture-standards,
acceptance-invariants,adversarial,cross-harness-adversarial}).
- `da --json workflow resolve-prompt --kind <verifier|reviewer> --slug <slug>` returns
  `matched:true` but the shared base layer (`verifiers/verifier.base.md` /
  `reviewers/reviewer.base.md`) AND the kind/lens layer (`<slug>.md`) come back
  `scope:"unresolved", exists:false`. Only the repo-local `<slug>.project.md` overlay resolves.
- Consequence: a cold verifier/reviewer runs on the project overlay ALONE — it gets the command
  matrix / hotspot addendum but NOT the base result-schema, evidence taxonomy, or the lens contract
  that DEFINES severities (BLOCKER/HIGH), active-probing rules, `sandbox_mutations`, etc. The CLI
  reports overall success anyway, so a consumer cannot distinguish "lens intentionally overlay-only"
  from "lens files not synced for this platform."
- Ask: either ship the base+lens defaults, or have `resolve-prompt` emit a first-class warning /
  `--require-complete` flag (nonzero exit when a declared composes-after layer is missing) so gating
  is scriptable.
- Related: resolve-prompt path fields are `~`-prefixed (`~/proj-docs/...`); a consumer feeding the
  path straight to a reader must tilde-expand first (embedded shell/read layer does not always
  auto-expand `~`). Prefer already-absolute paths. (review-architecture-standards.)

### 1.2 No da-native coordination/signal primitive — the swarm is hand-rolled file passing
Hit by readiness, review-architecture-standards, review-acceptance-invariants (and structurally by all).
- The whole swarm rides a parallel `refs/agents/state` lineage + flat `COORD/*.md` signal files whose
  **line 1 is a verdict token** the next stage greps. There is no `da workflow signal write/read`,
  no `da workflow gate --require <upstream>=PASS`, no `da workflow coord path <stage>`.
- Each stage re-implements grep-line-1 + the defensive-SKIP boilerplate by hand; a malformed line 1
  would silently mis-gate a downstream stage. Paths are not discoverable from cwd — stages had to
  `glob` for `CONVENTIONS.md` / `COORD/*.md` under `.agents/worktrees/_state/...`.
- Ask: first-class signal/gate/coord-path CLI + a structured reviewer verdict schema
  (`{verdict, findings:[{severity}]}`) so the gate greps structure, not natural language.

### 1.3 `da kg` — impact/code-status work, build/update/postprocess broken under omp
Hit by readiness (used impact), review-{acceptance-invariants,adversarial} (fell back to grep).
- `da kg impact <file> --json` + `da kg code-status` resolve cleanly (`graph_state:ready`); good.
- `da kg build/update/postprocess` are BROKEN here (stale CRG venv shebang, per CONVENTIONS) → the
  graph may be stale, so reviewers can't fully trust impact queries and lose the token-efficient KG
  context advantage. `impact` output also truncates to 50 nodes and has no `--scope`/
  `--exclude-nonsource` to drop the generic package closure (test scratch under `.agents/history/*`,
  `.agents/sandbox/*`), so a human must eyeball relevance.
- Ask: fix the CRG venv bootstrap for omp worktrees; add scope filtering to `kg impact`.

### 1.4 Per-worktree bootstrap is manual (venv symlink, build-from-source ordering)
Hit by readiness.
- Installed `da` 0.4.2 is stale (lacks `da run`, `kg link import`, etc.), so each slice must
  `go build -o bin/da ./cmd/da` and use that binary — but the readiness gate runs BEFORE the slice
  binary exists, so KG readback happens on the stale binary while impl/verify use the fresh one
  (ordering wart).
- `.venv` must be hand-symlinked into each new worktree (`git worktree add` doesn't carry
  gitignored/untracked roots), or CRG-backed `kg` silently breaks. No `da worktree add` wrapper
  provisions it.

### 1.5 Non-interactive / hermetic-platform ergonomics
Hit by impl, verify-cli-runner.
- `da add`/`da init` are interactive by default; without `-y` in the embedded (no-TTY) shell,
  `da add <path>` prints `Proceed? [y/N]` → `Add cancelled.` and writes NO config — easy to mistake
  for success. Want a da-native `DA_YES=1` non-interactive env.
- Platform enablement is driven by PATH probing (`IsInstalled()` → `exec.LookPath`), not the project
  manifest: even with isolated `HOME`/`AGENTS_HOME`, `DetectAndEnableNewPlatforms` force-enabled
  cursor/claude/codex/copilot, overriding `enabled:false`. No `da platform enable/disable` /
  `da refresh --platforms copilot` / `AGENTS_PLATFORMS` override → a hermetic per-platform dogfood
  can't pin the enabled set without removing CLIs from PATH.
- No clean lock-materialize verb: `.agentsrc.lock` is only produced as a side effect of
  `da config explain` (drives `EnsureResolved`). Want a dedicated `da lock`/`da config resolve <dir>`.
- `--dry-run` is NOT side-effect-free in 0.4.2 (CONVENTIONS RULE); can't be relied on.

### 1.6 Edit-tool file-identity hazard across nested worktrees (harness bug, impl)
- The `d14` code worktree is nested INSIDE the `_state` coordination repo; both are real git trees
  with a byte-identical `.gitignore`. The edit tool's `[path#TAG]` resolved the ambiguous
  filename+tag to the cwd-root (MAIN) file, so a `.gitignore` edit silently landed on the wrong tree.
  Only `git check-ignore -v` (pre-edit line numbers) surfaced it. Reverted main via
  `git checkout --`, rewrote the worktree file with a full-content write (explicit path, no tag).
- Ask: when the same relative name exists in multiple worktrees, key file identity on the ABSOLUTE
  path, not a content-derived tag.

### 1.7 `.agentsrc.lock` timestamp churn (committed contract not byte-stable)
Hit by verify-cli-runner, review-acceptance-invariants (non-blocking to D14, but real).
- Two identical no-op `da refresh` runs rewrite the committed lock's `refreshedAt` +
  per-unit `fetched_at`/`last_checked_at` (structural `inputs_digest`/`units` unchanged). The D14
  managed `.gitignore` IS byte-stable, but the committed lock is not → any consuming repo that
  commits the lock and runs refresh routinely gets spurious "modified `.agentsrc.lock`" noise.
- Ask: freeze volatile timestamps on a no-op re-resolve, or split volatile metadata from the
  deterministic digest record so the committed contract stays clean.

### 1.8 No sanctioned mutation/sandbox seam for verify + adversarial review
Hit by verify-unit, review-adversarial.
- Mutation-verify requires the verifier to temporarily edit SLICE code (revert a hunk, run, restore),
  but CONVENTIONS RULE 3 grants write ownership only to impl — the strongest verification technique
  technically violates single-writer. No `da verify mutate` / ephemeral-overlay seam.
- The adversarial lens gates active probing on `sandbox_mutations`, but omp exposes no sandbox and
  Go `internal/` can't be imported from an ad-hoc external module, so the CRLF-idempotency evidence
  is a faithful REPLICA of the strip logic, not the real function. Want a read-only copy-on-write
  scratch `_test.go` overlay of SLICE for reviewers.

---

## 2. git-ref-work-backend — D9/D10 manual operation

Coordination in this run is 100% manual git-ref plumbing (feeds git-ref-work-backend spec §9,
workflow-store-concurrency-safe-writes proposal, Phase-2 scheduler). See
`git-ref-reconciliation-model.md` for the full model. Key points:

- **Layered optimistic concurrency (CAS), two layers:**
  1. MECHANICAL (automatic): ref-level CAS `git update-ref refs/agents/state <new> <old>` (`<old>` =
     expected version; stale base ⇒ fails ⇒ re-read + retry). NOT bare `update-ref <new>` =
     last-writer-wins. PER-RECORD state files (D9 per-task files): unrelated concurrent updates land
     in DIFFERENT files ⇒ clean union merge, no reconcile (the common case; must be automatic). A
     per-record version/generation counter (or base content-hash) catches a genuinely stale
     same-record base.
  2. SEMANTIC (judgment, only when related): a clean textual merge can still be semantically wrong
     (two writers advanced a state only one should own). CAS says "base moved"; the semantic
     reconcile decides the merged MEANING.
- **Mechanical conflicts = a rare-by-design HEALTH SIGNAL.** When one occurs, root-cause it (don't
  just resolve the merge): coarse state layout → split into per-record files; tool/primitive gap →
  the `update-ref` CAS + WorkStore record API; operating-instruction gap (under-declared write_scope,
  agents writing a shared file) → fix the declaration.
- **Pre-detected risk:** PlansBreakdownDoc found 9 write_scope-OVERLAP pairs = the mechanical-conflict
  hotspots ⇒ Phase-2 scheduler rule: NEVER run write_scope-overlapping tasks in the same parallel
  wave; a conflict that slips through ⇒ an under-declared write_scope to fix.
- **Manual-operation pain in THIS run (D9/D10 not yet automated):** every stage hand-writes its
  signal file and the human/gate hand-runs `git add -A && git commit && git update-ref
  refs/agents/state <HEAD>`. There is no `da` WorkStore record API doing granular atomic writes; the
  ready_gate stage is the sole committer of the coordination ref. **This run was sequential
  (one writer) ⇒ no stale base**, so last-writer-wins did not bite — but the ref commit SHOULD move
  to CAS form (`update-ref <new> <old>`) so a stray concurrent writer is caught. That is the concrete
  D9/D10 gap this run exercised.

---

## 3. Execution-profile ↔ swarm mapping (+ recorded divergence)

The `go-cli` execution profile's stage DAG was realized as this swarm's stage chain:

```
readiness ─▶ impl ─▶ verify(unit) ─▶ verify(cli-runner) ─▶
  review(architecture-standards) ─▶ review(acceptance-invariants) ─▶
  review(adversarial) ─▶ review(cross-harness-adversarial) ─▶ ready_gate
```

- Each profile stage = one swarm node writing one `COORD/<stage>.md` signal (line-1 verdict token),
  reading its upstream signal(s). The profile's per-stage prompt was fetched via
  `da workflow resolve-prompt --kind <verifier|reviewer> --slug <slug>` (see 1.1 for the overlay-only
  caveat that partially undercuts profile fidelity — the base contract half of each stage's prompt
  did not resolve under omp).
- Gate policy (`impl==DONE ∧ both verify==PASS ∧ ALL reviews==APPROVE → READY else FOLD-BACK`) is the
  profile's terminal-gate stage, owner-held (no merge, no board mutation).

### 3.1 RECORDED DIVERGENCE — `lens_concurrency: gated` realized as a sequential lens chain
The execution profile declares the review lenses with `lens_concurrency: gated` (lenses conceptually
gated on the prior verdict, potentially short-circuiting). **The swarm has NO native failure
short-circuit** (CONVENTIONS RULE 0). So `gated` was realized as:

- a **SEQUENTIAL lens chain** (architecture-standards → acceptance-invariants → adversarial →
  cross-harness-adversarial), each lens reading the immediately-upstream lens's line-1 token, PLUS
- a **defensive upstream-verdict SKIP**: if a required upstream signal is missing or carries a
  blocking verdict (BLOCKED/FAIL/REJECT), the stage writes its own `SKIP` (reason
  `gated: upstream <name>=<verdict>`) and exits 0 WITHOUT doing its normal work — the hand-rolled
  stand-in for the profile's intended short-circuit.

Divergence consequence in THIS run: cross-harness-adversarial REJECTed, but because the chain is
sequential and the earlier lenses had already APPROVEd upstream of it, all four lenses actually RAN
(none was SKIP-gated) — the REJECT surfaced only at the ready_gate rollup, not as an early
short-circuit. A native `gated` short-circuit would have stopped the chain at the first blocking
verdict; the swarm instead runs every lens and lets the gate aggregate. This is the exact place omp
would benefit from a first-class `workflow gate`/dependency-verdict edge (see 1.2) so `lens_concurrency:
gated` is honored by the runtime instead of emulated by convention + defensive SKIP boilerplate.
