package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
)

func testMaterializeBundle(files map[string]string) config.Bundle {
	entries := make([]config.BundleEntry, 0, len(files))
	for path, content := range files {
		entries = append(entries, config.BundleEntry{Path: path, Data: []byte(content), Mode: 0o644})
	}
	return config.Bundle{Entries: entries}
}

// --- H3: source-id collision rejection --------------------------------------

func TestValidateSourceID_RejectsGlobal(t *testing.T) {
	t.Parallel()
	if err := ValidateSourceID("global"); err == nil {
		t.Fatal("expected an error for source id \"global\"")
	} else if !errors.Is(err, ErrReservedSourceID) {
		t.Fatalf("expected ErrReservedSourceID, got %v", err)
	}
}

func TestValidateSourceID_RejectsSourcedNamespaceLiteral(t *testing.T) {
	t.Parallel()
	if err := ValidateSourceID(config.SourcedScopeSegment); err == nil {
		t.Fatal("expected an error for source id equal to the reserved namespace literal")
	} else if !errors.Is(err, ErrReservedSourceID) {
		t.Fatalf("expected ErrReservedSourceID, got %v", err)
	}
}

func TestValidateSourceID_RejectsLocalProjectScope(t *testing.T) {
	t.Parallel()
	if err := ValidateSourceID("dot-agents", "dot-agents"); err == nil {
		t.Fatal("expected an error for a source id equal to the local project scope")
	} else if !errors.Is(err, ErrReservedSourceID) {
		t.Fatalf("expected ErrReservedSourceID, got %v", err)
	}
}

func TestValidateSourceID_RejectsEmpty(t *testing.T) {
	t.Parallel()
	if err := ValidateSourceID(""); err == nil {
		t.Fatal("expected an error for an empty source id")
	}
}

func TestValidateSourceID_AcceptsDistinctSourceID(t *testing.T) {
	t.Parallel()
	if err := ValidateSourceID("da-agc", "dot-agents"); err != nil {
		t.Fatalf("expected a distinct source id to be accepted, got %v", err)
	}
}

// --- H2/H3: MaterializeArtifact ---------------------------------------------

