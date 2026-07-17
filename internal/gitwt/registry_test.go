package gitwt

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
)

// newRegistry binds a Registry to the fixture manager with a fixed clock so
// created/last-used timestamps and the idle window are deterministic.
func newRegistry(t *testing.T, f *fixture, ttl time.Duration, now time.Time) *Registry {
	t.Helper()
	reg, err := NewRegistry(f.mgr, ttl)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	reg.now = func() time.Time { return now }
	return reg
}

// addBranch creates a linked worktree named name rooted at the fixture base.
func addBranch(t *testing.T, f *fixture, name string) {
	t.Helper()
	if err := f.mgr.AddBranch(name, f.wtPath(name), f.base); err != nil {
		t.Fatalf("AddBranch %s: %v", name, err)
	}
}

// advanceHead commits a new file inside the named worktree so its tip moves
// past the recorded base (simulating in-progress, unmerged work).
func advanceHead(t *testing.T, f *fixture, name string) {
	t.Helper()
	path := f.wtPath(name)
	wt := openWorktree(t, f, path, name)
	if err := os.WriteFile(filepath.Join(path, "work.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("write work.txt in %s: %v", name, err)
	}
	if err := wt.Stage("work.txt"); err != nil {
		t.Fatalf("Stage in %s: %v", name, err)
	}
	if _, err := wt.Commit("wip", &CommitOptions{AuthorName: "W", AuthorEmail: "w@x"}); err != nil {
		t.Fatalf("Commit in %s: %v", name, err)
	}
}

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestNewRegistryRejectsForeignManager(t *testing.T) {
	if _, err := NewRegistry(stubManager{}, time.Hour); err == nil {
		t.Fatal("NewRegistry accepted a non-go-git Manager, want error")
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	reg := newRegistry(t, f, time.Hour, created)
	addBranch(t, f, "feat")

	meta, err := reg.Create("feat", Metadata{Purpose: "impl X", ParentPR: 171})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if meta.Name != "feat" || meta.Purpose != "impl X" || meta.ParentPR != 171 {
		t.Fatalf("Create meta = %+v, want name=feat purpose=impl X pr=171", meta)
	}
	if !meta.CreatedAt.Equal(created) || !meta.LastUsed.Equal(created) {
		t.Fatalf("Create stamped {created:%s used:%s}, want both %s", meta.CreatedAt, meta.LastUsed, created)
	}

	// Round-trip read reproduces every field.
	got, err := reg.Get("feat")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "feat" || got.Purpose != "impl X" || got.ParentPR != 171 || !got.CreatedAt.Equal(created) {
		t.Fatalf("Get = %+v, want the created record", got)
	}

	// Create is create-once.
	if _, err := reg.Create("feat", Metadata{}); !errors.Is(err, ErrMetadataExists) {
		t.Fatalf("second Create err = %v, want ErrMetadataExists", err)
	}

	// Update mutates purpose while preserving the immutable created-at.
	upd, err := reg.Update("feat", func(m *Metadata) { m.Purpose = "review X" })
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if upd.Purpose != "review X" || !upd.CreatedAt.Equal(created) {
		t.Fatalf("Update = %+v, want purpose review X, created %s", upd, created)
	}

	// Touch advances last-used to the (now later) clock.
	later := created.Add(2 * time.Hour)
	reg.now = func() time.Time { return later }
	touched, err := reg.Touch("feat")
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if !touched.LastUsed.Equal(later) {
		t.Fatalf("Touch last-used = %s, want %s", touched.LastUsed, later)
	}
	if reread, _ := reg.Get("feat"); !reread.LastUsed.Equal(later) {
		t.Fatalf("persisted last-used = %s, want %s", reread.LastUsed, later)
	}
}

// TestRegistryRoundTripAgentConfig proves the resolved agent-config fields
// (app_type, profile, and the app_type-routed execution shape) persist through
// Create and reproduce exactly on Get.
func TestRegistryRoundTripAgentConfig(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	reg := newRegistry(t, f, time.Hour, created)
	addBranch(t, f, "feat")

	in := Metadata{
		Purpose:          "impl slice",
		AppType:          "go-cli",
		Profile:          "loop-worker",
		VerifierSequence: []string{"unit", "cli-runner"},
		LensSet:          []string{"architecture-standards", "adversarial"},
		LensConcurrency:  "gated",
		GraphBackend:     "dotagents-builtin:graph/none@^1.0",
	}
	if _, err := reg.Create("feat", in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := reg.Get("feat")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AppType != in.AppType || got.Profile != in.Profile ||
		got.LensConcurrency != in.LensConcurrency || got.GraphBackend != in.GraphBackend {
		t.Fatalf("scalar agent-config fields did not round-trip: got %+v", got)
	}
	if !reflect.DeepEqual(got.VerifierSequence, in.VerifierSequence) ||
		!reflect.DeepEqual(got.LensSet, in.LensSet) {
		t.Fatalf("slice agent-config fields did not round-trip: got %+v", got)
	}
}

func TestRegistryErrorsForUnknownAndUnrecorded(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())

	// Unknown worktree (no admin dir).
	if _, err := reg.Get("nope"); !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("Get unknown err = %v, want ErrWorktreeNotFound", err)
	}
	if _, err := reg.Create("nope", Metadata{}); !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("Create unknown err = %v, want ErrWorktreeNotFound", err)
	}

	// Known worktree, no metadata yet.
	addBranch(t, f, "bare")
	if _, err := reg.Get("bare"); !errors.Is(err, ErrMetadataNotRecorded) {
		t.Fatalf("Get unrecorded err = %v, want ErrMetadataNotRecorded", err)
	}
	if _, err := reg.Update("bare", nil); !errors.Is(err, ErrMetadataNotRecorded) {
		t.Fatalf("Update unrecorded err = %v, want ErrMetadataNotRecorded", err)
	}
}

