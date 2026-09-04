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

## Guard (regression-proofing)

Remembering `t.Setenv("HOME", ...)` in every new test is a discipline that
erodes over time — it did: `commands/skills/promote_test.go`'s pre-fix
`NewTempProject`/`WritePreservationManifest` sandboxed `AGENTS_HOME` but not
`HOME`, and the skill-promote mirror step (`config.UserHomeDir()` in
`commands/skills/promote.go`) leaked real symlinks (`my-skill`, `idem-skill`,
`extra-skill`) into the developer's actual `~/.agents/skills` and
`~/.claude/skills`.

Don't rely on discipline alone. `internal/testutil/homeguard.go` provides a
package-wide hermeticity guard: `HomeGuardBefore()` snapshots the developer's
real `~/.agents/{skills,agents,hooks,plugins}` and
`~/.claude/{skills,agents,hooks,plugins}` trees before any test runs;
`CheckAndReport()` re-snapshots after and fails the run if anything new
appeared — independent of which individual test forgot to sandbox HOME. Wire
it into every package's `TestMain` that resolves `AGENTS_HOME`/`HOME` in
tests:

```go
func TestMain(m *testing.M) {
    homeGuard := testutil.HomeGuardBefore()
    code := m.Run()
    if n := homeGuard.CheckAndReport(); n > 0 && code == 0 {
        code = 1
    }
    os.Exit(code)
}
```

As of the fix/test-isolation-real-home cleanup this is wired into every
package with `t.Setenv("AGENTS_HOME", ...)` in its tests (commands,
commands/agents, commands/config, commands/hooks, commands/internal/lifecycle,
commands/internal/mcp, commands/internal/rules, commands/internal/settings,
commands/skills, commands/sync, commands/workflow, internal/config [via the
external `config_test` package — importing `testutil` from an internal test
file would create an import cycle since `testutil` imports `config`],
internal/links, internal/platform, internal/projectsync,
internal/review/auth). Add it to any new package that gains AGENTS_HOME-
sandboxing tests.

A subprocess test helper that shells the real `da` binary (e.g.
`commands/workflow/testutil_test.go::runKGSetupViaCLI`) is a second escape
class the guard does not directly instrument (the write happens in a child
process, but still lands under the same real-home directories the guard
watches — so the package-level `TestMain` guard still catches it). Explicitly
override `HOME`/`AGENTS_HOME`/`USERPROFILE` in the subprocess's `cmd.Env`
too, rather than inheriting `os.Environ()` verbatim, so the helper is safe
even before the parent test remembers to sandbox anything.

## Related

- [[credstore-loader-test-hangs-on-macos-keychain]] — the keychain-specific
  instance: never resolve credentials through the real macOS Keychain in a unit
  test; this lesson generalizes it to all HOME/state-resolving tests.
- [[hermetic-env-for-cli-probe-tests]] — sibling for the PATH dimension: pin a
  hermetic base PATH so CLI-detection tests don't leak real subprocesses.
- [[match-ci-test-flags-locally]] — local-pass / CI-fail divergence often traces
  to non-hermetic environment, not the code.
