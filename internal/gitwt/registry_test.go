package gitwt

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
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

// --- error-path coverage ---------------------------------------------------

// TestCreateWriteSidecarFails proves Create surfaces a sidecar-write failure:
// with the admin dir write-denied, the metadata record cannot be persisted.
func TestCreateWriteSidecarFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "feat")
	dir, err := f.mgr.(*manager).adminDir("feat")
	if err != nil {
		t.Fatalf("adminDir: %v", err)
	}
	testutil.MakeDirWriteDenied(t, dir)
	if _, err := reg.Create("feat", Metadata{Purpose: "x"}); err == nil {
		t.Fatal("Create succeeded despite an unwritable admin dir, want error")
	}
}

// TestCreateAddToRosterFails proves Create aborts when, after the sidecar is
// written, the roster cannot be read (its path is a directory).
func TestCreateAddToRosterFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "feat")
	if err := os.Mkdir(reg.rosterPath(), 0o755); err != nil {
		t.Fatalf("mkdir roster: %v", err)
	}
	if _, err := reg.Create("feat", Metadata{}); err == nil {
		t.Fatal("Create succeeded despite an unreadable roster, want error")
	}
}

// TestUpdateWriteSidecarFails proves Update surfaces a sidecar-write failure
// after a successful read.
func TestUpdateWriteSidecarFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "feat")
	if _, err := reg.Create("feat", Metadata{Purpose: "x"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	dir, err := f.mgr.(*manager).adminDir("feat")
	if err != nil {
		t.Fatalf("adminDir: %v", err)
	}
	testutil.MakeDirWriteDenied(t, dir)
	if _, err := reg.Update("feat", func(m *Metadata) { m.Purpose = "y" }); err == nil {
		t.Fatal("Update succeeded despite an unwritable admin dir, want error")
	}
}

// TestDeregisterRemoveFails proves a non-not-found os.Remove failure on the
// sidecar (its path is a non-empty directory) surfaces from Deregister.
func TestDeregisterRemoveFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "feat")
	dir, err := f.mgr.(*manager).adminDir("feat")
	if err != nil {
		t.Fatalf("adminDir: %v", err)
	}
	// A NON-EMPTY directory at the sidecar path makes os.Remove fail with
	// ENOTEMPTY (a real error, not os.ErrNotExist).
	sidecar := filepath.Join(dir, registryFile)
	if err := os.Mkdir(sidecar, 0o755); err != nil {
		t.Fatalf("mkdir sidecar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sidecar, "block"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write block: %v", err)
	}
	if err := reg.Deregister("feat"); err == nil {
		t.Fatal("Deregister succeeded despite an unremovable sidecar, want error")
	}
}

// TestDeregisterAdminDirRealError proves a real (non-not-found) admin-dir stat
// failure surfaces from Deregister rather than being folded into not-found.
func TestDeregisterAdminDirRealError(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "feat")
	testutil.MakeDirUnreadable(t, dotgitWorktreesDir(f.repoPath))
	err := reg.Deregister("feat")
	if err == nil {
		t.Fatal("Deregister succeeded despite a real admin-dir stat failure, want error")
	}
	if errors.Is(err, ErrWorktreeNotFound) {
		t.Errorf("a real stat error must not be folded into ErrWorktreeNotFound: %v", err)
	}
}

// TestReconcileReadRosterFails proves Reconcile surfaces a roster read failure.
func TestReconcileReadRosterFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	if err := os.Mkdir(reg.rosterPath(), 0o755); err != nil {
		t.Fatalf("mkdir roster: %v", err)
	}
	if _, err := reg.Reconcile(); err == nil {
		t.Fatal("Reconcile succeeded despite an unreadable roster, want error")
	}
}

// TestReconcileReadRosterParseError proves a malformed roster YAML surfaces a
// parse error from Reconcile.
func TestReconcileReadRosterParseError(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	if err := os.WriteFile(reg.rosterPath(), []byte("names: [unterminated"), 0o644); err != nil {
		t.Fatalf("write bad roster: %v", err)
	}
	if _, err := reg.Reconcile(); err == nil {
		t.Fatal("Reconcile succeeded despite malformed roster YAML, want parse error")
	}
}

// TestReconcileListFails proves a Manager.List failure propagates from Reconcile.
func TestReconcileListFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	corruptWorktreesDir(t, f)
	if _, err := reg.Reconcile(); err == nil {
		t.Fatal("Reconcile succeeded despite a List failure, want error")
	}
}

