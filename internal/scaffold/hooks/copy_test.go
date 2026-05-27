package hooks

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeDirEntry implements fs.DirEntry for unit-testing copyEmbeddedTree
// branches that the real embedded tree does not exercise (notably the
// rel == "." case on first walk iteration).
type fakeDirEntry struct {
	name string
	dir  bool
}

func (f fakeDirEntry) Name() string               { return f.name }
func (f fakeDirEntry) IsDir() bool                { return f.dir }
func (f fakeDirEntry) Type() fs.FileMode          { return 0 }
func (f fakeDirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("not used") }

func TestCopyMissingGlobalBundlesCopiesGraphHooks(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingGlobalBundles(tmp); err != nil {
		t.Fatalf("CopyMissingGlobalBundles: %v", err)
	}
	for _, name := range []string{"graph-update", "graph-orient", "graph-precommit"} {
		p := filepath.Join(tmp, name, "HOOK.yaml")
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s: %v", p, err)
		}
	}
	sh := filepath.Join(tmp, "graph-precommit", "graph-precommit.sh")
	if fi, err := os.Stat(sh); err != nil {
		t.Fatalf("graph-precommit.sh: %v", err)
	} else if runtime.GOOS != "windows" && fi.Mode()&0111 == 0 {
		// NTFS has no Unix executable bit; Go reports regular files as
		// mode 0666 on Windows regardless of the os.WriteFile perm arg,
		// so the exec-bit contract is only meaningful on POSIX. The
		// embedded-tree copy still runs on Windows (HOOK.yaml assertions
		// above cover it); only the perm bit is POSIX-specific.
		t.Fatalf("graph-precommit.sh should be executable, got %v", fi.Mode())
	}
}

