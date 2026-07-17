package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// install_bench_test.go — repeatable baselines for the `da install` pass-2
// packages hydrate (package-artifact-install t9): HydratePackagesUnits'
// COLD path (resolvePackagesUnits — fetch + materialize + commit lock for
// every declared package, the path a `da install`/`da config sync` on a
// changed manifest takes) and WARM path (hydratePackagesFromLock — re-fetch
// pinned to the already-locked digest, the path every `da install` on an
// UNCHANGED manifest takes, since HydratePackagesUnits dispatches on
// ensureRes.ReResolved regardless of whether packages themselves changed).
// Regression guard:
// `go test ./commands/internal/lifecycle/... -bench BenchmarkHydratePackagesUnits -benchmem`.

// buildPackagesSourceFixture lays out n local-source skill packages under
// <root>/skill/pkg-NNNN/SKILL.md (the fetcher's family/name subtree
// convention, mirroring newPackagesSourceTree in packages_pass2_test.go) and
// returns (sourceRoot, packageRefs).
func buildPackagesSourceFixture(b *testing.B, root string, n int) (string, []config.PackageRef) {
	b.Helper()
	refs := make([]config.PackageRef, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("pkg-%04d", i)
		dir := filepath.Join(root, "skill", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		body := fmt.Sprintf("# %s\nBench fixture package body.\n", name)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
		refs = append(refs, config.PackageRef{Ref: fmt.Sprintf("bench-src:skill/%s@1", name)})
	}
	return root, refs
}

// runHydratePackagesUnitsBench builds n packages, hydrates them once (cold —
// resolvePackagesUnits) outside the timer, then times b.N further calls
// through either the cold (ReResolved=true, re-fetch+re-materialize+re-lock
// every ref every call) or warm (ReResolved=false, hydratePackagesFromLock —
// pinned re-fetch, no lock write) dispatch path.
func runHydratePackagesUnitsBench(b *testing.B, n int, reResolved bool) {
	b.Helper()
	tmp := b.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	b.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		b.Fatal(err)
	}
	srcRoot, refs := buildPackagesSourceFixture(b, filepath.Join(tmp, "src"), n)
	sources := []config.Source{{Type: "local", ID: "bench-src", Path: srcRoot}}
	snap := &config.Snapshot{Effective: config.AgentsRC{Sources: sources, Packages: refs}}

	proj := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		b.Fatal(err)
	}

	// Cold seed outside the timer: materializes + locks every ref once, so the
	// warm (ReResolved=false) loop has a lock to hydrate from.
	coldRes := &config.EnsureResult{Snapshot: snap, ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "bench-proj", coldRes); err != nil {
		b.Fatalf("seed HydratePackagesUnits: %v", err)
	}

	res := &config.EnsureResult{Snapshot: snap, ReResolved: reResolved}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		units, participated, err := HydratePackagesUnits(proj, "bench-proj", res)
		if err != nil {
			b.Fatalf("HydratePackagesUnits: %v", err)
		}
		if !participated || len(units) != n {
			b.Fatalf("expected %d participating units, got %d (participated=%v)", n, len(units), participated)
		}
	}
}

func BenchmarkHydratePackagesUnits_Cold_10Packages(b *testing.B) {
	runHydratePackagesUnitsBench(b, 10, true)
}
func BenchmarkHydratePackagesUnits_Cold_50Packages(b *testing.B) {
	runHydratePackagesUnitsBench(b, 50, true)
}
func BenchmarkHydratePackagesUnits_Warm_10Packages(b *testing.B) {
	runHydratePackagesUnitsBench(b, 10, false)
}
func BenchmarkHydratePackagesUnits_Warm_50Packages(b *testing.B) {
	runHydratePackagesUnitsBench(b, 50, false)
}