func TestMaterializeArtifact_ProjectsIntoReservedNamespace(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testMaterializeBundle(map[string]string{"SKILL.md": "# skill\n"})

	projPath, digest, err := MaterializeArtifact(home, "skills", "da-agc", "release-docs-refresh", bundle, "dot-agents")
	if err != nil {
		t.Fatalf("MaterializeArtifact: %v", err)
	}
	wantProj := SourcedProjectionPath(home, "skills", "da-agc", "release-docs-refresh")
	if projPath != wantProj {
		t.Fatalf("projection path = %q, want %q", projPath, wantProj)
	}
	if digest == "" {
		t.Fatal("expected a non-empty digest")
	}
	// The projection must be a managed link resolving under agentsHome (H2:
	// "the projected view is a link to the locked digest"), never a copy.
	if !links.IsManagedLinkUnder(projPath, home) {
		t.Fatalf("expected %s to be a managed link under %s", projPath, home)
	}
	got, err := os.ReadFile(filepath.Join(projPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("read through projection: %v", err)
	}
	if string(got) != "# skill\n" {
		t.Fatalf("content mismatch through projection: %q", got)
	}
}

// TestMaterializeArtifact_RejectsReservedSourceIDBeforeAnyWrite proves H3's
// gate runs BEFORE any filesystem write: a rejected call must leave no store
// entry and no projection path behind.
func TestMaterializeArtifact_RejectsReservedSourceIDBeforeAnyWrite(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testMaterializeBundle(map[string]string{"SKILL.md": "# skill\n"})

	if _, _, err := MaterializeArtifact(home, "skills", "global", "release-docs-refresh", bundle); err == nil {
		t.Fatal("expected source id \"global\" to be rejected")
	} else if !errors.Is(err, ErrReservedSourceID) {
		t.Fatalf("expected ErrReservedSourceID, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "cache")); !os.IsNotExist(err) {
		t.Fatalf("expected no store write before the H3 gate rejects, cache dir state: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", config.SourcedScopeSegment)); !os.IsNotExist(err) {
		t.Fatalf("expected no projection write before the H3 gate rejects: %v", err)
	}
}

// TestMaterializeArtifact_RefusesToReplaceNonSourcedPath is the H3
// adversarial claim: a real, unmanaged directory sitting at the reserved
// projection path (not something materialize itself created) must NEVER be
// silently clobbered.
func TestMaterializeArtifact_RefusesToReplaceNonSourcedPath(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	projPath := SourcedProjectionPath(home, "skills", "da-agc", "release-docs-refresh")
	if err := os.MkdirAll(projPath, 0o755); err != nil {
		t.Fatalf("seed unmanaged dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projPath, "user-file.txt"), []byte("do not eat me"), 0o644); err != nil {
		t.Fatalf("seed unmanaged file: %v", err)
	}

	bundle := testMaterializeBundle(map[string]string{"SKILL.md": "# skill\n"})
	if _, _, err := MaterializeArtifact(home, "skills", "da-agc", "release-docs-refresh", bundle); err == nil {
		t.Fatal("expected MaterializeArtifact to refuse replacing an unmanaged occupant")
	}
	// The unmanaged content must be completely untouched.
	got, err := os.ReadFile(filepath.Join(projPath, "user-file.txt"))
	if err != nil {
		t.Fatalf("unmanaged file was removed or made unreadable: %v", err)
	}
	if string(got) != "do not eat me" {
		t.Fatalf("unmanaged file content changed: %q", got)
	}
}

// TestMaterializeArtifact_ReMaterializeSameDigestIsNoOp proves R4 through the
// platform-layer entry point: re-materializing the identical bundle produces
// the identical projection path/digest and byte-identical content.
func TestMaterializeArtifact_ReMaterializeSameDigestIsNoOp(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testMaterializeBundle(map[string]string{"SKILL.md": "# skill\n"})

	proj1, digest1, err := MaterializeArtifact(home, "skills", "da-agc", "release-docs-refresh", bundle)
	if err != nil {
		t.Fatalf("first materialize: %v", err)
	}
	proj2, digest2, err := MaterializeArtifact(home, "skills", "da-agc", "release-docs-refresh", bundle)
	if err != nil {
		t.Fatalf("second materialize: %v", err)
	}
	if proj1 != proj2 || digest1 != digest2 {
		t.Fatalf("re-materialize diverged: (%q,%q) vs (%q,%q)", proj1, digest1, proj2, digest2)
	}
	got, err := os.ReadFile(filepath.Join(proj2, "SKILL.md"))
	if err != nil {
		t.Fatalf("read after re-materialize: %v", err)
	}
	if string(got) != "# skill\n" {
		t.Fatalf("content diverged after re-materialize: %q", got)
	}
}

// TestMaterializeArtifact_ChangedDigestRepointsProjection proves the H2
// "link moves, content does not mutate" contract at the platform layer: a
// second materialize of the SAME (family, source, name) with DIFFERENT
// content re-points the projection to the new digest's store entry.
func TestMaterializeArtifact_ChangedDigestRepointsProjection(t *testing.T) {
	home := t.TempDir()
	// links.Symlink's "is this an entry I already own" check resolves
	// ownership against the package-global config.AgentsHome() (env-var
	// backed), not the agentsHome parameter passed to MaterializeArtifact —
	// production always calls MaterializeArtifact with agentsHome ==
	// config.AgentsHome(), so this test must match that or the SECOND
	// materialize (re-pointing an EXISTING link) is misjudged as an
	// unmanaged occupant and refused instead of re-pointed.
	t.Setenv("AGENTS_HOME", home)
	v1 := testMaterializeBundle(map[string]string{"SKILL.md": "v1\n"})
	v2 := testMaterializeBundle(map[string]string{"SKILL.md": "v2\n"})

	proj1, digest1, err := MaterializeArtifact(home, "skills", "da-agc", "release-docs-refresh", v1)
	if err != nil {
		t.Fatalf("materialize v1: %v", err)
	}
	proj2, digest2, err := MaterializeArtifact(home, "skills", "da-agc", "release-docs-refresh", v2)
	if err != nil {
		t.Fatalf("materialize v2: %v", err)
	}
	if proj1 != proj2 {
		t.Fatalf("projection path should stay stable across a digest change: %q vs %q", proj1, proj2)
	}
	if digest1 == digest2 {
		t.Fatalf("expected distinct digests for distinct content")
	}
	got, err := os.ReadFile(filepath.Join(proj2, "SKILL.md"))
	if err != nil {
		t.Fatalf("read after repoint: %v", err)
	}
	if string(got) != "v2\n" {
		t.Fatalf("projection did not repoint to the new digest's content: %q", got)
	}
}

// --- H4/H11: removal prunes the projection, never the permanent ignore -----

func TestRemoveMaterializedArtifact_PrunesProjectionKeepsStoreAndIgnore(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testMaterializeBundle(map[string]string{"SKILL.md": "# skill\n"})

	projPath, _, err := MaterializeArtifact(home, "skills", "da-agc", "release-docs-refresh", bundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	storeRoot := config.ArtifactStoreRoot(home, "skills")
	before, err := os.ReadDir(storeRoot)
	if err != nil || len(before) == 0 {
		t.Fatalf("expected a populated store root before removal: entries=%v err=%v", before, err)
	}

	if err := RemoveMaterializedArtifact(home, "skills", "da-agc", "release-docs-refresh"); err != nil {
		t.Fatalf("RemoveMaterializedArtifact: %v", err)
	}
	if _, err := os.Lstat(projPath); !os.IsNotExist(err) {
		t.Fatalf("expected the projection to be pruned, got err=%v", err)
	}
	// H11: the immutable store entry is untouched by projection removal.
	after, err := os.ReadDir(storeRoot)
	if err != nil {
		t.Fatalf("read store root after removal: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("H11 violated: store entries changed by projection removal: before=%d after=%d", len(before), len(after))
	}
	// H4: the permanent ignore is untouched by a package removal.
	ok, err := config.NewLocalSource(home, nil).SourcedIgnoreInstalled()
	if err != nil {
		t.Fatalf("SourcedIgnoreInstalled: %v", err)
	}
	if !ok {
		t.Fatalf("H4 violated: permanent sourced ignore missing after removing the projection")
	}
}

// --- D4/Q3: RunSharedTargetProjectionExact treats the sourced scope as an
// additional input set --------------------------------------------------

// TestBuildSharedSkillMirrorIntents_IncludesMaterializedSourcedScope is the
// D4/Q3 acceptance test: a resource materialized ONLY under the reserved
// "_sourced/<source-id>" scope (no local-authored counterpart) must be
// discovered by the SAME shared mirror-intent builder RunSharedTargetProjectionExact
// already uses — proving materialize composes through the existing
// exact/prune projection rather than a parallel linker.
func TestBuildSharedSkillMirrorIntents_IncludesMaterializedSourcedScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	bundle := testMaterializeBundle(map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\n"})
	if _, _, err := MaterializeArtifact(agentsHome, "skills", "da-agc", "release-docs-refresh", bundle, "dot-agents"); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	intents, err := BuildSharedSkillMirrorIntents("dot-agents", filepath.Join(".claude", "skills"))
	if err != nil {
		t.Fatalf("BuildSharedSkillMirrorIntents: %v", err)
	}
	var found *ResourceIntent
	for i := range intents {
		if intents[i].LogicalName == "release-docs-refresh" {
			found = &intents[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a mirror intent for the materialized sourced-scope skill, got %+v", intents)
	}
	if !strings.Contains(found.SourceRef.Scope, config.SourcedScopeSegment) {
		t.Fatalf("expected SourceRef.Scope to reference the sourced namespace, got %q", found.SourceRef.Scope)
	}
	wantSrc := SourcedProjectionPath(agentsHome, "skills", "da-agc", "release-docs-refresh")
	if got := found.SourceRef.CanonicalPath(agentsHome); got != wantSrc {
		t.Fatalf("SourceRef.CanonicalPath = %q, want %q", got, wantSrc)
	}
}

// TestRunSharedTargetProjectionExact_ExecutesMaterializedSourcedResource
// drives the FULL D4 path end to end: materialize a skill under the sourced
// scope, run RunSharedTargetProjectionExact for a real platform, and assert
// the repo-local managed link lands and resolves to the materialized
// content.
func TestRunSharedTargetProjectionExact_ExecutesMaterializedSourcedResource(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	repo := filepath.Join(tmp, "repo")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	bundle := testMaterializeBundle(map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\n"})
	if _, _, err := MaterializeArtifact(agentsHome, "skills", "da-agc", "release-docs-refresh", bundle, "dot-agents"); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	platforms := []Platform{NewClaude()}
	if _, err := RunSharedTargetProjectionExact("dot-agents", repo, platforms, false, true); err != nil {
		t.Fatalf("RunSharedTargetProjectionExact: %v", err)
	}

	link := filepath.Join(repo, ".claude", "skills", "release-docs-refresh")
	wantTarget := SourcedProjectionPath(agentsHome, "skills", "da-agc", "release-docs-refresh")
	if !links.IsManagedLink(link, wantTarget) {
		t.Fatalf("expected a managed link at %s -> %s", link, wantTarget)
	}
	got, err := os.ReadFile(filepath.Join(link, "SKILL.md"))
	if err != nil {
		t.Fatalf("read through the full projection chain: %v", err)
	}
	if !strings.Contains(string(got), "release-docs-refresh") {
		t.Fatalf("unexpected content through the projection chain: %q", got)
	}
}
