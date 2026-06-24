package home

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// fakeDirEntry implements fs.DirEntry for unit-testing copyStarterEntry
// branches that the real embedded tree does not exercise (notably the .sh
// suffix mode-bump and the rel == "." early-return).
type fakeDirEntry struct {
	name string
	dir  bool
}

func (f fakeDirEntry) Name() string               { return f.name }
func (f fakeDirEntry) IsDir() bool                { return f.dir }
func (f fakeDirEntry) Type() fs.FileMode          { return 0 }
func (f fakeDirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("not used") }

func TestCopyMissingStarterAssetsCopiesStarterBundle(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	for _, rel := range []string{
		".gitignore",
		"README.md",
		"rules/global/rules.mdc",
		"settings/global/claude-code.json",
		"skills/global/agent-start/SKILL.md",
		"skills/global/review-pr/templates/review-output.md",
		// P3 starter-promotion: iteration-close, isp, loop-worker skills
		// plus the loop-worker agent and profile must land via the same
		// embedded-walk path. New top-level descendants under starter/
		// (agents/, profiles/) require representative assertions because
		// `da init` relies on them being copied without loader changes.
		"skills/global/iteration-close/SKILL.md",
		"skills/global/iteration-close/instructions/workflow.md",
		"skills/global/iteration-close/instructions/gotchas.md",
		"skills/global/iteration-close/instructions/proposal-criteria.md",
		"skills/global/iteration-close/scripts/propose.sh",
		"skills/global/iteration-close/templates/self-assessment-line.md",
		"skills/global/isp/SKILL.md",
		"skills/global/isp/instructions/orientation.md",
		"skills/global/isp/instructions/task-selection.md",
		"skills/global/isp/instructions/direct-vs-fanout.md",
		"skills/global/isp/instructions/fanout.md",
		"skills/global/isp/instructions/staged-runtime.md",
		"skills/global/loop-worker/SKILL.md",
		"skills/global/loop-worker/instructions/startup.md",
		"skills/global/loop-worker/instructions/gotchas.md",
		"agents/global/loop-worker/AGENT.md",
		"profiles/loop-worker.md",
		// P3b companion discipline skills: agent-handoff and
		// delegation-lifecycle ship their complete starter trees, not just
		// SKILL.md. Representative instruction/template files are asserted so
		// a partial copy (the agent-handoff stub regression) fails the build.
		"skills/global/agent-handoff/SKILL.md",
		"skills/global/agent-handoff/instructions/modes.md",
		"skills/global/agent-handoff/instructions/create-mode.md",
		"skills/global/agent-handoff/instructions/gotchas.md",
		"skills/global/agent-handoff/templates/ai-agent.md",
		"skills/global/agent-handoff/templates/coworker.md",
		"skills/global/agent-handoff/templates/self-later.md",
		"skills/global/delegation-lifecycle/SKILL.md",
		"skills/global/delegation-lifecycle/instructions/workflow.md",
		"skills/global/delegation-lifecycle/instructions/gotchas.md",
		"skills/global/delegation-lifecycle/instructions/bundle-to-execution.md",
	} {
		if _, err := os.Stat(filepath.Join(tmp, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}
}

func assertStarterText(t *testing.T, root, rel string, required, forbidden []string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read starter asset %s: %v", rel, err)
	}
	text := string(content)
	for _, needle := range required {
		if !strings.Contains(text, needle) {
			t.Errorf("%s missing required text %q", rel, needle)
		}
	}
	for _, needle := range forbidden {
		if strings.Contains(text, needle) {
			t.Errorf("%s retains forbidden legacy/staged drift %q", rel, needle)
		}
	}
}

func TestStarterStagedDispatchDoesNotInjectLegacyCloseoutSurface(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}

	assertStarterText(t, tmp, "profiles/loop-worker.md",
		[]string{"legacy/full-slice compatibility only", "direct, non-delegated completion path"},
		[]string{"After accepting delegate output: `workflow advance`"})
	assertStarterText(t, tmp, "agents/global/loop-worker/AGENT.md",
		[]string{"legacy/full-slice compatibility worker", "Do not use this agent for typed ISP"},
		[]string{"performs exactly one stage (impl, verify, or review)"})
	assertStarterText(t, tmp, "skills/global/isp/instructions/fanout.md",
		[]string{"Do not inject `.agents/active/active.loop.md`", "compatibility metadata"},
		[]string{"--project-overlay .agents/active/active.loop.md", "--prompt-file .agents/prompts/loop-worker.project.md"})
	assertStarterText(t, tmp, "skills/global/isp/instructions/staged-runtime.md",
		[]string{"Accepted delegation closeout completes the delegated task"},
		[]string{"After accepted closeout, run canonical advancement."})
	assertStarterText(t, tmp, "skills/global/iteration-close/instructions/workflow.md",
		[]string{"Accepted delegation closeout", "reserved\nfor direct, non-delegated completion"},
		[]string{"`workflow advance` and `workflow delegation closeout` after review"})
}

