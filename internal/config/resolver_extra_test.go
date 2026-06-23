package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestScalarKeyUnmarshalable exercises the json.Marshal error fallback: a channel
// cannot be marshaled, so scalarKey falls back to the %v form.
func TestScalarKeyUnmarshalable(t *testing.T) {
	got := scalarKey(make(chan int))
	if got == "" {
		t.Fatal("expected a non-empty fallback key")
	}
}

// TestDecodeObjectFileReadError points decodeObjectFile at a directory so
// os.ReadFile returns a non-NotExist error (distinct from the absent case).
func TestDecodeObjectFileReadError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "adir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeObjectFile(dir); err == nil {
		t.Fatal("expected read error for directory path")
	}
}

// TestDecodeEffectiveUnmarshalError feeds a merged object whose typed field has the
// wrong JSON type (version is an int), tripping the AgentsRC unmarshal error branch.
func TestDecodeEffectiveUnmarshalError(t *testing.T) {
	if _, err := decodeEffective(map[string]any{"version": "not-a-number"}); err == nil {
		t.Fatal("expected decode error for mistyped version field")
	}
}

// TestWriteConfigLockOpenError makes the lockfile path a directory so agentslock.Open
// fails before any section is written.
func TestWriteConfigLockOpenError(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(AgentsLockPath(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfigLock(proj, map[string]LockedLayer{}); err == nil {
		t.Fatal("expected open error when lock path is a directory")
	}
}

// TestReadLockedLayersOpenError mirrors the above for the read path.
func TestReadLockedLayersOpenError(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(AgentsLockPath(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readLockedLayersFromUnits(proj); err == nil {
		t.Fatal("expected open error when lock path is a directory")
	}
}

// TestResolveConcurrency checks the worker-pool bound: never below 1, never
// more workers than layers, and clamped to the oversubscribed CPU ceiling.
func TestResolveConcurrency(t *testing.T) {
	if got := resolveConcurrency(0); got != 1 {
		t.Errorf("resolveConcurrency(0) = %d, want 1", got)
	}
	if got := resolveConcurrency(2); got != 2 {
		t.Errorf("resolveConcurrency(2) = %d, want 2 (fewer layers than the cap)", got)
	}
	ceiling := runtime.GOMAXPROCS(0) * 4
	if got := resolveConcurrency(ceiling + 100); got != ceiling {
		t.Errorf("resolveConcurrency(%d) = %d, want clamp to %d", ceiling+100, got, ceiling)
	}
}

// TestLayeredResolverConcurrentExtendsOptionalSkip resolves three extends layers
// concurrently (an optional middle layer is missing). It asserts the precedence
// order of the imported stack, the set-union order, and the optional-skip
// warning are all deterministic regardless of fetch-completion order. Run under
// -race in CI, it also guards the parallel resolveExtends path against data
// races.
func TestLayeredResolverConcurrentExtendsOptionalSkip(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/app",
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": [
			"acme:org/base.json",
			{"ref": "acme:does/not-exist.json", "optional": true},
			"acme:team/frontend.json"
		],
		"skills": ["repo-skill"]
	}`)

	snap, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// The optional middle layer is skipped; surviving imports keep precedence order.
	ids := layerIDs(snap.Layers)
	want := []string{LayerProductDefaults, "acme:org/base.json", "acme:team/frontend.json", LayerRepoLocal}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("layer ids = %v, want %v", ids, want)
	}
	// set-union order stays deterministic across the concurrent fetch.
	wantSkills := []string{"org-base-skill", "frontend-skill", "repo-skill"}
	if !reflect.DeepEqual(snap.Effective.Skills, wantSkills) {
		t.Errorf("skills = %v, want %v", snap.Effective.Skills, wantSkills)
	}
	// A skip warning is recorded for the optional miss.
	var sawSkip bool
	for _, w := range snap.Warnings {
		if w.FieldPath == "acme:does/not-exist.json" && strings.HasPrefix(w.Outcome, "optional_skipped") {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Errorf("expected optional-skip warning, warnings = %+v", snap.Warnings)
	}
	// The lockfile records only the two resolved layers.
	assertLockfileSections(t, repo, []string{"acme:org/base.json", "acme:team/frontend.json"})
}
