package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
)

// This file adds focused coverage for the projection/prune branches of
// resource_plan.go that the primary suites leave uncovered. Helpers use the
// unique rpCov* prefix to avoid clashing with resource_plan_test.go /
// materialize_test.go, whose fixtures (materializeUnit, sourcedAgentBundle,
// stubPlatform, swapOSReadDir, swapExecuteResourcePlan, setupRepoAgentsHome)
// are reused here. Each Test stays small (Sonar S3776 ≤15) — one branch apiece.

// --- shared render stubs ----------------------------------------------------

// rpCovRenderStub implements ONLY ManagedRenderProjector so the render-prune
// helpers can be exercised directly with an injected provenance/removal fault.
type rpCovRenderStub struct {
	dir      string
	isRender func(string) (bool, error)
}

func (s rpCovRenderStub) ManagedRenderDir() string               { return s.dir }
func (s rpCovRenderStub) IsManagedRender(p string) (bool, error) { return s.isRender(p) }

// rpCovRenderPlat is a full Platform (via stubPlatform) that is ALSO a
// ManagedRenderProjector whose IsManagedRender always fails — used to drive the
// managed-render prune error paths through the public projection entry points.
type rpCovRenderPlat struct {
	stubPlatform
	renderDir string
	renderErr error
}

func (p rpCovRenderPlat) ManagedRenderDir() string             { return p.renderDir }
func (p rpCovRenderPlat) IsManagedRender(string) (bool, error) { return false, p.renderErr }

func rpCovSeedFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// --- BuildResourcePlan group-sort tiebreak (line 72) ------------------------

func TestRPCovBuildResourcePlan_ConflictKeySortTiebreak(t *testing.T) {
	t.Parallel()
	a := validSharedSkillIntent(".agents/skills/aaa", "stub-a")
	a.ConflictKey = "dup"
	b := validSharedSkillIntent(".agents/skills/bbb", "stub-b")
	b.ConflictKey = "dup"
	b.IntentID = "skills.proj.bbb.agents-skills"
	// Same conflict key, different target path: the group sort orders them by
	// TargetPath (line 72), then the compatibility check rejects the conflict.
	if _, err := BuildResourcePlan([]ResourceIntent{a, b}); err == nil {
		t.Fatal("expected a conflict for two incompatible intents sharing a conflict key")
	}
}

// --- buildCASIntents non-dir-mirror family skip (line 1475) -----------------

func TestRPCovBuildCASIntents_SkipsNonDirMirrorFamily(t *testing.T) {
	t.Parallel()
	units := []ResolvedUnit{{Family: "widgets", Name: "x", SourceID: "src", Digest: "sha256:" + strings.Repeat("a", 64)}}
	roots := map[string][]string{"skills": {".claude/skills"}}
	if got := buildCASIntents("proj", units, roots); len(got) != 0 {
		t.Fatalf("expected no intents for a non-dir-mirror family, got %d", len(got))
	}
}

// --- buildCASAgentFileIntents non-agent family skip (line 1548) -------------

func TestRPCovBuildCASAgentFileIntents_SkipsNonAgentFamily(t *testing.T) {
	t.Parallel()
	units := []ResolvedUnit{{Family: "skills", Name: "x", SourceID: "src", Digest: "sha256:" + strings.Repeat("a", 64)}}
	got := buildCASAgentFileIntents("proj", units, ".opencode/agent", ".md", "m")
	if len(got) != 0 {
		t.Fatalf("expected no file intents for a non-agents unit, got %d", len(got))
	}
}

// --- ValidateResolvedUnit malformed digest (line 1142) ----------------------

func TestRPCovValidateResolvedUnit_MalformedDigest(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	u := ResolvedUnit{Family: "skills", Name: "x", SourceID: "da-agc", Digest: "sha256:tooshort"}
	err := ValidateResolvedUnit(home, u, "proj")
	if err == nil || !strings.Contains(err.Error(), "malformed digest") {
		t.Fatalf("expected malformed-digest rejection, got %v", err)
	}
}

// --- sharedDirMirrorRoots on a non-provider platform (line 1373) ------------

