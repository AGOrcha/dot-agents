package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport"
	gogitssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh/sshagent"
	"github.com/go-git/go-git/v6/storage/memory"

	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/gitremote"
)

// maxLayerBytes caps a fetched layer.json so a hostile or runaway source cannot
// exhaust memory. Config layers are small policy fragments; 4 MiB is generous.
const maxLayerBytes = 4 << 20

// contentHash returns the hex sha256 of data, the content-addressed SHA for
// http/local layers (git layers use the commit SHA instead).
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// readAllLimited reads up to maxLayerBytes from r, erroring if the payload is
// larger so a runaway response cannot exhaust memory.
func readAllLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxLayerBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxLayerBytes {
		return nil, fmt.Errorf("layer exceeds %d bytes", maxLayerBytes)
	}
	return data, nil
}

// ReadLockedLayers reads the project's locked extends-layer set from
// .agentsrc.lock, keyed by the as-declared extends ref. It reads through the §7A
// units view (ReadUnits, which migrates a legacy config-only lock on read) and
// projects only the `layer` units, so callers get the same offline lock surface
// the resolver and verify paths consume. A missing/empty lock yields an empty
// (non-nil) map; a malformed lock surfaces an error. It performs NO fetch.
func ReadLockedLayers(projectPath string) (map[string]LockedLayer, error) {
	return readLockedLayersFromUnits(projectPath)
}

// LockedRemoteLayerBytes resolves the cached bytes of a LOCKED remote layer:
// given the parsed ref parts and the locked set, it returns the cached layer.json
// bytes at the recorded SHA. ok is false (and bytes nil) when the ref is not
// locked, has no resolved SHA, or its bytes are not in the local cache — the
// caller then treats the layer as an unlocked/uncached remote (skip + sync hint).
// It is read-only and never fetches.
func LockedRemoteLayerBytes(parts LayerRefParts, ref string, locked map[string]LockedLayer) ([]byte, bool) {
	lock, ok := locked[ref]
	if !ok || lock.ResolvedSHA == "" {
		return nil, false
	}
	return ReadCachedLayerBytes(parts.SourceID, parts.LayerPath, lock.ResolvedSHA)
}

// ImportFailReason classifies a config-layer import failure, mapping 1:1 to the
// `reason` field of the config.import.failed audit event (spec §9, §11).
type ImportFailReason string

const (
	// ReasonTransport: the source could not be reached / the fetch I/O failed.
	ReasonTransport ImportFailReason = "transport"
	// ReasonAuth: authentication or authorization was rejected.
	ReasonAuth ImportFailReason = "auth"
	// ReasonContent: the layer was fetched but its content was unreadable.
	ReasonContent ImportFailReason = "content"
	// ReasonSchema: a tier-constraint or layer-schema violation (surfaces
	// before any network call for tier constraints — spec §4).
	ReasonSchema ImportFailReason = "schema"
	// ReasonNotFound: the referenced source id or layer path does not exist.
	ReasonNotFound ImportFailReason = "not_found"
)

// ImportError carries the structured failure contract for an extends/packages
// import (spec §11): which ref failed, which source it came from, and the
// failure category. It is the Go form of a config.import.failed event.
type ImportError struct {
	// Ref is the failing extends/packages entry, e.g. "acme:org/base".
	Ref string
	// SourceID is the source the ref was expected from.
	SourceID string
	// Reason is the failure category.
	Reason ImportFailReason
	// Err is the underlying cause, if any.
	Err error
}

func (e *ImportError) Error() string {
	base := fmt.Sprintf("config.import.failed ref=%q source=%q reason=%s", e.Ref, e.SourceID, e.Reason)
	if e.Err != nil {
		return base + ": " + e.Err.Error()
	}
	return base
}

// Unwrap exposes the underlying cause for errors.Is/errors.As.
func (e *ImportError) Unwrap() error { return e.Err }