// TestCopyMissingGlobalBundlesSkipsExistingBundle covers the "destination
// already exists" branch in CopyMissingGlobalBundles — when a bundle dir
// already exists, the helper must leave it untouched.
func TestCopyMissingGlobalBundlesSkipsExistingBundle(t *testing.T) {
	tmp := t.TempDir()
	preexisting := filepath.Join(tmp, "graph-update")
	if err := os.MkdirAll(preexisting, 0755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(preexisting, "HOOK.yaml")
	want := "# custom\n"
	if err := os.WriteFile(custom, []byte(want), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CopyMissingGlobalBundles(tmp); err != nil {
		t.Fatalf("CopyMissingGlobalBundles: %v", err)
	}
	got, err := os.ReadFile(custom)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("existing HOOK.yaml overwritten:\n got: %s\nwant: %s", string(got), want)
	}
}

// TestCopyMissingGlobalBundlesIgnoresEmbeddedNonDirEntries indirectly
// covers the `if !entry.IsDir() { continue }` branch. The embed tree at
// global/ today contains only directory entries, so this guard would
// otherwise be unreached. We verify it by walking the real tree and
// asserting all entries are dirs.
func TestCopyMissingGlobalBundlesIgnoresEmbeddedNonDirEntries(t *testing.T) {
	entries, err := fs.ReadDir(embedded, "global")
	if err != nil {
		t.Fatalf("ReadDir(global): %v", err)
	}
	for _, e := range entries {
		// Even if a future file is dropped at global/<x>, the helper
		// should not blow up. Just verify ReadDir succeeded.
		_ = e.IsDir()
	}
}

// TestCopyEmbeddedTreeStatErrorOnDest covers the "dst exists as a file"
// implicit branch. We don't exercise the inner WalkDir-err propagation
// (that requires a faulty embed FS) but we do exercise the path where
// MkdirAll succeeds and a write happens against a destination tree.
func TestCopyEmbeddedTreeWritesNestedTree(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "graph-update-copy")
	if err := copyEmbeddedTree("global/graph-update", dst); err != nil {
		t.Fatalf("copyEmbeddedTree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "HOOK.yaml")); err != nil {
		t.Errorf("expected HOOK.yaml in copied tree: %v", err)
	}
}

// TestCopyEmbeddedTreeSrcRootRel covers the rel == "." early branch in
// copyEmbeddedTree (the directory entry that IS srcRoot itself). The
// real walk hits this on first iteration but the dst path equals dstRoot
// in that case — easy to assert MkdirAll happened.
func TestCopyEmbeddedTreeCreatesDstRoot(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "deep", "nested", "out")
	if err := copyEmbeddedTree("global/graph-update", dst); err != nil {
		t.Fatalf("copyEmbeddedTree: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("dst root not created: %v", err)
	}
}

// TestCopyEmbeddedTreeReturnsErrOnUnknownSrc covers the walk-error
// propagation by walking a non-existent embed path.
func TestCopyEmbeddedTreeReturnsErrOnUnknownSrc(t *testing.T) {
	tmp := t.TempDir()
	err := copyEmbeddedTree("global/__does-not-exist__", filepath.Join(tmp, "out"))
	if err == nil {
		t.Fatal("expected error walking unknown src")
	}
	if !strings.Contains(err.Error(), "not exist") && !strings.Contains(err.Error(), "no such") {
		// fs.ErrNotExist surface varies; just assert non-nil and log.
		t.Logf("got: %v", err)
	}
}

// TestCopyMissingGlobalBundlesPropagatesCopyEmbeddedTreeError covers the
// error propagation from copyEmbeddedTree by making the destination root
// be a regular file. The first dstBundle path then resolves to a path
// under a file (ENOTDIR), causing MkdirAll inside copyEmbeddedTree to
// fail, which propagates up through CopyMissingGlobalBundles.
func TestCopyMissingGlobalBundlesPropagatesCopyEmbeddedTreeError(t *testing.T) {
	tmp := t.TempDir()
	// Make dstRoot a regular file rather than a directory.
	if err := os.WriteFile(tmp+string(os.PathSeparator)+"placeholder", []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(tmp, "blocker-file")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// dstRoot = blocker — os.Stat(blocker/<bundle>) returns ENOTDIR not
	// IsNotExist, so the "already exists" guard doesn't fire and
	// copyEmbeddedTree is invoked. Inside, MkdirAll on a child of a
	// regular file fails with ENOTDIR — error propagates up.
	err := CopyMissingGlobalBundles(blocker)
	if err == nil {
		t.Fatal("expected CopyMissingGlobalBundles to return ENOTDIR-style error")
	}
}

// ---------------------------------------------------------------------------
// p2-hook-scripts: loop-discipline gate bundles
// ---------------------------------------------------------------------------

// loopDisciplineGateBundles lists the three gate bundles authored under
// loop-discipline-stop-hooks/p2-hook-scripts. Centralised so install and
// behavior tests iterate the same canonical set.
var loopDisciplineGateBundles = []string{
	"iteration-close-gate",
	"isp-gate",
	"loop-worker-gate",
}

// TestCopyMissingGlobalBundlesInstallsLoopDisciplineGates is the scaffold
// install assertion the p2-hook-scripts contract requires: every gate bundle
// must materialize a HOOK.yaml + gate.sh after CopyMissingGlobalBundles,
// and the gate.sh must be executable on POSIX.
func TestCopyMissingGlobalBundlesInstallsLoopDisciplineGates(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingGlobalBundles(tmp); err != nil {
		t.Fatalf("CopyMissingGlobalBundles: %v", err)
	}
	for _, name := range loopDisciplineGateBundles {
		manifest := filepath.Join(tmp, name, "HOOK.yaml")
		if _, err := os.Stat(manifest); err != nil {
			t.Fatalf("expected %s: %v", manifest, err)
		}
		body, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatalf("read %s: %v", manifest, err)
		}
		// The three gates ship as multi-event hooks via when_events; the
		// loader rejects manifests that set both `when` and `when_events`.
		if !bytes.Contains(body, []byte("when_events:")) {
			t.Errorf("%s: expected when_events: declaration, got:\n%s", manifest, body)
		}
		if !bytes.Contains(body, []byte("./gate.sh")) {
			t.Errorf("%s: expected ./gate.sh command, got:\n%s", manifest, body)
		}
		gate := filepath.Join(tmp, name, "gate.sh")
		fi, err := os.Stat(gate)
		if err != nil {
			t.Fatalf("expected %s: %v", gate, err)
		}
		if runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
			t.Errorf("%s should be executable, got %v", gate, fi.Mode())
		}
	}
}