// TestCopyMissingStarterAssetsSetsExecBitOnEmbeddedShScripts asserts that
// real `.sh` scripts in the embedded starter (e.g. iteration-close's
// propose.sh promoted in P3) land with the POSIX exec bit set. The generic
// TestCopyStarterEntryShSuffixSetsExecBit covers the branch logic with a
// synthetic dir-entry; this test pins the contract for the real starter
// shipped to `da init` consumers.
func TestCopyMissingStarterAssetsSetsExecBitOnEmbeddedShScripts(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Same [genuine-posix] classification as
		// TestCopyStarterEntryShSuffixSetsExecBit — NTFS has no per-user
		// exec mode bit, and the scaffolder's exec-bit branch is a no-op
		// on Windows by design.
		t.Skipf("exec bit not meaningful on %s", runtime.GOOS)
	}
	tmp := t.TempDir()
	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	script := filepath.Join(tmp, "skills", "global", "iteration-close", "scripts", "propose.sh")
	fi, err := os.Stat(script)
	if err != nil {
		t.Fatalf("stat propose.sh: %v", err)
	}
	if fi.Mode().Perm()&0111 == 0 {
		t.Errorf("expected exec bit on embedded propose.sh; got mode %v", fi.Mode())
	}
}

// starterVerifierSurface is the canonical set of verifier_profile IDs the
// starter's isp prompt files are allowed to reference. It mirrors the
// "Verifier prompt surfaces" enumeration in
// starter/skills/global/isp/instructions/staged-runtime.md and the registry
// shape consumed by `commands/workflow/delegation.go`'s
// `validateVerifierProfileRefs`. When a new verifier surface is added to a
// starter prompt, it must also be added here; conversely, removing one here
// without dropping its prompt references will surface the stranded ref.
//
// This is the test-side analog of the `verifier_profiles` map an installed
// project carries in its `.agentsrc.json`. The starter itself does not
// (today) ship a `.agentsrc.json` template, so the canonical surface for the
// scaffold-level cross-reference lives here — see
// [[verifier-owns-ci-watch-shift-left]] for the broader contract.
var starterVerifierSurface = map[string]bool{
	"unit":      true,
	"api":       true,
	"ui-e2e":    true,
	"batch":     true,
	"streaming": true,
}

// verifierSurfaceListPattern matches the inline backtick-quoted enumeration
// of verifier IDs in starter prompts, e.g. the
// "Verifier prompt surfaces: `unit`, `api`, `ui-e2e`, ..." line in
// staged-runtime.md. The regex captures one ID per match; the test iterates
// all matches across all walked files.
var verifierSurfaceListPattern = regexp.MustCompile("Verifier prompt surfaces:\\s*((?:`[a-z0-9-]+`(?:,\\s*)?)+)")

// verifierIDInListPattern extracts each backticked ID from a captured list
// (the inner group of verifierSurfaceListPattern).
var verifierIDInListPattern = regexp.MustCompile("`([a-z0-9-]+)`")

