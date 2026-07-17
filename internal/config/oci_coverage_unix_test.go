//go:build unix

package config

import (
	"syscall"
	"testing"
)

// This unix-only companion to oci_coverage_test.go covers
// writeConfinedPackagesCacheFile's temp-file WRITE failure leg. That leg is
// only reachable when a same-dir temp file is created successfully but the
// subsequent write to it errors — a condition that cannot be produced by
// permission or path manipulation (which fail earlier, at create/rename). The
// portable, deterministic trigger is RLIMIT_FSIZE=0: the zero-byte temp file is
// created, but growing it past zero bytes fails with EFBIG. The syscall
// primitives are unix-only, so this lives behind a build tag; the merged
// multi-OS coverage profile the gate consumes still counts the line as covered
// from the unix run. The Go runtime ignores SIGXFSZ, so the process survives.

// TestOCICovWriteConfinedWriteFails covers the "writing package cache temp"
// failure branch by capping the process file-size limit to zero for the
// duration of a single cache write, then restoring it immediately.
func TestOCICovWriteConfinedWriteFails(t *testing.T) {
	withPackagesCache(t)
	// Materialize the cache root BEFORE clamping the limit so no directory
	// creation is attempted under the zero cap (directories carry no size).
	digest := artifactDigest([]byte("payload"))
	if err := writeConfinedPackagesCacheFile(digestDir(digest), "warmup", []byte("x")); err != nil {
		t.Fatalf("warming cache root: %v", err)
	}

	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Skipf("cannot read RLIMIT_FSIZE: %v", err)
	}
	clamped := syscall.Rlimit{Cur: 0, Max: old.Max}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &clamped); err != nil {
		t.Skipf("cannot clamp RLIMIT_FSIZE: %v", err)
	}
	// Restore as soon as the write returns (and as a backstop on any panic), so
	// no other file write in this process runs under the zero cap.
	restore := func() { _ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old) }
	defer restore()

	err := writeCachedArtifact(digest, []byte("this content cannot be written under a zero file-size limit"))
	restore()
	if err == nil {
		t.Fatal("expected the cache temp write to fail under RLIMIT_FSIZE=0")
	}
}
