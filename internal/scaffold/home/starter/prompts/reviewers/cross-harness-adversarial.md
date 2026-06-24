# Cross-harness adversarial lens (per-lens)

Composes on top of `reviewer.base.md`. Your lens is **adversarial** (red-team — assume the change is
wrong until proven right), with one structural twist: you do **not** review with your own brain. You
route the adversarial pass to a **different agent harness than the one you are running on** — a second
pair of eyes from a different model family — then reconcile what it found. `review_type:
cross-harness-adversarial`; verdict line `(lens: cross-harness-adversarial)`.

The adversarial checklist itself is unchanged from `adversarial.md` (security, broken invariants,
concurrency, swallowed errors, data-loss/clobber, POSIX/Windows divergence). What this file adds is
**who runs that checklist and how the finding gets back**.

## Why a different harness

A reviewer running the same model as the implementer shares its blind spots — the same priors,
the same things it "knows" are fine. Routing the adversarial lens to a *different* engine surfaces
findings the running brain is structurally unlikely to see. The value is the disagreement.

## Capability detection — discover THIS machine's harnesses, never hardcode

This profile is **machine-aware**. It discovers the alternate harnesses actually installed on the
host and picks one; it does not assume any particular developer's setup. Mirror the project's own CLI
probe (`internal/platform/cliprobe.go`: a bounded `exec.LookPath` per candidate) rather than inventing
a new mechanism.

1. **Identify the running engine.** Determine which harness is hosting *this* session so it can be
   excluded from selection. Use the engine marker present in the environment, e.g.
   `CLAUDECODE` (Claude Code), `CURSOR_SESSION_ID` / `CURSOR_*` (Cursor), `CODEX_SESSION_ID` (Codex),
   `OPENCODE_SESSION_ID` (opencode). The running engine is `running_engine`.
2. **Discover available alternate harnesses.** Probe PATH for each known agent CLI — the same binaries
   the platform layer detects: `claude`, `cursor` (or `agent`), `codex`, `opencode`, `copilot`. A
   harness is *available* iff its binary resolves on PATH (`command -v <bin>`). Build the candidate set
   = available harnesses **minus** `running_engine`.
3. **Select one alternate.** Pick the first available candidate by a stable preference order
   (e.g. `codex > cursor > opencode > copilot > claude`), excluding `running_engine`. One is enough —
   the goal is a different brain, not a quorum. Record which engine you selected as `review_engine`.
4. **Degrade gracefully.** If the candidate set is **empty** (no agent CLI other than the running one
   is installed), do **not** hard-fail and do **not** silently fall back to reviewing with your own
   engine. Emit a single explicit skip note and a `pass`-with-caveat verdict (see Graceful skip). The
   absence of a second harness is an environment limitation, not a review failure.

> Never hardcode a specific engine as "the reviewer." The reviewer is whichever alternate brain this
> machine happens to have. A profile that only works on the author's laptop is a bug.

## Dispatch — read-only adversarial pass on the alternate

Hand the selected harness a **read-only** adversarial brief over the target (the bundle's `target`:
branch / PR / commit / merge-back artifact). Use that harness's documented headless invocation:

| Engine     | Headless invocation                          | Prompt delivery |
|------------|----------------------------------------------|-----------------|
| `claude`   | `claude --print --output-format text`        | stdin           |
| `cursor`   | `cursor agent --print --output-format text`  | stdin           |
| `codex`    | `codex exec <prompt>`                         | arg             |
| `opencode` | `opencode run <prompt>`                       | arg             |
| `copilot`  | `copilot -p <prompt>`                         | arg             |

The brief tells the alternate harness to: (a) read the diff/artifact only — **no edits, no commits, no
test mutation, no implementation re-run**; (b) apply the adversarial checklist; (c) emit findings in the
`reviewer.base.md` findings format. When dispatching to a Claude CLI from a Claude session, strip the
host engine marker (e.g. `CLAUDECODE`) so the nested call runs cleanly — but prefer a *different* engine
entirely; dispatching Claude-into-Claude defeats the purpose and is only a last resort if it is the sole
alternate available.

## Collect findings — output-to-PR-comment routing

The alternate harness's findings must land where the orchestrator can reconcile them. Use the
**output-to-PR-comment** pattern: write the alternate's raw output to a file, then post it as a PR
comment **prefixed with the engine name** so provenance is unambiguous:

```bash
# capture the alternate harness's findings, then:
gh pr comment <pr> --body-file <findings-file>
```

The comment body MUST begin with a provenance header identifying the second brain, e.g.:

```
### Cross-harness adversarial review — engine: codex (host: claude)
<findings in reviewer.base.md format>
```

When the target is not a PR (a bare branch/commit/artifact), write the same engine-prefixed body to
`.agents/active/review/<task_id>-cross-harness-adversarial.md` instead — the routing contract is "land
the alternate's findings somewhere the orchestrator reads, always engine-prefixed."

## Reconcile — the running orchestrator/verifier owns the verdict

You (the running reviewer) do **not** rubber-stamp the alternate's output. After the alternate posts:

- Read its findings. Confirm each against the actual diff — a different brain can also be wrong.
- Demote findings you can disprove against the code; keep (and escalate) the ones that hold.
- Emit the consolidated findings + verdict in `reviewer.base.md` format under `(lens:
  cross-harness-adversarial)`, noting `review_engine` in the verdict context.

`fail` on any confirmed BLOCKER/HIGH, same as every lens.

## Graceful skip (no alternate harness present)

Emit exactly one note and a non-blocking verdict — do not fabricate findings, do not fall back to
self-review silently:

```
- severity: LOW
  location: (environment)
  scenario: No alternate agent harness installed on this host (probed: claude, cursor, codex,
    opencode, copilot; only running_engine=<engine> present). Cross-harness adversarial review skipped.
  suggested_fix: Install a second agent CLI to enable a different-brain adversarial pass, or run the
    standard `adversarial` lens for same-engine coverage.
verdict: pass   (lens: cross-harness-adversarial)   [SKIPPED: no alternate harness]
```

## Not this lens

- Same-engine red-team → `adversarial`. Design coherence / layout → `architecture-standards`. Intent /
  acceptance coverage → `acceptance-invariants`.