func TestRPCovSharedDirMirrorRoots_NonProviderNil(t *testing.T) {
	t.Parallel()
	if got := sharedDirMirrorRoots(stubPlatform{id: "plain"}); got != nil {
		t.Fatalf("expected nil roots for a non-DirMirrorRootsProvider platform, got %v", got)
	}
}

// --- cursor + antigravity DirMirrorRoots (lines 1357, 1361) -----------------

func TestRPCovDirMirrorRoots_CursorAndAntigravity(t *testing.T) {
	t.Parallel()
	for _, p := range []Platform{NewCursor(), NewAntigravity()} {
		provider, ok := p.(DirMirrorRootsProvider)
		if !ok {
			t.Fatalf("%s does not implement DirMirrorRootsProvider", p.ID())
		}
		if len(provider.DirMirrorRoots()) == 0 {
			t.Fatalf("%s returned no dir-mirror roots", p.ID())
		}
	}
}

// --- verifyResolvedUnitsAtUse: CAS entry vanished (line 1189) ---------------

func TestRPCovVerifyResolvedUnitsAtUse_VanishedCAS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	u := materializeUnitWithContentDigest(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\n"})
	if err := os.RemoveAll(u.CASPath); err != nil {
		t.Fatalf("remove CAS entry: %v", err)
	}
	err := verifyResolvedUnitsAtUse(home, []ResolvedUnit{u})
	if err == nil || !strings.Contains(err.Error(), "vanished") {
		t.Fatalf("expected a vanished-CAS error, got %v", err)
	}
}

// --- formatSharedTargetPlanForDryRun: unknown source + preview (1028,1046) --

func TestRPCovFormatDryRun_UnknownSourceAndPreview(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	// Empty SourceRef → "(unknown source)"; unrecognized shape/transport → the
	// default "preview" line. A hand-built plan bypasses BuildResourcePlan's
	// validation so both fallbacks fire.
	plan := ResourcePlan{Resources: []plannedResource{{Intent: ResourceIntent{
		IntentID:   "weird",
		TargetPath: "some/target",
		Shape:      ResourceShape("mystery"),
		Transport:  ResourceTransport("mystery"),
	}}}}
	lines := formatSharedTargetPlanForDryRun(plan, filepath.Join(home, "repo"))
	if len(lines) != 1 || !strings.Contains(lines[0], "preview") {
		t.Fatalf("expected a preview line, got %v", lines)
	}
}

// --- prepareIntentTargetForReplacement Lstat branches (230, 233) ------------

func TestRPCovPrepareIntentTargetForReplacement_NotExist(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "absent")
	if err := prepareIntentTargetForReplacement(target, ResourceIntent{}); err != nil {
		t.Fatalf("absent target must be a no-op, got %v", err)
	}
}

func TestRPCovPrepareIntentTargetForReplacement_LstatError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ENOTDIR-under-file Lstat does not fail uniformly on Windows; covered on the POSIX legs")
	}
	file := rpCovSeedFile(t, t.TempDir(), "afile", "x")
	target := filepath.Join(file, "child") // path under a regular file → ENOTDIR
	if err := prepareIntentTargetForReplacement(target, ResourceIntent{}); err == nil {
		t.Fatal("expected an Lstat error for a path nested under a regular file")
	}
}

// --- ensureDirSymlinkIntent Lstat default error (line 204) ------------------

func TestRPCovEnsureDirSymlinkIntent_LstatError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ENOTDIR-under-file Lstat does not fail uniformly on Windows; covered on the POSIX legs")
	}
	file := rpCovSeedFile(t, t.TempDir(), "afile", "x")
	target := filepath.Join(file, "child")
	if err := ensureDirSymlinkIntent("/some/src", target, ResourceIntent{}); err == nil {
		t.Fatal("expected the non-NotExist Lstat error to surface")
	}
}

// --- executeResourceIntent: CAS-direct with empty source (line 150) ---------