// verifiersPromptPathPattern matches references to the
// `.agents/prompts/verifiers/<id>.project.md` overlay path that appears in
// staged-runtime.md and in any future prompt that names a concrete verifier
// surface. The `<type>` placeholder used in current prose is filtered out by
// the lookup below; only resolved IDs are checked.
var verifiersPromptPathPattern = regexp.MustCompile(`prompts/verifiers/([a-z0-9-]+)\.project\.md`)

// verifierRef pairs an extracted verifier_profile ID with the relative
// starter prompt file it was sourced from. Used by the cross-reference test
// helpers to report stranded refs with file context.
type verifierRef struct {
	id   string
	file string
}

// extractVerifierRefsFromText pulls every verifier_profile ID reference out
// of a single prompt file's text. Two surfaces are scanned: the inline
// backtick-quoted enumeration (verifierSurfaceListPattern) and the resolved
// `.agents/prompts/verifiers/<id>.project.md` path (verifiersPromptPathPattern).
func extractVerifierRefsFromText(text, rel string) []verifierRef {
	var refs []verifierRef
	// Capture IDs from the "Verifier prompt surfaces: `a`, `b`, ..." line.
	for _, listMatch := range verifierSurfaceListPattern.FindAllStringSubmatch(text, -1) {
		for _, idMatch := range verifierIDInListPattern.FindAllStringSubmatch(listMatch[1], -1) {
			refs = append(refs, verifierRef{id: idMatch[1], file: rel})
		}
	}
	// Capture IDs from any resolved `.agents/prompts/verifiers/<id>.project.md`
	// path reference. The literal `<type>` placeholder used in prose is
	// not a regex match (the character class excludes `<` and `>`) so it
	// is silently filtered out — only resolved IDs land in refs.
	for _, pathMatch := range verifiersPromptPathPattern.FindAllStringSubmatch(text, -1) {
		refs = append(refs, verifierRef{id: pathMatch[1], file: rel})
	}
	return refs
}

// collectVerifierRefs walks the starter skills tree under root and returns
// every verifier_profile reference extracted from markdown prompt files.
// Non-markdown files (templates, scripts) are skipped because they are not
// part of the prompt-reference surface.
func collectVerifierRefs(root, tmp string) ([]verifierRef, error) {
	var refs []verifierRef
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(tmp, path)
		refs = append(refs, extractVerifierRefsFromText(string(content), rel)...)
		return nil
	})
	return refs, walkErr
}

// findStrandedVerifierRefs returns the deduplicated, sorted list of
// "<id> @ <file>" entries whose ID is not present in the canonical
// starterVerifierSurface map.
func findStrandedVerifierRefs(refs []verifierRef) []string {
	var stranded []string
	seen := map[string]bool{}
	for _, r := range refs {
		if starterVerifierSurface[r.id] {
			continue
		}
		key := r.id + " @ " + r.file
		if seen[key] {
			continue
		}
		seen[key] = true
		stranded = append(stranded, key)
	}
	sort.Strings(stranded)
	return stranded
}

// TestStarterVerifierSurfaceCrossReference walks every starter prompt file
// shipped via CopyMissingStarterAssets and asserts that every verifier_profile
// ID referenced by name resolves to an entry in `starterVerifierSurface` —
// the test-side mirror of the `verifier_profiles` registry an installed
// project carries in `.agentsrc.json`.
//
// This is the verifier-surface companion to
// TestCopyStarterAssetsIncludesReviewerLensAgents: that test prevents
// stranded reviewer-lens AGENT.md refs; this test prevents stranded
// verifier_profile refs. Together they enforce that scaffold-shipped prompts
// never point at non-existent dispatched roles.
//
// Per [[verifier-owns-ci-watch-shift-left]]: the verifier_profiles registry
// is the canonical source of "what verifier runs when"; prompts reference by
// ID; this test enforces that invariant at scaffold-build time.
func TestStarterVerifierSurfaceCrossReference(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}

	refs, err := collectVerifierRefs(filepath.Join(tmp, "skills", "global"), tmp)
	if err != nil {
		t.Fatalf("walk starter skills: %v", err)
	}
	if len(refs) == 0 {
		// Sentinel: if the regexes stop matching the prompts entirely the
		// test silently becomes a no-op. Fail loudly so a future prompt
		// rewrite is forced to update the extraction patterns.
		t.Fatal("no verifier_profile references extracted from starter prompts; extraction regexes may be stale")
	}
	if stranded := findStrandedVerifierRefs(refs); len(stranded) > 0 {
		t.Fatalf("starter prompt files reference verifier_profile IDs not present in starterVerifierSurface (potential stranded refs after scaffold drift); either add the ID to starterVerifierSurface and the project's verifier_profiles registry, or drop the prompt reference:\n  - %s",
			strings.Join(stranded, "\n  - "))
	}
}

