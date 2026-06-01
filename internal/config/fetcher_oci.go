package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// This file adds the tier-2 (packages) source-type plumbing: the OCI registry
// pull path plus the http-as-packages path's shared types, and the signing-
// posture stub. Per config-distribution-model §4 the tier constraint is that
// `oci` is packages-only (never valid for extends) and `http` is valid for both
// tiers. The extends-only SelectFetcher (in fetcher.go) therefore continues to
// reject `oci`; package resolution (pass 2, p6) selects through
// SelectPackageFetcher here instead.

// SigningPosture is the declared verification stance for a fetched package
// artifact (config-distribution-model §12 scope boundary; signing brought in
// earlier per spec Q3). It is a stub in p5: the posture is recorded and the
// verify hook is wired, but real signature material (cosign/sigstore, spec
// external-agent-sources §6 roadmap) is not yet checked. The posture governs
// whether an unverifiable artifact is allowed to resolve.
type SigningPosture string

const (
	// PostureUnsigned: signatures are not expected; the artifact resolves on a
	// successful digest match alone. Default for the p5 stub.
	PostureUnsigned SigningPosture = "unsigned"
	// PostureOptional: a signature is verified when present but its absence is
	// not fatal (warn-and-continue once real verification lands).
	PostureOptional SigningPosture = "optional"
	// PostureRequired: a verified signature is mandatory; an unsigned or
	// unverifiable artifact must fail to resolve.
	PostureRequired SigningPosture = "required"
)

// Valid reports whether p is a recognized posture.
func (p SigningPosture) Valid() bool {
	switch p {
	case PostureUnsigned, PostureOptional, PostureRequired:
		return true
	default:
		return false
	}
}

// PostureFromSource derives the signing posture for a source. The posture is
// read from the opaque, pass-through auth block (the only source field whose
// schema is not owned by the config layer — external-agent-sources spec), under
// the well-known "signing" key, so no new typed Source field (and thus no
// agentsrc.go / struct-schema change) is required for the p5 stub. An absent,
// empty, or unrecognized value defaults to PostureUnsigned.
func PostureFromSource(src Source) SigningPosture {
	p := SigningPosture(strings.TrimSpace(authString(src.Auth, "signing")))
	if !p.Valid() {
		return PostureUnsigned
	}
	return p
}

// verifySignature is the signing-posture stub's enforcement hook. It does not
// yet check real signature material; it only enforces the posture contract
// against whether a verified signature was produced (always false in p5). When
// cosign/sigstore verification lands it replaces the `signed` argument with a
// real verification result and this function's logic is unchanged.
func verifySignature(posture SigningPosture, digest string, signed bool) error {
	if posture == PostureRequired && !signed {
		return &ImportError{
			Reason: ReasonAuth,
			Err:    fmt.Errorf("signing posture %q requires a verified signature for digest %s but none is available", posture, digest),
		}
	}
	return nil
}

// FetchedArtifact is the result of a tier-2 package pull: the raw artifact
// bytes, the content digest they were fetched at (the cache key and lockfile
// digest, spec §7), whether the bytes came from cache, and the signing posture
// that governed the pull.
type FetchedArtifact struct {
	// Data is the raw artifact blob.
	Data []byte
	// Digest is the canonical "sha256:<hex>" content digest (spec §7.2).
	Digest string
	// CacheHit reports whether Data came from the local package cache.
	CacheHit bool
	// Posture is the signing posture applied to this pull.
	Posture SigningPosture
}

// PackageRefParts is the parsed form of a "source-id:artifact-path@version-spec"
// packages ref (config-distribution-model §5). Unlike extends refs, the version
// spec is required for packages.
type PackageRefParts struct {
	SourceID     string
	ArtifactPath string
	VersionSpec  string
}

// ParsePackageRef splits "source-id:artifact-path@version-spec" into its parts.
// The source-id is everything before the first ':'; the version spec (required)
// is everything after the last '@'. A missing ':' / '@', or an empty component,
// is a parse error (spec §5: @version-spec is required for packages).
func ParsePackageRef(ref string) (PackageRefParts, error) {
	colon := strings.IndexByte(ref, ':')
	if colon <= 0 {
		return PackageRefParts{}, fmt.Errorf("package ref %q must be 'source-id:artifact-path@version-spec'", ref)
	}
	parts := PackageRefParts{SourceID: ref[:colon]}
	rest := ref[colon+1:]
	at := strings.LastIndexByte(rest, '@')
	if at < 0 {
		return PackageRefParts{}, fmt.Errorf("package ref %q is missing the required @version-spec", ref)
	}
	parts.ArtifactPath = rest[:at]
	parts.VersionSpec = rest[at+1:]
	if parts.ArtifactPath == "" {
		return PackageRefParts{}, fmt.Errorf("package ref %q has empty artifact-path", ref)
	}
	if parts.VersionSpec == "" {
		return PackageRefParts{}, fmt.Errorf("package ref %q has empty version-spec", ref)
	}
	return parts, nil
}

