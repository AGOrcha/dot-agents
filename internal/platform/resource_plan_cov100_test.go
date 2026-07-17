package platform

// resource_plan_cov100_test.go drives the last uncovered error-propagation
// branches of resource_plan.go: the removeDirectSymlinkTarget unlink fault, the
// ExecuteSharedSkillMirrorPlan plan-build fault, and the H17 CAS-direct atomic
// managed-link swap defensive legs (MkdirAll fail, symlink-create fail,
// concurrent-create win, temp-stage fail, exchange fail incl. the
// unsupported-OS fail-closed leg, superseded-link unlink fail, reverse-exchange
// fail). Cross-platform legs use file-component / absent-target faults; the
// genuinely-never-fail legs use the minimal resource_plan.go seams. Helpers are
// prefixed rp100. See per-test comments for which lines are unix-leg-only.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// --- seam swap helpers ------------------------------------------------------

func swapBuildResourcePlan(fn func([]ResourceIntent) (ResourcePlan, error)) func() {
	prev := buildResourcePlan
	buildResourcePlan = fn
	return func() { buildResourcePlan = prev }
}

func swapSymlinkFn(fn func(string, string) error) func() {
	prev := symlinkFn
	symlinkFn = fn
	return func() { symlinkFn = prev }
}

func swapSwapRenameFn(fn func(string, string) error) func() {
	prev := swapRenameFn
	swapRenameFn = fn
	return func() { swapRenameFn = prev }
}

func swapSwapRemoveFn(fn func(string) error) func() {
	prev := swapRemoveFn
	swapRemoveFn = fn
	return func() { swapRemoveFn = prev }
}

// rp100SkipIfNoSwap skips when the OS has no atomic path-exchange primitive
// (Windows, remaining BSDs), where atomicSwapReplaceManagedLink fails closed
// before reaching the post-exchange legs under test.
func rp100SkipIfNoSwap(t *testing.T) {
	t.Helper()
	probe := t.TempDir()
	if errors.Is(atomicSwapRename(filepath.Join(probe, "a"), filepath.Join(probe, "b")), errSwapUnsupported) {
		t.Skip("atomic path-exchange unsupported on this OS; post-exchange legs fail closed by design")
	}
}

// rp100Symlink creates a symlink src->dst, skipping on a symlink-unprivileged OS.
func rp100Symlink(t *testing.T, oldname, newname string) {
	t.Helper()
	testutil.SymlinkOrSkip(t)
	if err := os.Symlink(oldname, newname); err != nil {
		t.Fatalf("symlink %s -> %s: %v", newname, oldname, err)
	}
}

// --- removeDirectSymlinkTarget unlink fault (resource_plan.go:971) ----------

func TestRP100RemoveDirectSymlinkTarget_UnlinkErrorSurfaces(t *testing.T) {
	sentinel := errors.New("injected remove-symlink failure")
	defer swapRemoveIfSymlinkUnder(func(string, string) error { return sentinel })()

	// DirectDir shape avoids the DirectFile hard-link leg; the unlink error is a
	// non-NotExist failure that must be aggregated + surfaced.
	intent := ResourceIntent{Shape: ResourceShapeDirectDir, Transport: ResourceTransportSymlink}
	err := removeDirectSymlinkTarget(intent, filepath.Join(t.TempDir(), "target"), t.TempDir())
	if !errors.Is(err, sentinel) {
		t.Fatalf("removeDirectSymlinkTarget err = %v, want %v", err, sentinel)
	}
}

// --- ExecuteSharedSkillMirrorPlan plan-build fault (resource_plan.go:1063) --

