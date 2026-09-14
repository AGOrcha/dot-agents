package crgbehavior

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// ErrNativeUnimplemented reports that the kg-native adapter exposes NO
// counterpart for a surface the pinned release materializes.
//
// It is deliberately a divergence, not a skip. Criterion 2 asks whether the
// native backend preserves the behavior consumers get today; a surface the
// release produces and the native side cannot produce at all is the strongest
// possible negative answer for that surface, and recording it as "not
// exercised" would hide the gap behind an environment-shaped excuse.
var ErrNativeUnimplemented = errors.New("the kg-native adapter implements no counterpart for this release surface")

// noteFieldFilePath is the CRG adapter's file-path note field, read back to map
// a review task's changed files onto persisted symbol ids.
const noteFieldFilePath = "file_path"

// nativeSide is the kg-native adapter's state for one materialized commit: the
// persisted namespace plus the derived views computed from its READBACK, never
// from the ingestion input, so a dropped write is visible to the comparison.
type nativeSide struct {
	store    crg.StoreReader
	fileByID map[string]string
	flows    []crg.Flow
	post     crg.Postprocess
}

// bootstrapNative ingests one commit's release graph through the kg-native
// adapter and derives every view the comparison needs.
func bootstrapNative(store sdk.Store, views BridgeViews, commit string) (nativeSide, error) {
	s := sdk.For(crg.Name, store)
	if _, err := crg.Bootstrap(s, store, views.Corpus(commit), nil); err != nil {
		return nativeSide{}, fmt.Errorf("crgbehavior: native bootstrap: %w", err)
	}
	notes, err := store.Notes(sdk.OwnReadToken(crg.Name, "behavior-gate"), crg.Name)
	if err != nil {
		return nativeSide{}, fmt.Errorf("crgbehavior: native readback: %w", err)
	}
	fileByID := make(map[string]string, len(notes))
	for _, n := range notes {
		path, _ := n.Fields[noteFieldFilePath].(string)
		fileByID[n.ID] = path
	}
	flows, err := crg.FlowsFromStore(store, crg.Name)
	if err != nil {
		return nativeSide{}, fmt.Errorf("crgbehavior: native flows: %w", err)
	}
	post, err := crg.PostprocessFromStore(store, crg.Name)
	if err != nil {
		return nativeSide{}, fmt.Errorf("crgbehavior: native derived views: %w", err)
	}
	return nativeSide{store: store, fileByID: fileByID, flows: flows, post: post}, nil
}

// seedsFor resolves a task's changed files to native symbol ids, from the
// PERSISTED namespace readback (not the ingestion corpus).
func (n nativeSide) seedsFor(files []string) []string {
	want := setOf(files)
	var out []string
	for id, path := range n.fileByID {
		if want[path] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// impact answers the blast-radius query over the persisted edge graph.
func (n nativeSide) impact(seeds []string, depth int) ([]string, error) {
	rows, err := crg.ImpactRadiusFromStore(n.store, crg.Name, seeds, depth)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.NodeID)
	}
	sort.Strings(out)
	return out, nil
}

// flowRows lifts the native flows into the release's flow shape for an exact
// identity + ordered-path comparison.
//
// Only the fields the native derivation actually produces are filled. Depth,
// FileCount and the release's weighted criticality are NOT synthesized here:
// the native score is a step count on a different scale and the native
// traversal records no BFS depth, so those are reported through the
// flow_metrics surface as unimplemented rather than compared against a
// fabricated zero.
func (n nativeSide) flowRows() []BridgeFlow {
	out := make([]BridgeFlow, 0, len(n.flows))
	for _, f := range n.flows {
		out = append(out, BridgeFlow{
			Name:       symbolNameOf(f.EntryPoint),
			EntryPoint: f.ID,
			Path:       append([]string(nil), f.Members...),
			NodeCount:  len(f.Members),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EntryPoint < out[j].EntryPoint })
	return out
}

// communities returns the native cluster key for each requested id. The native
// partition already labels a component by its smallest member id, the same
// relabel-invariant key the release's autoincrement community ids are mapped
// onto, so the two are directly comparable.
func (n nativeSide) communities(ids []string) map[string]string {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		if cluster, ok := n.post.Communities[id]; ok {
			out[id] = cluster
		}
	}
	return out
}

// risk returns the native risk score for each requested id.
func (n nativeSide) risk(ids []string) map[string]float64 {
	out := make(map[string]float64, len(ids))
	for _, id := range ids {
		if score, ok := n.post.RiskIndex[id]; ok {
			out[id] = score
		}
	}
	return out
}

// ftsIndex returns the native searchable index content restricted to a task's
// changed files — the counterpart of the release's `nodes_fts` rows for those
// files.
func (n nativeSide) ftsIndex(files []string) []string {
	want := setOf(files)
	var out []string
	for _, token := range n.post.FTS {
		if want[filePartOf(token)] {
			out = append(out, token)
		}
	}
	sort.Strings(out)
	return out
}

// symbolIDOf builds the comparison id for a (qualified name, file path) pair.
func symbolIDOf(qualifiedName, filePath string) string {
	return crg.SymbolID(crg.Symbol{QualifiedName: qualifiedName, FilePath: filePath})
}

// filePartOf returns the file-path part of a "<file>::<symbol>" qualified name.
func filePartOf(qualified string) string {
	if i := strings.Index(qualified, qualifiedSep); i >= 0 {
		return qualified[:i]
	}
	return ""
}

// symbolNameOf returns the bare symbol name of a qualified name.
func symbolNameOf(qualified string) string {
	if i := strings.LastIndex(qualified, qualifiedSep); i >= 0 {
		return qualified[i+len(qualifiedSep):]
	}
	return qualified
}

// setOf builds a lookup set.
func setOf(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[x] = true
	}
	return out
}
