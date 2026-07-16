package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// resource_plan_bench_test.go — repeatable baselines for the shared-target
// projection (package-artifact-install t9): RunSharedTargetProjectionExact /
// ProjectResolvedUnits + the per-platform builders, over a REALISTIC N-package
// resolved-unit set projected across every platform builder (platform.All()),
// covering both DIR-MIRROR shapes (Claude/Cursor skills+agents dirs) and
// FILE-shaped agent surfaces (Codex rendered .toml, OpenCode/Copilot agent
// symlinks). Regression guard: `go test ./internal/platform/... -bench
// BenchmarkProjectResolvedUnits -benchmem`.

// canSymlinkBench probes symlink support once per benchmark (testutil.
// SymlinkOrSkip is *testing.T-typed; this is the *testing.B-safe equivalent).
func canSymlinkBench(b *testing.B) bool {
	b.Helper()
	dir := b.TempDir()
	link := filepath.Join(dir, "probe-link")
	if err := os.Symlink("probe-target", link); err != nil {
		return false
	}
	return true
}

// buildResolvedUnitsFixture materializes n "skills" family units (each a
// tiny SKILL.md bundle, mirroring materializeUnit in materialize_test.go)
// into the CAS store under home, and n "agents" family units (AGENT.md
// bundles) so both the dir-mirror (skills) and file-shaped (agents) intent
// builders do real work. Returns the combined unit set.
func buildResolvedUnitsFixture(b *testing.B, home string, n int) []ResolvedUnit {
	b.Helper()
	units := make([]ResolvedUnit, 0, n*2)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("skill-%04d", i)
		body := fmt.Sprintf("---\nname: %s\n---\n# %s\nBench fixture skill body for %s.\n", name, name, name)
		casPath, digest, err := MaterializeArtifact(home, "skills", "bench-src", name, testMaterializeBundle(map[string]string{"SKILL.md": body}))
		if err != nil {
			b.Fatalf("MaterializeArtifact(skills/%s): %v", name, err)
		}
		units = append(units, ResolvedUnit{Family: "skills", Name: name, SourceID: "bench-src", Digest: digest, CASPath: casPath})
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("agent-%04d", i)
		body := "---\nname: " + name + "\ndescription: bench fixture agent\n---\nBench fixture agent body.\n"
		casPath, digest, err := MaterializeArtifact(home, "agents", "bench-src", name, testMaterializeBundle(map[string]string{agentManifestName: body}))
		if err != nil {
			b.Fatalf("MaterializeArtifact(agents/%s): %v", name, err)
		}
		units = append(units, ResolvedUnit{Family: "agents", Name: name, SourceID: "bench-src", Digest: digest, CASPath: casPath})
	}
	return units
}

// runProjectResolvedUnitsBench times b.N repeated ProjectResolvedUnits calls
// against the SAME already-projected (warm) repo — the steady-state re-run
// shape every `da install` / `da refresh` on an unchanged package set takes
// (exact/prune re-verifies and no-ops rather than re-linking).
func runProjectResolvedUnitsBench(b *testing.B, n int) {
	b.Helper()
	if !canSymlinkBench(b) {
		b.Skip("platform cannot create symlinks")
	}
	home := b.TempDir()
	b.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(b.TempDir(), "repo")
	units := buildResolvedUnitsFixture(b, home, n)
	platforms := All()

	// Cold projection outside the timed loop: creates every symlink/render.
	if _, err := ProjectResolvedUnits("bench-proj", repo, units, platforms, false, true, "bench-proj"); err != nil {
		b.Fatalf("cold ProjectResolvedUnits: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ProjectResolvedUnits("bench-proj", repo, units, platforms, false, true, "bench-proj"); err != nil {
			b.Fatalf("ProjectResolvedUnits: %v", err)
		}
	}
}

func BenchmarkProjectResolvedUnits_Warm_10Packages(b *testing.B) { runProjectResolvedUnitsBench(b, 10) }
func BenchmarkProjectResolvedUnits_Warm_50Packages(b *testing.B) { runProjectResolvedUnitsBench(b, 50) }
func BenchmarkProjectResolvedUnits_Warm_200Packages(b *testing.B) {
	runProjectResolvedUnitsBench(b, 200)
}

// BenchmarkProjectResolvedUnits_Cold isolates the FIRST (cold-cache) run —
// every symlink/render is newly created — so cold vs warm cost is directly
// comparable in the same report (methodology: cold vs warm CAS cache).
func BenchmarkProjectResolvedUnits_Cold_50Packages(b *testing.B) {
	if !canSymlinkBench(b) {
		b.Skip("platform cannot create symlinks")
	}
	platforms := All()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		home := b.TempDir()
		b.Setenv("AGENTS_HOME", home)
		repo := filepath.Join(b.TempDir(), fmt.Sprintf("repo-%d", i))
		units := buildResolvedUnitsFixture(b, home, 50)
		b.StartTimer()
		if _, err := ProjectResolvedUnits("bench-proj", repo, units, platforms, false, true, "bench-proj"); err != nil {
			b.Fatalf("ProjectResolvedUnits: %v", err)
		}
	}
}