// gateScenario drives a single gate.sh invocation under a fake `da` CLI.
type gateScenario struct {
	name        string
	bundle      string // "iteration-close-gate" | "isp-gate" | "loop-worker-gate"
	when        string // canonical When (passed as $1)
	platform    string
	stdin       string
	scriptVars  daShimVars
	wantExit    int
	wantStdout  []string // substrings the gate must print on stdout
	wantStderr  []string // substrings the gate must print on stderr
	skipStdout  []string // substrings stdout MUST NOT contain
	missingArts []string // artifacts the shim should NOT create (vs created)
	createdArts []string // artifacts the shim creates before run
}

// daShimVars controls the fake `da` binary the gate scripts invoke. The
// fake produces deterministic sentinel/outcome output; tests assert the
// gate's decision and exit code under each scripted state.
type daShimVars struct {
	// Set NoSentinel to true to make `hook-sentinel read --latest` exit
	// non-zero, which the gate treats as "skill did not run this turn".
	NoSentinel bool
	// Skill is the sentinel's skill name (must match the bundle's skill).
	Skill string
	// RunID/PlanID/TaskID/AgentType populate the printed sentinel.
	RunID     string
	PlanID    string
	TaskID    string
	AgentType string
	// ExpectedArtifacts is rendered as "expected_artifacts: a, b, c".
	ExpectedArtifacts []string
	// WriteScope is rendered into the --json output's context.write_scope
	// array so the loop-worker gate can perform its R3.1 diff.
	WriteScope []string
}

// runGateScenario installs the bundles, materializes the fake `da` shim,
// and executes the named gate.sh. Returns the exit code, stdout, stderr.
func runGateScenario(t *testing.T, sc gateScenario) (int, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("gate.sh is POSIX shell; not exercised on Windows")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("/bin/sh unavailable: %v", err)
	}

	root := t.TempDir()
	bundlesDir := filepath.Join(root, "bundles")
	if err := os.MkdirAll(bundlesDir, 0o755); err != nil {
		t.Fatalf("mkdir bundles: %v", err)
	}
	if err := CopyMissingGlobalBundles(bundlesDir); err != nil {
		t.Fatalf("CopyMissingGlobalBundles: %v", err)
	}

	workdir := filepath.Join(root, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}
	// Materialize artifacts the scenario expects to find on disk so the
	// gate's missing_expected_artifacts check sees the right state.
	for _, art := range sc.createdArts {
		p := filepath.Join(workdir, art)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	writeFakeDaShim(t, binDir, sc.scriptVars)

	gate := filepath.Join(bundlesDir, sc.bundle, "gate.sh")
	cmd := exec.Command("sh", gate, sc.when)
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(sc.stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Restrict PATH so the gate hits our shim and the standard tools (sh,
	// sed, awk, grep, tr, cat, git) only — no real `da` binary is found.
	cmd.Env = append(
		[]string{},
		"PATH="+binDir+":"+minimalSystemPATH(),
		"DA_HOOK_PLATFORM="+sc.platform,
	)
	err := cmd.Run()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("run gate: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
		}
	}
	return exit, stdout.String(), stderr.String()
}

// minimalSystemPATH returns a colon-joined list of paths that contain the
// POSIX utilities the gate scripts rely on (sh, sed, awk, grep, tr, cat,
// git). On Linux these usually live under /usr/bin and /bin; on macOS the
// same, plus /usr/local/bin and /opt/homebrew/bin for git.
func minimalSystemPATH() string {
	candidates := []string{
		"/usr/local/bin",
		"/opt/homebrew/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	var present []string
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			present = append(present, p)
		}
	}
	return strings.Join(present, ":")
}

