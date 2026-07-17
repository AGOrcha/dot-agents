package config

// Coverage-directed tests driving the last unreached error legs of
// fetcher_git_artifact.go to 100% line coverage under the per-file ratchet.
// They exercise: the committed-blob reader/read legs (via the committedBlobReader
// seam, since an in-memory blob never fails to open or read), the SSH-auth
// branches of the quota-bounded clone, the MkdirTemp failure, and the
// quotaFilesystem.Chroot inner-error propagation. Helpers use the fetch100
// prefix to avoid colliding with the package's other test helpers.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

// fetch100BlobFile builds a real *object.File over an in-memory blob so
// readCommittedBlobFile clears its declared-size gate before the seam fires.
func fetch100BlobFile(t *testing.T, content []byte) *object.File {
	t.Helper()
	st := memory.NewStorage()
	obj := st.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		t.Fatalf("blob writer: %v", err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatalf("blob write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("blob close: %v", err)
	}
	h, err := st.SetEncodedObject(obj)
	if err != nil {
		t.Fatalf("set blob: %v", err)
	}
	blob, err := object.GetBlob(st, h)
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	return object.NewFile("x", filemode.Regular, blob)
}

// fetch100ErrReader is an io.ReadCloser that fails on Read, driving the
// io.ReadAll error leg of readCommittedBlobFile.
type fetch100ErrReader struct{}

func (fetch100ErrReader) Read([]byte) (int, error) { return 0, errors.New("blob read boom") }
func (fetch100ErrReader) Close() error             { return nil }

// TestFetch100CommittedBlobReaderErrors covers readCommittedBlobFile's two
// never-fail-in-practice legs: the blob reader open error and the content read
// error, both via the committedBlobReader seam.
func TestFetch100CommittedBlobReaderErrors(t *testing.T) {
	orig := committedBlobReader
	defer func() { committedBlobReader = orig }()
	f := fetch100BlobFile(t, []byte("hi"))
	limits := DefaultBundleLimits().orDefault()

	committedBlobReader = func(*object.File) (io.ReadCloser, error) {
		return nil, errors.New("open boom")
	}
	if _, err := readCommittedBlobFile("x", f, limits); err == nil {
		t.Fatal("expected a reader-open error")
	}

	committedBlobReader = func(*object.File) (io.ReadCloser, error) {
		return fetch100ErrReader{}, nil
	}
	if _, err := readCommittedBlobFile("x", f, limits); err == nil {
		t.Fatal("expected a content-read error")
	}
}

// TestFetch100CloneSSHAuthError covers gitCloneShallowQuotaBounded's early
// return when gitSSHAuth fails (an ssh source URL with no agent and no key).
func TestFetch100CloneSSHAuthError(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	_, _, err := gitCloneShallowQuotaBounded(context.Background(), "ssh://git@example.com/acme/repo.git", "refs/heads/main")
	if err == nil {
		t.Fatal("expected an ssh-auth error before any clone")
	}
}

// TestFetch100CloneSSHAuthBuiltThenClones covers the auth!=nil clientOpts
// branch and the non-ErrFilterNotSupported clone return: an ssh URL with a
// usable default key builds auth, then the (cancelled) clone fails with a
// transport error that is not the filter-capability sentinel.
func TestFetch100CloneSSHAuthBuiltThenClones(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeTestSSHKey(t, filepath.Join(home, ".ssh"), "id_ed25519", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	_, _, err := gitCloneShallowQuotaBounded(ctx, "ssh://git@127.0.0.1:1/acme/repo.git", "refs/heads/main")
	if err == nil {
		t.Fatal("expected the ssh clone to fail against an unreachable host")
	}
}

// TestFetch100CloneOnceMkdirTempError covers cloneQuotaBoundedOnce's
// MkdirTemp failure by pointing TMPDIR at a regular file.
func TestFetch100CloneOnceMkdirTempError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("TMPDIR-as-file MkdirTemp failure needs a non-root POSIX host")
	}
	fileTmp := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(fileTmp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", fileTmp)
	_, _, err := cloneQuotaBoundedOnce(context.Background(), "file:///x", "refs/heads/main", nil, "")
	if err == nil {
		t.Fatal("expected MkdirTemp to fail with TMPDIR pointing at a file")
	}
}

// fetch100ChrootErrFS is a billy.Filesystem whose Chroot always errors, driving
// quotaFilesystem.Chroot's inner-error propagation.
type fetch100ChrootErrFS struct{ billy.Filesystem }

func (fetch100ChrootErrFS) Chroot(string) (billy.Filesystem, error) {
	return nil, errors.New("chroot boom")
}

// TestFetch100QuotaFilesystemChrootError covers quotaFilesystem.Chroot's error
// return when the underlying filesystem's Chroot fails.
func TestFetch100QuotaFilesystemChrootError(t *testing.T) {
	t.Parallel()
	qfs := newQuotaFilesystem(fetch100ChrootErrFS{osfs.New(t.TempDir())}, 10)
	if _, err := qfs.Chroot("sub"); err == nil {
		t.Fatal("expected the inner Chroot error to propagate")
	}
}
