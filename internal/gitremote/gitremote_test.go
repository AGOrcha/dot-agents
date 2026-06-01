package gitremote

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
)

func TestCanonicalRepoID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// — SSH (SCP-style) —
		{"ssh github", "git@github.com:acme/repo.git", "github.com/acme/repo"},
		{"ssh github no .git", "git@github.com:acme/repo", "github.com/acme/repo"},
		{"ssh nested gitlab group", "git@gitlab.acme.internal:payments/svc/api.git", "gitlab.acme.internal/payments/svc/api"},
		{"ssh uppercase host lowercased", "git@GitHub.com:acme/repo.git", "github.com/acme/repo"},

		// — HTTPS / HTTP —
		{"https github", "https://github.com/acme/repo.git", "github.com/acme/repo"},
		{"https github no .git", "https://github.com/acme/repo", "github.com/acme/repo"},
		{"https with user", "https://nikash@github.com/acme/repo.git", "github.com/acme/repo"},
		{"https with user+token", "https://nikash:s3cret@github.com/acme/repo.git", "github.com/acme/repo"},
		{"https with port", "https://gitlab.acme.internal:8443/g/r.git", "gitlab.acme.internal/g/r"},
		{"http", "http://gitlab.acme.internal/g/r", "gitlab.acme.internal/g/r"},
		{"https trailing slash", "https://github.com/acme/repo/", "github.com/acme/repo"},

		// — git:// scheme —
		{"git scheme", "git://github.com/acme/repo.git", "github.com/acme/repo"},

		// — ssh:// scheme (explicit) —
		{"ssh scheme", "ssh://git@github.com/acme/repo.git", "github.com/acme/repo"},
		{"ssh scheme with port", "ssh://git@github.com:22/acme/repo.git", "github.com/acme/repo"},

		// — fallbacks: parses as non-remote or yields no canonical form —
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"bare path no host", "/just/a/path", ""},
		{"junk", "not a url at all", ""},
		// ".git" alone strips to empty path → no CanonicalForm.
		{"path is just .git", "https://github.com/.git", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanonicalRepoID(c.in); got != c.want {
				t.Errorf("CanonicalRepoID(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestParseRemoteURL_Fields(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantPath  string
	}{
		{"ssh github", "git@github.com:acme/repo.git", "github.com", "acme", "repo", "acme/repo"},
		{"https nested", "https://gitlab.acme.internal/payments/svc/api.git", "gitlab.acme.internal", "payments", "api", "payments/svc/api"},
		{"https with port + creds", "https://u:t@gitlab.acme.internal:8443/g/r.git", "gitlab.acme.internal", "g", "r", "g/r"},
		// Single-segment path: Owner empty, Repo == Path.
		{"https single segment", "https://example.com/repo.git", "example.com", "", "repo", "repo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ref, err := ParseRemoteURL(c.in)
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q) err = %v", c.in, err)
			}
			assertRemoteRefFields(t, c.in, ref, c.wantHost, c.wantOwner, c.wantRepo, c.wantPath)
		})
	}
}

func assertRemoteRefFields(t *testing.T, in string, ref RemoteRef, wantHost, wantOwner, wantRepo, wantPath string) {
	t.Helper()
	if ref.Host != wantHost {
		t.Errorf("ParseRemoteURL(%q) Host = %q, want %q", in, ref.Host, wantHost)
	}
	if ref.Owner != wantOwner {
		t.Errorf("ParseRemoteURL(%q) Owner = %q, want %q", in, ref.Owner, wantOwner)
	}
	if ref.Repo != wantRepo {
		t.Errorf("ParseRemoteURL(%q) Repo = %q, want %q", in, ref.Repo, wantRepo)
	}
	if ref.Path != wantPath {
		t.Errorf("ParseRemoteURL(%q) Path = %q, want %q", in, ref.Path, wantPath)
	}
}

func TestParseRemoteURL_MalformedInputPropagatesParseError(t *testing.T) {
	// Inputs go-git's transport.ParseURL rejects with a non-nil error
	// (vs. silently normalizing to file://). Covers the err-from-ParseURL
	// branch, which is otherwise hidden because most junk normalizes to a
	// file:// URL and surfaces as ErrNotRemote instead.
	cases := []string{
		"http://[::1",      // unclosed IPv6 literal
		"ftp://malformed[", // invalid IP-literal
		"http://\n/path",   // control character in URL
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := ParseRemoteURL(c)
			if err == nil {
				t.Fatalf("ParseRemoteURL(%q): want error, got nil", c)
			}
			if errors.Is(err, ErrNotRemote) {
				t.Errorf("ParseRemoteURL(%q) = ErrNotRemote, want wrapped parse error", c)
			}
		})
	}
}

func TestParseRemoteURL_NotRemote(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"bare path", "/just/a/path"},
		{"junk normalized to file", "not a url at all"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseRemoteURL(c.in)
			if !errors.Is(err, ErrNotRemote) {
				t.Errorf("ParseRemoteURL(%q) err = %v, want ErrNotRemote", c.in, err)
			}
		})
	}
}

