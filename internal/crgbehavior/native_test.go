package crgbehavior

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// nnTStoreReadErr is the failure the stub store injects at a chosen readback.
var nnTStoreReadErr = errors.New("nnT: injected namespace read failure")

// nnTFailingStore is a real kg-native store whose Nth namespace read fails.
//
// bootstrapNative reads the persisted namespace back four times — the
// bootstrap's own parity snapshot, the note readback, the flow derivation and
// the postprocess derivation — and each stage must surface WHICH readback
// broke. Returning a usable-looking zero nativeSide instead would make the
// comparison report "the native side has no flows" for what is really a
// storage failure.
type nnTFailingStore struct {
	sdk.Store
	failOnRead int
	reads      int
}

func (s *nnTFailingStore) Notes(token sdk.Token, ns string) ([]sdk.Note, error) {
	s.reads++
	if s.reads == s.failOnRead {
		return nil, nnTStoreReadErr
	}
	return s.Store.Notes(token, ns)
}

// nnTSymbol builds one release-side symbol in the comparison id space.
func nnTSymbol(file, name string) crg.Symbol {
	return crg.Symbol{
		QualifiedName: file + qualifiedSep + name,
		Kind:          "Function",
		Language:      "go",
		FilePath:      file,
		LineStart:     1,
		ContentHash:   "h-" + name,
	}
}

// nnTCalls builds one release-side CALLS edge between two symbols.
func nnTCalls(file, from, to string) BridgeEdge {
	return BridgeEdge{
		Kind:       "CALLS",
		From:       file + qualifiedSep + from,
		To:         file + qualifiedSep + to,
		FilePath:   file,
		Confidence: 1,
	}
}

// nnTChainViews is the release-side view set the native adapter ingests: a
// three-symbol CALLS chain in pkg/a.go plus a two-symbol chain in pkg/z.go, so
// the flow derivation, the impact radius and the note readback all have more
// than one row to be wrong about. The z.go chain is listed FIRST so an
// ordering claim cannot pass by accident of insertion order.
func nnTChainViews() BridgeViews {
	return BridgeViews{
		Symbols: []crg.Symbol{
			nnTSymbol("pkg/z.go", "Zeta"),
			nnTSymbol("pkg/z.go", "Zulu"),
			nnTSymbol("pkg/a.go", "Alpha"),
			nnTSymbol("pkg/a.go", "Beta"),
			nnTSymbol("pkg/a.go", "Gamma"),
		},
		Edges: []BridgeEdge{
			nnTCalls("pkg/z.go", "Zeta", "Zulu"),
			nnTCalls("pkg/a.go", "Alpha", "Beta"),
			nnTCalls("pkg/a.go", "Beta", "Gamma"),
		},
	}
}

// nnTBootstrap ingests views through a real in-memory kg-native store.
func nnTBootstrap(t *testing.T, views BridgeViews) nativeSide {
	t.Helper()
	side, err := bootstrapNative(sdk.NewMemStore(), views, "commit0")
	if err != nil {
		t.Fatalf("bootstrapNative: %v", err)
	}
	return side
}

// The native side must be derived from the PERSISTED namespace: the file map
// comes back off the stored notes, and the flow rows carry the release's flow
// shape — identity re-keyed onto the entry-point symbol, the ordered member
// path, and the node count. Depth, FileCount and Criticality are deliberately
// left zero: the native derivation produces no counterpart, and synthesizing
// one would compare the release against a fabricated value.
func TestNnTBootstrapNativeDerivesViewsFromTheReadback(t *testing.T) {
	side := nnTBootstrap(t, nnTChainViews())

	wantSeeds := []string{
		repoFile("pkg/a.go", "Alpha"),
		repoFile("pkg/a.go", "Beta"),
		repoFile("pkg/a.go", "Gamma"),
	}
	if got := side.seedsFor([]string{"pkg/a.go"}); !reflect.DeepEqual(got, wantSeeds) {
		t.Fatalf("seedsFor(pkg/a.go) = %v, want %v", got, wantSeeds)
	}
	wantRows := []BridgeFlow{
		{
			Name:       "Alpha",
			EntryPoint: repoFile("pkg/a.go", "Alpha"),
			Path:       wantSeeds,
			NodeCount:  3,
		},
		{
			Name:       "Zeta",
			EntryPoint: repoFile("pkg/z.go", "Zeta"),
			Path:       []string{repoFile("pkg/z.go", "Zeta"), repoFile("pkg/z.go", "Zulu")},
			NodeCount:  2,
		},
	}
	if got := side.flowRows(); !reflect.DeepEqual(got, wantRows) {
		t.Fatalf("flowRows() = %+v, want %+v", got, wantRows)
	}
}

