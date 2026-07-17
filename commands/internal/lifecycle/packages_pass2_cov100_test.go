package lifecycle

import (
	"fmt"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// packages_pass2_cov100_test.go drives the three defensive branches in
// packages_pass2.go that a single-threaded call cannot reach through the normal
// path: the verifyProjectionInputs failure in resolvePackagesUnits (177) and
// hydratePackagesFromLock (214), and the units-section SetSection failure in
// commitArtifactLock (315). Each is covered by overriding a minimal, unexported
// production seam (lc100 helper prefix).

// lc100OverrideVerify swaps verifyProjectionInputsFn for one that always fails
// and restores it on cleanup.
func lc100OverrideVerify(t *testing.T) {
	t.Helper()
	restore := verifyProjectionInputsFn
	t.Cleanup(func() { verifyProjectionInputsFn = restore })
	verifyProjectionInputsFn = func(string, []platform.ResolvedUnit, []string) error {
		return fmt.Errorf("lc100: injected projection-boundary failure")
	}
}

// TestLc100ResolvePackagesUnits_VerifyFails covers pass2 177: an empty
// packages[] set commits an (empty) artifact lock cleanly, then the injected
// projection-boundary re-check fails the pass closed.
func TestLc100ResolvePackagesUnits_VerifyFails(t *testing.T) {
	agentsHome, proj := lcCovProj(t)
	lc100OverrideVerify(t)

	snap := snapWithPackages(nil)
	if _, err := resolvePackagesUnits(proj, agentsHome, snap, []string{"proj"}); err == nil {
		t.Fatal("expected the injected projection-boundary re-check to fail resolvePackagesUnits")
	}
}

// TestLc100HydratePackagesFromLock_VerifyFails covers pass2 214: an empty
// packages[] set reads the lock cleanly, then the injected projection-boundary
// re-check fails the no-write hydrate path closed.
func TestLc100HydratePackagesFromLock_VerifyFails(t *testing.T) {
	agentsHome, proj := lcCovProj(t)
	lc100OverrideVerify(t)

	snap := snapWithPackages(nil)
	if _, err := hydratePackagesFromLock(proj, agentsHome, snap, []string{"proj"}); err == nil {
		t.Fatal("expected the injected projection-boundary re-check to fail hydratePackagesFromLock")
	}
}

// TestLc100CommitArtifactLock_SetUnitsFails covers pass2 315: the units-section
// write never errors through the normal path, so the seam is overridden to
// return an error and assert commitArtifactLock propagates it.
func TestLc100CommitArtifactLock_SetUnitsFails(t *testing.T) {
	_, proj := lcCovProj(t)

	restore := setUnitsSectionFn
	t.Cleanup(func() { setUnitsSectionFn = restore })
	setUnitsSectionFn = func(*agentslock.Lockfile, map[string]config.LockedUnit) error {
		return fmt.Errorf("lc100: injected units-section write failure")
	}
	if err := commitArtifactLock(proj, map[string]config.LockedUnit{}, map[string]string{}); err == nil {
		t.Fatal("expected the injected units-section write failure to propagate from commitArtifactLock")
	}
}