// TestCopyStarterAssetsIncludesReviewerLensAgents asserts the named
// reviewer-lens agent definitions (architecture-standards, acceptance-invariants,
// adversarial, cross-harness-adversarial) land via CopyMissingStarterAssets. Each lens is a separately
// spawnable bounded reviewer per the staged-dispatch contract, and the
// starter is the canonical distribution channel. Missing files here would
// silently degrade the staged-runtime review stage to inlined-prose lookup
// against the loop-worker profile only.
func TestCopyStarterAssetsIncludesReviewerLensAgents(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	for _, lens := range []string{"architecture-standards", "acceptance-invariants", "adversarial", "cross-harness-adversarial"} {
		path := filepath.Join(tmp, "agents", "global", lens+"-reviewer", "AGENT.md")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected reviewer lens agent file missing: %s (err: %v)", path, err)
		}
	}
}

func TestCopyMissingStarterAssetsPreservesExistingFiles(t *testing.T) {
	tmp := t.TempDir()
	skill := filepath.Join(tmp, "skills", "global", "agent-start", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatal(err)
	}
	want := "# custom\n"
	if err := os.WriteFile(skill, []byte(want), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("starter skill overwritten:\n got: %s\nwant: %s", string(got), want)
	}
}

// TestCopyStarterEntryRelDotSkips covers the rel == "." early-return at the
// top of the walk function — that branch never fires through normal
// CopyMissingStarterAssets because filepath.Rel returns "." only for the
// root, and the WalkDir callback skips the directory itself in practice.
func TestCopyStarterEntryRelDotSkips(t *testing.T) {
	tmp := t.TempDir()
	err := copyStarterEntry(tmp, "starter", fakeDirEntry{name: "starter", dir: true})
	if err != nil {
		t.Fatalf("copyStarterEntry(starter): %v", err)
	}
	// Should not have created tmp/starter — rel == "." causes a no-op return.
	if _, err := os.Stat(filepath.Join(tmp, "starter")); err == nil {
		t.Errorf("expected no tmp/starter created when rel == .")
	}
}