// LayerRefParts is the parsed form of a "source-id:layer-path[@version]" ref
// (spec §5). Version is the optional pin; for extends it overrides the source's
// declared ref (git SHA, tag, or branch).
type LayerRefParts struct {
	SourceID  string
	LayerPath string
	Version   string
}

// ParseLayerRef splits "source-id:layer-path[@version]" into its parts (spec §5).
// The source-id is everything before the first ':'; the version (if present) is
// everything after the last '@' in the remainder. A missing ':' or an empty
// source-id / layer-path is a parse error.
func ParseLayerRef(ref string) (LayerRefParts, error) {
	colon := strings.IndexByte(ref, ':')
	if colon <= 0 {
		return LayerRefParts{}, fmt.Errorf("ref %q must be 'source-id:layer-path[@version]'", ref)
	}
	parts := LayerRefParts{SourceID: ref[:colon]}
	rest := ref[colon+1:]
	if at := strings.LastIndexByte(rest, '@'); at >= 0 {
		parts.Version = rest[at+1:]
		rest = rest[:at]
	}
	parts.LayerPath = rest
	if parts.LayerPath == "" {
		return LayerRefParts{}, fmt.Errorf("ref %q has empty layer-path", ref)
	}
	return parts, nil
}

// FetchedLayer is the result of a successful layer fetch: the raw layer.json
// bytes, the resolved SHA/content hash they were fetched at, and whether the
// content came from cache (vs. a fresh network fetch).
type FetchedLayer struct {
	// Data is the raw layer.json bytes.
	Data []byte
	// ResolvedSHA is the git commit SHA (git) or content hash (http/local) the
	// layer was fetched at. It is the cache key and the lockfile resolved_sha.
	ResolvedSHA string
	// CacheHit reports whether Data came from the local cache.
	CacheHit bool
	// KeyInputs carries the resolved facts the fetcher observed for this source
	// (git commit, http ETag/Last-Modified/digest, local commit + worktree
	// state), so the resolver can derive the source's effective content cache key
	// (config-distribution-model §7A.4) via EffectiveCacheKey without re-running
	// the fetch. A zero value falls back to the kind default keyed on ResolvedSHA.
	KeyInputs CacheKeyInputs
}

// Fetcher fetches a config layer's bytes from a resolved source. One impl per
// source type (git, http, local); the resolver selects by Source.Type. The
// interface is the test seam: a fakeFetcher stands in so no test touches the
// network or a git binary.
type Fetcher interface {
	// Fetch returns the layer bytes for parts.LayerPath from src. cacheDir is
	// the content-addressed cache root for this source+layer
	// (~/.agents/cache/config/<source-id>/<layer-path>); the fetcher writes its
	// resolved <sha>/layer.json beneath it and returns the resolved SHA.
	Fetch(src Source, parts LayerRefParts, cacheDir string) (FetchedLayer, error)
}

// refreshingFetcher is the optional cache-key-aware extension of Fetcher
// (config-distribution-model §7A.4 / R6). When the resolver determines a
// layer's recorded cache key is stale — a `--refresh` / always_revalidate force
// escape, or a cache_keys override edit that changes the key shape — it asks the
// fetcher to bypass its SHA-addressed cache serve and re-read the upstream, so
// the consumption of cache_keys actually re-validates online instead of silently
// serving the cached bytes. A Fetcher that does not implement this is treated as
// always cache-serving (forceRefresh is a no-op for it), preserving the existing
// behavior for fakes and the cache-less local fetcher.
type refreshingFetcher interface {
	// FetchRefresh behaves like Fetch but, when forceRefresh is true, skips the
	// cached-SHA fast path and re-reads from the upstream so a stale cache key
	// re-validates. forceRefresh=false is identical to Fetch.
	FetchRefresh(src Source, parts LayerRefParts, cacheDir string, forceRefresh bool) (FetchedLayer, error)
}

