package platform

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// hooksPruneFixture builds an isolated ~/.agents/hooks/ tree (never the real
// home) for exercising ImportArtifactCandidates in isolation.
func hooksPruneFixture(t *testing.T) (agentsHome string) {
	t.Helper()
	agentsHome = filepath.Join(t.TempDir(), ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	return agentsHome
}

// writePruneBundle writes a canonical HOOK.yaml bundle, optionally with a
// sidecar script, under agentsHome/hooks/<scope>/<name>/.
func writePruneBundle(t *testing.T, agentsHome, scope, name, manifest string, script []byte) string {
	t.Helper()
	dir := filepath.Join(agentsHome, "hooks", scope, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if script != nil {
		if err := os.WriteFile(filepath.Join(dir, "gate.sh"), script, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// backdateManifest sets a bundle's HOOK.yaml mtime into the past, so tests
// can deterministically establish which of two byte-identical bundles is
// "older" (the presumed original) without depending on filesystem timestamp
// resolution between two fast, back-to-back writes.
func backdateManifest(t *testing.T, bundleDir string, age time.Duration) {
	t.Helper()
	manifest := filepath.Join(bundleDir, "HOOK.yaml")
	past := time.Now().Add(-age)
	if err := os.Chtimes(manifest, past, past); err != nil {
		t.Fatal(err)
	}
}

func ispGateManifest(name string) string {
	return `name: ` + name + `
when_events:
  - pre_compact
  - stop
run:
  command: ./gate.sh
enabled_on:
  - claude
`
}

// findCandidate returns the candidate matching scope/name, failing the test
// if it is absent.
func findCandidate(t *testing.T, candidates []ImportArtifactCandidate, scope, name string) ImportArtifactCandidate {
	t.Helper()
	for _, c := range candidates {
		if c.Scope == scope && c.Name == name {
			return c
		}
	}
	t.Fatalf("no candidate for %s/%s among %+v", scope, name, candidates)
	return ImportArtifactCandidate{}
}

func assertNoCandidate(t *testing.T, candidates []ImportArtifactCandidate, scope, name string) {
	t.Helper()
	for _, c := range candidates {
		if c.Scope == scope && c.Name == name {
			t.Fatalf("did not expect %s/%s to be flagged, got %+v", scope, name, c)
		}
	}
}

// TestImportArtifactCandidates_CommandOwnedByPathCapture pins the real
// production shape from #533's PR description: an artifact bundle whose
// manifest command is an absolute path into another bundle's directory.
func TestImportArtifactCandidates_CommandOwnedByPathCapture(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	realDir := writePruneBundle(t, agentsHome, "global", "isp-gate", ispGateManifest("isp-gate"), []byte("#!/bin/sh\nexit 0\n"))

	writePruneBundle(t, agentsHome, "global", "pre-compact-gate", `name: pre-compact-gate
when: pre_compact
run:
  command: `+filepath.Join(realDir, "gate.sh")+`
`, nil)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}

	got := findCandidate(t, candidates, "global", "pre-compact-gate")
	if got.Reason != ImportArtifactReasonCommandOwned {
		t.Errorf("reason = %q, want %q", got.Reason, ImportArtifactReasonCommandOwned)
	}
	if got.Owner() != "global/isp-gate" {
		t.Errorf("owner = %q, want global/isp-gate", got.Owner())
	}
	assertNoCandidate(t, candidates, "global", "isp-gate")
}

// TestImportArtifactCandidates_DuplicateManifestCapture covers the case
// command-ownership cannot see: a bundle whose OWN command still resolves
// inside its own directory (so it never looks "owned by someone else")
// because the whole bundle — manifest fields and sidecar script — is a
// byte-identical copy of another bundle's.
func TestImportArtifactCandidates_DuplicateManifestCapture(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	script := []byte("#!/bin/sh\nexit 0\n")
	realDir := writePruneBundle(t, agentsHome, "global", "isp-gate", ispGateManifest("isp-gate"), script)
	backdateManifest(t, realDir, time.Hour) // established as the older, original bundle
	writePruneBundle(t, agentsHome, "global", "isp-gate-copy", ispGateManifest("isp-gate-copy"), script)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}

	got := findCandidate(t, candidates, "global", "isp-gate-copy")
	if got.Reason != ImportArtifactReasonDuplicateManifest {
		t.Errorf("reason = %q, want %q", got.Reason, ImportArtifactReasonDuplicateManifest)
	}
	if got.Owner() != "global/isp-gate" {
		t.Errorf("owner = %q, want global/isp-gate", got.Owner())
	}
	assertNoCandidate(t, candidates, "global", "isp-gate")
}

// TestImportArtifactCandidates_DuplicateManifestSameAgeIsAmbiguous pins the
// tiebreaker's fail-safe: two byte-identical bundles with indistinguishable
// (equal) mtimes must not have a direction guessed — both are reported
// ambiguous rather than one being arbitrarily crowned "the artifact".
func TestImportArtifactCandidates_DuplicateManifestSameAgeIsAmbiguous(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	script := []byte("#!/bin/sh\nexit 0\n")
	dirA := writePruneBundle(t, agentsHome, "global", "isp-gate", ispGateManifest("isp-gate"), script)
	dirB := writePruneBundle(t, agentsHome, "global", "isp-gate-copy", ispGateManifest("isp-gate-copy"), script)
	same := time.Now().Add(-time.Hour)
	for _, dir := range []string{dirA, dirB} {
		if err := os.Chtimes(filepath.Join(dir, "HOOK.yaml"), same, same); err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}
	for _, name := range []string{"isp-gate", "isp-gate-copy"} {
		got := findCandidate(t, candidates, "global", name)
		if got.Reason != ImportArtifactReasonAmbiguous {
			t.Errorf("%s: reason = %q, want ambiguous", name, got.Reason)
		}
	}
}

// TestImportArtifactCandidates_BareCommandCollisionNotFlagged is the false
// positive guard: two independently authored bundles that happen to invoke
// the exact same bare tool must NOT flag each other. Only an already-
// absolute manifest command is trusted as a command-ownership signal.
func TestImportArtifactCandidates_BareCommandCollisionNotFlagged(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	writePruneBundle(t, agentsHome, "global", "gate-a", `name: gate-a
when: stop
run:
  command: da workflow checkpoint
`, nil)
	writePruneBundle(t, agentsHome, "global", "gate-b", `name: gate-b
when: session_start
run:
  command: da workflow checkpoint
`, nil)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}
	assertNoCandidate(t, candidates, "global", "gate-a")
	assertNoCandidate(t, candidates, "global", "gate-b")
}

// TestImportArtifactCandidates_AmbiguousDuplicateMatchesSkipped verifies
// that when a bundle's content matches MORE THAN ONE other bundle, no owner
// is reported — the caller cannot safely attribute or delete it.
func TestImportArtifactCandidates_AmbiguousDuplicateMatchesSkipped(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	stubManifest := func(name string) string {
		return `name: ` + name + `
when: stop
run:
  command: true
`
	}
	writePruneBundle(t, agentsHome, "global", "stub-a", stubManifest("stub-a"), nil)
	writePruneBundle(t, agentsHome, "global", "stub-b", stubManifest("stub-b"), nil)
	writePruneBundle(t, agentsHome, "global", "stub-c", stubManifest("stub-c"), nil)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}
	for _, name := range []string{"stub-a", "stub-b", "stub-c"} {
		got := findCandidate(t, candidates, "global", name)
		if got.Reason != ImportArtifactReasonAmbiguous {
			t.Errorf("%s: reason = %q, want ambiguous", name, got.Reason)
		}
		if got.Owner() != "" {
			t.Errorf("%s: owner = %q, want empty for an ambiguous candidate", name, got.Owner())
		}
	}
}

// TestImportArtifactCandidates_EmptyAgentsHome guards the degrade-to-empty
// contract: no agents home (or none of its scopes have hooks yet) must never
// error, just report no candidates.
func TestImportArtifactCandidates_EmptyAgentsHome(t *testing.T) {
	if candidates, err := ImportArtifactCandidates(""); err != nil || len(candidates) != 0 {
		t.Fatalf("empty agentsHome: got %v, %v", candidates, err)
	}
	agentsHome := filepath.Join(t.TempDir(), ".agents")
	if candidates, err := ImportArtifactCandidates(agentsHome); err != nil || len(candidates) != 0 {
		t.Fatalf("missing hooks dir: got %v, %v", candidates, err)
	}
}

// TestImportArtifactCandidates_MultiProjectScopes proves scanning covers
// every scope directory on disk, not just "global".
func TestImportArtifactCandidates_MultiProjectScopes(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	realDir := writePruneBundle(t, agentsHome, "my-project", "isp-gate", `name: isp-gate
when: stop
run:
  command: ./gate.sh
`, []byte("#!/bin/sh\nexit 0\n"))
	writePruneBundle(t, agentsHome, "my-project", "stop-gate", `name: stop-gate
when: stop
run:
  command: `+filepath.Join(realDir, "gate.sh")+`
`, nil)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}
	got := findCandidate(t, candidates, "my-project", "stop-gate")
	if got.Owner() != "my-project/isp-gate" {
		t.Errorf("owner = %q, want my-project/isp-gate", got.Owner())
	}
}

