# P5 End-to-End Integration Contract

- task: `p5-e2e-integration`
- requirements: DC7, DC8, D7
- dependencies: `p2-hook-scripts`, `p4-sentinel-wiring`

## Goal

Prove the wired starter hooks perform the intended native
remediation/advisory behavior in the repository's established shell-smoke
style.

## Test File

Create `tests/test-loop-discipline-stop-hooks.sh`. The repository uses flat
`tests/test-*.sh` smoke tests; do not create a new integration-test directory
for this single workflow.

## Required Cases

1. Write an appropriate loop-worker sentinel, create an out-of-scope change,
   invoke the gate, and assert the platform-native hard-remediation response
   names the scope violation.
2. Where P1c and P2 produce an executable verified-trace path, supply trace
   input proving a forbidden command and assert hard remediation. If no portable
   trace consumption is shipped, assert the documented coverage advisory
   instead and leave the hard-trace case pending with an explicit reason.
3. Exercise a soft condition such as dirty-but-in-scope state or unavailable
   trace and assert success with explanatory stderr.
4. Copy or refresh the embedded starter into a sandbox home and assert the
   three skill bundles, loop-worker agent/profile, and three hook bundles are
   materialized without directory replacement warnings.

## Harness Rules

- Isolate filesystem state beneath a temporary directory and clean it on
  exit.
- Reuse the real `da` binary or repository test convention for invoking the
  CLI; mocks may isolate vendor hook input only where the contract under test
  is explicit.
- Assert exit status, JSON block shapes for Claude/Codex/Copilot,
  Cursor `followup_message`, and stderr text for advisories separately.

## Acceptance

- The test is runnable alongside the repository shell smoke suite.
- The test distinguishes proof-backed blocking from advisory-only missing
  trace coverage; it must not label an inferred transcript rule as enforced.

## Out of Scope

- Vendor UI automation.
- Performance or outcome-scoring telemetry.