func TestRPCovExecuteResourceIntent_CASDirectEmptySource(t *testing.T) {
	t.Parallel()
	intent := ResourceIntent{
		IntentID:  "cas.empty",
		Shape:     ResourceShapeDirectDir,
		Transport: ResourceTransportSymlink,
		SourceRef: ResourceSourceRef{Origin: casDirectOrigin}, // empty scope/bucket/rel → "" canonical path
	}
	err := executeResourceIntent(intent, t.TempDir(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), emptySourcePathErr) {
		t.Fatalf("expected empty-source-path error from a CAS-direct intent, got %v", err)
	}
}

// --- removeDirectSymlinkTarget: DirectFile empty source (line 976) ----------

func TestRPCovRemoveDirectSymlinkTarget_FileEmptySource(t *testing.T) {
	t.Parallel()
	intent := ResourceIntent{
		IntentID:  "file.empty",
		Shape:     ResourceShapeDirectFile,
		Transport: ResourceTransportSymlink,
		SourceRef: ResourceSourceRef{}, // "" canonical path
	}
	target := filepath.Join(t.TempDir(), "absent-link")
	err := removeDirectSymlinkTarget(intent, target, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), emptySourcePathErr) {
		t.Fatalf("expected the empty-source-path error to be aggregated, got %v", err)
	}
}

// --- MaterializeArtifact: store write error (line 1125) ---------------------

func TestRPCovMaterializeArtifact_StoreError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file-where-a-dir-is-expected store write does not fail uniformly on Windows; covered on the POSIX legs")
	}
	home := t.TempDir()
	// A regular file where the CAS root's `cache/` dir must live makes every
	// store write under it fail (ENOTDIR) — a deterministic MaterializeToStore
	// error AFTER the identity gate passes.
	rpCovSeedFile(t, home, "cache", "not a dir")
	bundle := config.Bundle{Entries: []config.BundleEntry{{Path: "SKILL.md", Data: []byte("x"), Mode: 0o644}}}
	if _, _, err := MaterializeArtifact(home, "skills", "da-agc", "x", bundle); err == nil {
		t.Fatal("expected the store write to fail when the CAS root cannot be created")
	}
}

// --- projectResolvedUnitsInexact (0% → all branches) ------------------------

func rpCovSkillUnit(t *testing.T, home string) ResolvedUnit {
	t.Helper()
	return materializeUnit(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\n"})
}

func TestRPCovProjectResolvedUnitsInexact_DryRunResources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	u := rpCovSkillUnit(t, home)
	repo := filepath.Join(t.TempDir(), "repo")
	lines, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{u}, []Platform{NewClaude()}, true, false, "proj")
	if err != nil {
		t.Fatalf("inexact dry-run: %v", err)
	}
	if len(lines) == 0 || lines[0] == sharedTargetsNoneLine {
		t.Fatalf("expected preview lines for a unit, got %v", lines)
	}
}

func TestRPCovProjectResolvedUnitsInexact_DryRunEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	lines, err := ProjectResolvedUnits("proj", repo, nil, []Platform{stubPlatform{id: "plain"}}, true, false)
	if err != nil {
		t.Fatalf("inexact dry-run empty: %v", err)
	}
	if len(lines) != 1 || lines[0] != sharedTargetsNoneLine {
		t.Fatalf("expected the none line, got %v", lines)
	}
}

func TestRPCovProjectResolvedUnitsInexact_ApplyWritesNoPrune(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	u := rpCovSkillUnit(t, home)
	// A pre-existing stale managed link that inexact must NOT prune.
	stale := filepath.Join(repo, ".claude", "skills", "gone")
	seedManagedLink(t, home, "gone", stale)

	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{u}, []Platform{NewClaude()}, false, false, "proj"); err != nil {
		t.Fatalf("inexact apply: %v", err)
	}
	link := filepath.Join(repo, ".claude", "skills", "release-docs-refresh")
	if !links.IsManagedLink(link, u.CASPath) {
		t.Fatalf("expected the unit linked at %s", link)
	}
	if _, err := os.Lstat(stale); err != nil {
		t.Fatalf("inexact apply must leave stale managed output in place, lstat err=%v", err)
	}
}

