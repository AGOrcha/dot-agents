package codegraph

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// ── bulk export / derived-read error propagation ─────────────────────────────

// TestReadEdgesPropagatesStoreFailure covers ReadEdges' single read.
func TestReadEdgesPropagatesStoreFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{allEdgesErr: errFake})
	if _, err := e.ReadEdges(0); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected edge-read failure", err)
	}
}

// TestReadNodesPropagatesStoreFailure covers ReadNodes' single read.
func TestReadNodesPropagatesStoreFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{allNodesErr: errFake})
	if _, err := e.ReadNodes(0); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected node-read failure", err)
	}
}

// TestListCommunitiesPropagatesDerivedReadFailure covers the derived-table
// read every community query starts from.
func TestListCommunitiesPropagatesDerivedReadFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{communitiesErr: errFake})
	if _, err := e.ListCommunities(0, ""); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected communities-read failure", err)
	}
}

// TestListFlowsPropagatesDerivedReadFailure covers the same for flows.
func TestListFlowsPropagatesDerivedReadFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{flowsErr: errFake})
	if _, err := e.ListFlows(0, ""); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected flows-read failure", err)
	}
}

// ── scan walk arms ───────────────────────────────────────────────────────────

// TestGoFilesReportsWalkFailure covers the walk-failure arm. The callback
// swallows per-entry errors, so the seam is the only way in.
func TestGoFilesReportsWalkFailure(t *testing.T) {
	orig := walkDir
	t.Cleanup(func() { walkDir = orig })
	walkDir = func(string, fs.WalkDirFunc) error { return errFake }
	if _, err := goFiles(t.TempDir()); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected walk failure", err)
	}
}

// TestScanSkipsUnreadableSubtree asserts an unreadable directory is skipped
// rather than failing the whole scan.
func TestScanSkipsUnreadableSubtree(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not block reads here")
	}
	root := writeFixture(t)
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeExtra(t, root, "locked/hidden.go", "package locked\n\nfunc Hidden() {}\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	files, _, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan must skip the unreadable subtree, not fail: %v", err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.RelPath, "locked/") {
			t.Fatalf("unreadable subtree produced a unit: %+v", f)
		}
	}
}

// TestScanSkipsUnreadableSourceFile asserts a path that enumerates as a Go
// file but cannot be read is dropped rather than failing the scan. A dangling
// symlink is unreadable for every user, including root.
func TestScanSkipsUnreadableSourceFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	root := writeFixture(t)
	if err := os.Symlink(filepath.Join(root, "absent-target"), filepath.Join(root, "broken.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files, _, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan must skip the unreadable file, not fail: %v", err)
	}
	for _, f := range files {
		if f.RelPath == "broken.go" {
			t.Fatal("unreadable file produced an ingestion unit")
		}
	}
}

// TestScanLeavesMethodExpressionTargetBare covers the selector arm where the
// qualifier is a same-package receiver TYPE rather than a package. The old
// unique-member resolver turned `Config.Label(c)` into the receiver-qualified
// symbol; the release does not — any selector callee keeps the bare member
// name, because the member lives on the operand's type and the release does no
// type inference. Verified against the v2.3.8 parser on this same source.
func TestScanLeavesMethodExpressionTargetBare(t *testing.T) {
	root := writeFixture(t)
	writeExtra(t, root, "lib/method.go", `package lib

// Label names the config.
func (c Config) Label() string { return c.Name }

// LabelOf applies the method expression, so the reference qualifier is the
// receiver type rather than an imported package.
func LabelOf(c Config) string { return Config.Label(c) }
`)
	_, corpus, err := Scan(root, "")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	caller := fixtureSymbol(root, "lib/method.go", "LabelOf")
	var targets []string
	for _, ref := range corpus.References {
		if ref.Kind == edgeCalls && ref.From == caller {
			targets = append(targets, ref.To)
		}
	}
	if len(targets) != 1 || targets[0] != "Label" {
		t.Fatalf("method-expression CALLS targets = %v, want exactly [Label] (bare)", targets)
	}
}

// ── query limit / ordering arms ──────────────────────────────────────────────

// TestListFlowsTruncatesToLimit covers the explicit-limit truncation against
// more persisted flows than a caller asked for.
func TestListFlowsTruncatesToLimit(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{
		flows: []graphstore.FlowRow{
			{ID: 1, Name: "a", Criticality: 3},
			{ID: 2, Name: "b", Criticality: 2},
			{ID: 3, Name: "c", Criticality: 1},
		},
	})
	result, err := e.ListFlows(2, "")
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(result.Flows) != 2 {
		t.Fatalf("ListFlows(2) = %d flows, want 2", len(result.Flows))
	}
	if result.Flows[0].Name != "a" || result.Flows[1].Name != "b" {
		t.Fatalf("truncated the wrong flows: %+v", result.Flows)
	}
}

// TestListFlowsAppliesDefaultLimit covers the non-positive-limit default.
func TestListFlowsAppliesDefaultLimit(t *testing.T) {
	rows := make([]graphstore.FlowRow, defaultFlowLimit+3)
	for i := range rows {
		rows[i] = graphstore.FlowRow{ID: int64(i + 1), Name: fmt.Sprintf("f%02d", i)}
	}
	e := engineWithStore(t, writeFixture(t), &fakeStore{flows: rows})
	result, err := e.ListFlows(-1, sortByCriticality)
	if err != nil {
		t.Fatalf("ListFlows: %v", err)
	}
	if len(result.Flows) != defaultFlowLimit {
		t.Fatalf("ListFlows(-1) = %d flows, want the %d default", len(result.Flows), defaultFlowLimit)
	}
}

// TestSortFlowsRanksHigherCriticalityFirst covers the criticality comparison
// itself (stubFlows deliberately ties, exercising only the tie-break).
func TestSortFlowsRanksHigherCriticalityFirst(t *testing.T) {
	flows := []graphstore.FlowInfo{
		{ID: 1, Name: "a", Criticality: 1},
		{ID: 2, Name: "z", Criticality: 5},
	}
	sortFlows(flows, sortByCriticality)
	if flows[0].Name != "z" {
		t.Fatalf("criticality sort = %+v, want the most critical flow first", flows)
	}
}

// TestDetectChangesPropagatesDerivedReadFailure covers the risk-index read
// DetectChanges performs after the diff resolves.
func TestDetectChangesPropagatesDerivedReadFailure(t *testing.T) {
	e := engineWithStore(t, writeFixture(t), &fakeStore{riskErr: errFake})
	if _, err := e.DetectChanges(graphstore.DetectChangesOptions{Files: []string{"lib/lib.go"}}); !errors.Is(err, errFake) {
		t.Fatalf("err = %v, want the injected risk-index failure", err)
	}
}
