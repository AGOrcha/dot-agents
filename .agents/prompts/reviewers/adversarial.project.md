# Adversarial lens — dot-agents repo overlay

Repo-local committed layer. Composes **after** `reviewers/reviewer.base.md` (the contract) and
`reviewers/adversarial.md` (the lens: red-team). This file adds **only** the dot-agents hotspots.

## dot-agents-specific attack surface

- **PATH / exec hardening:** subprocess spawns resolve absolute or `execabs`-checked binaries, never a
  relative/poisonable PATH lookup (SonarCloud go:S4036). Flag any `exec.Command` that takes a
  caller-influenced binary name.
- **POSIX/Windows divergence:** raw `os.*` fs calls or `runtime.GOOS` branches in business/test logic
  that skipped tests won't catch — should route through `internal/fsops` (mutations) /
  `internal/testutil.Make*Unreadable` (forced errors). See lesson `leverage-cross-platform-fs-helpers`.
- **Config-layer trust:** an inherited org/team layer (or a `sources` entry it declares) injecting a
  malicious source, package ref, or protected-field override; secret/credential leakage into the lock,
  generated outputs, or logs.
- **Swallowed results:** discarded `err` / `_ =` on `os.Stat`/`os.Remove`/link operations that hide a
  managed-output clobber or a partial write.
- **Clobber paths:** writes/relinks that overwrite a non-managed or user-authored file; `da sync`
  pruning that could delete content outside the resolved managed set.

Active probing only when `sandbox_mutations` is true (see the lens template). Verdict line
`(lens: adversarial)`; `fail` on any BLOCKER/HIGH.
