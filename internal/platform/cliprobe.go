package platform

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// cliprobe.go is the single, normalized seam for detecting and probing
// user-installed agent CLIs / IDEs. Each platform's IsInstalled/Version keeps
// its own discovery fallbacks (extension dirs, app bundles, home dirs) and its
// own version-string parsing; only the PATH lookup and the bounded subprocess
// exec live here so a future IDE-detection can reuse the same probe.

const (
	// cliVersionProbeTimeout bounds subprocess wall time for --version / defaults probes.
	cliVersionProbeTimeout = 5 * time.Second
	// cliExecPipeWaitDelay is exec.Cmd.WaitDelay: without this, Cmd.Output can block forever
	// in awaitGoroutines after the process is killed if pipe copy goroutines stall (Go 1.20+).
	cliExecPipeWaitDelay = 3 * time.Second
)

// probeInstalled reports whether bin resolves on PATH. This is the shared
// lookup behind every platform's IsInstalled (platforms layer their own
// extension-dir / app-bundle / home-dir fallbacks on top of it).
func probeInstalled(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// probeVersion resolves bin on PATH and runs `bin <args...>` under a wall-clock
// bound, returning the raw combined stdout. Callers keep their own parsing of
// the bytes. The timeout + WaitDelay guard against a shim that blocks on TTY or
// GUI interaction. Returns an error if bin is missing or the probe fails.
func probeVersion(bin string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, err
	}
	return probeVersionAtPath(path, args...)
}

// probeVersionAtPath runs an already-resolved binary path under the same bound
// as probeVersion. Used by callers (e.g. Cursor) that resolve the path
// themselves across several candidate names.
func probeVersionAtPath(path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliVersionProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = cliExecPipeWaitDelay
	return cmd.Output()
}

// probeVersionLine is the common "first non-empty line of `bin --version`"
// convenience used by the simple CLI platforms. It returns the trimmed first
// output line, or "" if the binary is missing or errors.
func probeVersionLine(bin string) string {
	out, err := probeVersion(bin, "--version")
	if err != nil {
		return ""
	}
	return firstLine(out)
}

// firstLine trims whitespace and returns the first line of out.
func firstLine(out []byte) string {
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}
