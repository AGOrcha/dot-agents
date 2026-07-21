package config

import (
	"os"
	"testing"
)

// BenchmarkSetWindowsMirrorContext guards the regexp hoist: wslWindowsMountRE is
// compiled once at package init, so the per-call cost is a match + Setenv, not a
// recompile. Before the hoist this benchmark paid a regexp.MustCompile on every
// call (hundreds of allocs); after, allocs/op reflect only the match + env writes.
func BenchmarkSetWindowsMirrorContext(b *testing.B) {
	prevMirror, hadMirror := os.LookupEnv("DOT_AGENTS_WINDOWS_MIRROR")
	prevHome, hadHome := os.LookupEnv("DOT_AGENTS_WINDOWS_HOME")
	b.Cleanup(func() {
		restoreEnv("DOT_AGENTS_WINDOWS_MIRROR", prevMirror, hadMirror)
		restoreEnv("DOT_AGENTS_WINDOWS_HOME", prevHome, hadHome)
	})
	const repoPath = "/mnt/c/Users/dev/proj"
	b.ReportAllocs()
	for b.Loop() {
		SetWindowsMirrorContext(repoPath)
	}
}

func restoreEnv(key, val string, had bool) {
	if had {
		_ = os.Setenv(key, val)
		return
	}
	_ = os.Unsetenv(key)
}
