package journal

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/gitremote"
)

// fingerprintLen is how many hex characters of the SHA-256 the fingerprint keeps
// for the per-repo subdir name. 16 hex chars (64 bits) is collision-safe across
// the handful of repos one machine touches while keeping the path short and
// inspectable.
const fingerprintLen = 16

// trustedRepoID resolves the canonical, portable repo identity for repoPath via
// the FORK-1 hybrid rule (config.DeriveTrustedRepoID): a non-empty string only
// when the git origin is an unambiguous, trustworthy identity. It is a seam so
// fingerprint tests can drive both the identity and path-fallback branches
// without standing up real git remotes.
var trustedRepoID = func(repoPath string) string {
	id, ambiguous := config.DeriveTrustedRepoID(repoPath)
	if ambiguous {
		return ""
	}
	return id
}

// originRemoteURL returns the raw origin remote URL for repoPath, or "" when the
// path is not a git checkout / has no origin. Seam for hermetic identity tests.
var originRemoteURL = func(repoPath string) string {
	url, err := gitremote.ReadOriginURL(repoPath)
	if err != nil {
		return ""
	}
	return url
}

// absFn resolves a path to absolute form; a seam so tests can exercise the
// Abs-failure fallback (which os.Getwd makes practically unreachable otherwise).
var absFn = filepath.Abs

// absRepoPath canonicalizes repoPath to a cleaned absolute path so the same
// checkout always fingerprints identically regardless of how the caller spelled
// it. On the rare Abs failure it falls back to a plain Clean — still stable for a
// given input.
func absRepoPath(repoPath string) string {
	abs, err := absFn(repoPath)
	if err != nil {
		return filepath.Clean(repoPath)
	}
	return filepath.Clean(abs)
}

// Fingerprint returns the stable per-repo key used for the journal subdirectory.
// It prefers the trusted canonical repo identity (so the same logical repo maps
// to one journal across clones/worktrees on different paths) and falls back to a
// hash of the absolute checkout path when no portable identity is resolvable
// (e.g. a plain `git init`, a non-git directory, or an ambiguous fork origin).
// The two domains are prefixed before hashing so a repo id can never collide with
// a filesystem path that happens to equal it.
func Fingerprint(repoPath string) string {
	abs := absRepoPath(repoPath)
	if id := trustedRepoID(abs); id != "" {
		return hashKey("id:" + id)
	}
	return hashKey("path:" + abs)
}

// hashKey returns the first fingerprintLen hex chars of SHA-256(key).
func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:fingerprintLen]
}

// Identity is the D8 composite key that uniquely pins a journal entry to a repo
// state, so read-back can quarantine a handoff captured against a different
// remote/branch/HEAD than the resuming session (spec D8 — "a wrong handoff is
// worse than none").
//
// ResolveIdentity populates the in-process, locally-knowable fields
// (Fingerprint, RepoID, RemoteURL, WorktreePath). The live git-state fields
// (DefaultBranch, CurrentBranch, Head, PRNumber) are part of the keying tuple but
// are populated by the verified-recovery layer (a later task), which already runs
// the git/gh reads required to determine and re-verify them; they stay zero-value
// here and omitempty out of the serialized key until then.
type Identity struct {
	Fingerprint   string `json:"fingerprint"`
	RepoID        string `json:"repo_id,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	CurrentBranch string `json:"current_branch,omitempty"`
	Head          string `json:"head,omitempty"`
	WorktreePath  string `json:"worktree_path,omitempty"`
	PRNumber      int    `json:"pr_number,omitempty"`
}

// ResolveIdentity builds the locally-knowable portion of the composite identity
// for the checkout at repoPath. It never fails: a non-git or unidentifiable path
// still yields a usable Identity carrying the path-derived Fingerprint and the
// absolute WorktreePath, with the git-derived fields left blank.
func ResolveIdentity(repoPath string) Identity {
	abs := absRepoPath(repoPath)
	return Identity{
		Fingerprint:  Fingerprint(abs),
		RepoID:       trustedRepoID(abs),
		RemoteURL:    originRemoteURL(abs),
		WorktreePath: abs,
	}
}