// TestImportArtifactCandidates_UnreadableHooksDirPropagatesError pins the
// swallow-guard: a permission error listing agentsHome/hooks itself (as
// opposed to a legitimate absence) must surface, never be misread as "no
// bundles anywhere".
func TestImportArtifactCandidates_UnreadableHooksDirPropagatesError(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	testutil.MakeDirUnreadable(t, filepath.Join(agentsHome, "hooks"))

	if _, err := ImportArtifactCandidates(agentsHome); err == nil {
		t.Fatal("expected an error for an unreadable hooks/ directory")
	}
}

// TestImportArtifactCandidates_UnreadableScopeDirPropagatesError covers the
// same guard one level down: agentsHome/hooks/ itself is listable, but one
// scope subdirectory is not.
func TestImportArtifactCandidates_UnreadableScopeDirPropagatesError(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	writePruneBundle(t, agentsHome, "global", "isp-gate", ispGateManifest("isp-gate"), []byte("#!/bin/sh\nexit 0\n"))
	testutil.MakeDirUnreadable(t, filepath.Join(agentsHome, "hooks", "global"))

	if _, err := ImportArtifactCandidates(agentsHome); err == nil {
		t.Fatal("expected an error for an unreadable scope directory")
	}
}

// TestImportArtifactCandidates_OrphanedTargetSiblingsNotCircularlyOwned is a
// regression test for a real bug a production dry run surfaced: two sibling
// captures in DIFFERENT scopes both point at the exact same absolute script
// path, but that path's OWN bundle directory has no HOOK.yaml (its manifest
// was deleted or never existed — an orphaned script). Neither sibling
// actually owns the other's directory, so pre-fix, falling through to the
// exact-string commands map produced a circular false command_owned
// attribution: A "owned by" B AND B "owned by" A, BOTH becoming
// "removable" in the same dry-run listing — a genuine data-loss risk had
// --apply run (deleting both loses the hook entirely).
//
// The siblings here are also fully content-identical (same When, matcher,
// command, enabled_on — exactly the real shape found in production), so the
// duplicate-manifest signal legitimately fires once the buggy command_owned
// attribution is out of the way: consolidating to keep the OLDER of two
// truly-identical captures and drop the newer is safe (no hook behavior is
// lost, since the two are functionally interchangeable). What must never
// happen is BOTH being flagged, or either owning the other via the
// commands-map fallback.
func TestImportArtifactCandidates_OrphanedTargetSiblingsNotCircularlyOwned(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	// The orphaned target: a directory shaped like a bundle (matching what
	// ResolveHookCommand would have produced for a real one), but with only
	// a script and no HOOK.yaml — its own canonical manifest is gone.
	orphanDir := filepath.Join(agentsHome, "hooks", "global", "post-commit-checkpoint")
	mustMkdirAllT(t, orphanDir)
	orphanScript := filepath.Join(orphanDir, "post-commit-checkpoint.sh")
	if err := os.WriteFile(orphanScript, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	siblingManifest := `name: post-tool-use-post-commit-checkpoint
when: post_tool_use
match:
  tools:
    - Bash
run:
  command: ` + orphanScript + `
enabled_on:
  - claude
  - copilot
`
	olderDir := writePruneBundle(t, agentsHome, "swarm-cd", "post-tool-use-post-commit-checkpoint", siblingManifest, nil)
	backdateManifest(t, olderDir, time.Hour)
	writePruneBundle(t, agentsHome, "sync-engine", "post-tool-use-post-commit-checkpoint", siblingManifest, nil)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}

	// The older sibling (swarm-cd) must never be a candidate — neither
	// "owned by" its younger sibling (the pre-fix bug) nor by anything else.
	assertNoCandidate(t, candidates, "swarm-cd", "post-tool-use-post-commit-checkpoint")

	// The newer sibling (sync-engine) is legitimately consolidated into the
	// older one via the duplicate-manifest signal, never command_owned
	// (which would mean the buggy circular attribution survived).
	got := findCandidate(t, candidates, "sync-engine", "post-tool-use-post-commit-checkpoint")
	if got.Reason != ImportArtifactReasonDuplicateManifest {
		t.Fatalf("reason = %q, want %q (command_owned would mean the circular-attribution bug is back)", got.Reason, ImportArtifactReasonDuplicateManifest)
	}
	if got.Owner() != "swarm-cd/post-tool-use-post-commit-checkpoint" {
		t.Errorf("owner = %q, want swarm-cd/post-tool-use-post-commit-checkpoint", got.Owner())
	}
}

