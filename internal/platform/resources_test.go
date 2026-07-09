package platform

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// captureSlog redirects the default slog logger into a buffer for the
// duration of the test and restores the prior default at cleanup.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestResolveScopedFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "mcp", "myproj", "claude.json", []byte("{}"))

	got := resolveScopedFile(agentsHome, "mcp", "myproj", "claude.json", "mcp.json")
	want := filepath.Join(agentsHome, "mcp", "myproj", "claude.json")
	if got != want {
		t.Fatalf("resolveScopedFile() = %q, want %q", got, want)
	}
}

// TestResolveScopedFileMissingIsSilent covers legitimate absence: no warning
// is logged, matching the existing (unchanged) behavior for the common case
// where a bucket/scope/name combination simply has no source.
func TestResolveScopedFileMissingIsSilent(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	buf := captureSlog(t)

	if got := resolveScopedFile(agentsHome, "mcp", "myproj", "nope.json"); got != "" {
		t.Fatalf("resolveScopedFile(missing) = %q, want empty", got)
	}
	if buf.Len() != 0 {
		t.Errorf("legitimate absence must not log, got %q", buf.String())
	}
}

// TestResolveScopedFileStatErrorLogged covers the real-error branch: a
// candidate whose immediate parent directory is unreadable must not be
// treated as legitimately absent. resolveScopedFile still returns "" (its
// string-only contract can't change without editing every platform caller,
// several of which are out of scope here), but the swallow must now be
// logged so it is distinguishable from a genuine miss.
func TestResolveScopedFileStatErrorLogged(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "myproj"
	testutil.WriteScopeFile(t, agentsHome, "mcp", scope, "claude.json", []byte("{}"))
	testutil.MakeDirUnreadable(t, filepath.Join(agentsHome, "mcp", scope))
	buf := captureSlog(t)

	if got := resolveScopedFile(agentsHome, "mcp", scope, "claude.json"); got != "" {
		t.Fatalf("resolveScopedFile(unreadable parent) = %q, want empty", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("stat failed")) {
		t.Errorf("expected a warning log for the permission-denied stat, got %q", buf.String())
	}
}

// TestResolveScopedFileFromBucketsStatErrorLogged mirrors
// TestResolveScopedFileStatErrorLogged for the cross-bucket variant.
func TestResolveScopedFileFromBucketsStatErrorLogged(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "noproj"
	testutil.WriteScopeFile(t, agentsHome, "hooks", scope, "other.json", []byte("{}"))
	testutil.MakeDirUnreadable(t, filepath.Join(agentsHome, "hooks", scope))
	buf := captureSlog(t)

	if got := resolveScopedFileFromBuckets(agentsHome, []string{"hooks"}, scope, "other.json"); got != "" {
		t.Fatalf("resolveScopedFileFromBuckets(unreadable) = %q, want empty", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("stat failed")) {
		t.Errorf("expected a warning log for the permission-denied stat, got %q", buf.String())
	}
}
