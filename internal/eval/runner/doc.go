// Package runner abstracts over invoking a CLI agent against a sandbox working
// directory. It is the R4 harness's per-platform binding: the same Runner
// interface drives claude, codex, and gh-copilot from the same eval pipeline.
//
// # Canonical AgentTelemetry
//
// AgentTelemetry is the canonical type for agent-runner identity and usage
// telemetry across the eval harness. The scoringbridge package defines its own
// interim AgentTelemetry shape (introduced before this package existed); a
// follow-up should switch scoringbridge to import runner.AgentTelemetry directly
// to avoid the two-type maintenance burden. Until then both shapes are
// structurally identical and the harness driver maps between them at the
// scoringbridge call site.
//
// # OQ1 ruling (v1 adapter set)
//
// v1 runners: claude, codex, AND copilot all ship (repo-owner ruling
// 2026-07-02), overriding R4 OQ1's "others stubbed" default
// (design.md:101). The owner ruling is the canonical authorization;
// additional platforms (cursor, others) remain deferred behind the Runner
// interface and can be added as follow-on per-adapter tasks without harness
// changes.
//
// # Exec seam
//
// Every adapter struct carries a run field of type cmdFn. In production the
// field is set to realExec, which wraps exec.CommandContext. Tests replace it
// with a deterministic fake to avoid spawning real subprocesses. Using a
// per-instance field rather than a package-level var keeps tests safe under
// -race: each test constructs its own adapter instance with its own fake, so
// no goroutine races on shared state.
//
// # Token telemetry
//
// Claude emits structured usage data via --output-format json; the claude
// adapter parses it when present. Codex and copilot do not expose
// machine-readable token counts in their v1 CLI surfaces; their adapters leave
// Telemetry.Tokens nil, which scoringbridge records as absent (the rubric
// renormalizes over present signals).
package runner
