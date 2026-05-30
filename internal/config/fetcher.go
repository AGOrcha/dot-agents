package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/fsops"
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

// SelectFetcher returns the Fetcher for a source type, or an error for an
// unsupported or tier-invalid type. oci is valid only for packages (pass 2),
// never for extends, so it is rejected here as a schema violation.
func SelectFetcher(sourceType string) (Fetcher, error) {
	switch sourceType {
	case "git":
		return &gitFetcher{}, nil
	case "http":
		return &httpFetcher{}, nil
	case "local":
		return &localFetcher{}, nil
	case "oci":
		return nil, fmt.Errorf("source type %q is not valid for extends (oci is packages-only)", sourceType)
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
func cachedLayerPath(cacheDir, sha string) string {
	return filepath.Join(cacheDir, sha, "layer.json")
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

// writeCachedLayer persists layer bytes under <cacheDir>/<sha>/layer.json.
func writeCachedLayer(cacheDir, sha string, data []byte) error {
	dir := filepath.Join(cacheDir, sha)
	if err := fsops.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating layer cache dir: %w", err)
	}
	return fsops.WriteFile(filepath.Join(dir, "layer.json"), data, 0o644)
}

// --- git fetcher -----------------------------------------------------------

// gitFetcher resolves a git source ref to a commit SHA, fetches the layer file
// at that SHA, and caches it content-addressed by SHA. It shells out to `git`
// via os/exec; on Go 1.26 os/exec already carries the execabs CWD guard, so a
// bare exec.Command("git", …) is safe — no golang.org/x/sys/execabs needed.
type gitFetcher struct {
	// runner is a test seam over command execution. Nil uses the real git.
	runner func(args ...string) ([]byte, error)
}

func (f *gitFetcher) run(args ...string) ([]byte, error) {
	if f.runner != nil {
		return f.runner(args...)
	}
	// Constant binary name + arg slice (never a shell string): the safe exec
	// pattern. On Go 1.26 os/exec already carries the execabs CWD guard.
	return exec.Command("git", args...).Output()
}

func (f *gitFetcher) Fetch(src Source, parts LayerRefParts, cacheDir string) (FetchedLayer, error) {
	ref := parts.Version
	if ref == "" {
		ref = src.Ref
	}
	if ref == "" {
		ref = "main"
	}
	// Resolve the ref to an immutable SHA so the cache is content-addressed.
	out, err := f.run("ls-remote", src.URL, ref)
	if err != nil {
		return FetchedLayer{}, fmt.Errorf("git ls-remote %s %s: %w", src.URL, ref, err)
	}
	sha := firstField(string(out))
	if sha == "" {
		return FetchedLayer{}, fmt.Errorf("git ref %q not found at %s", ref, src.URL)
	}
	if cached, ok := readCachedLayer(cacheDir, sha); ok {
		return FetchedLayer{Data: cached, ResolvedSHA: sha, CacheHit: true}, nil
	}
	// Fetch just the layer file content at the resolved SHA without a full
	// checkout: archive the single path and read it from the cache.
	data, err := f.run("archive", "--remote="+src.URL, sha, parts.LayerPath)
	if err != nil {
		return FetchedLayer{}, fmt.Errorf("git fetch %s@%s: %w", parts.LayerPath, sha, err)
	}
	if err := writeCachedLayer(cacheDir, sha, data); err != nil {
		return FetchedLayer{}, err
	}
	return FetchedLayer{Data: data, ResolvedSHA: sha, CacheHit: false}, nil
}

// firstField returns the first whitespace-delimited field of s (the SHA column
// of `git ls-remote` output), or "" if s is blank.
func firstField(s string) string {
	for _, f := range strings.Fields(s) {
		return f
	}
	return ""
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
	if cached, ok := readCachedLayer(cacheDir, sha); ok {
		return FetchedLayer{Data: cached, ResolvedSHA: sha, CacheHit: true}, nil
	}
	if err := writeCachedLayer(cacheDir, sha, data); err != nil {
		return FetchedLayer{}, err
	}
	return FetchedLayer{Data: data, ResolvedSHA: sha, CacheHit: false}, nil
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
	if err := writeCachedLayer(cacheDir, sha, data); err != nil {
		return FetchedLayer{}, err
	}
	return FetchedLayer{Data: data, ResolvedSHA: sha, CacheHit: false}, nil
}