// TestCommandOwnedByOtherBundle_ForeignAbsoluteCommandNotOwned covers the
// "no owner found" path: an absolute command that points nowhere any
// existing bundle explains (a genuinely hand-authored absolute-path hook)
// must not be flagged.
func TestCommandOwnedByOtherBundle_ForeignAbsoluteCommandNotOwned(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	writePruneBundle(t, agentsHome, "global", "isp-gate", ispGateManifest("isp-gate"), []byte("#!/bin/sh\nexit 0\n"))
	writePruneBundle(t, agentsHome, "global", "hand-authored", `name: hand-authored
when: stop
run:
  command: /opt/tools/totally-unrelated.sh
`, nil)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}
	assertNoCandidate(t, candidates, "global", "hand-authored")
}

// TestCommandOwnedByOtherBundle_ExactStringMatchWithoutDirectoryContainmentNotTrusted
// is the counterpart to the orphaned-siblings regression: two bundles share
// an identical absolute command that is NOT a path under either bundle's own
// directory (so directory containment resolves neither), and they differ
// enough (different `when`) that the duplicate-manifest signal cannot step
// in either. commandOwnedByOtherBundle must decline for BOTH — trusting the
// bare exact-string match is exactly the signal the orphaned-target
// regression proved unsafe, so neither bundle may be flagged at all here.
func TestCommandOwnedByOtherBundle_ExactStringMatchWithoutDirectoryContainmentNotTrusted(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	writePruneBundle(t, agentsHome, "global", "notify-real", `name: notify-real
when: stop
run:
  command: /opt/tools/notify-done.sh
`, nil)
	writePruneBundle(t, agentsHome, "global", "notify-capture", `name: notify-capture
when: session_start
run:
  command: /opt/tools/notify-done.sh
`, nil)
	backdateManifest(t, filepath.Join(agentsHome, "hooks", "global", "notify-real"), time.Hour)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}
	assertNoCandidate(t, candidates, "global", "notify-real")
	assertNoCandidate(t, candidates, "global", "notify-capture")
}

