package gitremote

import (
	"errors"
	"testing"
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
			if ref.Host != c.wantHost {
				t.Errorf("Host = %q, want %q", ref.Host, c.wantHost)
			}
			if ref.Owner != c.wantOwner {
				t.Errorf("Owner = %q, want %q", ref.Owner, c.wantOwner)
			}
			if ref.Repo != c.wantRepo {
				t.Errorf("Repo = %q, want %q", ref.Repo, c.wantRepo)
			}
			if ref.Path != c.wantPath {
				t.Errorf("Path = %q, want %q", ref.Path, c.wantPath)
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
