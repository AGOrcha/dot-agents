package hooks

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// session_handoff_test.go covers the p7 session-handoff hook bundles:
//   - the scaffold install assertion (both bundles materialize HOOK.yaml + an
//     executable script), and
//   - the BEST-EFFORT behavior contract: a journal failure (or an absent `da`)
//     must NEVER block compaction or session start, the snapshot stays silent
//     on stdout (its output would be discarded by compaction anyway), and the
//     recover view is surfaced on stdout so it re-enters the fresh context.

var sessionHandoffBundles = []struct {
	dir    string
	script string
	when   string // canonical event the manifest declares (documentation only)
}{
	{dir: "session-handoff-snapshot", script: "snapshot.sh", when: "pre_compact"},
	{dir: "session-handoff-recover", script: "recover.sh", when: "session_start"},
}

// TestCopyMissingGlobalBundlesInstallsSessionHandoff asserts both bundles ship
// a HOOK.yaml plus an executable script after a scaffold install.
func TestCopyMissingGlobalBundlesInstallsSessionHandoff(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingGlobalBundles(tmp); err != nil {
		t.Fatalf("CopyMissingGlobalBundles: %v", err)
	}
	for _, b := range sessionHandoffBundles {
		manifest := filepath.Join(tmp, b.dir, "HOOK.yaml")
		body, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatalf("read %s: %v", manifest, err)
		}
		if !bytes.Contains(body, []byte("when: "+b.when)) {
			t.Errorf("%s: expected when: %s, got:\n%s", manifest, b.when, body)
		}
		if !bytes.Contains(body, []byte("./"+b.script)) {
			t.Errorf("%s: expected ./%s command, got:\n%s", manifest, b.script, body)
		}
		script := filepath.Join(tmp, b.dir, b.script)
		fi, err := os.Stat(script)
		if err != nil {
			t.Fatalf("expected %s: %v", script, err)
		}
		if runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
			t.Errorf("%s should be executable, got %v", script, fi.Mode())
		}
	}
}

// sessionHandoffScenario drives one script invocation under a fake `da` shim.
type sessionHandoffScenario struct {
	dir        string
	script     string
	daPresent  bool   // when false, no `da` is on PATH
	journalOK  bool   // when false, `da workflow journal <verb>` exits non-zero
	verb       string // "snapshot" | "recover" — the verb the shim prints for
	wantStdout []string
	skipStdout []string
	wantStderr []string
	skipStderr []string
}

// writeFakeJournalDaShim writes a `da` script that answers `workflow journal
// --help` (exit 0) and `workflow journal <verb>` by printing a recognizable
// marker on stdout, controllably succeeding or failing.
func writeFakeJournalDaShim(t *testing.T, binDir, verb string, journalOK bool) {
	t.Helper()
	exitCode := "0"
	if !journalOK {
		exitCode = "1"
	}
	script := "#!/bin/sh\n" +
		"set -u\n" +
		"if [ \"${1:-}\" != \"workflow\" ] || [ \"${2:-}\" != \"journal\" ]; then exit 0; fi\n" +
		"if [ \"${3:-}\" = \"--help\" ]; then exit 0; fi\n" +
		"if [ \"${3:-}\" = \"" + verb + "\" ]; then\n" +
		"  printf 'DA-JOURNAL-" + verb + "-OUTPUT\\n'\n" +
		"  exit " + exitCode + "\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "da"), []byte(script), 0o755); err != nil {
		t.Fatalf("write da shim: %v", err)
	}
}