func TestRPCovProjectResolvedUnitsInexact_ApplyEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	lines, err := ProjectResolvedUnits("proj", repo, nil, []Platform{stubPlatform{id: "plain"}}, false, false)
	if err != nil || lines != nil {
		t.Fatalf("inexact apply empty: lines=%v err=%v", lines, err)
	}
}

// --- projectResolvedUnitsInexact apply verify-at-use failure (line 1288) ----

func TestRPCovProjectResolvedUnitsInexact_ApplyVerifyFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	u := materializeUnitWithContentDigest(t, home, "skills", "da-agc", "release-docs-refresh", map[string]string{"SKILL.md": "---\nname: release-docs-refresh\n---\noriginal\n"})
	// Tamper AFTER the anchor was recorded → the inexact apply's at-use verify
	// (the only mutating branch) must fail closed before any Execute.
	tamperCASEntry(t, u.CASPath, "SKILL.md", "TAMPERED")
	if _, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{u}, []Platform{NewClaude()}, false, false, "proj"); err == nil {
		t.Fatal("expected the inexact apply to fail on a tampered CAS entry")
	}
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "skills", "release-docs-refresh")); !os.IsNotExist(err) {
		t.Fatalf("no link must be created when the at-use verify fails, err=%v", err)
	}
}

// --- ExecuteSharedSkillMirrorPlan build-intents error (line 1059) -----------

func TestRPCovExecuteSharedSkillMirrorPlan_BuildError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file-where-a-dir-is-expected ReadDir does not fail uniformly on Windows; covered on the POSIX legs")
	}
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	// A regular file where the canonical skills bucket dir must live makes the
	// mirror-intent listing fail with a non-ENOENT error.
	rpCovSeedFile(t, filepath.Join(home, "skills"), "proj", "not a dir")
	err := ExecuteSharedSkillMirrorPlan("proj", filepath.Join(t.TempDir(), "repo"), ".agents/skills")
	if err == nil {
		t.Fatal("expected BuildSharedSkillMirrorIntents to surface the listing error")
	}
}

// --- staleManagedRenderTargets: missing dir + wanted skip (770, 775) --------

func TestRPCovStaleManagedRenderTargets_MissingDir(t *testing.T) {
	t.Parallel()
	// No .codex/agents dir exists → os.ReadDir errors → the platform is skipped.
	repo := filepath.Join(t.TempDir(), "repo")
	if stale := staleManagedRenderTargets(ResourcePlan{}, []Platform{NewCodex()}, repo); len(stale) != 0 {
		t.Fatalf("expected no stale renders for a missing dir, got %v", stale)
	}
}

func TestRPCovStaleManagedRenderTargets_SkipsWanted(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	dir := filepath.Join(repo, codexDir, "agents")
	wanted := rpCovSeedFile(t, dir, "wanted.toml", codexManagedTomlMarker+"\nname = \"wanted\"\n")
	stale := rpCovSeedFile(t, dir, "stale.toml", codexManagedTomlMarker+"\nname = \"stale\"\n")
	// A plan that WANTS wanted.toml — it must be skipped even though it is a
	// managed render; only stale.toml is reported.
	plan := ResourcePlan{Resources: []plannedResource{{Intent: ResourceIntent{
		TargetPath: filepath.Join(codexDir, "agents", "wanted.toml"),
	}}}}
	got := staleManagedRenderTargets(plan, []Platform{NewCodex()}, repo)
	if len(got) != 1 || got[0] != stale {
		t.Fatalf("expected only the stale render, got %v (wanted=%s)", got, wanted)
	}
}

// --- projectResolvedUnitsExact dry-run branches (1299-1304) -----------------

func TestRPCovProjectResolvedUnitsExact_DryRunEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	lines, err := ProjectResolvedUnits("proj", repo, nil, []Platform{stubPlatform{id: "plain"}}, true, true)
	if err != nil {
		t.Fatalf("exact dry-run empty: %v", err)
	}
	if len(lines) != 1 || lines[0] != sharedTargetsNoneLine {
		t.Fatalf("expected the none line, got %v", lines)
	}
}

// --- ProjectResolvedUnits collect + plan error (1232, 1244) -----------------

