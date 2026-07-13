# Live smoke must run on every target OS — and against real first-run preconditions

## Pattern

A core command (`da config explain`, `da install`, `config sync`) faulted at
runtime for **every Windows user** in the `agentslock` write-lock acquire
(`mkdir ...\.agentsrc.lock.lock: The system cannot find the file specified`) —
the lock's parent directory did not exist at acquire time. The bug shipped past:

- `go test` on **windows-latest** (green), with **~98.5% line coverage** on
  `internal/agentslock/lockfile.go`;
- a live-binary CI smoke (`scripts/verify.sh`) that runs on windows-latest and
  invokes the *exact* failing commands with content assertions (`explain --all`,
  `install --yes`, `config sync` all reported `✓` on the Windows runner);
- a documented cross-platform fs lesson and a freshly-activated cli-runner
  verifier.

Two distinct "we ran it on Windows" claims were both true and both worthless
here:

1. **Unit tests ran on Windows** — but every test seeded its path with
   `t.TempDir()`, which *pre-creates the parent dir*, so the lock dir was always
   `mkdir`'d into an existing directory. The missing-parent precondition the real
   command hits was never reproduced.
2. **The live smoke ran the real binary on Windows** — but the harness did
   `mkdir -p "${PROJ}"` and `cd`'d in before every lock-acquiring command, so the
   lock's parent always existed in the scenario. The first-run "parent not yet
   materialized" state was never exercised.

## Root cause

We conflated three different assurances and treated them as one:

- *the code compiles and unit-passes on OS X* (proven),
- *the assembled binary runs on OS X* (proven, but only on a pre-created happy
  path), and
- *the binary's core runtime paths run on OS X under the preconditions a real
  first-run user hits* (**never proven**).

OS error semantics diverge precisely at the preconditions test harnesses paper
over. `os.Mkdir` of a path with a missing parent returns an `IsNotExist`-class
error on POSIX too, but it surfaces as the cryptic `ERROR_PATH_NOT_FOUND`
("The system cannot find the file specified") on Windows — and a harness that
always pre-creates the parent (via `t.TempDir()` or `mkdir -p`) makes the whole
class invisible on every OS at once. Line coverage compounded the illusion: every
line of the acquire ran, but only under the one precondition that hides the bug.
The fix-path also bypassed `internal/fsops` (raw `os.Mkdir`), the exact thing
[[leverage-cross-platform-fs-helpers]] tells us not to do.

## Rule

- For core-command runtime paths that touch **locks, filesystem, or path
  resolution**, a passing unit suite on an OS is NOT sufficient. There must be a
  **real-invocation smoke of the assembled binary on every target OS**
  (windows-latest included, unguarded) that drives the command end-to-end.
- That smoke must exercise the **real first-run / "not yet materialized"
  preconditions**, not a pre-created happy path. Do not `mkdir -p` (or
  `t.TempDir()`) the directory the command under test is itself responsible for
  creating, then claim the path is covered.
- Assert on **emitted content + exit code**, so "ran but returned the wrong
  thing" and "crashed" both fail. A swallowed stderr + exit-0-only check is not a
  gate.
- A lock/fs/path change must not be allowed to merge on a suite that is green but
  Windows-skipped, and must route fs mutations through `internal/fsops`, not raw
  `os.*` (mechanize the [[leverage-cross-platform-fs-helpers]] rule rather than
  leaving it advisory).

## How to apply

- When adding or changing a lock/fs/path code path, add a CI smoke scenario that
  invokes the **real binary** from a project state where the target directory
  does **not** pre-exist, and assert it succeeds and produces the artifact (e.g.
  the lock file appears). Run it on `windows-latest, macos-latest, ubuntu-latest`.
- Audit existing fs/lock tests for `t.TempDir()` parents and `mkdir -p` fixtures
  that mask missing-parent / first-run states; add at least one scenario that
  removes that crutch.
- Do not treat ~95%+ line coverage as cross-OS assurance — pair the line-coverage
  ratchet with the behavioral smoke and say so explicitly in the gate rationale.
- If a delegated task's write_scope touches `internal/fsops`,
  `internal/agentslock`, `internal/kg/lockfile`, `*_windows.go`, or path
  resolution, make "exercised via the real-first-run OS-matrix smoke" a done
  criterion.

## Related

- [[leverage-cross-platform-fs-helpers]] — route fs mutations through `fsops.*`,
  not raw `os.*`; the agentslock lock-dir creation violated this and is the
  direct sibling of the Windows error-class divergence here.
- [[hermetic-home-for-state-resolving-tests]] — the hermeticity fix that
  reinforced the `t.TempDir()` habit which pre-creates parents and masks this
  bug; hermetic isolation and first-run-precondition coverage are complementary,
  not the same.
- [[gates-must-be-locally-reproducible]] — a gate is only as good as the
  precondition it actually exercises.
- Post-mortem: `.agents/history/rca-windows-agentslock-escape.md`.