// fetchWithRefresh dispatches to a fetcher's refresh-aware path when it supports
// one, falling back to the plain Fetch otherwise. It is the single point the
// resolver routes a layer fetch through, so the force-refresh signal threads to
// every refresh-aware fetcher uniformly while plain Fetchers stay unaffected.
func fetchWithRefresh(f Fetcher, src Source, parts LayerRefParts, cacheDir string, forceRefresh bool) (FetchedLayer, error) {
	if rf, ok := f.(refreshingFetcher); ok {
		return rf.FetchRefresh(src, parts, cacheDir, forceRefresh)
	}
	return f.Fetch(src, parts, cacheDir)
}

// SelectFetcher returns the Fetcher for a source type, or an error for an
// unsupported type. Per config-distribution-model §15 D13 there is no
// source/kind asymmetry: every source type — git, http, local, and oci — is
// valid for extends (config layers), just as every type is valid for packages.
// An oci layer is pulled over the same plumbing as an oci artifact, guarded by
// the config-layer media type (ociLayerFetcher), so `kind` stays meaningful.
func SelectFetcher(sourceType string) (Fetcher, error) {
	switch sourceType {
	case "git":
		return &gitFetcher{}, nil
	case "http":
		return &httpFetcher{}, nil
	case "local":
		return &localFetcher{}, nil
	case "oci":
		return &ociLayerFetcher{}, nil
	default:
		return nil, fmt.Errorf("unsupported source type %q", sourceType)
	}
}

// configCacheRoot is the tier-1 layer cache root: ~/.agents/cache/config.
func configCacheRoot() string {
	return filepath.Join(AgentsHome(), "cache", "config")
}

// layerCacheDir is the content-addressed cache directory for one source+layer:
// <root>/<source-id>/<layer-path>. The resolved <sha>/layer.json lives beneath
// it (spec §8). layerPath is slash-cleaned so a path like "team/frontend.json"
// nests as directories rather than colliding.
func layerCacheDir(sourceID, layerPath string) string {
	return filepath.Join(configCacheRoot(), sourceID, filepath.FromSlash(layerPath))
}

// cachedLayerPath is the absolute path of a cached layer.json for a given SHA.
// The SHA is mapped through digestDir so an OCI source's "sha256:<hex>" digest
// (D13) becomes a colon-free directory segment — a colon is an illegal path
// char on Windows. git/http/local SHAs are already bare hex, so this is a no-op
// for them; the on-disk layout matches the packages/artifact cache.
func cachedLayerPath(cacheDir, sha string) string {
	return filepath.Join(cacheDir, digestDir(sha), "layer.json")
}

// ReadCachedLayerBytes returns the cached layer.json bytes for a source+layer at
// the given resolved SHA/digest, read from the content-addressed layer cache with
// NO fetch. ok is false when nothing is cached at that SHA. It is the exported,
// offline read commands use to validate a LOCKED remote layer against the bytes
// already on disk (e.g. `da config lint`), mirroring the resolver's offline
// cache-hit path (readOneLockedLayer) without re-exposing the internal layout.
func ReadCachedLayerBytes(sourceID, layerPath, sha string) ([]byte, bool) {
	return readCachedLayer(layerCacheDir(sourceID, layerPath), sha)
}

// readCachedLayer returns the cached layer bytes for sha, or (nil,false) if not
// present. It is the offline / cache-hit fast path.
func readCachedLayer(cacheDir, sha string) ([]byte, bool) {
	data, err := os.ReadFile(cachedLayerPath(cacheDir, sha))
	if err != nil {
		return nil, false
	}
	return data, true
}