// TestReconcileWriteRosterFails proves that when an orphan is detected but the
// roster cannot be rewritten (git dir write-denied), Reconcile surfaces the
// write error instead of silently dropping the orphan cleanup.
func TestReconcileWriteRosterFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "gone")
	if _, err := reg.Create("gone", Metadata{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Remove the admin dir so List() no longer returns "gone": it becomes an
	// orphan Reconcile wants to drop from the roster.
	adminDir, err := f.mgr.(*manager).adminDir("gone")
	if err != nil {
		t.Fatalf("adminDir: %v", err)
	}
	if err := os.RemoveAll(adminDir); err != nil {
		t.Fatalf("rm admin dir: %v", err)
	}
	// Deny writes on the git dir so the roster rewrite (atomic temp create)
	// fails while its read side stays open.
	testutil.MakeDirWriteDenied(t, f.mgr.(*manager).gitDir())
	if _, err := reg.Reconcile(); err == nil {
		t.Fatal("Reconcile succeeded despite a roster-rewrite failure, want error")
	}
}

// TestPruneScanReadRosterFails proves PruneScan surfaces a roster read failure.
func TestPruneScanReadRosterFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	if err := os.Mkdir(reg.rosterPath(), 0o755); err != nil {
		t.Fatalf("mkdir roster: %v", err)
	}
	if _, err := reg.PruneScan(); err == nil {
		t.Fatal("PruneScan succeeded despite an unreadable roster, want error")
	}
}

// TestPruneScanListFails proves a Manager.List failure propagates from PruneScan.
func TestPruneScanListFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	corruptWorktreesDir(t, f)
	if _, err := reg.PruneScan(); err == nil {
		t.Fatal("PruneScan succeeded despite a List failure, want error")
	}
}

// TestPruneScanUnchangedSinceBaseIndeterminate drives the three indeterminate
// legs of unchangedSinceBase (worktree dir unresolvable, worktree unopenable,
// worktree HEAD unreadable): each keeps the worktree rather than auto-pruning
// on data it cannot read. The base ref stays recorded so the check gets past
// the base lookup into the tip resolution that fails.
func TestPruneScanUnchangedSinceBaseIndeterminate(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	t.Run("worktree dir unresolvable", func(t *testing.T) {
		assertPruneScanKeepsIndeterminate(t, now, "wd", func(t *testing.T, f *fixture) {
			dir, _ := f.mgr.(*manager).adminDir("wd")
			if err := os.Remove(filepath.Join(dir, "gitdir")); err != nil {
				t.Fatalf("rm gitdir: %v", err)
			}
		})
	})

	t.Run("worktree unopenable", func(t *testing.T) {
		assertPruneScanKeepsIndeterminate(t, now, "op", func(t *testing.T, f *fixture) {
			// Corrupt the worktree's .git pointer so Open fails while the admin
			// gitdir pointer (worktreeDir) still resolves.
			if err := os.WriteFile(filepath.Join(f.wtPath("op"), ".git"), []byte("garbage"), 0o644); err != nil {
				t.Fatalf("corrupt .git: %v", err)
			}
		})
	})

	t.Run("worktree head unreadable", func(t *testing.T) {
		assertPruneScanKeepsIndeterminate(t, now, "hd", func(t *testing.T, f *fixture) {
			// Point the worktree HEAD at a dangling ref so Open succeeds but Head
			// resolution fails.
			dir, _ := f.mgr.(*manager).adminDir("hd")
			if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/does-not-exist\n"), 0o644); err != nil {
				t.Fatalf("corrupt HEAD: %v", err)
			}
		})
	})
}

// assertPruneScanKeepsIndeterminate registers an expired worktree, applies
// corrupt to break its tip resolution, then asserts PruneScan KEEPS it (never
// auto-prunes on data it cannot read).
func assertPruneScanKeepsIndeterminate(t *testing.T, now time.Time, name string, corrupt func(t *testing.T, f *fixture)) {
	t.Helper()
	expired := now.Add(-2 * time.Hour)
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, now)
	addBranch(t, f, name)
	if _, err := reg.Create(name, Metadata{CreatedAt: expired, LastUsed: expired}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	corrupt(t, f)
	scan, err := reg.PruneScan()
	if err != nil {
		t.Fatalf("PruneScan: %v", err)
	}
	if !contains(scan.Kept, name) || contains(scan.Eligible, name) {
		t.Fatalf("scan=%+v, want %s kept (indeterminate)", scan, name)
	}
}

// TestGetReadSidecarRealError proves a real (non-not-found) sidecar read error
// is not folded into ErrMetadataNotRecorded.
func TestGetReadSidecarRealError(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "feat")
	dir, _ := f.mgr.(*manager).adminDir("feat")
	if err := os.Mkdir(filepath.Join(dir, registryFile), 0o755); err != nil {
		t.Fatalf("mkdir sidecar: %v", err)
	}
	_, err := reg.Get("feat")
	if err == nil {
		t.Fatal("Get succeeded despite an unreadable sidecar, want error")
	}
	if errors.Is(err, ErrMetadataNotRecorded) {
		t.Errorf("a real read error must not be folded into ErrMetadataNotRecorded: %v", err)
	}
}

