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

// DefaultSpearmanTau is the pinned Spearman rank-correlation floor for
// rank-ordered derived tables such as risk_index (O6 refinement C, "pin τ —
// likely 0.85"). A correlation strictly below this fails parity.
const DefaultSpearmanTau = 0.85

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

// PartitionAgreement is the pair-agreement score between two community
// partitions over the union of their members (O6 refinement C: partition
// equivalence via pair-agreement, "simplest, defensible, computable"). It
// returns the fraction of co-membership decisions the two partitions agree on,
// in [0,1] (1.0 = identical partition up to cluster relabeling). community maps
// node id → cluster id; cluster ids need not match between the two inputs.
//
// The second return is false when the two partitions do NOT cover the same node
// set — a missing or extra node is a parity divergence, not a free pass, so the
// score is meaningless and callers must treat ok=false as a failure. (Earlier
// this leniently returned 1.0 for <2 ids, which masked a dropped node; that was
// MEDIUM #5.) When ok is true and there is at most one shared node, agreement is
// trivially 1.0 (no pair to disagree on).
func PartitionAgreement(a, b map[string]string) (float64, bool) {
	if !sameKeySet(a, b) {
		return 0, false
	}
	ids := partitionMembers(a)
	if len(ids) < 2 {
		return 1.0, true
	}
	var agree, total int
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			total++
			if (a[ids[i]] == a[ids[j]]) == (b[ids[i]] == b[ids[j]]) {
				agree++
			}
		}
	}
	return float64(agree) / float64(total), true
}

// partitionMembers returns the sorted node ids of a partition.
func partitionMembers(a map[string]string) []string {
	out := make([]string, 0, len(a))
	for k := range a {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sameKeySet reports whether two string-keyed maps have identical key sets.
func sameKeySet[V any](a, b map[string]V) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// SpearmanTau is the Spearman rank correlation between two rankings keyed by the
// same ids (O6 refinement C: risk_index parity via Spearman τ). Each map is
// id → score. The second return is false when the two rankings do NOT cover the
// same id set — a missing or extra ranked node is a parity divergence, not a
// free pass (MEDIUM #5: this no longer silently correlates only the shared ids
// and passes). When ok is true the score is in [-1,1]; 1.0 is identical rank
// order. With at most one shared id the order is trivially identical (1.0).
func SpearmanTau(a, b map[string]float64) (float64, bool) {
	if !sameKeySet(a, b) {
		return 0, false
	}
	ids := make([]string, 0, len(a))
	for id := range a {
		ids = append(ids, id)
	}
	if len(ids) < 2 {
		return 1.0, true
	}
	return pearson(ranksOf(ids, a), ranksOf(ids, b)), true
}

// scoredID pairs a node id with its score, for rank assignment.
type scoredID struct {
	id string
	v  float64
}

// ranksOf returns fractional ranks (ties averaged) for the given ids by their
// score in m, indexed parallel to ids.
func ranksOf(ids []string, m map[string]float64) []float64 {
	sorted := make([]scoredID, len(ids))
	for i, id := range ids {
		sorted[i] = scoredID{id, m[id]}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].v < sorted[j].v })
	rankByID := assignAveragedRanks(sorted)
	out := make([]float64, len(ids))
	for i, id := range ids {
		out[i] = rankByID[id]
	}
	return out
}

// assignAveragedRanks assigns 1-based ranks to a value-sorted slice, averaging
// ranks within tie groups. Returns id → rank.
func assignAveragedRanks(sorted []scoredID) map[string]float64 {
	rankByID := make(map[string]float64, len(sorted))
	i := 0
	for i < len(sorted) {
		j := i
		for j < len(sorted) && sorted[j].v == sorted[i].v {
			j++
		}
		avg := float64(i+j+1) / 2.0 // mean of 1-based ranks i+1..j
		for k := i; k < j; k++ {
			rankByID[sorted[k].id] = avg
		}
		i = j
	}
	return rankByID
}

// pearson is the Pearson correlation of two equal-length slices. On a rank
// vector this is Spearman's ρ. Zero variance in either input yields 1.0
// (all-equal ranks — degenerate agreement).
func pearson(x, y []float64) float64 {
	n := float64(len(x))
	var sx, sy float64
	for i := range x {
		sx += x[i]
		sy += y[i]
	}
	mx, my := sx/n, sy/n
	var cov, vx, vy float64
	for i := range x {
		dx, dy := x[i]-mx, y[i]-my
		cov += dx * dy
		vx += dx * dx
		vy += dy * dy
	}
	if vx == 0 || vy == 0 {
		return 1.0
	}
	return cov / math.Sqrt(vx*vy)
}
