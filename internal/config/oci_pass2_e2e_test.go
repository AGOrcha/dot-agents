package config_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// oci_pass2_e2e_test.go is the t6 acceptance test the delegation's feedback_goal
// names directly: prove an OCI-source package ref materializes+projects through
// the SAME mechanism a git/local/http ref uses (package-artifact-install spec
// D9/H1/H13) — config.ociFetcher's FetchArtifact output is handed to
// platform.MaterializeArtifact + platform.ProjectResolvedUnits exactly as
// commands/internal/lifecycle/packages_pass2.go's fetchAndMaterializePackage
// does for every other source type, with no OCI-specific branch in either t2
// function. This is an external test package (config_test) so it can import
// internal/platform (which itself imports internal/config) without a cycle.

func buildOCIArtifactTarGz(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"SKILL.md":               "---\nname: release-docs-refresh\n---\nfrom oci\n",
		"instructions/detail.md": "nested instructions\n",
	}
	if err := tw.WriteHeader(&tar.Header{Name: "instructions", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatalf("tar dir header: %v", err)
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// TestOCIArtifactMaterializesAndProjectsLikeAnyOtherSource is the t6
// feedback_goal's core claim, proven directly: FetchArtifact("oci", ...)'s
// Bundle is not a special case for t2 — it materializes into the same
// content-addressed store and projects into the same repo-relative link a
// git/local/http artifact would, using zero OCI-aware code in either
// MaterializeArtifact or ProjectResolvedUnits.
func TestOCIArtifactMaterializesAndProjectsLikeAnyOtherSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)

	blob := buildOCIArtifactTarGz(t)
	src := config.Source{Type: "oci", URL: "oci://reg.example/dot-agents"}
	f, err := config.SelectPackageFetcher("oci")
	if err != nil {
		t.Fatalf("SelectPackageFetcher(oci): %v", err)
	}
	// This external test package has no access to ociFetcher's unexported
	// puller seam, so it drives FetchArtifact through its OTHER real
	// (non-test-only) fast path instead: a digest-pinned cache hit. Seed the
	// packages cache exactly as a prior successful pull would have left it,
	// then pin — FetchArtifact's cache-hit branch is production code, not a
	// test shortcut (the not-yet-wired live registry client is out of this
	// task's scope; this exercises everything downstream of it exactly as a
	// wired client's result would flow).
	digest := "sha256:" + sha256HexForTest(blob)
	if err := writeCachedArtifactForTest(t, digest, blob); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	fetched, err := f.FetchArtifact(src, config.PackageRefParts{SourceID: "da-agc", ArtifactPath: "skill/release-docs-refresh", VersionSpec: "pinned:" + digest})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if fetched.Bundle == nil {
		t.Fatal("expected FetchArtifact to populate Bundle for an OCI artifact-bundle payload (the t3-flagged CRITICAL gap)")
	}

	// From here on this is EXACTLY commands/internal/lifecycle/packages_pass2.go's
	// fetchAndMaterializePackage: MaterializeArtifact + a ResolvedUnit fed to
	// ProjectResolvedUnits. No OCI-specific code exists in either function.
	casPath, storeDigest, err := platform.MaterializeArtifact(home, "skills", "da-agc", "release-docs-refresh", *fetched.Bundle, "myproj")
	if err != nil {
		t.Fatalf("MaterializeArtifact: %v", err)
	}
	unit := platform.ResolvedUnit{Family: "skills", Name: "release-docs-refresh", SourceID: "da-agc", Digest: storeDigest, CASPath: casPath}

	repo := filepath.Join(t.TempDir(), "myproj")
	platforms := []platform.Platform{platform.NewClaude()}
	if _, err := platform.ProjectResolvedUnits("myproj", repo, []platform.ResolvedUnit{unit}, platforms, false, true, "myproj"); err != nil {
		t.Fatalf("ProjectResolvedUnits: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(repo, ".claude", "skills", "release-docs-refresh", "SKILL.md"))
	if err != nil {
		t.Fatalf("read projected SKILL.md: %v", err)
	}
	if !strings.Contains(string(got), "from oci") {
		t.Fatalf("projected content = %q, want the OCI-fetched body", got)
	}
	nested, err := os.ReadFile(filepath.Join(repo, ".claude", "skills", "release-docs-refresh", "instructions", "detail.md"))
	if err != nil {
		t.Fatalf("read projected nested file: %v", err)
	}
	if !strings.Contains(string(nested), "nested instructions") {
		t.Fatalf("nested projected content = %q", nested)
	}
}

// sha256HexForTest and writeCachedArtifactForTest reach the packages cache
// only through its documented on-disk layout and the public
// config.AgentsHome()-rooted AGENTS_HOME env var — no unexported config
// symbol is used, keeping this file a true external (config_test) consumer of
// the package's public surface.
func sha256HexForTest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeCachedArtifactForTest(t *testing.T, digest string, data []byte) error {
	t.Helper()
	// The packages cache layout (~/.agents/cache/packages/<hex>/artifact.blob)
	// is an internal, unexported contract; recreate it here via the same
	// AGENTS_HOME env var config.AgentsHome() reads, rather than importing an
	// unexported helper.
	home := os.Getenv("AGENTS_HOME")
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	dir := filepath.Join(home, "cache", "packages", hexDigest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "artifact.blob"), data, 0o644)
}
