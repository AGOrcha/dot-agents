# RCA: Windows `agentslock` acquire failure escaped every quality gate

**Date:** 2026-06-24
**Author:** Nikash Prakash
**Severity:** High — two constantly-used core commands (`da config explain`, `da install`) fail at runtime for **every Windows user**.
**Status:** Code fix tracked separately; this document is the process / machinery post-mortem.

## Symptom

On Windows, `da config explain --all` and `da install` fail in the `agentslock`
write-lock acquire:

```
mkdir ...\.agentsrc.lock.lock: The system cannot find the file specified
```

The lock dir is `path + ".lock"` where `path` is `<projectPath>/.agentsrc.lock`
(`internal/config/resolver.go:AgentsLockPath`), so the `os.Mkdir` target is
`<projectPath>/.agentsrc.lock.lock`. "The system cannot find the file specified"
is the Windows `ERROR_PATH_NOT_FOUND` surfaced when the **parent directory does
not exist** at acquire time — i.e. the lock is taken before the directory that
will hold `.agentsrc.lock` has been materialized.

## The acquire path that breaks

`internal/agentslock/lockfile.go:acquireFileLock` (called from `Flush`):

```go
func acquireFileLock(path string) (func(), error) {
	lockDir := path + ".lock"
	...
	err := os.Mkdir(lockDir, 0o700)   // line 237 — raw os.Mkdir, no parent creation
	if err == nil { ... }
	if !os.IsExist(err) {
		return nil, fmt.Errorf("agentslock: acquire lock %s: %w", lockDir, err)
	}
	...
}
```

Two design facts combine into the escape:

1. **No parent-dir guarantee.** `acquireFileLock` uses a single-level `os.Mkdir`
   and assumes the parent exists. The `Flush` doc comment even states "The
   parent directory must exist" (line 171) — a precondition that is never
   enforced and never tested against the live command.
2. **Raw `os.Mkdir`, not the project's `fsops` wrapper.** The atomic *write*
   goes through `fsops.WriteFileAtomic` (line 188), which has Windows-aware
   fallbacks, but the lock-dir *creation* bypasses `internal/fsops` entirely.
   This is the exact anti-pattern the existing lesson
   [`leverage-cross-platform-fs-helpers`](../lessons/leverage-cross-platform-fs-helpers/LESSON.md)
   warns about: route OS-divergent fs mutations through `fsops.*`, not raw
   `os.*`, so the Windows behavior is uniform.

## Why every gate was blind — layer by layer

### Layer 1 — Multi-OS CI (`Test` on windows-latest, `go test`)

**Verdict: green, and structurally incapable of catching it.**

