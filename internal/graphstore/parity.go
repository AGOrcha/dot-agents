// Package graphstore — CRG dual-read parity surface (t4-crg-dual-read).
//
// This file ships the structural-equivalence parity oracle that the CRG
// migration (spec §11) is gated on. It folds the O6 proposal refinements
// (.agents/proposals/crg-dual-read-parity-surface-2026-05.md):
//
//   - A: per-kind ±tolerance on nodes.kind / nodes.language / edges.kind,
//     replacing the under-specified "±1% of kg build output" total check.
//     A bootstrap that drops every Type row but doubles Function rows stays
//     within 1% on the grand total — per-kind tolerance catches it.
//   - C: STRUCTURAL equivalence (set-equality, partition-equivalence via
//     pair-agreement, Spearman rank correlation > τ) replacing the
//     "bytes-equivalent" criterion, which can never pass against LLM-derived
//     summary fields.
//   - D: a STRUCTURED upsert-tuple oracle — the set of
//     (qualified_name, kind, file_path, line_start, op) tuples an update
//     produces — replacing the free-text parseCRGMutationSummary regex
//     (crg.go:504+) as the `update` row oracle.
//
// O6 item G (SQL-callable parity views) is REJECTED here: it violates the
// §2.2/§5.2 no-raw-SQL invariant. The oracle is computed in Go over data both
// adapters expose through the Store seam, never via adapter-authored SQL views.
package graphstore

import (
	"fmt"
	"math"
	"sort"
)

// DefaultKindTolerance is the per-kind drift tolerance (O6 refinement A:
// "±1% per (kind) AND per (language)"). A divergence of this fraction or less
// on any single anchor bucket passes; anything larger fails parity.
const DefaultKindTolerance = 0.01

// ParitySnapshot is the structured build/status oracle for one adapter at one
// commit (O6 refinement A, replacing the under-specified §11.1 "build" row).
// It is computed in Go from data the adapter exposes through the Store seam —
// it is NOT a SQL-callable view (O6 item G rejected, §2.2/§5.2 no-raw-SQL).
type ParitySnapshot struct {
	// Adapter is the adapter name the snapshot was taken from ("crg" or
	// "crg-bridge").
	Adapter string
	// SchemaDigest is the adapter's schema digest at snapshot time.
	SchemaDigest string
	// Commit is the pinned source commit the snapshot was bootstrapped at.
	Commit string
	// NodesTotal is the grand total node (symbol) count.
	NodesTotal int
	// NodesByKind maps a node kind (Function, Type, ...) to its count. This is
	// the per-kind anchor column O6 refinement A requires.
	NodesByKind map[string]int
	// NodesByLanguage maps a node language (go, ts, ...) to its count — the
	// second per-kind anchor column.
	NodesByLanguage map[string]int
	// EdgesByKind maps an edge kind (CALLS, TESTED_BY, ...) to its count.
	EdgesByKind map[string]int
	// Files is the exact distinct file count (O6 refinement A: "total file
	// count exact", not toleranced).
	Files int
}

// UpsertOp is the operation an update applied to a single symbol.
type UpsertOp string

const (
	// OpInsert is a newly-added symbol.
	OpInsert UpsertOp = "insert"
	// OpUpdate is an existing symbol whose content changed.
	OpUpdate UpsertOp = "update"
	// OpDelete is a removed symbol.
	OpDelete UpsertOp = "delete"
)

// UpsertTuple is the structured upsert oracle of O6 refinement D — the unit a
// `kg update` produces. The set of these tuples replaces the free-text
// parseCRGMutationSummary regex as the `update`-row parity oracle: two
// adapters agree iff they produce set-equal upsert tuples.
type UpsertTuple struct {
	QualifiedName string
	Kind          string
	FilePath      string
	LineStart     int
	Op            UpsertOp
}

// key is the set-membership key for an upsert tuple. Two tuples are the same
// upsert iff their keys match (all five fields).
func (u UpsertTuple) key() string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s", u.QualifiedName, u.Kind, u.FilePath, u.LineStart, u.Op)
}

// ImpactRow is one row of an impact-radius result (O6 refinement C: the
// impact-radius row compares by node-id SET equality, "may differ in order").
type ImpactRow struct {
	NodeID        string
	Kind          string
	QualifiedName string
	FilePath      string
	Hop           int
}

// ParityReport is the verdict of a single parity comparison. Pass is the
// row-level gate; Detail carries human-readable divergence for diagnostics.
type ParityReport struct {
	Row    string
	Pass   bool
	Detail []string
}

// fail appends a divergence reason and marks the report failed.
func (r *ParityReport) fail(format string, args ...any) {
	r.Pass = false
	r.Detail = append(r.Detail, fmt.Sprintf(format, args...))
}