// PackageFetcher pulls a tier-2 package artifact from a resolved source. One
// impl per packages-valid source type (oci, http). The interface is the test
// seam: a fake stands in so no test touches a real registry or the network.
type PackageFetcher interface {
	// FetchArtifact returns the artifact blob for parts.ArtifactPath@VersionSpec
	// from src, content-addressed and cached under the packages cache root.
	FetchArtifact(src Source, parts PackageRefParts) (FetchedArtifact, error)
}

// SelectPackageFetcher returns the PackageFetcher for a source type, or an error
// for a source type that is not valid for packages. This is the pass-2 (p6)
// counterpart to SelectFetcher: packages accept oci and http; git and local are
// rejected as a tier/schema violation (spec §4).
func SelectPackageFetcher(sourceType string) (PackageFetcher, error) {
	switch sourceType {
	case "oci":
		return &ociFetcher{}, nil
	case "http":
		return &httpArtifactFetcher{}, nil
	case "git", "local":
		return nil, fmt.Errorf("source type %q is not valid for packages (use oci or http)", sourceType)
	default:
		return nil, fmt.Errorf("unsupported source type %q", sourceType)
	}
}

// packagesCacheRoot is the tier-2 artifact cache root: ~/.agents/cache/packages.
// Artifacts are strictly content-addressed by digest and never expire (spec §8).
func packagesCacheRoot() string {
	return filepath.Join(AgentsHome(), "cache", "packages")
}

// cachedArtifactPath is the absolute path of a cached artifact blob for a
// digest. The "sha256:" prefix is stripped so the on-disk directory is a clean
// hex name (spec §8: ~/.agents/cache/packages/<digest>/).
func cachedArtifactPath(digest string) string {
	return filepath.Join(packagesCacheRoot(), digestDir(digest), "artifact.blob")
}

// digestDir maps a canonical "sha256:<hex>" digest to its cache subdirectory
// name (the bare hex), tolerating a digest passed without the algo prefix.
func digestDir(digest string) string {
	if i := strings.IndexByte(digest, ':'); i >= 0 {
		return digest[i+1:]
	}
	return digest
}

// artifactDigest computes the canonical "sha256:<hex>" content digest of data.
func artifactDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// readCachedArtifact returns the cached blob for digest, or (nil,false).
func readCachedArtifact(digest string) ([]byte, bool) {
	data, err := os.ReadFile(cachedArtifactPath(digest))
	if err != nil {
		return nil, false
	}
	return data, true
}

// authString returns the string value of a top-level key in an opaque auth
// block, or "" if the block is empty, not an object, or the key is absent or
// not a string. It lets the config layer read well-known pass-through keys
// (e.g. "signing") without owning the auth schema (external-agent-sources spec).
func authString(auth json.RawMessage, key string) string {
	if len(auth) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(auth, &m); err != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// writeCachedArtifact persists blob under the content-addressed packages cache.
func writeCachedArtifact(digest string, data []byte) error {
	dir := filepath.Join(packagesCacheRoot(), digestDir(digest))
	if err := fsops.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating package cache dir: %w", err)
	}
	return fsops.WriteFile(filepath.Join(dir, "artifact.blob"), data, 0o644)
}

// --- oci fetcher -----------------------------------------------------------

// ociFetcher pulls a package artifact over the OCI Distribution wire protocol
// and caches it content-addressed by digest. The wire protocol (manifest +
// blob fetch, token auth) is owned by the external-agent-sources spec; this
// p5 plumbing models the registry pull behind a `puller` test seam so pass-2
// (p6) and tests can drive it without a live registry. The signing-posture stub
// gates whether an unverifiable pull is allowed.
type ociFetcher struct {
	// puller is the test seam over the real OCI Distribution pull. Nil uses
	// ociPull, which is the not-yet-wired real registry client (returns a
	// transport error in p5 — the live wire protocol lands with p6). It returns
	// the artifact bytes and the registry-reported digest.
	puller func(ctx context.Context, ref ociRef, auth []byte) (data []byte, digest string, err error)
}