// writeCachedLayer persists layer bytes under <cacheDir>/<sha>/layer.json. The
// SHA segment is mapped through digestDir (see cachedLayerPath) so an OCI
// "sha256:<hex>" digest never embeds a colon in a path segment (illegal on
// Windows); the read path uses the same mapping.
//
// Perf (package-artifact-install t9): the target path is content-addressed by
// sha, which the caller derives from data itself — so a resolve that re-fetches
// an UNCHANGED layer (the common steady-state case: every `da config sync` /
// `da status` / `da install` on an unmodified manifest, across every extends
// layer) writes byte-identical content to the same path every single time.
// Profiling a 100-layer local-extends chain showed writeCachedLayer's
// unconditional MkdirAll+WriteFile pair as ~79% of total resolve CPU time
// (README: docs/PERF_BUDGET.md), almost entirely redundant I/O. A full
// read-back + byte-compare fast path — NOT a bare existence/size check —
// avoids silently trusting a possibly-corrupt cache entry (e.g. left by a
// prior interrupted process): only a verified byte-identical match skips the
// write, so a stale/corrupt entry at the right path is still self-healed by
// falling through to a real (now atomic, temp+rename) write.
func writeCachedLayer(cacheDir, sha string, data []byte) error {
	target := cachedLayerPath(cacheDir, sha)
	if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	dir := filepath.Join(cacheDir, digestDir(sha))
	if err := fsops.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating layer cache dir: %w", err)
	}
	if err := fsops.WriteFileAtomic(target, data); err != nil {
		return fmt.Errorf("writing cached layer: %w", err)
	}
	return nil
}

// --- git fetcher -----------------------------------------------------------

// gitFetcher resolves a git source ref to a commit SHA, reads the layer file at
// that SHA, and caches it content-addressed by SHA. It uses go-git (no `git`
// subprocess, so no exec/PATH lookup and no go:S4036 hotspot): a single shallow
// single-branch clone resolves the ref→SHA and the file@SHA in one operation,
// replacing the old `git ls-remote` + `git archive` pair.
type gitFetcher struct {
	// cloner is a test seam over the go-git clone. Nil uses gitCloneShallow.
	// It returns the cloned repository (for HEAD resolution) and the worktree
	// filesystem (for reading the layer file).
	cloner func(ctx context.Context, url, ref string) (*gogit.Repository, billy.Filesystem, error)
}

func (f *gitFetcher) clone(ctx context.Context, url, ref string) (*gogit.Repository, billy.Filesystem, error) {
	if f.cloner != nil {
		return f.cloner(ctx, url, ref)
	}
	return gitCloneShallow(ctx, url, ref)
}

// gitCloneShallow performs a Depth:1, single-branch in-memory clone of url at
// ref. The objects live in an in-memory storer and the checkout in an in-memory
// billy filesystem, so nothing is written to disk and no temp dir cleanup is
// needed. Returns the repository (for HEAD) and the worktree filesystem.
func gitCloneShallow(ctx context.Context, url, ref string) (*gogit.Repository, billy.Filesystem, error) {
	auth, err := gitSSHAuth(url)
	if err != nil {
		return nil, nil, err
	}
	var clientOpts []client.Option
	if auth != nil {
		clientOpts = []client.Option{client.WithSSHAuth(auth)}
	}
	fs := memfs.New()
	repo, err := gogit.CloneContext(ctx, memory.NewStorage(), fs, &gogit.CloneOptions{
		URL:           url,
		ClientOptions: clientOpts,
		ReferenceName: plumbing.ReferenceName(ref),
		SingleBranch:  true,
		Depth:         1,
		Tags:          plumbing.NoTags,
	})
	if err != nil {
		return nil, nil, err
	}
	return repo, fs, nil
}