// TestClassifyImportArtifact_BothSignalsAgreeReportsCommandOwned covers the
// switch branch where both detection signals fire AND agree on the same
// owner: command_owned takes precedence in the reported reason (it is
// checked first), rather than the classification being ambiguous.
func TestClassifyImportArtifact_BothSignalsAgreeReportsCommandOwned(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	script := []byte("#!/bin/sh\nexit 0\n")
	realDir := writePruneBundle(t, agentsHome, "global", "isp-gate", ispGateManifest("isp-gate"), script)

	// A capture whose manifest is byte-identical to isp-gate's content (dup
	// signal) AND whose command is the resolved absolute path into
	// isp-gate's own directory (command signal): both signals should agree
	// on "global/isp-gate".
	captureManifest := `name: isp-gate-capture
when_events:
  - pre_compact
  - stop
run:
  command: ` + filepath.Join(realDir, "gate.sh") + `
enabled_on:
  - claude
`
	writePruneBundle(t, agentsHome, "global", "isp-gate-capture", captureManifest, nil)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}
	got := findCandidate(t, candidates, "global", "isp-gate-capture")
	if got.Reason != ImportArtifactReasonCommandOwned {
		t.Errorf("reason = %q, want %q", got.Reason, ImportArtifactReasonCommandOwned)
	}
	if got.Owner() != "global/isp-gate" {
		t.Errorf("owner = %q, want global/isp-gate", got.Owner())
	}
}

