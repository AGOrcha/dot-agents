package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFileContent drops body at path (creating parent dirs), for seeding the
// local config scopes a staleness check hashes.
func writeFileContent(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stalenessSeed sets up a project with a repo-local manifest and a separate
// user-local manifest path, returning both the project dir and user-local path
// so a test can mutate either scope and recompute.
func stalenessSeed(t *testing.T, repoManifest, userManifest string) (repo, userPath string) {
	t.Helper()
	repo = t.TempDir()
	writeManifest(t, repo, repoManifest)
	userPath = filepath.Join(t.TempDir(), AgentsRCFile)
	if userManifest != "" {
		writeFileContent(t, userPath, userManifest)
	}
	return repo, userPath
}

func TestComputeInputsDigest_StableAndPrefixed(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"version":2,"project":"x"}`, `{"version":2}`)

	d1, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatalf("ComputeInputsDigest: %v", err)
	}
	if !strings.HasPrefix(d1, inputsDigestPrefix) {
		t.Errorf("expected %s prefix, got %q", inputsDigestPrefix, d1)
	}
	d2, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("digest not stable across calls: %q vs %q", d1, d2)
	}
}

func TestComputeInputsDigest_InsensitiveToWhitespaceAndKeyOrder(t *testing.T) {
	repoA, userA := stalenessSeed(t, `{"version":2,"project":"x"}`, "")
	repoB, userB := stalenessSeed(t, "{\n  \"project\":   \"x\",\n  \"version\": 2\n}\n", "")

	da, err := ComputeInputsDigest(repoA, userA)
	if err != nil {
		t.Fatal(err)
	}
	db, err := ComputeInputsDigest(repoB, userB)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Errorf("expected whitespace/key-order-insensitive digest, got %q vs %q", da, db)
	}
}

func TestComputeInputsDigest_ChangesWhenAnyScopeChanges(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"version":2}`, `{"version":2}`)
	base, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the project-local overlay scope only.
	writeFileContent(t, filepath.Join(repo, AgentsRCLocalFile), `{"project":"overlay"}`)
	withOverlay, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	if base == withOverlay {
		t.Error("expected digest to change when project-local overlay appears")
	}
}

func TestComputeInputsDigest_MissingScopesAreEmptyNotError(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2}`)
	// No user-local file, no overlay.
	missingUser := filepath.Join(t.TempDir(), AgentsRCFile)

	if _, err := ComputeInputsDigest(repo, missingUser); err != nil {
		t.Fatalf("missing scopes should not error: %v", err)
	}
}

func TestComputeInputsDigest_MalformedManifestErrors(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{not json`)
	if _, err := ComputeInputsDigest(repo, ""); err == nil {
		t.Fatal("expected error for malformed manifest")
	}
}

func TestStaleness_FreshWhenLockMatches(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	digest, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	seedUnits(t, repo, digest, map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:d1"},
	})

	res, err := Staleness(repo, userPath, nil)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.Fresh || res.IsStale() {
		t.Fatalf("expected fresh lock, got %+v", res)
	}
	if len(res.Reasons) != 0 {
		t.Errorf("expected no reasons, got %+v", res.Reasons)
	}
}

func TestStaleness_InputsDigestMismatch(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	seedUnits(t, repo, "sha256:stale-digest", map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:d1"},
	})

	res, err := Staleness(repo, userPath, nil)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.IsStale() || !hasReason(res, ReasonInputsDigest) {
		t.Fatalf("expected inputs-digest-mismatch, got %+v", res)
	}
	if res.ExpectedInputsDigest == "" || res.ExpectedInputsDigest == "sha256:stale-digest" {
		t.Errorf("expected a freshly-computed digest, got %q", res.ExpectedInputsDigest)
	}
}

func TestStaleness_DeclaredSetChanged(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json","acme:org/new.json"]}`, "")
	digest, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	// Lock only has one of the two declared layers.
	seedUnits(t, repo, digest, map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:d1"},
	})

	res, err := Staleness(repo, userPath, nil)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.IsStale() || !hasReason(res, ReasonDeclaredSet) {
		t.Fatalf("expected declared-set-changed, got %+v", res)
	}
}

