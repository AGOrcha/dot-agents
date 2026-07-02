// Package kgquery is the R4 harness's read-only adapter over the code
// knowledge graph. It surfaces candidate code sites (seed symbols), their
// call-graph neighborhoods, and a reproducible complexity proxy that the
// per-language task generators and the difficulty-derivation step consume.
//
// The package is a pure consumer of the published graphstore contract: it
// depends only on the narrow [graphstore.CodeGraphReader] role and issues no
// writes. Holding the narrow role (rather than the whole Store) keeps the
// dependency honest and lets tests back the querier with a tiny in-memory
// fake that stubs only the read methods.
//
// All results are deterministic: seed lists and neighborhoods are sorted by
// qualified name and the complexity proxy is derived from stored node spans
// and call edges, so re-running against the same graph state yields the same
// output (R4 requirement R2 — reproducible difficulty signals).
//
// This layer performs no task synthesis. It answers "what could we build a
// task around?" and "how complex is this symbol?"; the generators
// (internal/eval/gen/<lang>) and internal/eval/difficulty own the decisions
// that turn these signals into a TaskSpec.
package kgquery