// TestCopyStarterEntryShSuffixSetsExecBit covers the .sh-suffix branch of
// copyStarterEntry by routing a synthetic dir-entry through the real
// embedded read. The embedded tree has no .sh files of its own, so this
// branch is otherwise unreachable.
func TestCopyStarterEntryShSuffixSetsExecBit(t *testing.T) {
	// POSIX-only: this test asserts the 0o755 exec-bit is set on copied
	// `.sh` files. The POSIX exec bit has no Windows analog — NTFS does
	// not encode per-user execute permission as a file mode bit, and Go's
	// os.Stat on Windows synthesizes mode bits from file attributes
	// (FILE_ATTRIBUTE_READONLY → 0o444 vs 0o666), never 0o755.
	//
	// Classification: [genuine-posix] (see
	// .agents/workflow/plans/cross-platform-test-skips-audit/ and the
	// catalogue findings.md entry for copy_test.go:89). This is NOT a
	// shortcut that a testutil helper can paper over — the assertion
	// itself is about a POSIX-only semantic. Do NOT try to "abstract"
	// this skip away; doing so would change what the test asserts.
	//
	// The matching scaffolder behavior is intentionally a no-op on
	// Windows (the exec-bit branch in copyStarterEntry has no effect on
	// NTFS), so there is no Windows-side assertion to cover here.
	if runtime.GOOS == "windows" {
		t.Skip("file modes differ on windows")
	}
	tmp := t.TempDir()
	// Pick a real embedded file path, but pretend its base name ends in .sh.
	// We borrow README.md content via embedded.ReadFile in copyStarterEntry.
	srcPath := "starter/README.md"
	// Force the destination filename to end in .sh so the mode branch fires.
	// copyStarterEntry computes dstPath from filepath.Rel("starter", path);
	// path "starter/x.sh" yields rel "x.sh" — but we need that path to exist
	// in the embed FS, so we pass d.Name() ending in .sh while srcPath is
	// a real file. The function only consults d.Name() for the suffix check
	// and uses `path` for embedded.ReadFile.
	err := copyStarterEntry(tmp, srcPath, fakeDirEntry{name: "fake.sh"})
	if err != nil {
		t.Fatalf("copyStarterEntry: %v", err)
	}
	// rel of srcPath under starter/ is README.md, so file is written there.
	dst := filepath.Join(tmp, "README.md")
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if fi.Mode().Perm()&0111 == 0 {
		t.Errorf("expected exec bit on .sh-named entry; got mode %v", fi.Mode())
	}
}

// TestCopyStarterEntryStatErrorPropagates covers the non-IsNotExist branch
// in copyStarterEntry by passing a destination path whose parent component
// is a regular file rather than a directory — os.Stat then returns
// ENOTDIR, which is not os.IsNotExist.
func TestCopyStarterEntryStatErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	// Create a regular file at <tmp>/blocker so any child stat returns ENOTDIR.
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// dstRoot = blocker means filepath.Join(blocker, rel) becomes
	// blocker/README.md and os.Stat on that returns ENOTDIR.
	err := copyStarterEntry(blocker, "starter/README.md", fakeDirEntry{name: "README.md"})
	if err == nil {
		t.Fatal("expected error from stat on path under a regular file")
	}
	if strings.Contains(err.Error(), "no such") {
		t.Errorf("got IsNotExist style error; wanted other (ENOTDIR/etc): %v", err)
	}
}

// checkPathIsCrossPlatformSafe verifies that a single path is cross-platform
// safe: absolute, lexically clean, with no double separators, and using the
// host OS's native separator style.
func checkPathIsCrossPlatformSafe(t *testing.T, path string, doubleSep string) {
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
	if cleaned := filepath.Clean(path); cleaned != path {
		t.Errorf("path not lexically clean: got %q, want %q", path, cleaned)
	}
	if strings.Contains(path, doubleSep) {
		t.Errorf("path contains double separator %q: %q", doubleSep, path)
	}
	// On Windows the native separator is `\`; a forward slash in a
	// path emitted by the scaffolder indicates an embed-FS path
	// leaked through without filepath.FromSlash conversion. On
	// POSIX `/` is the native separator and a `\` would be the
	// inverse smell (rare, but check both for symmetry).
	if runtime.GOOS == "windows" && strings.Contains(path, "/") {
		t.Errorf("windows path contains forward slash: %q", path)
	}
	if runtime.GOOS != "windows" && strings.Contains(path, `\`) {
		t.Errorf("posix path contains backslash: %q", path)
	}
}

// TestCopyStarterAssetsPathsAreCrossPlatform walks the tree produced by
// CopyMissingStarterAssets and asserts that every emitted path is
// cross-platform safe: absolute, lexically clean (no `..` or double
// separators), and using the host OS's native separator (no mixed
// slash styles). This guards against a regression where an embed-FS
// path (which always uses `/`) leaks into the destination unchanged on
// Windows, producing paths like `C:\tmp\skills/global/foo` that break
// downstream tooling.
func TestCopyStarterAssetsPathsAreCrossPlatform(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	sep := string(filepath.Separator)
	doubleSep := sep + sep
	walkErr := filepath.WalkDir(tmp, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		checkPathIsCrossPlatformSafe(t, path, doubleSep)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
}