// gitSSHAuth builds go-git's SSH ClientConfig for an ssh:// (or SCP-style
// git@host:path) rawURL, so a `git` config/package source authenticates the
// same way the user's own `git clone`/`git push` for that URL already does —
// no separate `eval $(ssh-agent) && ssh-add` just to run da.
//
// go-git v6 has no such fallback on its own: leaving ClientOptions unset
// makes it call ssh.NewSSHAgentAuth unconditionally, which hard-fails with
// "SSH agent requested but SSH_AUTH_SOCK not-specified" the instant
// SSH_AUTH_SOCK is unset — even when the user has a perfectly usable
// unencrypted default key (the common case: a plain shell often does not
// have SSH_AUTH_SOCK exported even though a keychain/GUI agent exists and
// `git clone`/`git push` for the same URL work fine).
//
// The preference order mirrors OpenSSH's own client:
//  1. A running SSH agent (SSH_AUTH_SOCK set) — unchanged from before.
//  2. Default identity files, in the order ssh(1) tries them, with any
//     ~/.ssh/config `IdentityFile` for the host tried first (a more specific
//     match than the bare defaults).
//
// A passphrase-protected key with no agent available surfaces a clear,
// actionable error instead of go-git's raw "SSH_AUTH_SOCK not-specified".
//
// Returns (nil, nil) for a non-ssh URL: the caller then leaves ClientOptions
// unset, so http(s)/file sources are completely unaffected by this fetcher's
// auth building.
func gitSSHAuth(rawURL string) (client.SSHAuth, error) {
	u, err := transport.ParseURL(rawURL)
	if err != nil || u.Scheme != "ssh" {
		return nil, nil
	}
	user := gogitssh.DefaultUsername
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			user = name
		}
	}

	if sshagent.Available() {
		return gogitssh.NewSSHAgentAuth(user)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("git ssh auth for %s: no SSH agent (SSH_AUTH_SOCK unset) and $HOME is unavailable to locate a default key: %w; run `eval $(ssh-agent) && ssh-add`, or export SSH_AUTH_SOCK", rawURL, err)
	}
	sshDir := filepath.Join(home, ".ssh")
	candidates := append(sshConfigIdentityFiles(sshDir, u.Hostname()),
		filepath.Join(sshDir, "id_ed25519"),
		filepath.Join(sshDir, "id_rsa"),
		filepath.Join(sshDir, "id_ecdsa"),
	)

	var lastErr error
	for _, path := range candidates {
		if _, statErr := os.Stat(path); statErr != nil {
			continue
		}
		keyAuth, keyErr := gogitssh.NewPublicKeysFromFile(user, path, "")
		if keyErr == nil {
			return keyAuth, nil
		}
		lastErr = fmt.Errorf("%s: %w", path, keyErr)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("git ssh auth for %s: no SSH agent (SSH_AUTH_SOCK unset) and the default SSH key needs a passphrase da cannot prompt for; run `eval $(ssh-agent) && ssh-add` first, or export SSH_AUTH_SOCK to your running agent: %w", rawURL, lastErr)
	}
	return nil, fmt.Errorf("git ssh auth for %s: no SSH agent (SSH_AUTH_SOCK unset) and no default SSH key found in %s (looked for id_ed25519, id_rsa, id_ecdsa); run `eval $(ssh-agent) && ssh-add` first, or export SSH_AUTH_SOCK to your running agent", rawURL, sshDir)
}

// sshConfigIdentityFiles returns the IdentityFile paths <sshDir>/config
// declares for host, in file order, tilde-expanded. It implements just the
// slice of ssh_config(5) this fetcher needs — literal/glob (`*`/`?`) `Host`
// patterns naming one or more `IdentityFile` lines — instead of pulling in a
// full config-parser dependency. A missing config file or no matching block
// yields nil, so the caller falls through to the OpenSSH default identity
// list unchanged.
func sshConfigIdentityFiles(sshDir, host string) []string {
	data, err := os.ReadFile(filepath.Join(sshDir, "config"))
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir() // best-effort tilde-expansion below

	var files []string
	matched := false
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			matched = hostPatternMatches(fields[1:], host)
		case "identityfile":
			if matched {
				files = append(files, expandTildePath(fields[1], home))
			}
		}
	}
	return files
}

// hostPatternMatches reports whether any of an ssh_config `Host` line's
// literal/glob (`*`/`?`) patterns matches host.
func hostPatternMatches(patterns []string, host string) bool {
	for _, pat := range patterns {
		if ok, _ := filepath.Match(pat, host); ok {
			return true
		}
	}
	return false
}

