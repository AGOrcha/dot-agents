// Package eval is the foundation of the R4 code-task generation and
// evaluation harness. It defines the pure, I/O-free data contracts the rest
// of the harness binds to: a versioned [TaskSpec], the [Generator] interface
// per-language adapters implement, and a language [Registry] that maps a
// [Language] to the [Generator] that produces tasks for it.
//
// This package is deliberately free of I/O. It performs no sandbox
// provisioning, no knowledge-graph queries, and no filesystem access beyond
// in-memory (de)serialization helpers. Those concerns live in downstream R4
// packages (internal/eval/kgquery, internal/eval/sandbox,
// internal/eval/gen/<lang>, ...) which import these contracts.
//
// # Versioning
//
// Every TaskSpec carries a [TaskSpec.TaskSpecVersion]. Schema evolution is
// explicit and auditable: consumers bind to a version, and [CurrentTaskSpecVersion]
// names the version this build produces. See decision D4.5 in the R4 spec
// (.agents/workflow/specs/r4-code-task-generation-eval/design.md).
package eval