// ociRef is a resolved OCI reference: registry + repository + the tag or digest
// to pull (config-distribution-model §5).
type ociRef struct {
	Registry   string // e.g. "registry.acme.internal"
	Repository string // e.g. "dot-agents/skill/review-pr"
	Tag        string // resolved tag or version spec
	Digest     string // optional digest pin ("sha256:..."); when set, Tag is ignored
}

// parseOCIRef builds an ociRef from a source URL (oci://registry/base-path) and
// a package ref's artifact path + version spec. A "pinned:sha256:..." version
// spec (spec §5) becomes a digest pin; any other spec is treated as a tag.
func parseOCIRef(src Source, parts PackageRefParts) (ociRef, error) {
	url := strings.TrimSpace(src.URL)
	if url == "" {
		return ociRef{}, fmt.Errorf("oci source has no url")
	}
	// A non-oci scheme is a hard error (an http(s) URL is the http source type,
	// not oci). A bare "registry/base" with no scheme is accepted as oci.
	if i := strings.Index(url, "://"); i >= 0 && url[:i] != "oci" {
		return ociRef{}, fmt.Errorf("oci source url must use the oci:// scheme: %q", url)
	}
	rest := strings.TrimPrefix(url, "oci://")
	rest = strings.Trim(rest, "/")
	registry := rest
	basePath := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		registry = rest[:i]
		basePath = rest[i+1:]
	}
	if registry == "" {
		return ociRef{}, fmt.Errorf("oci source url %q has no registry host", url)
	}
	repo := strings.Trim(parts.ArtifactPath, "/")
	if basePath != "" {
		repo = basePath + "/" + repo
	}
	ref := ociRef{Registry: registry, Repository: repo}
	if d, ok := digestFromVersionSpec(parts.VersionSpec); ok {
		ref.Digest = d
	} else {
		ref.Tag = parts.VersionSpec
	}
	return ref, nil
}

// digestFromVersionSpec extracts a "sha256:..." digest from a pinned version
// spec ("pinned:sha256:abc..."), returning ok=false for tag/range specs.
func digestFromVersionSpec(spec string) (string, bool) {
	const pin = "pinned:"
	if strings.HasPrefix(spec, pin) {
		d := spec[len(pin):]
		if strings.HasPrefix(d, "sha256:") {
			return d, true
		}
	}
	return "", false
}

func (f *ociFetcher) FetchArtifact(src Source, parts PackageRefParts) (FetchedArtifact, error) {
	posture := PostureFromSource(src)
	ref, err := parseOCIRef(src, parts)
	if err != nil {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonSchema, Err: err}
	}

	// A digest-pinned ref is content-addressed up front, so the cache is checked
	// before any network work (offline fast path, spec §8).
	if ref.Digest != "" {
		if cached, ok := readCachedArtifact(ref.Digest); ok {
			if err := verifySignature(posture, ref.Digest, false); err != nil {
				return FetchedArtifact{}, err
			}
			return FetchedArtifact{Data: cached, Digest: ref.Digest, CacheHit: true, Posture: posture}, nil
		}
	}

	pull := f.puller
	if pull == nil {
		pull = ociPull
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, digest, err := pull(ctx, ref, src.Auth)
	if err != nil {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonTransport, Err: err}
	}
	if digest == "" {
		digest = artifactDigest(data)
	}
	// A digest pin must match the registry-reported digest, else the content is
	// not what was requested (tamper / mismatch -> content failure).
	if ref.Digest != "" && digest != ref.Digest {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonContent, Err: fmt.Errorf("digest mismatch: pinned %s but registry served %s", ref.Digest, digest)}
	}
	if err := verifySignature(posture, digest, false); err != nil {
		return FetchedArtifact{}, err
	}
	if err := writeCachedArtifact(digest, data); err != nil {
		return FetchedArtifact{}, err
	}
	return FetchedArtifact{Data: data, Digest: digest, CacheHit: false, Posture: posture}, nil
}

// ociPull is the real OCI Distribution pull, not yet wired in p5. The live wire
// protocol (manifest fetch, blob fetch, token auth) lands with pass-2 (p6); for
// now it deterministically reports a transport error so a misconfigured run
// fails loudly rather than silently, while the `puller` seam lets tests and p6
// drive the fetcher's caching/posture logic.
func ociPull(_ context.Context, ref ociRef, _ []byte) ([]byte, string, error) {
	return nil, "", fmt.Errorf("oci wire protocol not yet wired (registry=%s repo=%s); pass-2 packages resolution implements the pull", ref.Registry, ref.Repository)
}
