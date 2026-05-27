// Package gitremote is the canonical home for parsing and canonicalizing
// git remote URLs in dot-agents. All code that needs to interpret a git
// remote URL (repo_id derivation, status display, source description, etc.)
// should call into this package rather than rolling its own string ops.
//
// Parsing is delegated to go-git/v6's transport.ParseURL, which already
// understands the full git remote URL grammar:
//   - Standard schemes: https://, http://, ssh://, git://
//   - SCP-style SSH: git@host:path (normalized to ssh:// internally)
//   - Embedded userinfo (https://user:token@host/path) — stripped from Host
//   - Ports (https://host:8443/path) — stripped from Host
//   - Local paths (/path, C:\path) — surfaced as Scheme=="file"
//
// On top of that grammar this package owns the project canonicalization
// layer used by repo_id and source descriptions: lowercase host, strip
// leading/trailing slashes from path, strip .git suffix.
package gitremote

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// ErrNotRemote is returned when the input parses successfully but does not
// describe a remote (bare local path, junk string normalized to file://).
// Callers that need a remote should treat this the same as a parse error
// and fall back (e.g. leave repo_id blank rather than fabricate one).
var ErrNotRemote = errors.New("gitremote: URL is not a remote (scheme=file)")

// RemoteRef is the structured form of a parsed git remote URL.
//
// All string fields are canonical (no leading/trailing slashes, no .git
// suffix, host lowercased). CanonicalForm is the "<host>/<path>" pair
// used as repo_id per the org-config-resolution spec.
type RemoteRef struct {
	// Scheme is the URL scheme as classified by go-git: "https", "http",
	// "ssh", "git", or "file". SCP-style URLs (git@host:path) report as
	// "ssh". A "file" scheme means the input was a local path or junk
	// string — callers should generally treat that as "no remote".
	Scheme string

	// Host is the lowercased hostname, with userinfo and port removed.
	// e.g. "github.com", "gitlab.acme.internal".
	Host string

	// Path is the repo path with leading/trailing slashes and the .git
	// suffix stripped. e.g. "acme/repo", "payments/svc/api".
	// May be empty for unusual inputs (e.g. "https://host/.git").
	Path string

	// Owner is the first segment of Path (e.g. "acme"), or "" if Path
	// has no slash. Provided as a convenience for callers that want to
	// split the path without re-parsing.
	Owner string

	// Repo is the last segment of Path (e.g. "repo"), or Path itself
	// if Path has no slash. Empty when Path is empty.
	Repo string

	// CanonicalForm is "<Host>/<Path>" — the spec-§5.2 repo_id form.
	// Empty when Host or Path is empty (caller should treat as fallback).
	CanonicalForm string
}

// ParseRemoteURL parses raw into a RemoteRef.
//
// Returns ErrNotRemote when the input parses as a local path or junk
// (transport.ParseURL classifies these as scheme=file). Returns a wrapped
// transport.ParseURL error for malformed inputs. Empty/whitespace input
// is treated as ErrNotRemote.
//
// On success, all RemoteRef fields are populated with canonical forms.
// CanonicalForm is "" when Host or Path is empty (e.g. "https://host/.git"
// strips down to empty Path), so callers that depend on a non-empty repo_id
// should check CanonicalForm rather than err alone.
func ParseRemoteURL(raw string) (RemoteRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RemoteRef{}, ErrNotRemote
	}

	u, err := transport.ParseURL(raw)
	if err != nil {
		return RemoteRef{}, err
	}
	if u.Scheme == "file" {
		return RemoteRef{Scheme: "file"}, ErrNotRemote
	}

	host := strings.ToLower(u.Hostname()) // strips userinfo + port
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	ref := RemoteRef{
		Scheme: u.Scheme,
		Host:   host,
		Path:   path,
	}
	if path != "" {
		if idx := strings.Index(path, "/"); idx >= 0 {
			ref.Owner = path[:idx]
			ref.Repo = path[strings.LastIndex(path, "/")+1:]
		} else {
			ref.Repo = path
		}
	}
	if host != "" && path != "" {
		ref.CanonicalForm = host + "/" + path
	}
	return ref, nil
}

// ErrNoOrigin is returned by ReadOriginURL when the repository at repoPath
// exists but does not have an `origin` remote configured. Distinguished from
// "not a git repo" so callers that want to differentiate "no checkout" from
// "checkout without origin" can branch on the sentinel.
var ErrNoOrigin = errors.New("gitremote: repository has no `origin` remote")

// ReadOriginURL returns the first configured URL of the `origin` remote for
// the repository at repoPath, read directly from the on-disk git config via
// go-git/v6 — no `git` subprocess, no PATH lookup, no porcelain text parsing.
//
// DetectDotGit is enabled so calls from a subdirectory still resolve the
// enclosing repo (matches the `git -C <path>` ergonomics this replaces).
//
// Returns:
//   - the raw URL string (NOT canonicalized — callers that want the
//     repo_id form should pipe it through CanonicalRepoID) and nil error
//     on success
//   - ErrNoOrigin when the repo exists but lacks an `origin` remote (or
//     the remote is configured with an empty URLs slice — a corrupt-
//     config edge case go-git surfaces as len(URLs)==0)
//   - a wrapped error when repoPath is not a git checkout, or any other
//     go-git failure (config parse error, etc.)
//
// This is the canonical replacement for the historical pattern
// `execabs.Command("git", "-C", repoPath, "remote", "get-url", "origin")`
// across the codebase — repo_id derivation, status display, sync probes.
func ReadOriginURL(repoPath string) (string, error) {
	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return "", fmt.Errorf("gitremote: open %s: %w", repoPath, err)
	}
	remote, err := repo.Remote("origin")
	if err != nil {
		// go-git returns ErrRemoteNotFound here; collapse to our sentinel
		// so callers don't need to import go-git just to recognize the
		// "no origin" branch.
		if errors.Is(err, git.ErrRemoteNotFound) {
			return "", ErrNoOrigin
		}
		return "", fmt.Errorf("gitremote: lookup origin in %s: %w", repoPath, err)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 || urls[0] == "" {
		return "", ErrNoOrigin
	}
	return urls[0], nil
}

// CanonicalRepoID is a convenience wrapper around ParseRemoteURL that
// returns just the "<host>/<path>" canonical repo_id form, or "" for any
// input that does not yield a valid host+path pair.
//
// This is the form used by .agentsrc.json repo_id (per org-config-
// resolution spec §5.2) and is the most common call site. Errors and
// ErrNotRemote are swallowed by design — callers that need to distinguish
// "no remote" from "malformed" should use ParseRemoteURL directly.
func CanonicalRepoID(raw string) string {
	ref, err := ParseRemoteURL(raw)
	if err != nil {
		return ""
	}
	return ref.CanonicalForm
}