// expandTildePath expands a leading "~/" in path against home. Returns path
// unchanged when it has no such prefix or home is unknown.
func expandTildePath(path, home string) string {
	if home == "" || !strings.HasPrefix(path, "~/") {
		return path
	}
	return filepath.Join(home, path[2:])
}

func (f *gitFetcher) Fetch(src Source, parts LayerRefParts, cacheDir string) (FetchedLayer, error) {
	return f.FetchRefresh(src, parts, cacheDir, false)
}

// FetchRefresh resolves the ref→SHA, then serves the SHA-addressed cache unless
// forceRefresh is set (a stale cache key), in which case it re-reads the layer
// from the freshly cloned worktree and rewrites the cache.
func (f *gitFetcher) FetchRefresh(src Source, parts LayerRefParts, cacheDir string, forceRefresh bool) (FetchedLayer, error) {
	ref := parts.Version
	if ref == "" {
		ref = src.Ref
	}
	if ref == "" {
		ref = "main"
	}
	// Validate/classify the source URL up front so a malformed remote fails
	// before any network work. file:// (local fixture / on-disk repo) is a
	// legitimate clone source for hermetic use, so an ErrNotRemote "file"
	// classification is not itself an error here — only a hard parse failure
	// (and a non-file, non-empty result confirms a real remote).
	if _, err := gitremote.ParseRemoteURL(src.URL); err != nil && !errors.Is(err, gitremote.ErrNotRemote) {
		return FetchedLayer{}, fmt.Errorf("git source url %q: %w", src.URL, err)
	}

	// A clone resolves both the ref→SHA and the file@SHA. The ref may be a
	// branch or a tag; normalize to a full ref so go-git's single-branch
	// clone targets it. We accept the caller's value as-is first (already a
	// full refs/… name), else try it as a branch.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	repo, wfs, err := f.clone(ctx, src.URL, gitFullRef(ref))
	if err != nil {
		return FetchedLayer{}, fmt.Errorf("git clone %s @ %s: %w", src.URL, ref, err)
	}

	head, err := repo.Head()
	if err != nil {
		return FetchedLayer{}, fmt.Errorf("git resolve HEAD for %s @ %s: %w", src.URL, ref, err)
	}
	sha := head.Hash().String()
	if sha == "" {
		return FetchedLayer{}, fmt.Errorf("git ref %q not found at %s", ref, src.URL)
	}
	if !forceRefresh {
		if cached, ok := readCachedLayer(cacheDir, sha); ok {
			return FetchedLayer{Data: cached, ResolvedSHA: sha, CacheHit: true, KeyInputs: CacheKeyInputs{ResolvedCommit: sha}}, nil
		}
	}

	fh, err := wfs.Open(filepath.FromSlash(parts.LayerPath))
	if err != nil {
		return FetchedLayer{}, fmt.Errorf("git read %s@%s: %w", parts.LayerPath, sha, err)
	}
	defer func() { _ = fh.Close() }()
	data, err := readAllLimited(fh)
	if err != nil {
		return FetchedLayer{}, fmt.Errorf("git read %s@%s: %w", parts.LayerPath, sha, err)
	}

	if err := writeCachedLayer(cacheDir, sha, data); err != nil {
		return FetchedLayer{}, err
	}
	return FetchedLayer{Data: data, ResolvedSHA: sha, CacheHit: false, KeyInputs: CacheKeyInputs{ResolvedCommit: sha}}, nil
}

// gitFullRef expands a bare branch/tag name to a full refs/heads/<name> so
// go-git's single-branch clone targets it. A value already starting with
// "refs/" is passed through unchanged (caller pinned a full ref). This mirrors
// how clients normally resolve a branch-name clone target.
func gitFullRef(ref string) string {
	if strings.HasPrefix(ref, "refs/") {
		return ref
	}
	return "refs/heads/" + ref
}

// --- http fetcher ----------------------------------------------------------