// Each readback stage must name itself in the error, and must not hand back a
// half-built native side that a later comparison would read as "empty".
func TestNnTBootstrapNativeNamesTheFailedReadback(t *testing.T) {
	cases := []struct {
		name       string
		failOnRead int
		want       string
	}{
		{"bootstrap parity snapshot", 1, "native bootstrap"},
		{"symbol note readback", 2, "native readback"},
		{"flow derivation", 3, "native flows"},
		{"postprocess derivation", 4, "native derived views"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &nnTFailingStore{Store: sdk.NewMemStore(), failOnRead: c.failOnRead}
			side, err := bootstrapNative(store, nnTChainViews(), "commit0")
			nnTAssertStageFailure(t, side, err, c.want)
		})
	}
}

// nnTAssertStageFailure checks a bootstrap failure is attributed and total.
func nnTAssertStageFailure(t *testing.T, side nativeSide, err error, stage string) {
	t.Helper()
	if !errors.Is(err, nnTStoreReadErr) {
		t.Fatalf("error = %v, want it to wrap the injected store failure", err)
	}
	if !strings.Contains(err.Error(), stage) {
		t.Fatalf("error = %q, want it to name the %q stage", err, stage)
	}
	if side.store != nil || side.fileByID != nil || side.flows != nil {
		t.Fatalf("a failed bootstrap returned a partially built native side: %+v", side)
	}
}

// The release can persist an edge row the kg-native store legitimately refuses
// to ingest — here an edge with no kind at all. The gate must report the
// rejection instead of quietly comparing against a graph that is missing the
// edge: a silently dropped edge changes the impact radius and the flow set.
func TestNnTBootstrapNativeSurfacesAnIngestionRejection(t *testing.T) {
	views := nnTChainViews()
	views.Edges = append(views.Edges, BridgeEdge{
		From:     "pkg/a.go" + qualifiedSep + "Alpha",
		To:       "pkg/a.go" + qualifiedSep + "Gamma",
		FilePath: "pkg/a.go",
	})
	side, err := bootstrapNative(sdk.NewMemStore(), views, "commit0")
	if err == nil {
		t.Fatalf("an edge with no kind was ingested; native side = %+v", side)
	}
	if !strings.Contains(err.Error(), "native bootstrap") {
		t.Fatalf("error = %q, want it attributed to the native bootstrap", err)
	}
	if errors.Is(err, nnTStoreReadErr) {
		t.Fatalf("error = %v, want the store's own rejection, not an injected one", err)
	}
}