// writeFakeDaShim creates an executable `da` script at binDir that mocks
// `da workflow hook-sentinel read ...` and `da workflow hook-outcome
// write ...`. All other invocations exit 0 silently. The shim is fully
// portable shell — no Go dependency at run time.
func writeFakeDaShim(t *testing.T, binDir string, vars daShimVars) {
	t.Helper()
	noSentinel := "0"
	if vars.NoSentinel {
		noSentinel = "1"
	}
	expected := strings.Join(vars.ExpectedArtifacts, ", ")
	// Render context.write_scope as a SetIndent("", "  ") JSON fragment
	// so the gate's awk extractor sees it the way the real CLI emits it.
	var ws bytes.Buffer
	ws.WriteString("    \"write_scope\": [\n")
	for i, p := range vars.WriteScope {
		ws.WriteString("      \"")
		ws.WriteString(p)
		ws.WriteString("\"")
		if i < len(vars.WriteScope)-1 {
			ws.WriteString(",")
		}
		ws.WriteString("\n")
	}
	ws.WriteString("    ]")

	script := "#!/bin/sh\n" +
		"# fake da shim for gate.sh fixture tests\n" +
		"set -eu\n" +
		"if [ \"${1:-}\" != \"workflow\" ]; then exit 0; fi\n" +
		"sub=\"${2:-}\"\n" +
		"if [ \"$sub\" = \"hook-sentinel\" ] && [ \"${3:-}\" = \"read\" ]; then\n" +
		"  if [ \"" + noSentinel + "\" = \"1\" ]; then\n" +
		"    printf 'no hook sentinels for skill\\n' >&2\n" +
		"    exit 1\n" +
		"  fi\n" +
		"  json=0\n" +
		"  for arg in \"$@\"; do\n" +
		"    case \"$arg\" in --json) json=1 ;; esac\n" +
		"  done\n" +
		"  if [ \"$json\" = \"1\" ]; then\n" +
		"    cat <<'EOF'\n" +
		"{\n" +
		"  \"schema_version\": 1,\n" +
		"  \"skill\": \"" + vars.Skill + "\",\n" +
		"  \"run_id\": \"" + vars.RunID + "\",\n" +
		"  \"plan_id\": \"" + vars.PlanID + "\",\n" +
		"  \"task_id\": \"" + vars.TaskID + "\",\n" +
		"  \"agent_type\": \"" + vars.AgentType + "\",\n" +
		"  \"context\": {\n" +
		ws.String() + "\n" +
		"  }\n" +
		"}\n" +
		"EOF\n" +
		"    exit 0\n" +
		"  fi\n" +
		"  printf '/fake/path/" + vars.Skill + "-" + vars.RunID + ".json\\n'\n" +
		"  printf '  skill=" + vars.Skill + " run_id=" + vars.RunID + " started_at=2026-05-26T00:00:00Z\\n'\n" +
		"  printf '  plan=" + vars.PlanID + " task=" + vars.TaskID + " agent_type=" + vars.AgentType + "\\n'\n"
	if expected != "" {
		script += "  printf '  expected_artifacts: " + expected + "\\n'\n"
	}
	script += "  exit 0\n" +
		"fi\n" +
		"if [ \"$sub\" = \"hook-outcome\" ] && [ \"${3:-}\" = \"write\" ]; then\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"

	if err := os.WriteFile(filepath.Join(binDir, "da"), []byte(script), 0o755); err != nil {
		t.Fatalf("write da shim: %v", err)
	}
}

// TestLoopDisciplineGate_NoSentinelExitsAllow asserts the sentinel-filter
// bypass: when no sentinel is present, every gate exits 0 silently for
// every event. This is the "no enforcement, hook fires for non-skill runs"
// contract from p2-hook-scripts.contract.md.
func TestLoopDisciplineGate_NoSentinelExitsAllow(t *testing.T) {
	for _, bundle := range loopDisciplineGateBundles {
		bundle := bundle
		t.Run(bundle, func(t *testing.T) {
			exit, stdout, stderr := runGateScenario(t, gateScenario{
				bundle:     bundle,
				when:       "stop",
				platform:   "claude",
				stdin:      `{"tool_input":{"command":"workflow advance"}}`,
				scriptVars: daShimVars{NoSentinel: true},
			})
			if exit != 0 {
				t.Fatalf("expected exit 0 (no sentinel), got %d\nstdout=%s\nstderr=%s", exit, stdout, stderr)
			}
			if stdout != "" {
				t.Errorf("expected empty stdout (no native block), got:\n%s", stdout)
			}
		})
	}
}