func TestParseRemoteURL_SchemeReported(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantScheme string
	}{
		{"https", "https://github.com/a/b.git", "https"},
		{"http", "http://github.com/a/b", "http"},
		{"git scheme", "git://github.com/a/b.git", "git"},
		{"ssh explicit", "ssh://git@github.com/a/b.git", "ssh"},
		// transport.ParseURL normalizes SCP-style to ssh://.
		{"scp normalized to ssh", "git@github.com:a/b.git", "ssh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ref, err := ParseRemoteURL(c.in)
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q) err = %v", c.in, err)
			}
			if ref.Scheme != c.wantScheme {
				t.Errorf("Scheme = %q, want %q", ref.Scheme, c.wantScheme)
			}
		})
	}
}

// initRepoWithOrigin creates an empty git repo under t.TempDir() and, when
// originURL is non-empty, registers it as the `origin` remote. Returns the
// repo path so the test can hand it to ReadOriginURL.
func initRepoWithOrigin(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	if originURL != "" {
		if _, err := repo.CreateRemote(&config.RemoteConfig{
			Name: "origin",
			URLs: []string{originURL},
		}); err != nil {
			t.Fatalf("CreateRemote: %v", err)
		}
	}
	return dir
}

func TestReadOriginURL_ReturnsConfiguredURL(t *testing.T) {
	want := "git@github.com:AGOrcha/dot-agents.git"
	dir := initRepoWithOrigin(t, want)
	got, err := ReadOriginURL(dir)
	if err != nil {
		t.Fatalf("ReadOriginURL err = %v", err)
	}
	if got != want {
		t.Errorf("ReadOriginURL = %q, want %q", got, want)
	}
}

func TestReadOriginURL_DetectDotGitFromSubdir(t *testing.T) {
	// DetectDotGit lets a call from a subdirectory still resolve the
	// enclosing repo — matches the `git -C <path>` ergonomics this
	// replaces. Without DetectDotGit the call would fail with
	// ErrRepositoryNotExists from inside a non-repo subdir.
	want := "https://github.com/acme/repo.git"
	repoDir := initRepoWithOrigin(t, want)
	sub := filepath.Join(repoDir, "nested", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, err := ReadOriginURL(sub)
	if err != nil {
		t.Fatalf("ReadOriginURL err = %v", err)
	}
	if got != want {
		t.Errorf("ReadOriginURL = %q, want %q", got, want)
	}
}

func TestReadOriginURL_NoOriginReturnsSentinel(t *testing.T) {
	dir := initRepoWithOrigin(t, "")
	_, err := ReadOriginURL(dir)
	if !errors.Is(err, ErrNoOrigin) {
		t.Errorf("ReadOriginURL err = %v, want ErrNoOrigin", err)
	}
}

func TestReadOriginURL_NotARepoErrors(t *testing.T) {
	dir := t.TempDir() // no .git anywhere
	_, err := ReadOriginURL(dir)
	if err == nil {
		t.Fatalf("ReadOriginURL on non-repo dir: want error, got nil")
	}
	if errors.Is(err, ErrNoOrigin) {
		t.Errorf("ReadOriginURL on non-repo dir should not return ErrNoOrigin (got %v)", err)
	}
}

func TestReadOriginURL_OriginWithEmptyURLReturnsSentinel(t *testing.T) {
	// Corrupt-config edge case: an `[remote "origin"]` section with no
	// `url = ...` line, or with an empty url. go-git surfaces this as
	// len(URLs) == 0 (or urls[0] == "") rather than ErrRemoteNotFound,
	// so ReadOriginURL must collapse it to ErrNoOrigin the same way.
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	// Overwrite .git/config with an origin section whose url= line is empty.
	cfgPath := filepath.Join(dir, ".git", "config")
	cfgBody := "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = \n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := ReadOriginURL(dir)
	if !errors.Is(err, ErrNoOrigin) {
		t.Errorf("ReadOriginURL with empty origin url: err = %v, want ErrNoOrigin", err)
	}
}

func TestReadOriginURL_CorruptConfigWrappedError(t *testing.T) {
	// PlainOpenWithOptions parses .git/config during Open, so a malformed
	// config surfaces here as a wrapped open error — distinct from
	// ErrNoOrigin, so callers can tell "corrupt repo" from "no origin
	// configured". Without this guarantee, status/probe code would
	// silently render an empty remote on a broken config.
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	cfgPath := filepath.Join(dir, ".git", "config")
	// Unterminated section header — git config parsers reject this.
	if err := os.WriteFile(cfgPath, []byte("[remote \"origin\"\n\turl = bogus\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := ReadOriginURL(dir)
	if err == nil {
		t.Fatalf("ReadOriginURL with corrupt config: want error, got nil")
	}
	if errors.Is(err, ErrNoOrigin) {
		t.Errorf("ReadOriginURL with corrupt config should not collapse to ErrNoOrigin (got %v)", err)
	}
}