// TestHookManifestIsNewer_UnreadableSourceNotDetermined exercises both stat-
// error branches directly: hookManifestIsNewer must decline to guess a
// direction when either file's mtime cannot be read.
func TestHookManifestIsNewer_UnreadableSourceNotDetermined(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	real := writePruneBundle(t, agentsHome, "global", "isp-gate", ispGateManifest("isp-gate"), nil)
	realSpec := HookSpec{Scope: "global", Name: "isp-gate", SourcePath: filepath.Join(real, "HOOK.yaml")}
	missingSpec := HookSpec{Scope: "global", Name: "ghost", SourcePath: filepath.Join(agentsHome, "hooks", "global", "ghost", "HOOK.yaml")}

	if _, determined := hookManifestIsNewer(missingSpec, realSpec); determined {
		t.Error("expected undetermined when spec's own file is missing")
	}
	if _, determined := hookManifestIsNewer(realSpec, missingSpec); determined {
		t.Error("expected undetermined when other's file is missing")
	}
}

// TestHookBundleScriptBytesEqual_AsymmetricScriptReference covers the "one
// bundle references a local script, the other doesn't" branch directly.
func TestHookBundleScriptBytesEqual_AsymmetricScriptReference(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	scripted := writePruneBundle(t, agentsHome, "global", "scripted", `name: scripted
when: stop
run:
  command: ./gate.sh
`, []byte("#!/bin/sh\nexit 0\n"))
	bare := writePruneBundle(t, agentsHome, "global", "bare", `name: bare
when: stop
run:
  command: true
`, nil)

	scriptedSpec := HookSpec{Scope: "global", Name: "scripted", SourcePath: filepath.Join(scripted, "HOOK.yaml"), Command: "./gate.sh"}
	bareSpec := HookSpec{Scope: "global", Name: "bare", SourcePath: filepath.Join(bare, "HOOK.yaml"), Command: "true"}

	if hookBundleScriptBytesEqual(scriptedSpec, bareSpec) {
		t.Error("expected false when only one side references a local script")
	}
}

// TestBundleRelativeScriptFile_EmptyCommand covers the empty-command guard
// directly.
func TestBundleRelativeScriptFile_EmptyCommand(t *testing.T) {
	if _, ok := bundleRelativeScriptFile(HookSpec{}); ok {
		t.Error("expected no script file for an empty command")
	}
}

// TestCommandOwnedByOtherBundle_EmptyCommand covers the blank-command guard
// directly (a manifest with no run.command at all).
func TestCommandOwnedByOtherBundle_EmptyCommand(t *testing.T) {
	if _, _, ok := commandOwnedByOtherBundle(nil, HookSpec{Scope: "global", Name: "x"}); ok {
		t.Error("expected false for a spec with an empty command")
	}
}

// TestImportArtifactCandidates_SignalsDisagreeIsAmbiguous covers the switch
// branch in classifyImportArtifact where command-ownership and duplicate-
// manifest detection both fire but name DIFFERENT owners: the candidate's
// command resolves into isp-gate's directory, but its manifest content is a
// byte-duplicate of an unrelated bundle ("decoy") that merely happens to
// share the same (absolute, isp-gate-pointing) command text and shape.
func TestImportArtifactCandidates_SignalsDisagreeIsAmbiguous(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	script := []byte("#!/bin/sh\nexit 0\n")
	realDir := writePruneBundle(t, agentsHome, "global", "isp-gate", ispGateManifest("isp-gate"), script)
	absCommand := filepath.Join(realDir, "gate.sh")

	decoyManifest := `name: decoy
when_events:
  - pre_compact
  - stop
run:
  command: ` + absCommand + `
enabled_on:
  - claude
`
	decoyDir := writePruneBundle(t, agentsHome, "global", "decoy", decoyManifest, nil)
	backdateManifest(t, decoyDir, time.Hour) // decoy is the "older" half of its duplicate pair

	captureManifest := `name: capture
when_events:
  - pre_compact
  - stop
run:
  command: ` + absCommand + `
enabled_on:
  - claude
`
	writePruneBundle(t, agentsHome, "global", "capture", captureManifest, nil)

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}
	got := findCandidate(t, candidates, "global", "capture")
	if got.Reason != ImportArtifactReasonAmbiguous {
		t.Fatalf("reason = %q, want ambiguous; detail=%q", got.Reason, got.Detail)
	}
	if got.Owner() != "" {
		t.Errorf("owner = %q, want empty for an ambiguous candidate", got.Owner())
	}
}

