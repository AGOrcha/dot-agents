package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// packages_coverage_test.go drives the malformed-ref, fetch/materialize-failure,
// integrity-mismatch and malformed-lock error branches of packages_pass2.go and
// packages_digest_resolver.go directly (helpers prefixed lcCov to avoid
// collisions with the acceptance-shape fixtures in the sibling test files).

// lcCovWriteRawLock writes raw bytes to a project's .agentsrc.lock, simulating a
// corrupt/hostile lock the readers must tolerate (invalid JSON) or reject
// (well-formed JSON with a mistyped section value).
func lcCovWriteRawLock(t *testing.T, proj, raw string) {
	t.Helper()
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.AgentsLockPath(proj), []byte(raw), 0o644); err != nil {
		t.Fatalf("write raw lock: %v", err)
	}
}

// lcCovProj returns a fresh AGENTS_HOME-scoped temp project.
func lcCovProj(t *testing.T) (agentsHome, proj string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	proj = filepath.Join(tmp, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	return agentsHome, proj
}

// --- splitPackageArtifactFamily malformed shapes (pass2 71, 76) --------------

func TestLcCovSplitPackageArtifactFamily(t *testing.T) {
	bad := []string{
		"noslash",   // no separator
		"trailing/", // trailing separator
		"/leading",  // leading separator
		"a/b/c",     // more than one segment pair
	}
	for _, p := range bad {
		if _, _, err := splitPackageArtifactFamily(p); err == nil {
			t.Fatalf("expected %q to be rejected", p)
		}
	}
	if _, _, err := splitPackageArtifactFamily("widget/x"); err == nil {
		t.Fatal("expected an unsupported family label to be rejected")
	}
	bucket, name, err := splitPackageArtifactFamily("skill/demo")
	if err != nil || bucket != "skills" || name != "demo" {
		t.Fatalf("valid split failed: bucket=%q name=%q err=%v", bucket, name, err)
	}
}

// --- packageArtifactBucket malformed refs (resolver 78, 82) ------------------

func TestLcCovPackageArtifactBucket(t *testing.T) {
	if _, ok := packageArtifactBucket("no-colon-and-no-at"); ok {
		t.Fatal("expected an unparseable ref to yield ok=false")
	}
	if _, ok := packageArtifactBucket("src:bogus/name@1"); ok {
		t.Fatal("expected an unsupported artifact family to yield ok=false")
	}
	if bucket, ok := packageArtifactBucket("src:skill/demo@1"); !ok || bucket != "skills" {
		t.Fatalf("expected a valid ref to resolve to skills, got %q ok=%v", bucket, ok)
	}
}

// --- verifyProjectionInputs: CAS entry vanished (pass2 275) -------------------

func TestLcCovVerifyProjectionInputsMissingEntry(t *testing.T) {
	agentsHome, _ := lcCovProj(t)
	unit := platform.ResolvedUnit{
		Family: "skills",
		Name:   "ghost",
		Digest: "sha256:" + strings.Repeat("a", 64),
	}
	err := verifyProjectionInputs(agentsHome, []platform.ResolvedUnit{unit}, []string{"sha256:" + strings.Repeat("b", 64)})
	if err == nil || !strings.Contains(err.Error(), "vanished") {
		t.Fatalf("expected a vanished-CAS-entry error, got %v", err)
	}
}

// --- fetchAndMaterializePackage error branches (pass2 229, 244, 251, 255) ----

func TestLcCovFetchAndMaterializeErrors(t *testing.T) {
	agentsHome, _ := lcCovProj(t)
	tmp := t.TempDir()
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")

	// 229: an unparseable packages ref (no @version-spec) fails at ParsePackageRef.
	if _, _, err := fetchAndMaterializePackage(agentsHome, nil, "bad-ref-no-at", "", nil); err == nil {
		t.Fatal("expected a malformed ref to be rejected")
	}

	// 244: a source whose type has no fetcher fails at SelectPackageFetcher.
	badType := []config.Source{{Type: "bogus", ID: "dev", Path: srcRoot}}
	if _, _, err := fetchAndMaterializePackage(agentsHome, badType, "dev:skill/demo@1", "", nil); err == nil {
		t.Fatal("expected an unsupported source type to be rejected")
	}

	// 251: a local source whose artifact-path resolves to a single FILE (not a
	// directory) fetches a nil-Bundle artifact — not installable via packages.
	fileSrc := filepath.Join(tmp, "filesrc")
	if err := os.MkdirAll(filepath.Join(fileSrc, "skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fileSrc, "skill", "demo"), []byte("blob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	singleSrc := []config.Source{{Type: "local", ID: "dev", Path: fileSrc}}
	_, _, err := fetchAndMaterializePackage(agentsHome, singleSrc, "dev:skill/demo@1", "", nil)
	if err == nil || !strings.Contains(err.Error(), "not a directory-shaped bundle") {
		t.Fatalf("expected a single-file artifact to be rejected as a non-bundle, got %v", err)
	}

	// 255: a ref whose source-id is a store-invalid path segment ("..") fetches
	// fine but fails inside MaterializeArtifact's identity validation.
	dotdot := []config.Source{{Type: "local", ID: "..", Path: srcRoot}}
	if _, _, err := fetchAndMaterializePackage(agentsHome, dotdot, "..:skill/demo@1", "", nil); err == nil {
		t.Fatal("expected a traversal-shaped source id to fail materialization")
	}
}

// --- hydratePackagesFromLock error branches (pass2 192, 205, 208) ------------

// TestLcCovHydrateFromLock_MalformedLock hits the ReadUnits error branch (192).
func TestLcCovHydrateFromLock_MalformedLock(t *testing.T) {
	agentsHome, proj := lcCovProj(t)
	lcCovWriteRawLock(t, proj, "this is not json")

	snap := snapWithPackages(nil, "dev:skill/demo@1")
	if _, err := hydratePackagesFromLock(proj, agentsHome, snap, []string{"proj"}); err == nil {
		t.Fatal("expected a corrupt lock to fail the hydrate read")
	}
}

// TestLcCovHydrateFromLock_FetchFails hits the fetch-error branch (205): a
// locked artifact entry exists, but the source tree is gone so the pinned fetch
// fails.
func TestLcCovHydrateFromLock_FetchFails(t *testing.T) {
	agentsHome, proj := lcCovProj(t)
	if err := config.WriteUnitsLock(proj, config.UnitsLock{
		Units:        map[string]config.LockedUnit{"dev:skill/demo@1": {Kind: config.UnitKindArtifact, Digest: "sha256:" + strings.Repeat("a", 64)}},
		InputsDigest: "sha256:seed",
	}); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	sources := []config.Source{{Type: "local", ID: "dev", Path: filepath.Join(proj, "does-not-exist")}}
	snap := snapWithPackages(sources, "dev:skill/demo@1")
	if _, err := hydratePackagesFromLock(proj, agentsHome, snap, []string{"proj"}); err == nil {
		t.Fatal("expected the hydrate fetch to fail on a missing source tree")
	}
}

// TestLcCovHydrateFromLock_DigestMismatch hits the post-fetch digest-mismatch
// branch (208): the locked entry carries an EMPTY digest, so fetchAndMaterialize
// resolves live (no pin) and the freshly-resolved digest cannot equal the
// locked one.
func TestLcCovHydrateFromLock_DigestMismatch(t *testing.T) {
	agentsHome, proj := lcCovProj(t)
	tmp := t.TempDir()
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	if err := config.WriteUnitsLock(proj, config.UnitsLock{
		Units:        map[string]config.LockedUnit{"dev:skill/demo@1": {Kind: config.UnitKindArtifact, Digest: ""}},
		InputsDigest: "sha256:seed",
	}); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	sources := []config.Source{{Type: "local", ID: "dev", Path: srcRoot}}
	snap := snapWithPackages(sources, "dev:skill/demo@1")
	_, err := hydratePackagesFromLock(proj, agentsHome, snap, []string{"proj"})
	if err == nil || !strings.Contains(err.Error(), "does not match locked digest") {
		t.Fatalf("expected a hydrated-vs-locked digest mismatch, got %v", err)
	}
}

// --- HydratePackagesUnits ReadUnits error (pass2 128) ------------------------

func TestLcCovHydratePackagesUnits_MalformedLock(t *testing.T) {
	_, proj := lcCovProj(t)
	lcCovWriteRawLock(t, proj, "not json")

	res := &config.EnsureResult{Snapshot: snapWithPackages(nil, "dev:skill/demo@1"), ReResolved: true}
	if _, _, err := HydratePackagesUnits(proj, "proj", res); err == nil {
		t.Fatal("expected a corrupt lock to fail HydratePackagesUnits' read")
	}
}

// --- resolvePackagesUnits commit failure + commitArtifactLock section error --
// (pass2 174 and 302)

// TestLcCovResolvePackagesUnits_CommitFails seeds a lock whose "units" section
// is a JSON string (not a map): the fetch/materialize loop succeeds, but the
// combined-lock commit fails when agentslock.Update decodes the mistyped units
// section — exercising resolvePackagesUnits' commit-error branch (174) and
// commitArtifactLock's own Section-decode error (302).
func TestLcCovResolvePackagesUnits_CommitFails(t *testing.T) {
	agentsHome, proj := lcCovProj(t)
	tmp := t.TempDir()
	srcRoot := newPackagesSourceTree(t, filepath.Join(tmp, "src"), "skill", "demo", "# demo\n")
	lcCovWriteRawLock(t, proj, `{"lock_version":2,"units":"not-a-map"}`)

	sources := []config.Source{{Type: "local", ID: "dev", Path: srcRoot}}
	snap := snapWithPackages(sources, "dev:skill/demo@1")
	if _, err := resolvePackagesUnits(proj, agentsHome, snap, []string{"proj"}); err == nil {
		t.Fatal("expected the combined-lock commit to fail on a mistyped units section")
	}
}

// --- readArtifactContentDigests error branches (pass2 328, 332) --------------

func TestLcCovReadArtifactContentDigests_Errors(t *testing.T) {
	// 328: an unparseable lock cannot be opened → empty (non-nil) map.
	_, proj := lcCovProj(t)
	lcCovWriteRawLock(t, proj, "definitely not json")
	if got := readArtifactContentDigests(proj); len(got) != 0 {
		t.Fatalf("expected an empty map on a corrupt lock, got %v", got)
	}

	// 332: a well-formed lock whose artifact-content section is mistyped →
	// Section decode fails → empty map.
	tmp := t.TempDir()
	proj2 := filepath.Join(tmp, "proj2")
	lcCovWriteRawLock(t, proj2, `{"lock_version":2,"artifact-content":"not-a-map"}`)
	if got := readArtifactContentDigests(proj2); len(got) != 0 {
		t.Fatalf("expected an empty map on a mistyped artifact-content section, got %v", got)
	}
}

// --- PackagesArtifactDigestResolver error branches (resolver 38, 53) ---------

// TestLcCovDigestResolver_MalformedLock hits the ReadUnits error branch (38).
func TestLcCovDigestResolver_MalformedLock(t *testing.T) {
	_, proj := lcCovProj(t)
	lcCovWriteRawLock(t, proj, "not json")
	if _, ok := PackagesArtifactDigestResolver(proj)("dev:skill/demo@1"); ok {
		t.Fatal("expected a corrupt lock to make the resolver skip (ok=false)")
	}
}

// TestLcCovDigestResolver_BadFamilyRefSkips hits the packageArtifactBucket
// failure branch (53): a locked kind:artifact unit WITH a committed content
// anchor, but whose ref carries an unsupported artifact family, cannot resolve a
// CAS bucket, so the resolver skips.
func TestLcCovDigestResolver_BadFamilyRefSkips(t *testing.T) {
	_, proj := lcCovProj(t)
	ref := "dev:bogus/name@1"
	if err := commitArtifactLock(proj,
		map[string]config.LockedUnit{ref: {Kind: config.UnitKindArtifact, Digest: "sha256:" + strings.Repeat("a", 64)}},
		map[string]string{ref: "sha256:" + strings.Repeat("b", 64)},
	); err != nil {
		t.Fatalf("seed artifact unit + content anchor: %v", err)
	}
	if _, ok := PackagesArtifactDigestResolver(proj)(ref); ok {
		t.Fatal("expected a ref with an unsupported artifact family to skip (no CAS bucket)")
	}
}