func TestReconcileDetectsOrphan(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	for _, n := range []string{"live", "gone"} {
		addBranch(t, f, n)
		if _, err := reg.Create(n, Metadata{Purpose: n}); err != nil {
			t.Fatalf("Create %s: %v", n, err)
		}
	}
	// An unrecorded worktree exists on disk but was never registered.
	addBranch(t, f, "untracked")

	// Remove "gone"'s admin dir out-of-band so List() no longer returns it,
	// while its roster name persists.
	adminDir, err := f.mgr.(*manager).adminDir("gone")
	if err != nil {
		t.Fatalf("adminDir gone: %v", err)
	}
	if err := os.RemoveAll(adminDir); err != nil {
		t.Fatalf("rm gone admin dir: %v", err)
	}

	res, err := reg.Reconcile()
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !contains(res.Orphaned, "gone") {
		t.Fatalf("Orphaned = %v, want it to contain gone", res.Orphaned)
	}
	if !contains(res.Tracked, "live") {
		t.Fatalf("Tracked = %v, want it to contain live", res.Tracked)
	}
	if !contains(res.Untracked, "untracked") {
		t.Fatalf("Untracked = %v, want it to contain untracked", res.Untracked)
	}
	if contains(res.Orphaned, "live") || contains(res.Tracked, "gone") {
		t.Fatalf("misclassified: orphaned=%v tracked=%v", res.Orphaned, res.Tracked)
	}

	// The orphan was dropped from the roster, not silently kept: a second
	// reconcile reports it no more.
	res2, err := reg.Reconcile()
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if contains(res2.Orphaned, "gone") {
		t.Fatalf("orphan still reported after cleanup: %v", res2.Orphaned)
	}
}