// TestImportArtifactCandidates_SortsAcrossScopes exercises the Scope-
// differs branch of the final sort comparator: with candidates in two
// different scopes, cross-scope ordering must be alphabetical by scope.
func TestImportArtifactCandidates_SortsAcrossScopes(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	// Distinct `when` per scope's real bundle keeps the two real bundles
	// from also byte-matching each other (which would add unrelated
	// duplicate_manifest candidates and muddy the ordering assertion).
	scopeWhen := map[string]string{"z-project": "stop", "a-project": "pre_compact"}
	for scope, when := range scopeWhen {
		realDir := writePruneBundle(t, agentsHome, scope, "isp-gate", `name: isp-gate
when: `+when+`
run:
  command: ./gate.sh
`, []byte("#!/bin/sh\nexit 0\n"))
		writePruneBundle(t, agentsHome, scope, "stop-gate", `name: stop-gate
when: `+when+`
run:
  command: `+filepath.Join(realDir, "gate.sh")+`
`, nil)
	}

	candidates, err := ImportArtifactCandidates(agentsHome)
	if err != nil {
		t.Fatalf("ImportArtifactCandidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates across 2 scopes, got %d: %+v", len(candidates), candidates)
	}
	if candidates[0].Scope != "a-project" || candidates[1].Scope != "z-project" {
		t.Fatalf("expected scope-sorted candidates, got %q then %q", candidates[0].Scope, candidates[1].Scope)
	}
}

// TestAllCanonicalHookSpecs_SkipsNonDirEntries covers the "not a directory"
// continue branch: a stray file sitting directly under agentsHome/hooks/
// (not itself a scope directory) must be skipped without erroring.
func TestAllCanonicalHookSpecs_SkipsNonDirEntries(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	writePruneBundle(t, agentsHome, "global", "isp-gate", ispGateManifest("isp-gate"), nil)
	mustWriteFileForPruneTest(t, filepath.Join(agentsHome, "hooks", "stray.txt"), "not a scope directory\n")

	specs, err := allCanonicalHookSpecs(agentsHome)
	if err != nil {
		t.Fatalf("allCanonicalHookSpecs: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "isp-gate" {
		t.Fatalf("expected only the real bundle's spec, got %+v", specs)
	}
}

func mustWriteFileForPruneTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestHookBundleScriptBytesEqual_UnreadableScriptFiles covers both
// os.ReadFile error branches directly: a bundle-relative command that
// references a script file that does not actually exist.
func TestHookBundleScriptBytesEqual_UnreadableScriptFiles(t *testing.T) {
	agentsHome := hooksPruneFixture(t)
	dirA := filepath.Join(agentsHome, "hooks", "global", "a")
	dirB := filepath.Join(agentsHome, "hooks", "global", "b")
	mustMkdirAllT(t, dirA)
	mustMkdirAllT(t, dirB)
	// Neither "a" nor "b" actually has a gate.sh on disk: both reference it
	// explicitly (./gate.sh), which bundleRelativeScriptFile trusts without
	// an existence check, so os.ReadFile must fail for each in turn.
	specA := HookSpec{Scope: "global", Name: "a", SourcePath: filepath.Join(dirA, "HOOK.yaml"), Command: "./gate.sh"}
	specB := HookSpec{Scope: "global", Name: "b", SourcePath: filepath.Join(dirB, "HOOK.yaml"), Command: "./gate.sh"}

	if hookBundleScriptBytesEqual(specA, specB) {
		t.Error("expected false when neither side's referenced script is readable")
	}

	// Now make A's script readable but leave B's missing, to cover the
	// second os.ReadFile error branch independently of the first.
	mustWriteFileForPruneTest(t, filepath.Join(dirA, "gate.sh"), "#!/bin/sh\nexit 0\n")
	if hookBundleScriptBytesEqual(specA, specB) {
		t.Error("expected false when only B's referenced script is unreadable")
	}
}