func TestRPCovProjectResolvedUnits_CollectError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	boom := errors.New("collect boom")
	_, err := ProjectResolvedUnits("proj", t.TempDir(), nil, []Platform{stubPlatform{id: "bad", err: boom}}, false, true)
	if !errors.Is(err, boom) {
		t.Fatalf("expected the collect error to propagate, got %v", err)
	}
}

func TestRPCovProjectResolvedUnits_PlanError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	a := validSharedSkillIntent(".agents/skills/aaa", "stub-a")
	a.ConflictKey = "dup"
	b := validSharedSkillIntent(".agents/skills/bbb", "stub-b")
	b.ConflictKey = "dup"
	b.IntentID = "skills.proj.bbb.agents-skills"
	plat := stubPlatform{id: "conflict", intents: []ResourceIntent{a, b}}
	_, err := ProjectResolvedUnits("proj", t.TempDir(), nil, []Platform{plat}, false, true)
	if err == nil || !strings.Contains(err.Error(), "conflicting intents") {
		t.Fatalf("expected a plan-build conflict error, got %v", err)
	}
}

// --- staleManagedRenderTargets + dry-run render prune line (768, 664) -------

func rpCovAgentUnit(t *testing.T, home string) ResolvedUnit {
	t.Helper()
	return materializeUnit(t, home, "agents", "da-agc", "platform-dirs-change-analyst", sourcedAgentBundle("platform-dirs-change-analyst", "reviews platform dir changes"))
}

func TestRPCovStaleManagedRenderTargets_ScansCodex(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	dir := filepath.Join(repo, codexDir, "agents")
	managed := rpCovSeedFile(t, dir, "stale.toml", codexManagedTomlMarker+"\nname = \"stale\"\n")
	rpCovSeedFile(t, dir, "user.toml", "name = \"user\"\n") // no marker → never stale

	stale := staleManagedRenderTargets(ResourcePlan{}, []Platform{NewCodex()}, repo)
	if len(stale) != 1 || stale[0] != managed {
		t.Fatalf("expected only the managed render reported stale, got %v", stale)
	}
}

func TestRPCovProjectResolvedUnitsExact_DryRunRenderPruneLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	// A stale managed codex render sitting where the projection prunes.
	rpCovSeedFile(t, filepath.Join(repo, codexDir, "agents"), "stale.toml", codexManagedTomlMarker+"\nname = \"stale\"\n")
	u := rpCovAgentUnit(t, home)
	lines, err := ProjectResolvedUnits("proj", repo, []ResolvedUnit{u}, []Platform{NewCodex()}, true, true, "proj")
	if err != nil {
		t.Fatalf("exact dry-run: %v", err)
	}
	var sawPrune bool
	for _, l := range lines {
		if strings.Contains(l, "prune managed") && strings.Contains(l, "stale") {
			sawPrune = true
		}
	}
	if !sawPrune {
		t.Fatalf("expected a managed-render prune line, got %v", lines)
	}
}

// --- pruneManagedRenderEntry error branches (745, 751) ----------------------

func TestRPCovPruneManagedRenderEntry_ProvenanceError(t *testing.T) {
	t.Parallel()
	boom := errors.New("prov boom")
	stub := rpCovRenderStub{isRender: func(string) (bool, error) { return false, boom }}
	removed, err := pruneManagedRenderEntry(stub, "/whatever")
	if removed || err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("expected a provenance error, got removed=%v err=%v", removed, err)
	}
}

func TestRPCovPruneManagedRenderEntry_RemoveError(t *testing.T) {
	t.Parallel()
	// A non-empty directory claimed as "our render" → os.Remove fails ENOTEMPTY
	// (deterministic on every OS, no permission fault needed).
	candidate := t.TempDir()
	rpCovSeedFile(t, candidate, "child", "x")
	stub := rpCovRenderStub{isRender: func(string) (bool, error) { return true, nil }}
	removed, err := pruneManagedRenderEntry(stub, candidate)
	if removed || err == nil || !strings.Contains(err.Error(), "prune managed render") {
		t.Fatalf("expected a remove error, got removed=%v err=%v", removed, err)
	}
}

// --- pruneManagedRendersForPlatform: entry error aggregation (line 729) -----