func TestReconcileAndDeregister(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "temp")
	if _, err := reg.Create("temp", Metadata{}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := reg.Deregister("temp"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	// Sidecar gone, roster entry gone: no longer tracked, and the still-live
	// worktree now reads as untracked.
	if _, err := reg.Get("temp"); !errors.Is(err, ErrMetadataNotRecorded) {
		t.Fatalf("Get after Deregister = %v, want ErrMetadataNotRecorded", err)
	}
	res, err := reg.Reconcile()
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if contains(res.Tracked, "temp") || !contains(res.Untracked, "temp") {
		t.Fatalf("after Deregister: tracked=%v untracked=%v, want temp untracked only", res.Tracked, res.Untracked)
	}
	// Idempotent.
	if err := reg.Deregister("temp"); err != nil {
		t.Fatalf("second Deregister: %v", err)
	}
}

func TestPruneScanMatrix(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	reg := newRegistry(t, f, time.Hour, now)

	expired := now.Add(-2 * time.Hour) // idle past the 1h TTL
	fresh := now.Add(-1 * time.Minute) // used recently

	// unchanged + expired => prune-eligible.
	addBranch(t, f, "unchanged-expired")
	if _, err := reg.Create("unchanged-expired", Metadata{CreatedAt: expired, LastUsed: expired}); err != nil {
		t.Fatalf("Create unchanged-expired: %v", err)
	}

	// changed + expired => abandoned (kept, never pruned).
	addBranch(t, f, "changed-expired")
	advanceHead(t, f, "changed-expired")
	if _, err := reg.Create("changed-expired", Metadata{CreatedAt: expired, LastUsed: expired}); err != nil {
		t.Fatalf("Create changed-expired: %v", err)
	}

	// unchanged + fresh => kept.
	addBranch(t, f, "unchanged-fresh")
	if _, err := reg.Create("unchanged-fresh", Metadata{CreatedAt: fresh, LastUsed: fresh}); err != nil {
		t.Fatalf("Create unchanged-fresh: %v", err)
	}

	scan, err := reg.PruneScan()
	if err != nil {
		t.Fatalf("PruneScan: %v", err)
	}

	if !contains(scan.Eligible, "unchanged-expired") {
		t.Fatalf("Eligible = %v, want unchanged-expired", scan.Eligible)
	}
	if contains(scan.Eligible, "changed-expired") || contains(scan.Eligible, "unchanged-fresh") {
		t.Fatalf("Eligible over-broad: %v", scan.Eligible)
	}
	if !contains(scan.Abandoned, "changed-expired") {
		t.Fatalf("Abandoned = %v, want changed-expired (commits past base, idle: surfaced not pruned)", scan.Abandoned)
	}
	if !contains(scan.Kept, "unchanged-fresh") {
		t.Fatalf("Kept = %v, want unchanged-fresh", scan.Kept)
	}
}

func TestPruneScanIndeterminateKept(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	reg := newRegistry(t, f, time.Hour, now)
	expired := now.Add(-2 * time.Hour)

	// Idle worktree whose recorded base ref is missing: unchanged is
	// indeterminate, so it is kept rather than pruned.
	addBranch(t, f, "no-base")
	if _, err := reg.Create("no-base", Metadata{CreatedAt: expired, LastUsed: expired}); err != nil {
		t.Fatalf("Create no-base: %v", err)
	}
	adminDir, err := f.mgr.(*manager).adminDir("no-base")
	if err != nil {
		t.Fatalf("adminDir: %v", err)
	}
	if err := os.Remove(filepath.Join(adminDir, baseRefFile)); err != nil {
		t.Fatalf("rm base-ref: %v", err)
	}

	// Rostered worktree whose metadata sidecar was removed: idleness cannot be
	// judged, so it is kept.
	addBranch(t, f, "meta-gone")
	if _, err := reg.Create("meta-gone", Metadata{CreatedAt: expired, LastUsed: expired}); err != nil {
		t.Fatalf("Create meta-gone: %v", err)
	}
	metaDir, err := f.mgr.(*manager).adminDir("meta-gone")
	if err != nil {
		t.Fatalf("adminDir meta-gone: %v", err)
	}
	if err := os.Remove(filepath.Join(metaDir, registryFile)); err != nil {
		t.Fatalf("rm sidecar: %v", err)
	}

	scan, err := reg.PruneScan()
	if err != nil {
		t.Fatalf("PruneScan: %v", err)
	}
	if !contains(scan.Kept, "no-base") {
		t.Fatalf("Kept = %v, want no-base (indeterminate base)", scan.Kept)
	}
	if !contains(scan.Kept, "meta-gone") {
		t.Fatalf("Kept = %v, want meta-gone (missing metadata)", scan.Kept)
	}
	if contains(scan.Eligible, "no-base") || contains(scan.Eligible, "meta-gone") {
		t.Fatalf("Eligible over-broad on indeterminate data: %v", scan.Eligible)
	}
}

// stubManager is a non-go-git Manager used to prove NewRegistry rejects any
// implementation it cannot reach the admin dir through.
type stubManager struct{}

func (stubManager) AddBranch(string, string, plumbing.Hash) error   { return nil }
func (stubManager) AddDetached(string, string, plumbing.Hash) error { return nil }
func (stubManager) Remove(string, string) error                     { return nil }
func (stubManager) List() ([]string, error)                         { return nil, nil }
func (stubManager) Prune() ([]string, error)                        { return nil, nil }
func (stubManager) Open(string) (Worktree, error)                   { return nil, nil }
func (stubManager) RecordBaseRef(string, plumbing.Hash) error       { return nil }
func (stubManager) BaseRef(string) (plumbing.Hash, error)           { return plumbing.ZeroHash, nil }
