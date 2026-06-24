# Lesson: Gates must be locally reproducible

## Pattern

A CI gate (Sonar scanner, coverage gate, lint, a pre-push hook) fails or flakes
in a way that **can't be reproduced locally**, so the agent falls into one of two
anti-loops:

- **CI round-tripping** — push a guess, wait for the remote gate, read the
  failure, guess again. Each cycle costs a full pipeline and the local tree never
  actually proves the fix.
- **Bypassing** — reach for `--no-verify` (or disable the check) to "just get
  past it," shipping the unverified change and the latent gate breakage together.

In the recent run the Sonar pre-push scanner flaked because it walked build
output (`dist/`) and its own work dir (`.scannerwork`) — a self-inflicted,
non-deterministic gate — which drove a large number of `--no-verify` pushes.

## Root cause

The gate's environment was not hermetic and not faithfully runnable on a dev box:

- The scanner had **no exclusions** for generated/transient dirs (`dist/`,
  `.scannerwork`), so it indexed bytes that change between runs and produced
  nondeterministic results.
- There was no documented, working local invocation of the gate, so the only
  feedback loop was the remote — pushing the agent toward round-tripping or
  bypassing.

When the gate itself is the broken thing, fixing the *symptom* (bypass) or
brute-forcing the *loop* (round-trip) both leave the real defect — an
unreproducible gate — in place to recur.

## Rule

If a CI gate is not locally reproducible, **fix the gate**, not the symptom:

1. **Make it hermetic.** Add exclusions for generated/transient paths
   (`dist/`, `.scannerwork`, vendored output) so the analysis indexes only
   source and is deterministic across runs.
2. **Make it locally runnable.** Ensure the exact gate (scanner config, coverage
   command, the pre-push hook) can be invoked on a dev box with the same config
   CI uses — install/enable the hook, pin the config, document the command.
3. **Never `--no-verify` past it.** Bypassing is a hard anti-pattern here: it
   ships unverified code AND leaves the broken gate for the next person. The pre-push
   hook is a project constraint, not an obstacle.

A gate you can run locally turns a multi-minute remote round-trip into a tight
local loop, and removes the only reason anyone reaches for the bypass.

## How to apply

- When a gate flakes nondeterministically, first ask "is it indexing something it
  shouldn't?" — add the exclusion before touching application code.
- Reproduce the gate locally with CI's exact config before pushing a fix; prove
  the tree is clean locally, don't discover it on the remote.
- If the gate isn't installed locally (e.g. a pre-push hook), install/enable it
  rather than working around its absence.
- A genuine transient infra flake (e.g. the analysis CE returning "Task finished
  abnormally") is the exception — re-drive it; that's not the same as an
  unreproducible *config*. See [[sonarcloud-gate-mechanics]].

## Related

- [[sonar-container-fsmonitor-socket]] — a concrete hermeticity failure: the
  containerized scanner crashed walking the git fsmonitor socket; fix the
  scanner's view of the tree, don't bypass.
- [[ssh-keepalive-for-slow-pre-push-hook]] — push fails after green hooks; the
  fix is the keepalive, never `--no-verify`.
- [[sonarcloud-gate-mechanics]] — gate mechanics, including which failures are
  retryable infra flakes vs. real findings.
- [[no-lazy-allowlist-tech-debt]] — silencing a gate via an unscheduled
  exception is the same class of "make the gate lie" shortcut.
- [[pin-release-toolchain-and-make-releases-retryable]] — release-pipeline
  sibling: make the step reproducible and re-drivable instead of patching around it.