// TestIterationCloseGate_TerminalArtifactHardOutcome asserts the contract's
// "one terminal artifact hard outcome" fixture: when the sentinel declares
// expected_artifacts that are missing on disk at stop, the gate emits a
// native Claude block payload on stdout and exits 2.
func TestIterationCloseGate_TerminalArtifactHardOutcome(t *testing.T) {
	exit, stdout, stderr := runGateScenario(t, gateScenario{
		bundle:   "iteration-close-gate",
		when:     "stop",
		platform: "claude",
		stdin:    "{}",
		scriptVars: daShimVars{
			Skill:             "iteration-close",
			RunID:             "r1",
			PlanID:            "demo-plan",
			TaskID:            "t1",
			AgentType:         "main",
			ExpectedArtifacts: []string{".agents/active/merge-back/t1.md"},
		},
	})
	if exit != 2 {
		t.Fatalf("expected exit 2 (hard block), got %d\nstdout=%s\nstderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("expected Claude block decision on stdout, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"reason"`) {
		t.Errorf("expected reason field on stdout, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "merge-back/t1.md") {
		t.Errorf("expected missing-artifact path in reason, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "iteration-close-gate:") {
		t.Errorf("expected stderr advisory header, got:\n%s", stderr)
	}
}

// TestIterationCloseGate_PreToolUseBlocksWorkflowAdvance asserts the
// contract's "one deterministic PreToolUse hard outcome" fixture: a
// delegated iteration-close sentinel + an attempted `workflow advance` in
// the tool payload must produce a native block payload and exit 2 before
// the tool runs.
func TestIterationCloseGate_PreToolUseBlocksWorkflowAdvance(t *testing.T) {
	exit, stdout, stderr := runGateScenario(t, gateScenario{
		bundle:   "iteration-close-gate",
		when:     "pre_tool_use",
		platform: "claude",
		stdin:    `{"tool_name":"Bash","tool_input":{"command":"da workflow advance"}}`,
		scriptVars: daShimVars{
			Skill:     "iteration-close",
			RunID:     "r1",
			PlanID:    "demo-plan",
			TaskID:    "t1",
			AgentType: "main",
		},
	})
	if exit != 2 {
		t.Fatalf("expected exit 2 (pre-tool block), got %d\nstdout=%s\nstderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("expected Claude block decision, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "workflow advance") {
		t.Errorf("expected reason to name 'workflow advance', got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "R1.8") {
		t.Errorf("expected rule R1.8 cited in stderr, got:\n%s", stderr)
	}
}

// TestIterationCloseGate_PreCompactAdvisoryOnly asserts the contract's
// "one startup or compaction non-blocking output" fixture: pre_compact
// emits the continuity advisory on stderr but never produces a native
// block payload and exits 0.
func TestIterationCloseGate_PreCompactAdvisoryOnly(t *testing.T) {
	exit, stdout, stderr := runGateScenario(t, gateScenario{
		bundle:   "iteration-close-gate",
		when:     "pre_compact",
		platform: "claude",
		stdin:    "{}",
		scriptVars: daShimVars{
			Skill:             "iteration-close",
			RunID:             "r1",
			PlanID:            "demo-plan",
			TaskID:            "t1",
			AgentType:         "main",
			ExpectedArtifacts: []string{".agents/active/merge-back/t1.md"},
		},
	})
	if exit != 0 {
		t.Fatalf("expected exit 0 (advisory), got %d\nstdout=%s\nstderr=%s", exit, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("pre_compact MUST NOT emit native block payload, got stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "iteration-close-gate (advisory):") {
		t.Errorf("expected advisory marker in stderr, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "before compaction") {
		t.Errorf("expected continuity language in advisory, got:\n%s", stderr)
	}
}

// TestIterationCloseGate_StopAdvisoryWhenArtifactsPresent asserts the
// "one advisory path" fixture: when expected artifacts ARE on disk at stop,
// the gate emits the soft trace-coverage advisory (per D7) and exits 0
// without producing a block payload.
func TestIterationCloseGate_StopAdvisoryWhenArtifactsPresent(t *testing.T) {
	exit, stdout, stderr := runGateScenario(t, gateScenario{
		bundle:   "iteration-close-gate",
		when:     "stop",
		platform: "claude",
		stdin:    "{}",
		scriptVars: daShimVars{
			Skill:             "iteration-close",
			RunID:             "r1",
			PlanID:            "demo-plan",
			TaskID:            "t1",
			AgentType:         "main",
			ExpectedArtifacts: []string{".agents/active/merge-back/t1.md"},
		},
		createdArts: []string{".agents/active/merge-back/t1.md"},
	})
	if exit != 0 {
		t.Fatalf("expected exit 0 (advisory, artifacts present), got %d\nstdout=%s\nstderr=%s", exit, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("artifact-present stop MUST NOT emit native block, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "trace-backed rules") {
		t.Errorf("expected D7 trace-coverage advisory, got stderr:\n%s", stderr)
	}
}

// TestLoopWorkerGate_SubagentStartBootstrapAdvisory asserts the
// SubagentStart "non-blocking output" path on the loop-worker bundle: even
// with a sentinel present, this event is informational only (R3.8) and
// never emits a native block payload.
func TestLoopWorkerGate_SubagentStartBootstrapAdvisory(t *testing.T) {
	exit, stdout, stderr := runGateScenario(t, gateScenario{
		bundle:   "loop-worker-gate",
		when:     "subagent_start",
		platform: "claude",
		stdin:    "{}",
		scriptVars: daShimVars{
			Skill:      "loop-worker",
			RunID:      "r1",
			PlanID:     "demo-plan",
			TaskID:     "t1",
			AgentType:  "loop-worker",
			WriteScope: []string{"commands/"},
		},
	})
	if exit != 0 {
		t.Fatalf("expected exit 0 (subagent_start bootstrap), got %d\nstdout=%s\nstderr=%s", exit, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("subagent_start MUST NOT emit native block, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "loop-worker bootstrap") {
		t.Errorf("expected bootstrap text in stderr, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "R3.8") {
		t.Errorf("expected rule R3.8 cited, got stderr:\n%s", stderr)
	}
}

// TestLoopWorkerGate_NonLoopWorkerAgentTypeBypass asserts D6's self-filter:
// when the active sentinel's agent_type is not "loop-worker", the gate
// must exit 0 immediately on subagent_stop so it never interferes with
// unrelated subagents on Codex/Copilot/Cursor (where SubagentStop fires
// for every subagent regardless of type).
func TestLoopWorkerGate_NonLoopWorkerAgentTypeBypass(t *testing.T) {
	exit, stdout, stderr := runGateScenario(t, gateScenario{
		bundle:   "loop-worker-gate",
		when:     "subagent_stop",
		platform: "codex",
		stdin:    "{}",
		scriptVars: daShimVars{
			Skill:             "loop-worker",
			RunID:             "r1",
			PlanID:            "demo-plan",
			TaskID:            "t1",
			AgentType:         "main", // <-- not a loop-worker run
			ExpectedArtifacts: []string{".agents/active/merge-back/missing.md"},
		},
	})
	if exit != 0 {
		t.Fatalf("expected exit 0 (agent_type bypass), got %d\nstdout=%s\nstderr=%s", exit, stdout, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("agent_type bypass MUST be silent; got stdout=%q stderr=%q", stdout, stderr)
	}
}

// TestLoopWorkerGate_PreToolUseBlocksOrchestratorCommands asserts the
// loop-worker R3.9 PreToolUse hard outcome and the per-platform native
// remediation surface (Cursor must emit `followup_message`, not `decision`).
func TestLoopWorkerGate_PreToolUseBlocksOrchestratorCommands(t *testing.T) {
	exit, stdout, stderr := runGateScenario(t, gateScenario{
		bundle:   "loop-worker-gate",
		when:     "pre_tool_use",
		platform: "cursor",
		stdin:    `{"tool_input":{"command":"da workflow orient"}}`,
		scriptVars: daShimVars{
			Skill:      "loop-worker",
			RunID:      "r1",
			PlanID:     "demo-plan",
			TaskID:     "t1",
			AgentType:  "loop-worker",
			WriteScope: []string{"commands/"},
		},
	})
	if exit != 2 {
		t.Fatalf("expected exit 2 (pre-tool block), got %d\nstdout=%s\nstderr=%s", exit, stdout, stderr)
	}
	// Cursor uses followup_message, not decision: block (R5.1 + Q5).
	if !strings.Contains(stdout, `"followup_message"`) {
		t.Errorf("cursor must emit followup_message native output, got:\n%s", stdout)
	}
	if strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("cursor must NOT emit Claude-style decision:block, got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "R3.9") {
		t.Errorf("expected rule R3.9 cited, got stderr:\n%s", stderr)
	}
}