// httpFetcher GETs a layer.json over HTTPS and caches it content-addressed by
// the content hash. HTTPS is enforced — a non-https URL is rejected before any
// request so the layer transport is always encrypted.
type httpFetcher struct {
	// client is a test seam; nil uses a default client with a timeout.
	client *http.Client
}

func (f *httpFetcher) Fetch(src Source, parts LayerRefParts, cacheDir string) (FetchedLayer, error) {
	return f.FetchRefresh(src, parts, cacheDir, false)
}

// FetchRefresh GETs the layer, then serves the SHA-addressed cache unless
// forceRefresh is set (a stale cache key), in which case it rewrites the cache
// with the freshly fetched bytes so the upstream is re-validated.
func (f *httpFetcher) FetchRefresh(src Source, parts LayerRefParts, cacheDir string, forceRefresh bool) (FetchedLayer, error) {
	url := strings.TrimRight(src.URL, "/") + "/" + strings.TrimLeft(parts.LayerPath, "/")
	if !strings.HasPrefix(strings.ToLower(url), "https://") {
		return FetchedLayer{}, fmt.Errorf("http source url must be https: %q", url)
	}
	client := f.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return FetchedLayer{}, fmt.Errorf("building request for %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return FetchedLayer{}, fmt.Errorf("http get %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return FetchedLayer{}, fmt.Errorf("http get %s: status %d", url, resp.StatusCode)
	}
	data, err := readAllLimited(resp.Body)
	if err != nil {
		return FetchedLayer{}, fmt.Errorf("reading %s: %w", url, err)
	}
	sha := contentHash(data)
	// Capture the upstream validators (ETag / Last-Modified) the http cache-key
	// default prefers over a content digest (config-distribution-model §7A.4), so
	// the resolver can derive an effective key that re-checks on a validator
	// change even when the SHA-addressed bytes are still cached.
	keyInputs := CacheKeyInputs{
		ETag:          resp.Header.Get("ETag"),
		LastModified:  resp.Header.Get("Last-Modified"),
		ContentDigest: sha,
	}
	if !forceRefresh {
		if cached, ok := readCachedLayer(cacheDir, sha); ok {
			return FetchedLayer{Data: cached, ResolvedSHA: sha, CacheHit: true, KeyInputs: keyInputs}, nil
		}
	}
	if err := writeCachedLayer(cacheDir, sha, data); err != nil {
		return FetchedLayer{}, err
	}
	return FetchedLayer{Data: data, ResolvedSHA: sha, CacheHit: false, KeyInputs: keyInputs}, nil
}

// --- local fetcher ---------------------------------------------------------

// localFetcher reads a layer file directly from the filesystem (dev/test only,
// spec §4). The "SHA" is the content hash of the bytes, so the lockfile is still
// well-formed and the cache is still content-addressed.
type localFetcher struct{}

func (f *localFetcher) Fetch(src Source, parts LayerRefParts, cacheDir string) (FetchedLayer, error) {
	base := src.Path
	if base == "" {
		base = src.URL
	}
	path := filepath.Join(base, filepath.FromSlash(parts.LayerPath))
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FetchedLayer{}, fmt.Errorf("local layer %s not found: %w", path, err)
		}
		return FetchedLayer{}, fmt.Errorf("reading local layer %s: %w", path, err)
	}
	sha := contentHash(data)
	// A local source has no committed SHA to pin against, so its working-tree
	// content IS the content (config-distribution-model §7A.4 / D6): mark the tree
	// dirty and supply the content hash as the precise worktree key, so authoring
	// before a commit still derives a distinct effective cache key.
	keyInputs := CacheKeyInputs{WorktreeDirty: true, WorktreeContentHash: sha}
	if err := writeCachedLayer(cacheDir, sha, data); err != nil {
		return FetchedLayer{}, err
	}
	return FetchedLayer{Data: data, ResolvedSHA: sha, CacheHit: false, KeyInputs: keyInputs}, nil
}