func TestRP100ExecuteSharedSkillMirrorPlan_BuildPlanErrorSurfaces(t *testing.T) {
	t.Setenv("AGENTS_HOME", filepath.Join(t.TempDir(), ".agents"))
	sentinel := errors.New("injected build-resource-plan failure")
	defer swapBuildResourcePlan(func([]ResourceIntent) (ResourcePlan, error) {
		return ResourcePlan{}, sentinel
	})()

	// No skills bucket → BuildSharedSkillMirrorIntents returns empty intents, nil;
	// the seamed buildResourcePlan then fails so the error leg runs.
	repo := filepath.Join(t.TempDir(), "repo")
	err := ExecuteSharedSkillMirrorPlan("proj", repo, filepath.Join(".claude", "skills"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("ExecuteSharedSkillMirrorPlan err = %v, want %v", err, sentinel)
	}
}

// --- atomicManagedSymlinkSwap: MkdirAll fail (resource_plan.go:1622) --------

// A regular file stands in for a parent path component, so MkdirAll of the
// target's parent fails with ENOTDIR on every OS.
func TestRP100AtomicManagedSymlinkSwap_MkdirAllErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	fileComponent := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(fileComponent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(fileComponent, "sub", "link")
	if err := atomicManagedSymlinkSwap(filepath.Join(tmp, "src"), target); err == nil {
		t.Fatal("expected MkdirAll of a parent under a file component to fail")
	}
}

// --- atomicManagedSymlinkSwap: symlink-create fail non-EEXIST (:1629) -------

func TestRP100AtomicManagedSymlinkSwap_CreateErrorSurfaces(t *testing.T) {
	sentinel := errors.New("injected symlink-create failure")
	defer swapSymlinkFn(func(string, string) error { return sentinel })()

	tmp := t.TempDir()
	err := atomicManagedSymlinkSwap(filepath.Join(tmp, "src"), filepath.Join(tmp, "target"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("atomicManagedSymlinkSwap err = %v, want %v", err, sentinel)
	}
}

// --- atomicManagedSymlinkSwap: concurrent-create win (:1640) ----------------

// TestRP100AtomicManagedSymlinkSwap_ConcurrentCreateWins simulates a racer that
// lands target->src between our absent-check and our create: the create reports
// EEXIST, the re-read finds exactly src, and the swap returns nil idempotently.
func TestRP100AtomicManagedSymlinkSwap_ConcurrentCreateWins(t *testing.T) {
	testutil.SymlinkOrSkip(t)
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	target := filepath.Join(tmp, "target")
	defer swapSymlinkFn(func(oldname, newname string) error {
		// Stand in for the racing projector, then report the create as occupied.
		_ = os.Symlink(oldname, newname)
		return os.ErrExist
	})()

	if err := atomicManagedSymlinkSwap(src, target); err != nil {
		t.Fatalf("expected idempotent nil when a racer set target->src, got %v", err)
	}
	if dest, _ := os.Readlink(target); dest != src {
		t.Fatalf("target resolves to %q, want %q", dest, src)
	}
}

// --- atomicSwapReplaceManagedLink: temp-stage fail (:1670) ------------------

// The target's parent is a regular file, so staging the temp symlink beside it
// fails ENOTDIR on every OS.
func TestRP100AtomicSwapReplaceManagedLink_StageSymlinkErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	fileComponent := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(fileComponent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(fileComponent, "link")
	if err := atomicSwapReplaceManagedLink(filepath.Join(tmp, "src"), target, tmp); err == nil {
		t.Fatal("expected staging the temp symlink under a file component to fail")
	}
}

// --- atomicSwapReplaceManagedLink: exchange fail, supported OS (:1673/:1678) -

// With an absent target the real path-exchange fails with a non-unsupported
// error (ENOENT on Linux/macOS) — covering the temp cleanup + the generic
// exchange-error return. On a no-exchange OS the same call yields
// errSwapUnsupported and covers the sibling fail-closed leg instead.
func TestRP100AtomicSwapReplaceManagedLink_ExchangeErrorSurfaces(t *testing.T) {
	testutil.SymlinkOrSkip(t)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target") // absent → exchange has nothing to swap with
	if err := atomicSwapReplaceManagedLink(filepath.Join(tmp, "src"), target, tmp); err == nil {
		t.Fatal("expected the path-exchange against an absent target to fail")
	}
	if _, statErr := os.Lstat(target + ".casswap-"); statErr != nil && !os.IsNotExist(statErr) {
		t.Fatalf("unexpected stat of staged temp: %v", statErr)
	}
}

// --- atomicSwapReplaceManagedLink: unsupported-OS fail-closed (:1675-:1677) --

// TestRP100AtomicSwapReplaceManagedLink_UnsupportedFailsClosed exercises the
// errSwapUnsupported fail-closed leg deterministically on every OS by seaming
// the exchange to report unsupported — the exact behavior the real Windows /
// no-exchange build returns. This covers the leg that the Windows-skipping
// repoint test leaves uncovered.
func TestRP100AtomicSwapReplaceManagedLink_UnsupportedFailsClosed(t *testing.T) {
	testutil.SymlinkOrSkip(t)
	defer swapSwapRenameFn(func(string, string) error { return errSwapUnsupported })()

	tmp := t.TempDir()
	err := atomicSwapReplaceManagedLink(filepath.Join(tmp, "src"), filepath.Join(tmp, "target"), tmp)
	if !errors.Is(err, errSwapUnsupported) {
		t.Fatalf("expected a fail-closed errSwapUnsupported error, got %v", err)
	}
}

// --- atomicSwapReplaceManagedLink: superseded-link unlink fail (:1683) ------

// A successful repoint whose former (managed) occupant cannot be unlinked must
// surface the error. The unlink cannot fail on a healthy fs, so swapRemoveFn
// injects it. Requires a real path-exchange (unix legs).
func TestRP100AtomicSwapReplaceManagedLink_SupersededRemoveErrorSurfaces(t *testing.T) {
	rp100SkipIfNoSwap(t)
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	oldCanonical := filepath.Join(root, "old")
	newCanonical := filepath.Join(root, "new")
	if err := os.MkdirAll(oldCanonical, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newCanonical, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, "target")
	rp100Symlink(t, oldCanonical, target) // managed link (target under root)

	sentinel := errors.New("injected superseded-remove failure")
	defer swapSwapRemoveFn(func(string) error { return sentinel })()

	if err := atomicSwapReplaceManagedLink(newCanonical, target, root); !errors.Is(err, sentinel) {
		t.Fatalf("atomicSwapReplaceManagedLink err = %v, want %v", err, sentinel)
	}
}

// --- atomicSwapReplaceManagedLink: reverse-exchange fail (:1691) ------------

// When the post-exchange occupant is NOT our managed link (a racer's user file),
// the exchange is reversed to restore it; if that reverse itself fails the call
// surfaces the critical error. The reverse cannot fail on a healthy fs, so the
// exchange seam succeeds the first (real) swap and fails the reverse. Requires a
// real path-exchange (unix legs).
func TestRP100AtomicSwapReplaceManagedLink_ReverseExchangeErrorSurfaces(t *testing.T) {
	rp100SkipIfNoSwap(t)
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	newCanonical := filepath.Join(root, "new")
	if err := os.MkdirAll(newCanonical, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, "target")
	rp100Symlink(t, outside, target) // NON-managed occupant (resolves outside root)

	sentinel := errors.New("injected reverse-exchange failure")
	calls := 0
	defer swapSwapRenameFn(func(a, b string) error {
		calls++
		if calls == 1 {
			return atomicSwapRename(a, b) // real forward exchange
		}
		return sentinel // reverse exchange fails
	})()

	if err := atomicSwapReplaceManagedLink(newCanonical, target, root); !errors.Is(err, sentinel) {
		t.Fatalf("atomicSwapReplaceManagedLink err = %v, want %v", err, sentinel)
	}
}
