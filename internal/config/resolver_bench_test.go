package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// resolver_bench_test.go — repeatable baselines for the two-pass resolution
// pass-1 (LayeredResolver.Resolve / resolveExtends) required by
// package-artifact-install t9 (perf testing + optimization). Regression guard:
// `go test ./internal/config/... -bench=BenchmarkLayeredResolver -benchmem`.
//
// Fixture shape mirrors TestLayeredResolverLocalTwoLayerEndToEnd (local
// source, no network) but scaled to N extends layers, each carrying a modest
// skills/rules/features payload, to approximate a REALISTIC multi-team
// layering chain (config-distribution-model org -> team -> repo).

// buildResolverLayerFixture writes N standalone layer JSON files under
// <root>/layers/layerNNN.json (each contributing a couple of set-union and
// map-merge fields, so mergeField/unionSlices/mergeMaps all do real work) plus
// a repo manifest whose "extends" chains through all of them via a single
// local source. Returns the repo dir.
func buildResolverLayerFixture(b testing.TB, n int) string {
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
			"features": map[string]any{
				fmt.Sprintf("feature-%03d", i): "on",
				"shared-toggle":                "on",
			},
			"verifier_profiles": map[string]any{
				"unit": map[string]any{"label": "Unit", "kind": fmt.Sprintf("go-%03d", i)},
			},
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
	extendsJSON, err := json.Marshal(extends)
	if err != nil {
		b.Fatal(err)
	}
	manifest := fmt.Sprintf(`{
		"version": 2,
		"repo_id": "github.com/bench/repo",
		"sources": [{"id": "bench-src", "type": "local", "path": %s, "cache_ttl": "4h"}],
		"extends": %s,
		"skills": ["repo-skill"]
	}`, jsonPathBench(layersDir), string(extendsJSON))
	if err := os.WriteFile(filepath.Join(repo, AgentsRCFile), []byte(manifest), 0o644); err != nil {
		b.Fatal(err)
	}
	return repo
}

// jsonPathBench mirrors resolver_test.go's jsonPath (escapes a filesystem
// path for safe embedding in a JSON string literal) without depending on the
// *testing.T-typed test helper, so it is usable from *testing.B fixtures.
func jsonPathBench(p string) string {
	b, _ := json.Marshal(p)
	return string(b)
}

// runLayeredResolverBench resolves a fresh repo (cold — no lock yet) once,
// then times b.N further resolves against a freshly-reset AGENTS_HOME per
// benchmark run (warm CAS-equivalent: the layer cache dir under AGENTS_HOME
// persists across b.N iterations, only the project's own .agentsrc.lock is
// rewritten each time — matching a real repeated `da config sync`).
func runLayeredResolverBench(b *testing.B, n int) {
	b.Helper()
	agentsHome := b.TempDir()
	b.Setenv("AGENTS_HOME", agentsHome)
	repo := buildResolverLayerFixture(b, n)

	// Warm the on-disk layer cache once outside the timed loop — a real
	// repeated `da config sync` on an unchanged manifest hits the same cache
	// entries every time (CacheKey match), so the steady-state cost is what
	// the loop measures, not the first cold fetch.
	if _, err := NewLayeredResolver().Resolve(repo); err != nil {
		b.Fatalf("warm resolve: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewLayeredResolver().Resolve(repo); err != nil {
			b.Fatalf("Resolve: %v", err)
		}
	}
}

func BenchmarkLayeredResolverResolve_5Layers(b *testing.B)   { runLayeredResolverBench(b, 5) }
func BenchmarkLayeredResolverResolve_25Layers(b *testing.B)  { runLayeredResolverBench(b, 25) }
func BenchmarkLayeredResolverResolve_100Layers(b *testing.B) { runLayeredResolverBench(b, 100) }

// BenchmarkLayeredResolverResolveLocked isolates the read-only fast path
// (ResolveLocked — no fetch, no lock rewrite) EnsureResolved takes on every
// fresh command invocation, the single hottest call in the whole `da` chain
// since it runs on every `da status` / `da config explain` / pre-flight check.
func BenchmarkLayeredResolverResolveLocked_25Layers(b *testing.B) {
	agentsHome := b.TempDir()
	b.Setenv("AGENTS_HOME", agentsHome)
	repo := buildResolverLayerFixture(b, 25)
	r := NewLayeredResolver()
	if _, err := r.Resolve(repo); err != nil {
		b.Fatalf("seed resolve: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.ResolveLocked(repo); err != nil {
			b.Fatalf("ResolveLocked: %v", err)
		}
	}
}

// BenchmarkLayeredResolverResolveLocked_NProjects is the "N projects"
// methodology fixture: N separate small repos (5 extends layers each)
// sharing ONE AGENTS_HOME layer cache, each resolved read-only once per
// iteration — approximating `da status` / a multi-repo workstation refresh
// sweep, where the shared cache is warm but each project's own
// .agentsrc.lock read is distinct per project.
func BenchmarkLayeredResolverResolveLocked_20Projects(b *testing.B) {
	const nProjects = 20
	agentsHome := b.TempDir()
	b.Setenv("AGENTS_HOME", agentsHome)
	r := NewLayeredResolver()
	repos := make([]string, nProjects)
	for i := 0; i < nProjects; i++ {
		repos[i] = buildResolverLayerFixture(b, 5)
		if _, err := r.Resolve(repos[i]); err != nil {
			b.Fatalf("seed resolve project %d: %v", i, err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, repo := range repos {
			if _, err := r.ResolveLocked(repo); err != nil {
				b.Fatalf("ResolveLocked: %v", err)
			}
		}
	}
}