// CompareSnapshots checks build/status parity between two snapshots under the
// O6 refinement-A per-kind tolerance: file count exact, every node-kind,
// node-language and edge-kind bucket within tol. Buckets present in one
// snapshot but absent in the other are compared against zero (so a dropped
// kind fails). tol is a fraction (e.g. 0.01 for ±1%).
func CompareSnapshots(a, b ParitySnapshot, tol float64) ParityReport {
	rep := ParityReport{Row: "build", Pass: true}
	if a.Files != b.Files {
		rep.fail("file count: %s=%d %s=%d (must be exact)", a.Adapter, a.Files, b.Adapter, b.Files)
	}
	compareBuckets(&rep, "nodes.kind", a.NodesByKind, b.NodesByKind, tol)
	compareBuckets(&rep, "nodes.language", a.NodesByLanguage, b.NodesByLanguage, tol)
	compareBuckets(&rep, "edges.kind", a.EdgesByKind, b.EdgesByKind, tol)
	return rep
}

// compareBuckets fails rep for any bucket whose two counts diverge by more
// than tol (relative to the larger of the two, so a dropped bucket — larger
// nonzero, other zero — always fails).
func compareBuckets(rep *ParityReport, anchor string, a, b map[string]int, tol float64) {
	for _, k := range unionKeys(a, b) {
		av, bv := a[k], b[k]
		if av == bv {
			continue
		}
		denom := av
		if bv > denom {
			denom = bv
		}
		drift := math.Abs(float64(av-bv)) / float64(denom)
		if drift > tol {
			rep.fail("%s[%s]: %d vs %d (drift %.3f > tol %.3f)", anchor, k, av, bv, drift, tol)
		}
	}
}

// unionKeys returns the sorted union of two int-map key sets.
func unionKeys(a, b map[string]int) []string {
	seen := make(map[string]bool, len(a)+len(b))
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CompareUpserts checks `update`-row parity via O6 refinement D: the two
// adapters agree iff their upsert-tuple SETS are equal. Reports the symmetric
// difference on failure.
func CompareUpserts(a, b []UpsertTuple) ParityReport {
	rep := ParityReport{Row: "update", Pass: true}
	sa, sb := tupleSet(a), tupleSet(b)
	for k, t := range sa {
		if _, ok := sb[k]; !ok {
			rep.fail("only in A: %s/%s@%s:%d (%s)", t.QualifiedName, t.Kind, t.FilePath, t.LineStart, t.Op)
		}
	}
	for k, t := range sb {
		if _, ok := sa[k]; !ok {
			rep.fail("only in B: %s/%s@%s:%d (%s)", t.QualifiedName, t.Kind, t.FilePath, t.LineStart, t.Op)
		}
	}
	return rep
}

// tupleSet keys a tuple slice by its membership key.
func tupleSet(ts []UpsertTuple) map[string]UpsertTuple {
	out := make(map[string]UpsertTuple, len(ts))
	for _, t := range ts {
		out[t.key()] = t
	}
	return out
}

// CompareImpactRadius checks impact-radius parity via O6 refinement C: node-id
// SET equality ("same node set, may differ in order"). Edges and hop depth are
// not compared here — the row's pinned criterion is the node set.
func CompareImpactRadius(a, b []ImpactRow) ParityReport {
	rep := ParityReport{Row: "impact-radius", Pass: true}
	sa, sb := impactIDSet(a), impactIDSet(b)
	for id := range sa {
		if !sb[id] {
			rep.fail("node %q only in A", id)
		}
	}
	for id := range sb {
		if !sa[id] {
			rep.fail("node %q only in B", id)
		}
	}
	return rep
}

// impactIDSet collects the distinct node ids of an impact result.
func impactIDSet(rows []ImpactRow) map[string]bool {
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.NodeID] = true
	}
	return out
}

// The derived-view oracles that used to live here — PartitionAgreement
// (community partition equivalence via pair agreement) and SpearmanTau
// (risk_index rank correlation) — are GONE, deliberately.
//
// They were statistical predicates comparing two NATIVE computations to each
// other, and they could not fail on a wrong-but-self-consistent algorithm.
// Both algorithms were in fact wrong: communities were weakly-connected
// components over CALLS ∪ IMPORTS where upstream groups by directory, and
// risk_index was degree centrality where upstream uses a
// caller/coverage/security formula. A loose agreement threshold over two
// copies of the same mistake reports perfect parity.
//
// The derived views are now compared field for field against upstream's own
// recorded rows in testdata/crg-release/v2.3.8/graph.json — see
// internal/adapters/builtin/crg/postprocess_test.go. That oracle has no
// tolerance to tune and no way to pass on a divergent algorithm. The
// per-kind tolerance and upsert-tuple oracles above remain: they compare the
// native adapter to the BRIDGE, which is a genuinely independent
// implementation, and their inputs (counts and mutation tuples) have no
// canonical recorded form to compare against instead.