The agentslock tests (`internal/agentslock/lockfile_test.go`, PR #91) are
exhaustive *unit* tests — round-trip, sibling-section preservation, concurrent
flush, stale-lock reclaim, timeout — and the file was ~98.5% covered. But
**every single test seeds its path with `t.TempDir()`** (e.g. lines 44, 60, 94,
125, 301, 446), and `t.TempDir()` *always pre-creates the parent directory*. The
lock dir `path + ".lock"` is therefore always created inside an existing
directory. The exact production precondition — parent absent at acquire — is
never reproduced.

`TestFlushWriteError` (line 282) does use a missing parent (`no-such-dir`), but
it asserts the *write* fails as a negative test of `WriteFileAtomic`; it is not
a positive assertion about `acquireFileLock` behaving correctly when it must
create the directory. So the missing-parent path is touched as a "must error"
case, never as a "must succeed by creating the parent" case.

Net: `go test` on windows-latest exercised the happy-path mkdir-into-existing-dir
on Windows and stayed green. **Unit coverage of the lock function ≠ the real
acquire precondition ever occurring on Windows.**

### Layer 2 — `cli-runner` verifier (#146) + the live-binary CI smoke (`verify.sh`)

**Verdict: this is the primary gap — but with a twist worth recording precisely.**

There are *two* things named "cli-runner":

- **PR #146 ("verifier: activate cli-runner built-binary smoke")** is **prompt
  files only** (`.agents/prompts/verifiers/cli-runner.project.md`,
  `internal/scaffold/.../verifiers/cli-runner.md`, a spec refresh). It instructs
  a **verifier *agent*** to `go build` and `bash scripts/verify.sh` — and that
  agent runs on the **developer's mac/linux machine**, never on Windows. So the
  reviewer/verifier-chain assertion "the assembled binary runs and returns the
  right content" was, in practice, only ever proven on the dev's host OS.

- **The live-binary CI smoke** (`scripts/verify.sh`, wired into `test.yml` as
  "Test config-v2 + kg surface (verify.sh, content assertions)", PR #101) *does*
  run on the full matrix **including windows-latest** (the step has **no
  `if: runner.os` guard**). And it *does* invoke the exact failing commands with
  content assertions: `config explain --all --json` (verify.sh:227), `install
  --yes` (272), `install wrote lock` (273), `config sync` (288).

So why did the Windows `verify.sh` run pass? **Confirmed from the live CI log**
(run 28116521242, windows-latest):

```
Asserting explain --all --json merges layers... ✓
Testing install --yes (isolated)... ✓
Testing install wrote lock... ✓
Testing config sync (isolated)... ✓
All tests passed!
```

The reason it passes is the **same hole as Layer 1, lifted up one level**:
`verify.sh` builds its isolated fixture with `mkdir -p "${PROJ}"` (verify.sh:172)
and then `cd`s into it before every `install`/`explain`/`sync` call. The project
directory — the lock's parent — **always exists** in the harness scenario. The
smoke never exercises "run a lock-acquiring command where the resolved lockfile
parent does not yet exist."

**Conclusion:** real-command integration on Windows *does* run, but it only ever
runs against a pre-materialized directory. The agent-side cli-runner verifier
(#146) that would assert content never runs on Windows at all. Either way, the
missing-parent acquire was invisible.

### Layer 3 — verifier/reviewer chain + worker self-gate (#144) + cross-platform lessons

**Verdict: pattern-recognition existed on paper but had no mechanical Windows gate.**

- We already ship [`leverage-cross-platform-fs-helpers`](../lessons/leverage-cross-platform-fs-helpers/LESSON.md),
  whose root cause is *literally* a Windows fs error-class divergence and whose
  rule is "route fs mutations through `fsops.*`, not raw `os.*`." `agentslock`
  uses raw `os.Mkdir` for the lock dir. The lesson was not applied to the new
  code, and **nothing mechanically enforces it** — it is advisory prose.
- [`hermetic-home-for-state-resolving-tests`](../lessons/hermetic-home-for-state-resolving-tests/LESSON.md)
  fixed the *test-hermeticity* sibling but reinforced the very `t.TempDir()`
  habit that pre-creates parents and masks this class of bug.
- The executor self-gate (#144, "harden executor app-type prompts") tightens
  *prompt* guidance for the loop-worker; it does **not** require an OS-matrix
  live-binary smoke for changes that touch path/lock/fs code.

So the chain "we have a Windows fs lesson + we recently reasoned about Windows
paths" never converted into a gate that would have *executed* the new lock path
on Windows. **Cross-platform correctness was reasoned about, not run.**

### Layer 4 — `config explain` auto-locks (design / blast radius)

**Verdict: the auto-lock-on-explain design is defensible; the blast radius it
creates is what made a lock bug catastrophic.**

`da config explain` consumes an **auto-lock seam** (`commands/config/explain.go`,
the "uv tree" auto-lock behavior): even a read-ish command re-resolves and
*rewrites* the units lock when the lock is stale or **absent**. Combined with the
missing-parent acquire bug, "absent lock" is exactly the first-run state — so the
single most common diagnostic command (`explain`) faults on first use for every
Windows user, the same fault as `install`.

Recommendation: keep auto-lock (it is the intended uv-style freshness behavior),
but (a) ensure the lock's parent is guaranteed before any acquire, and
(b) consider a **read-only fast path** for `explain` when the lock is fresh so a
purely informational command does not require write-lock semantics at all. The
correctness fix is the lock's Windows-correctness and parent-guarantee; the
design review item is narrowing when a read command must take a write lock.

### Layer 5 — Coverage gate (95% per-file enforce)

**Verdict: ~98.5% line coverage on `lockfile.go`, bug still shipped — coverage
measured the wrong thing.**

The per-file coverage ratchet (`scripts/coverage-gate.sh`, threshold 95, enforce)
counted *which lines executed under test*. Every line of `acquireFileLock` did
execute — but only ever with an existing parent dir. Coverage is a proxy for
"lines run," not "behaviors exercised under the OS-specific preconditions that
matter." A line-coverage gate cannot distinguish "mkdir succeeded into an
existing dir" from "mkdir into a missing parent," and the merged multi-OS profile
credits Windows-tagged lines from *any* OS run, further diluting the per-OS
behavioral signal. **High coverage gave false confidence; behavioral/integration
coverage on the target OS is the missing axis.**

## 5 Whys

1. **Why did `config explain`/`install` fail on Windows?**
   `agentslock.acquireFileLock` does `os.Mkdir(path+".lock")` assuming the parent
   exists; on the first-run / no-lock-yet path the parent dir is absent, and
   Windows returns `ERROR_PATH_NOT_FOUND`.
2. **Why did unit tests not catch it?**
   Every agentslock test seeds with `t.TempDir()`, which pre-creates the parent —
   so the missing-parent acquire precondition is never reproduced; ~98.5%
   coverage was all happy-path-into-existing-dir.
3. **Why did the Windows live-binary smoke (`verify.sh`) not catch it?**
   `verify.sh` `mkdir -p`s its fixture project dir and `cd`s in before every
   lock-acquiring command, so the lock's parent always exists in the harness —
   the same blind spot as the unit tests, one level up. (The #146 agent-side
   cli-runner verifier that asserts content never runs on Windows at all.)
4. **Why did our cross-platform lessons / pattern-recognition not prevent it?**
   The relevant lesson (`leverage-cross-platform-fs-helpers`: use `fsops.*`, not
   raw `os.*`) is advisory prose with no mechanical gate; the new lock code used
   raw `os.Mkdir`, and no check executed the new path on Windows.
5. **Why did the coverage gate give us confidence anyway?**
   Line coverage is a proxy for "lines executed," not "OS-specific behaviors
   exercised." Every line ran — under the one precondition (existing parent) that
   hides the bug.

**Root cause:** we proved the *code* compiles-and-passes-unit-tests on Windows,
but we never proved the *assembled binary's core runtime paths (lock/fs/path)
actually ran end-to-end on Windows under the preconditions a real first-run user
hits.* Unit tests on an OS, and even a live smoke whose harness pre-creates the
happy-path directory, are not the same as the binary having genuinely exercised
the path on that OS.

## Machinery fixes (prioritized)

**P0 — Run the `verify.sh` live-binary smoke as a *real-first-run* scenario on
windows-latest (and the whole matrix).**
The step already runs on Windows; the gap is the harness pre-creates the parent.
Add at least one scenario that invokes a lock-acquiring command
(`config explain --all`, `install`, `config sync`) where the resolved lockfile
parent does **not** pre-exist — e.g. run from a freshly-resolved project path the
command itself is expected to materialize, and assert success + the lock file
appears. This is the single change that would have caught this bug. Keep it
unguarded across `windows-latest, macos-latest, ubuntu-latest`.

**P1 — Add a dedicated Windows-integration smoke step for path/lock/fs core
commands.** A small matrix step that drives the real binary through the
first-run lifecycle (`init` → `config explain --all` in a not-yet-locked project
→ `install` → `config sync`) with content assertions, run on every OS. Treat a
non-zero exit from any core command as a hard failure (the current `test_command`
swallows stderr — keep the exit-code check but also assert on emitted content so
"ran but returned wrong thing" fails too).

**P2 — Promote "cross-platform validated on the target OS, not just reasoned"
from advisory prose to a gated check.** Two mechanizable pieces:
- An `importguard`-style lint (or `go vet`-adjacent rule) that flags raw
  `os.Mkdir`/`os.MkdirAll`/`os.Remove*` in production packages that should route
  through `internal/fsops` (allowlist `fsops` itself + tests). This converts the
  `leverage-cross-platform-fs-helpers` lesson into enforcement.
- A required-on-Windows status check so a path/lock/fs PR cannot merge on a
  green-but-Windows-skipped suite.

**P3 — Executor self-gate (#144): require an OS-matrix live smoke for path/lock/fs
changes.** When a delegated task's write_scope touches `internal/fsops`,
`internal/agentslock`, `internal/kg/lockfile`, or any `*_windows.go` / path-
resolution code, the worker's done-criteria must include "exercised via the
real-first-run smoke on the OS matrix," not just `go test`.

**P4 — Reframe the coverage gate's contract.** Keep the 95% line ratchet, but
document explicitly (in `coverage-gate.sh` rationale / the coverage history) that
line coverage does not certify OS-precondition behavior, and pair it with the
behavioral smoke (P0/P1) as the real cross-OS gate. Optionally add a per-OS
behavioral checklist for `*_windows.go` files that the merged profile dilutes.

**P5 — Design review (non-blocking): narrow `explain`'s write-lock.** Consider a
read-only fast path for `da config explain` when the lock is already fresh, so a
purely informational command does not invoke write-lock acquire at all — shrinking
the blast radius of any future lock-layer bug.

## Lesson captured

[`live-smoke-must-run-on-every-target-os`](../lessons/live-smoke-must-run-on-every-target-os/LESSON.md)
— unit tests passing on an OS, and even a live smoke whose harness pre-creates
the happy-path directory, are not the same as the assembled binary having
genuinely run a core runtime path (lock/fs/path) on that OS under real
first-run preconditions.
