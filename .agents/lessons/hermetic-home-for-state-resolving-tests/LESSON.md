# Lesson: Hermetic HOME for state-resolving tests

## Pattern

A test that resolves a user-scoped path — `$HOME`, the graph-home / state dir,
platform config/data/cache dirs, or the OS keychain — passes or fails depending
on the **machine it runs on**, not on the code under test. It reads (or writes)
the real developer/CI home, so its result is contaminated by whatever happens to
be on disk there.

This was the root of the flaky `TestRunWorkflowPlanDeriveScopeDegradesGracefully`:
it resolved a real home/state location, so its outcome varied with the ambient
environment — flaky enough that a push went out under `--no-verify` to dodge it.
That is a double failure: a non-hermetic test, *and* a bypassed gate to work
around it.

## Root cause

Path-resolution helpers (`os.UserHomeDir`, platform-dir lookups, graph-home,
keychain handles) read process environment and the real filesystem. A test that
exercises them without isolating that environment inherits the runner's actual
HOME — so two machines (or the same machine on two days) resolve different paths,
find different files, and disagree. The test asserts behavior that depends on
state it never controlled.

## Rule

Any test that resolves a user-scoped path or state location MUST first pin an
**isolated, temporary** environment so the resolution lands inside the test's own
sandbox:

```go
home := t.TempDir()
t.Setenv("HOME", home)                 // POSIX home
t.Setenv("USERPROFILE", home)          // Windows home
t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
// + any project-specific override the resolver honors (graph-home, state dir).
```

`t.Setenv` auto-restores after the test and forces serial execution, so the
isolation is clean and concurrency-safe. The test now resolves only paths it
created, on every machine.

Never reach for `--no-verify` to ship past a flaky home-resolving test — that
buries the bug AND defeats the gate. Make the test hermetic instead.

## How to apply

- Before any code path that can touch `$HOME`, platform dirs, graph-home, the
  state dir, or the keychain: `t.Setenv` them all to subdirs of `t.TempDir()`.
- If the resolver honors a project-specific override env var (state dir, credentials
  file), set that too — don't assume only `HOME` matters.
- For the keychain specifically, do not resolve through the real OS keyring at
  all (it blocks headless CI) — see [[credstore-loader-test-hangs-on-macos-keychain]].
- Treat "this test only fails on one machine / one shard" as a hermeticity smell
  first, not an infra flake.

## Related

- [[credstore-loader-test-hangs-on-macos-keychain]] — the keychain-specific
  instance: never resolve credentials through the real macOS Keychain in a unit
  test; this lesson generalizes it to all HOME/state-resolving tests.
- [[hermetic-env-for-cli-probe-tests]] — sibling for the PATH dimension: pin a
  hermetic base PATH so CLI-detection tests don't leak real subprocesses.
- [[match-ci-test-flags-locally]] — local-pass / CI-fail divergence often traces
  to non-hermetic environment, not the code.
