package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// ensure_resolved_bench_test.go — repeatable baseline for the §7A.5 auto-sync
// seam (package-artifact-install t9): EnsureResolved's fresh-path dispatch,
// the single call every config-consuming command (`da install`, `da refresh`,
// `da config sync|explain|verify|lint`, `da status`) makes before doing any
// real work. Regression guard:
// `go test ./internal/config/... -bench BenchmarkEnsureResolved -benchmem`.
//
// This isolates the EnsureResolved dispatch + Staleness() computation itself
// (LoadAgentsRC + ReadUnits + ComputeInputsDigest) from the full resolve —
// BenchmarkLayeredResolverResolveLocked_25Layers (resolver_bench_test.go)
// already covers the heavier "stale, must re-resolve" and "fresh, resolve
// from lock" paths end to end; this benchmark's job is the steady-state
// no-op-when-fresh decision cost on its own.

// buildEnsureResolvedFixture writes N extends layers WITHOUT any
// verifier_profiles/stage_profiles content, so ProfileUnitsForSnapshot emits
// no kind:profile lock units and declaredSetChanged's declared-vs-locked ref
// count matches exactly on an unmodified repo (a genuinely Fresh() steady
// state, not a profile-unit artifact of the fixture).
func buildEnsureResolvedFixture(b *testing.B, n int) string {
	b.Helper()
	root := b.TempDir()
	layersDir := filepath.Join(root, "layers")
	if err := os.MkdirAll(layersDir, 0o755); err != nil {
		b.Fatal(err)
	}
	extends := make([]string, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("layer%03d.json", i)
		body := map[string]any{
			"skills": []string{fmt.Sprintf("skill-%03d-a", i), fmt.Sprintf("skill-%03d-b", i)},
			"rules":  []string{fmt.Sprintf("rule-%03d", i)},
		}
		data, err := json.Marshal(body)
		if err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(layersDir, name), data, 0o644); err != nil {
			b.Fatal(err)
		}
		extends = append(extends, "bench-src:"+name)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		b.Fatal(err)
	}
	extendsJSON, _ := json.Marshal(extends)
	manifest := fmt.Sprintf(`{
		"version": 2,
		"repo_id": "github.com/bench/repo",
		"sources": [{"id": "bench-src", "type": "local", "path": %s, "cache_ttl": "4h"}],
		"extends": %s
	}`, jsonPathBench(layersDir), string(extendsJSON))
	if err := os.WriteFile(filepath.Join(repo, AgentsRCFile), []byte(manifest), 0o644); err != nil {
		b.Fatal(err)
	}
	return repo
}

func BenchmarkEnsureResolved_Fresh_25Layers(b *testing.B) {
	agentsHome := b.TempDir()
	b.Setenv("AGENTS_HOME", agentsHome)
	repo := buildEnsureResolvedFixture(b, 25)
	r := NewLayeredResolver()
	if _, err := r.Resolve(repo); err != nil {
		b.Fatalf("seed resolve: %v", err)
	}

	opts := EnsureOpts{Resolver: r}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := EnsureResolved(repo, opts)
		if err != nil {
			b.Fatalf("EnsureResolved: %v", err)
		}
		if !res.Fresh {
			b.Fatalf("expected fresh (no-op) result on an unchanged repo, got stale reasons=%v", res.Reasons)
		}
	}
}

// BenchmarkEnsureResolved_Frozen isolates the --frozen fast path (skips the
// staleness check entirely, resolves read-only from the lock/cache) — the
// CI/reproducible-build mode every locked pipeline run takes.
func BenchmarkEnsureResolved_Frozen_25Layers(b *testing.B) {
	agentsHome := b.TempDir()
	b.Setenv("AGENTS_HOME", agentsHome)
	repo := buildEnsureResolvedFixture(b, 25)
	r := NewLayeredResolver()
	if _, err := r.Resolve(repo); err != nil {
		b.Fatalf("seed resolve: %v", err)
	}

	opts := EnsureOpts{Resolver: r, Frozen: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EnsureResolved(repo, opts); err != nil {
			b.Fatalf("EnsureResolved: %v", err)
		}
	}
}
