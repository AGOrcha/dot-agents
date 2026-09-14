package crgbehavior

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// metadataLastPostprocessed is the release's postprocess stamp.
const metadataLastPostprocessed = "last_postprocessed_at"

// LifecycleObservation records what the pinned release's `build` and standalone
// `postprocess` commands actually do to the derived summary tables.
//
// This is a real, consumer-visible contract, not trivia. In v2.3.8 a FULL build
// recomputes the summary tables (`community_summaries`, `flow_snapshots`,
// `risk_index`), while standalone `postprocess` rebuilds flows, communities and
// FTS but does NOT recompute the summaries. Because `flows.id` is an
// AUTOINCREMENT rowid that `store_flows` deletes and re-inserts, a standalone
// postprocess leaves every `flow_snapshots.flow_id` pointing at a flow row that
// no longer exists — silently stale data for anything that reads snapshots
// after a postprocess. An adapter that eagerly recomputes summaries there, or
// one that assumes snapshots are fresh, diverges from the release.
type LifecycleObservation struct {
	// Observed reports whether the probe could run at all.
	Observed bool `json:"observed"`
	// Reason explains an unobservable probe.
	Reason string `json:"reason,omitempty"`
	// SnapshotsConsistentAfterBuild — after a full build every
	// flow_snapshots.flow_id resolves to a live flows.id.
	SnapshotsConsistentAfterBuild bool `json:"snapshots_consistent_after_build"`
	// PostprocessRebuiltFlows — standalone postprocess re-created the flows
	// rows (the autoincrement id space advanced).
	PostprocessRebuiltFlows bool `json:"postprocess_rebuilt_flows"`
	// SnapshotsStaleAfterPostprocess — the snapshot rows were left untouched
	// and now dangle against the rebuilt flows.
	SnapshotsStaleAfterPostprocess bool `json:"snapshots_stale_after_postprocess"`
	// PostprocessStampedMetadata — the release recorded the postprocess run.
	PostprocessStampedMetadata bool `json:"postprocess_stamped_metadata"`
}

// Surface renders the lifecycle observation as a comparison verdict against the
// pinned release's documented behavior.
func (l LifecycleObservation) Surface() Surface {
	if !l.Observed {
		return notExercised(SurfaceLifecycle, l.Reason)
	}
	expectations := []struct {
		got  bool
		want string
	}{
		{l.SnapshotsConsistentAfterBuild, "a full build leaves every flow_snapshots.flow_id resolvable"},
		{l.PostprocessRebuiltFlows, "standalone postprocess rebuilds the flows rows"},
		{l.SnapshotsStaleAfterPostprocess, "standalone postprocess leaves flow_snapshots stale (it does not recompute summaries)"},
		{l.PostprocessStampedMetadata, "standalone postprocess stamps metadata." + metadataLastPostprocessed},
	}
	var detail []string
	for _, e := range expectations {
		if !e.got {
			detail = append(detail, "release did NOT behave as pinned: "+e.want)
		}
	}
	metric := fmt.Sprintf("%d of %d pinned lifecycle expectation(s) held", len(expectations)-len(detail), len(expectations))
	if len(detail) == 0 {
		return agree(SurfaceLifecycle, metric)
	}
	return diverge(SurfaceLifecycle, metric, detail)
}

// FlowState is the summary-table state the lifecycle probe samples.
type FlowState struct {
	flowIDs         []int64
	snapshotFlowIDs []int64
	postprocessedAt string
}

// ReadFlowState samples the flow/snapshot id spaces and the postprocess stamp
// from an open release graph.
func ReadFlowState(db *sql.DB) (FlowState, error) {
	var st FlowState
	var err error
	if st.flowIDs, err = readIDs(db, `SELECT id FROM flows`, tableFlows); err != nil {
		return FlowState{}, err
	}
	if st.snapshotFlowIDs, err = readIDs(db, `SELECT flow_id FROM flow_snapshots`, tableFlowSnapshots); err != nil {
		return FlowState{}, err
	}
	err = db.QueryRow(`SELECT value FROM metadata WHERE key = ?`, metadataLastPostprocessed).Scan(&st.postprocessedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return FlowState{}, fmt.Errorf("crgbehavior: read %s: %w", metadataLastPostprocessed, err)
	}
	return st, nil
}

// readIDs reads a sorted id column.
func readIDs(db *sql.DB, query, table string) ([]int64, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("crgbehavior: query bridge %s ids: %w", table, err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("crgbehavior: scan bridge %s id: %w", table, err)
		}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, rowsErr(rows, table)
}

// ObserveLifecycle folds a before/after sample pair into the observation. The
// "before" sample is taken straight after a full build; the "after" sample
// straight after a standalone postprocess of the same graph.
func ObserveLifecycle(before, after FlowState) LifecycleObservation {
	switch {
	case len(before.flowIDs) == 0:
		return LifecycleObservation{Reason: "the release traced no execution flow for this commit, " +
			"so the build/postprocess summary-staleness contract cannot be observed"}
	case len(before.snapshotFlowIDs) == 0:
		return LifecycleObservation{Reason: "the release recorded no flow_snapshots for this commit, " +
			"so the build/postprocess summary-staleness contract cannot be observed"}
	}
	liveBefore := int64Set(before.flowIDs)
	liveAfter := int64Set(after.flowIDs)
	return LifecycleObservation{
		Observed:                       true,
		SnapshotsConsistentAfterBuild:  allIn(before.snapshotFlowIDs, liveBefore),
		PostprocessRebuiltFlows:        len(after.flowIDs) > 0 && noneIn(after.flowIDs, liveBefore),
		SnapshotsStaleAfterPostprocess: sameIDs(before.snapshotFlowIDs, after.snapshotFlowIDs) && noneIn(after.snapshotFlowIDs, liveAfter),
		PostprocessStampedMetadata:     after.postprocessedAt != "",
	}
}

// int64Set builds a lookup set of ids.
func int64Set(ids []int64) map[int64]bool {
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// allIn reports whether every id is present in the set.
func allIn(ids []int64, set map[int64]bool) bool {
	for _, id := range ids {
		if !set[id] {
			return false
		}
	}
	return true
}

// noneIn reports whether no id is present in the set.
func noneIn(ids []int64, set map[int64]bool) bool {
	for _, id := range ids {
		if set[id] {
			return false
		}
	}
	return true
}

// sameIDs compares two sorted id slices.
func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