// TestGetReadSidecarParseError proves a malformed sidecar YAML surfaces a parse
// error from Get.
func TestGetReadSidecarParseError(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "feat")
	dir, _ := f.mgr.(*manager).adminDir("feat")
	if err := os.WriteFile(filepath.Join(dir, registryFile), []byte("name: [unterminated"), 0o644); err != nil {
		t.Fatalf("write bad sidecar: %v", err)
	}
	if _, err := reg.Get("feat"); err == nil {
		t.Fatal("Get succeeded despite malformed sidecar YAML, want parse error")
	}
}

// TestWriteSidecarUnknownWorktree proves writeSidecar surfaces sidecarPath's
// unknown-worktree error before attempting any write.
func TestWriteSidecarUnknownWorktree(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	if err := reg.writeSidecar("ghost", Metadata{}); !errors.Is(err, ErrWorktreeNotFound) {
		t.Fatalf("writeSidecar unknown worktree = %v, want ErrWorktreeNotFound", err)
	}
}

// TestAddToRosterIdempotent proves re-adding a name already in the roster is a
// no-op that leaves the set unchanged.
func TestAddToRosterIdempotent(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	if err := reg.addToRoster("dup"); err != nil {
		t.Fatalf("addToRoster: %v", err)
	}
	if err := reg.addToRoster("dup"); err != nil {
		t.Fatalf("addToRoster (repeat): %v", err)
	}
	set, err := reg.readRoster()
	if err != nil {
		t.Fatalf("readRoster: %v", err)
	}
	if len(set) != 1 || !set["dup"] {
		t.Fatalf("roster=%v, want exactly {dup}", set)
	}
}

// TestDeregisterRemoveFromRosterReadFails proves Deregister surfaces a
// roster-read failure from removeFromRoster after the sidecar is removed.
func TestDeregisterRemoveFromRosterReadFails(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "feat")
	if _, err := reg.Create("feat", Metadata{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Replace the roster file with a directory so removeFromRoster's readRoster
	// fails (the sidecar removal still succeeds first).
	if err := os.Remove(reg.rosterPath()); err != nil {
		t.Fatalf("rm roster: %v", err)
	}
	if err := os.Mkdir(reg.rosterPath(), 0o755); err != nil {
		t.Fatalf("mkdir roster: %v", err)
	}
	if err := reg.Deregister("feat"); err == nil {
		t.Fatal("Deregister succeeded despite a roster-read failure, want error")
	}
}

// corruptWorktreesDir replaces .git/worktrees with a regular file so
// Manager.List (and thus Reconcile/PruneScan) fails on read.
func corruptWorktreesDir(t *testing.T, f *fixture) {
	t.Helper()
	wtDir := dotgitWorktreesDir(f.repoPath)
	if err := os.RemoveAll(wtDir); err != nil {
		t.Fatalf("rm worktrees dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(wtDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write worktrees file: %v", err)
	}
}

// TestWriteSidecarMarshalError proves writeSidecar surfaces (wrapped) a YAML
// marshal failure and does not persist a partial record. The marshal step is
// unreachable for the concrete Metadata type, so the marshalYAML seam injects
// the failure — the assertion is on the real error contract, not a line touch.
func TestWriteSidecarMarshalError(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())
	addBranch(t, f, "feat")

	boom := errors.New("boom-marshal")
	orig := marshalYAML
	t.Cleanup(func() { marshalYAML = orig })
	marshalYAML = func(any) ([]byte, error) { return nil, boom }

	err := reg.writeSidecar("feat", Metadata{Purpose: "x"})
	if !errors.Is(err, boom) {
		t.Fatalf("writeSidecar err = %v, want it to wrap the marshal error", err)
	}
	// No partial sidecar was written: the record is still absent.
	marshalYAML = orig
	if _, gErr := reg.Get("feat"); !errors.Is(gErr, ErrMetadataNotRecorded) {
		t.Fatalf("Get after failed writeSidecar = %v, want ErrMetadataNotRecorded (nothing persisted)", gErr)
	}
}

// TestWriteRosterMarshalError proves writeRoster surfaces (wrapped) a YAML
// marshal failure. The roster type also marshals cleanly for every value, so
// the failure is injected through the marshalYAML seam and the assertion checks
// the wrapped error contract.
func TestWriteRosterMarshalError(t *testing.T) {
	f := newFixture(t)
	reg := newRegistry(t, f, time.Hour, time.Now())

	boom := errors.New("boom-roster-marshal")
	orig := marshalYAML
	t.Cleanup(func() { marshalYAML = orig })
	marshalYAML = func(any) ([]byte, error) { return nil, boom }

	err := reg.writeRoster(map[string]bool{"feat": true})
	if !errors.Is(err, boom) {
		t.Fatalf("writeRoster err = %v, want it to wrap the marshal error", err)
	}
}