// runSessionHandoffScenario installs the bundles, optionally materializes the
// fake `da`, and runs the named script. Returns exit code, stdout, stderr.
func runSessionHandoffScenario(t *testing.T, sc sessionHandoffScenario) (int, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell hooks are not exercised on Windows")
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

	pathEnv := minimalSystemPATH()
	if sc.daPresent {
		binDir := filepath.Join(root, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}
		writeFakeJournalDaShim(t, binDir, sc.verb, sc.journalOK)
		pathEnv = binDir + ":" + pathEnv
	}

	script := filepath.Join(bundlesDir, sc.dir, sc.script)
	cmd := exec.Command("sh", script)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = []string{"PATH=" + pathEnv, "CLAUDE_PROJECT_DIR=" + workdir}

	exit := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("run %s: %v", sc.script, err)
		}
	}
	return exit, stdout.String(), stderr.String()
}

func TestSessionHandoffScriptsBestEffort(t *testing.T) {
	cases := []struct {
		name string
		sc   sessionHandoffScenario
	}{
		{
			name: "snapshot success stays silent on stdout",
			sc: sessionHandoffScenario{
				dir: "session-handoff-snapshot", script: "snapshot.sh",
				daPresent: true, journalOK: true, verb: "snapshot",
				skipStdout: []string{"DA-JOURNAL"},
				skipStderr: []string{"warning"},
			},
		},
		{
			name: "snapshot failure never blocks compaction",
			sc: sessionHandoffScenario{
				dir: "session-handoff-snapshot", script: "snapshot.sh",
				daPresent: true, journalOK: false, verb: "snapshot",
				skipStdout: []string{"DA-JOURNAL"},
				wantStderr: []string{"snapshot failed", "compaction not blocked"},
			},
		},
		{
			name: "recover success surfaces the view on stdout",
			sc: sessionHandoffScenario{
				dir: "session-handoff-recover", script: "recover.sh",
				daPresent: true, journalOK: true, verb: "recover",
				wantStdout: []string{"DA-JOURNAL-recover-OUTPUT"},
				skipStderr: []string{"warning"},
			},
		},
		{
			name: "recover failure never blocks session start and never leaks a partial view",
			sc: sessionHandoffScenario{
				dir: "session-handoff-recover", script: "recover.sh",
				daPresent: true, journalOK: false, verb: "recover",
				// The shim prints a marker on stdout BEFORE its non-zero exit;
				// the hook must NOT re-inject that partial/failed view.
				skipStdout: []string{"DA-JOURNAL"},
				wantStderr: []string{"recover failed", "session start not blocked"},
			},
		},
		{
			name: "snapshot silent when da is absent",
			sc: sessionHandoffScenario{
				dir: "session-handoff-snapshot", script: "snapshot.sh",
				daPresent:  false,
				skipStdout: []string{"DA-JOURNAL"},
				skipStderr: []string{"warning"},
			},
		},
		{
			name: "recover silent when da is absent",
			sc: sessionHandoffScenario{
				dir: "session-handoff-recover", script: "recover.sh",
				daPresent:  false,
				skipStdout: []string{"DA-JOURNAL"},
				skipStderr: []string{"warning"},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			exit, stdout, stderr := runSessionHandoffScenario(t, tc.sc)
			if exit != 0 {
				t.Fatalf("best-effort hook must exit 0, got %d\nstdout=%s\nstderr=%s", exit, stdout, stderr)
			}
			assertContainsAll(t, "stdout", stdout, tc.sc.wantStdout)
			assertContainsNone(t, "stdout", stdout, tc.sc.skipStdout)
			assertContainsAll(t, "stderr", stderr, tc.sc.wantStderr)
			assertContainsNone(t, "stderr", stderr, tc.sc.skipStderr)
		})
	}
}

func assertContainsAll(t *testing.T, stream, got string, want []string) {
	t.Helper()
	for _, w := range want {
		if !bytes.Contains([]byte(got), []byte(w)) {
			t.Errorf("%s missing %q, got:\n%s", stream, w, got)
		}
	}
}

func assertContainsNone(t *testing.T, stream, got string, banned []string) {
	t.Helper()
	for _, b := range banned {
		if bytes.Contains([]byte(got), []byte(b)) {
			t.Errorf("%s must not contain %q, got:\n%s", stream, b, got)
		}
	}
}