// The impact radius is a bounded expansion over the edges ACTUALLY persisted,
// so the hop budget has to bite: one hop from the chain head reaches only its
// direct callee, two hops reach the tail, and the seeds themselves are never
// reported as impacted.
func TestNnTImpactHonoursTheHopBudget(t *testing.T) {
	side := nnTBootstrap(t, nnTChainViews())
	seeds := []string{repoFile("pkg/a.go", "Alpha")}
	cases := []struct {
		name  string
		depth int
		want  []string
	}{
		{"no hops", 0, []string{}},
		{"one hop", 1, []string{repoFile("pkg/a.go", "Beta")}},
		{"two hops", 2, []string{repoFile("pkg/a.go", "Beta"), repoFile("pkg/a.go", "Gamma")}},
		{"beyond the chain", 5, []string{repoFile("pkg/a.go", "Beta"), repoFile("pkg/a.go", "Gamma")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := side.impact(seeds, c.depth)
			if err != nil {
				t.Fatalf("impact: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("impact(depth=%d) = %v, want %v", c.depth, got, c.want)
			}
		})
	}
}

// A commit the release persisted nothing for must produce an EMPTY native side,
// not a nil one: the comparison distinguishes "no rows" from "no answer", and
// the no-answer case is the error return.
func TestNnTEmptyNativeSideYieldsEmptyViews(t *testing.T) {
	side := nnTBootstrap(t, BridgeViews{})
	got, err := side.impact([]string{repoFile("pkg/a.go", "Alpha")}, 3)
	if err != nil {
		t.Fatalf("impact on an empty store: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("impact = %#v, want an empty non-nil id set", got)
	}
	if rows := side.flowRows(); rows == nil || len(rows) != 0 {
		t.Fatalf("flowRows = %#v, want an empty non-nil row set", rows)
	}
}

// A storage failure during the impact query is an ERROR, never an empty blast
// radius — an empty radius would be read as "this change touches nothing".
func TestNnTImpactPropagatesAReadFailure(t *testing.T) {
	side := nativeSide{store: &nnTFailingStore{Store: sdk.NewMemStore(), failOnRead: 1}}
	got, err := side.impact([]string{repoFile("pkg/a.go", "Alpha")}, 2)
	if !errors.Is(err, nnTStoreReadErr) {
		t.Fatalf("impact error = %v, want the store failure", err)
	}
	if got != nil {
		t.Fatalf("impact returned %v alongside an error, want nil", got)
	}
}

// Flow rows are keyed on the entry-point symbol and ordered by it, so two
// runs that derived the same flows in a different order compare equal. The
// returned path must also be a COPY: a caller that reorders or truncates it
// must not corrupt the native side's own flow record.
func TestNnTFlowRowsSortByEntryPointAndCopyThePath(t *testing.T) {
	members := []string{"zz.go::Z@zz.go", "zz.go::Y@zz.go"}
	side := nativeSide{flows: []crg.Flow{
		{ID: "zz.go::Z@zz.go", EntryPoint: "zz.go" + qualifiedSep + "Z", Members: members},
		{ID: "aa.go::A@aa.go", EntryPoint: "aa.go" + qualifiedSep + "A", Members: []string{"aa.go::A@aa.go"}},
	}}
	rows := side.flowRows()
	if len(rows) != 2 || rows[0].EntryPoint != "aa.go::A@aa.go" || rows[1].EntryPoint != "zz.go::Z@zz.go" {
		t.Fatalf("flowRows = %+v, want them sorted by entry point", rows)
	}
	if rows[0].Name != "A" || rows[1].Name != "Z" {
		t.Fatalf("flow names = %q/%q, want the bare symbol names", rows[0].Name, rows[1].Name)
	}
	rows[1].Path[0] = "mutated"
	if members[0] != "zz.go::Z@zz.go" {
		t.Fatalf("flowRows aliased the native flow's member slice: %v", members)
	}
}

// filePartOf feeds the per-file FTS restriction, so it must split at the FIRST
// separator and report NO file for a target that names none — a bare call
// target must never be attributed to a changed file.
func TestNnTFilePartOfSplitsAtTheFirstSeparator(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"file and symbol", "pkg/a.go" + qualifiedSep + "Entry", "pkg/a.go"},
		{"nested symbol keeps the first split", "pkg/a.go::Widget::Render", "pkg/a.go"},
		{"unresolved bare target", "append", ""},
		{"empty id", "", ""},
		{"leading separator names no file", qualifiedSep + "Entry", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := filePartOf(c.in); got != c.want {
				t.Fatalf("filePartOf(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// symbolNameOf supplies the release-comparable flow NAME, so it must take the
// LAST segment (a method on a nested type) and return a separator-less value
// unchanged rather than emptying it.
func TestNnTSymbolNameOfTakesTheLastSegment(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"file and symbol", "pkg/a.go" + qualifiedSep + "Entry", "Entry"},
		{"nested symbol keeps the last split", "pkg/a.go::Widget::Render", "Render"},
		{"unresolved bare target is unchanged", "append", "append"},
		{"empty id", "", ""},
		{"trailing separator", "pkg/a.go" + qualifiedSep, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := symbolNameOf(c.in); got != c.want {
				t.Fatalf("symbolNameOf(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