func TestRPCovPruneManagedRendersForPlatform_EntryErrorAggregated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rpCovSeedFile(t, dir, "a.toml", "x")
	boom := errors.New("prov boom")
	stub := rpCovRenderStub{dir: dir, isRender: func(string) (bool, error) { return false, boom }}
	pruned, errs := pruneManagedRendersForPlatform(stub, string(os.PathSeparator), map[string]bool{})
	if len(pruned) != 0 || len(errs) == 0 {
		t.Fatalf("expected an aggregated entry error, got pruned=%v errs=%v", pruned, errs)
	}
}

// --- pruneManagedRendersForPlatform: ReadDir error via seam (line 717) ------

func TestRPCovPruneManagedRendersForPlatform_ReadDirError(t *testing.T) {
	sentinel := errors.New("injected render readdir failure")
	defer swapOSReadDir(func(string) ([]os.DirEntry, error) { return nil, sentinel })()
	stub := rpCovRenderStub{dir: ".codex/agents", isRender: func(string) (bool, error) { return true, nil }}
	_, errs := pruneManagedRendersForPlatform(stub, t.TempDir(), map[string]bool{})
	if len(errs) != 1 || !errors.Is(errs[0], sentinel) {
		t.Fatalf("expected the injected readdir failure, got %v", errs)
	}
}

// --- RunSharedTargetProjectionExact render-prune error (line 643) -----------

func TestRPCovRunSharedTargetProjectionExact_RenderPruneError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	boom := errors.New("render boom")
	plat := rpCovRenderPlat{stubPlatform: stubPlatform{id: "codexlike"}, renderDir: filepath.Join(codexDir, "agents"), renderErr: boom}
	rpCovSeedFile(t, filepath.Join(repo, codexDir, "agents"), "x.toml", "x")
	_, err := RunSharedTargetProjectionExact("proj", repo, []Platform{plat}, false, true)
	if !errors.Is(err, boom) {
		t.Fatalf("expected the render-prune error to propagate, got %v", err)
	}
}

// --- projectResolvedUnitsExact render-prune error (line 1321) ---------------

func TestRPCovProjectResolvedUnitsExact_RenderPruneError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	boom := errors.New("render boom")
	plat := rpCovRenderPlat{stubPlatform: stubPlatform{id: "codexlike"}, renderDir: filepath.Join(codexDir, "agents"), renderErr: boom}
	rpCovSeedFile(t, filepath.Join(repo, codexDir, "agents"), "x.toml", "x")
	_, err := ProjectResolvedUnits("proj", repo, nil, []Platform{plat}, false, true)
	if !errors.Is(err, boom) {
		t.Fatalf("expected the render-prune error to propagate, got %v", err)
	}
}

// --- RunSharedTargetProjectionExact symlink-prune error via seam (line 634) -

func TestRPCovRunSharedTargetProjectionExact_PruneError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	writeFixtureImportedSkillPair(t, repo, home, "proj", "review")

	defer swapExecuteResourcePlan(func(ResourcePlan, string, string) error { return nil })()
	sentinel := errors.New("injected prune readdir failure")
	defer swapOSReadDir(func(string) ([]os.DirEntry, error) { return nil, sentinel })()

	_, err := RunSharedTargetProjectionExact("proj", repo, []Platform{NewClaude()}, false, true)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the prune ReadDir error to propagate, got %v", err)
	}
}

// --- projectResolvedUnitsExact symlink-prune error via seam (line 1317) -----

func TestRPCovProjectResolvedUnitsExact_PruneError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")

	defer swapExecuteResourcePlan(func(ResourcePlan, string, string) error { return nil })()
	sentinel := errors.New("injected prune readdir failure")
	defer swapOSReadDir(func(string) ([]os.DirEntry, error) { return nil, sentinel })()

	// Claude contributes dir-mirror prune roots, so the exact prune scan visits
	// them and hits the injected ReadDir failure even with an empty unit set.
	_, err := ProjectResolvedUnits("proj", repo, nil, []Platform{NewClaude()}, false, true, "proj")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the prune ReadDir error to propagate, got %v", err)
	}
}