func TestStaleness_UnitDigestMismatch(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	digest, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	seedUnits(t, repo, digest, map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:recorded"},
	})

	// recompute reports a different current digest for the locked unit.
	recompute := func(ref string) (string, bool) {
		if ref == "acme:org/base.json@a1" {
			return "sha256:drifted", true
		}
		return "", false
	}
	res, err := Staleness(repo, userPath, recompute)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.IsStale() || !hasReason(res, ReasonUnitDigest) {
		t.Fatalf("expected unit-digest-mismatch, got %+v", res)
	}
}

func TestStaleness_UnitDigestSkippedWhenUnavailable(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	digest, err := ComputeInputsDigest(repo, userPath)
	if err != nil {
		t.Fatal(err)
	}
	seedUnits(t, repo, digest, map[string]LockedUnit{
		"acme:org/base.json@a1": {Kind: UnitKindLayer, Digest: "sha256:recorded"},
	})

	// recompute always reports "not available" — the digest check is skipped, so
	// the lock stays fresh.
	recompute := func(string) (string, bool) { return "", false }
	res, err := Staleness(repo, userPath, recompute)
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}
	if !res.Fresh {
		t.Fatalf("expected fresh when digests unavailable, got %+v", res)
	}
}

func TestStaleness_MissingManifestErrors(t *testing.T) {
	repo := t.TempDir()
	if _, err := Staleness(repo, "", nil); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestStaleness_MalformedOverlayScopeErrors(t *testing.T) {
	// Repo manifest is valid (LoadAgentsRC succeeds) but the project-local
	// overlay scope is malformed, so the inputs-digest computation fails inside
	// Staleness rather than at manifest load.
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	writeFileContent(t, filepath.Join(repo, AgentsRCLocalFile), `{bad json`)
	if _, err := Staleness(repo, userPath, nil); err == nil {
		t.Fatal("expected error when an overlay scope is malformed")
	}
}

func TestStaleness_CorruptLockfileErrors(t *testing.T) {
	repo, userPath := stalenessSeed(t, `{"extends":["acme:org/base.json"]}`, "")
	writeFileContent(t, AgentsLockPath(repo), `{not valid json`)
	if _, err := Staleness(repo, userPath, nil); err == nil {
		t.Fatal("expected error when the lockfile is corrupt")
	}
}

func TestDeclaredSetChanged_EqualLengthDifferentRef(t *testing.T) {
	rc := AgentsRC{Extends: []LayerRef{{Ref: "acme:org/base"}}}
	// Same cardinality, different identity → changed.
	units := map[string]LockedUnit{"acme:org/other@a1": {Kind: UnitKindLayer}}
	if !declaredSetChanged(rc, units) {
		t.Error("expected change when an equal-sized lock set holds a different ref")
	}
}

func TestDeclaredSetChanged_PackagesCounted(t *testing.T) {
	rc := AgentsRC{
		Extends:  []LayerRef{{Ref: "acme:org/base"}},
		Packages: []PackageRef{{Ref: "acme:skill/x@^1"}},
	}
	// Lock has the layer but not the package → changed.
	units := map[string]LockedUnit{"acme:org/base@a1": {Kind: UnitKindLayer}}
	if !declaredSetChanged(rc, units) {
		t.Error("expected declared-set change when a declared package is unlocked")
	}
	// Lock has both → unchanged.
	units["acme:skill/x@1.0.0"] = LockedUnit{Kind: UnitKindArtifact}
	if declaredSetChanged(rc, units) {
		t.Error("expected no change when every declared unit is locked")
	}
}

func TestDeclaredRefOf(t *testing.T) {
	cases := map[string]string{
		"acme:org/base@a1":  "acme:org/base",
		"acme:org/base":     "acme:org/base",
		"local:a/b@c@1.2.3": "local:a/b@c",
	}
	for in, want := range cases {
		if got := declaredRefOf(in); got != want {
			t.Errorf("declaredRefOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func hasReason(r StalenessResult, want StalenessReason) bool {
	for _, got := range r.Reasons {
		if got == want {
			return true
		}
	}
	return false
}
