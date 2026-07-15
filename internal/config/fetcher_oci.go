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

// This file adds the OCI registry source-type plumbing: the shared blob pull
// (manifest + blob fetch, token auth, digest/posture/cache) plus the artifact
// (packages) fetcher built on it. Per config-distribution-model §15 D8+D13 any
// source type may serve any unit kind — `oci` is valid for both `packages`
// (artifacts) and `extends` (config layers). What stays meaningful is the unit's
// media type: an artifact pull must carry the artifact-bundle media type and a
// layer pull the config-layer media type (the guard below + its mirror in
// fetcher_oci_layer.go), so `kind` is enforced by the blob, not by which source
// is permitted. The shared pull is factored here and reused by the layer fetcher.

const (
	// ociArtifactMediaType is the media type of a tier-2 package artifact bundle
	// published to OCI (config-distribution-model §15 D8). A `packages` pull must
	// resolve to this media type.
	ociArtifactMediaType = "application/vnd.dot-agents.artifact-bundle.v1+tar+gzip"
	// ociLayerMediaType is the dedicated media type of a config layer published to
	// OCI as a single blob carrying the layer.json document (config-distribution-
	// model §15 D13). An `extends` pull must resolve to this media type.
	ociLayerMediaType = "application/vnd.dot-agents.config-layer.v1+json"
)

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
	// KeyInputs carries the resolved facts for the source's effective content
	// cache key (config-distribution-model §7A.4): the manifest digest for oci,
	// the digest (plus any ETag/Last-Modified validator) for http-as-packages, so
	// the package resolver can derive an effective key via EffectiveCacheKey. A
	// zero value falls back to the kind default keyed on Digest.
	KeyInputs CacheKeyInputs
	// Bundle is the normalized, in-memory file tree (package-artifact-install
	// spec D3/H1) for an artifact whose content layout is "tree" (a git/local
	// subtree walk) or "tarball" (an archive untar, sniffed from Data). It is
	// nil for a plain single-file artifact pull. Every non-nil Bundle has
	// already passed the H1 fail-closed normalizer (NormalizeBundle /
	// UntarBundle); Data/Digest continue to address the fetched bytes exactly
	// as before a Bundle is populated, unaffected by it.
	Bundle *Bundle
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
// impl per source type (oci, http, git, local). The interface is the test
// seam: a fake stands in so no test touches a real registry or the network.
type PackageFetcher interface {
	// FetchArtifact returns the artifact blob for parts.ArtifactPath@VersionSpec
	// from src, content-addressed and cached under the packages cache root.
	FetchArtifact(src Source, parts PackageRefParts) (FetchedArtifact, error)
}

// SelectPackageFetcher returns the PackageFetcher for a source type, or an error
// for an unsupported source type. This is the packages counterpart to
// SelectFetcher: per config-distribution-model §15 D3+D8+D13, any source type
// may serve any unit kind — the KIND (governed by the unit's media type), not
// the source, decides merge/trust. Packages/artifacts accept all four source
// types (git, local, http, oci), and after D13 so does extends; there is no
// remaining source/kind asymmetry.
func SelectPackageFetcher(sourceType string) (PackageFetcher, error) {
	switch sourceType {
	case "oci":
		return &ociFetcher{}, nil
	case "http":
		return &httpArtifactFetcher{}, nil
	case "git":
		return &gitArtifactFetcher{}, nil
	case "local":
		return &localArtifactFetcher{}, nil
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

// readCachedArtifact returns the cached blob for digest, or (nil,false). Per
// H8 (package-artifact-install spec §3A), every cache hit is re-verified
// against digest before it is trusted: a concurrent writer's same-dir
// temp+rename (writeCachedArtifact) means a reader never observes a partial
// file, but a corrupt or tampered blob that was fully written under the
// wrong digest is still possible, so the digest is recomputed here rather
// than trusted from the cache path alone. A verification failure is treated
// exactly like a cache miss (ok=false): the caller falls through to a fresh
// fetch instead of ever returning bytes that do not match what was asked
// for.
func readCachedArtifact(digest string) ([]byte, bool) {
	data, err := os.ReadFile(cachedArtifactPath(digest))
	if err != nil {
		return nil, false
	}
	if artifactDigest(data) != digest {
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

// writeCachedArtifact persists blob under the content-addressed packages
// cache. Per H8, the publish is a same-dir temp+rename
// (fsops.WriteFileAtomic): the blob is written to a temp file in dir and
// renamed into place, so a concurrent reader (readCachedArtifact) never
// observes a partially-written file — closing the torn-read window a plain
// truncate-and-write leaves open.
func writeCachedArtifact(digest string, data []byte) error {
	dir := filepath.Join(packagesCacheRoot(), digestDir(digest))
	if err := fsops.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating package cache dir: %w", err)
	}
	if err := fsops.WriteFileAtomic(filepath.Join(dir, "artifact.blob"), data); err != nil {
		return fmt.Errorf("writing package cache blob: %w", err)
	}
	return nil
}

// --- oci fetcher -----------------------------------------------------------

// ociBlob is the result of a single OCI blob pull: the blob bytes, the
// registry-reported content digest ("" lets the caller compute it), and the
// blob's media type (from the resolving manifest's config/layer descriptor).
// The media type is what keeps `kind` meaningful now that any source serves any
// kind (config-distribution-model §15 D13): an artifact pull and a layer pull
// share this pull but guard on different media types.
type ociBlob struct {
	Data      []byte
	Digest    string
	MediaType string
}

// ociPuller is the shared OCI Distribution pull seam used by both the artifact
// fetcher and the layer fetcher. Nil uses ociPull, the not-yet-wired real
// registry client (returns a transport error until the live wire protocol lands;
// the seam lets tests and the resolver drive the caching/posture/media-type
// logic without a live registry).
type ociPuller func(ctx context.Context, ref ociRef, auth []byte) (ociBlob, error)

// ociFetcher pulls a package artifact over the OCI Distribution wire protocol
// and caches it content-addressed by digest. The wire protocol (manifest +
// blob fetch, token auth) is owned by the external-agent-sources spec; the
// registry pull is modeled behind the shared `puller` seam so the resolver and
// tests can drive it without a live registry. The signing-posture stub gates
// whether an unverifiable pull is allowed; the media-type guard rejects a
// config-layer blob served to a `packages` ref (mirror of the layer fetcher's
// guard), so `kind` stays meaningful.
type ociFetcher struct {
	puller ociPuller
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
	importRef := parts.SourceID + ":" + parts.ArtifactPath
	ref, err := parseOCIRef(src, parts)
	if err != nil {
		return FetchedArtifact{}, &ImportError{Ref: importRef, SourceID: parts.SourceID, Reason: ReasonSchema, Err: err}
	}
	pulled, err := pullOCIContent(f.puller, src, ref, importRef, parts.SourceID, ociArtifactMediaType)
	if err != nil {
		return FetchedArtifact{}, err
	}
	if err := writeCachedArtifact(pulled.Digest, pulled.Data); err != nil {
		return FetchedArtifact{}, err
	}
	return FetchedArtifact{
		Data:      pulled.Data,
		Digest:    pulled.Digest,
		CacheHit:  pulled.CacheHit,
		Posture:   pulled.Posture,
		KeyInputs: CacheKeyInputs{OCIDigest: pulled.Digest},
	}, nil
}

// ociContent is the shared, kind-agnostic result of pullOCIContent: the resolved
// blob bytes + canonical digest, whether they came from cache, and the signing
// posture applied. The caller persists them under its own cache root (packages
// vs config) and wraps them in its own typed result.
type ociContent struct {
	Data     []byte
	Digest   string
	CacheHit bool
	Posture  SigningPosture
}

// pullOCIContent is the single OCI pull shared by the artifact and layer
// fetchers (config-distribution-model §15 D13: one pull, two kinds). It applies
// the offline digest-pin cache fast path, runs the pull seam, computes/validates
// the digest, enforces the signing posture, and guards the media type so the
// pulled blob's kind matches the caller's declared kind. wantMediaType is the
// media type the caller (packages vs extends) requires; a mismatch is a clear
// schema error so `kind` stays meaningful even though source is unrestricted.
func pullOCIContent(puller ociPuller, src Source, ref ociRef, importRef, sourceID, wantMediaType string) (ociContent, error) {
	posture := PostureFromSource(src)
	// A digest-pinned ref is content-addressed up front, so the cache is checked
	// before any network work (offline fast path, spec §8). A cache hit has no
	// manifest to re-read, so the media type was already validated when it was
	// first pulled and cached.
	if ref.Digest != "" {
		if cached, ok := readCachedArtifact(ref.Digest); ok {
			if err := verifySignature(posture, ref.Digest, false); err != nil {
				return ociContent{}, err
			}
			return ociContent{Data: cached, Digest: ref.Digest, CacheHit: true, Posture: posture}, nil
		}
	}

	pull := puller
	if pull == nil {
		pull = ociPull
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	blob, err := pull(ctx, ref, src.Auth)
	if err != nil {
		return ociContent{}, &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonTransport, Err: err}
	}
	digest := blob.Digest
	if digest == "" {
		digest = artifactDigest(blob.Data)
	}
	if err := guardOCIPull(ref, digest, blob.MediaType, wantMediaType, importRef, sourceID); err != nil {
		return ociContent{}, err
	}
	if err := verifySignature(posture, digest, false); err != nil {
		return ociContent{}, err
	}
	return ociContent{Data: blob.Data, Digest: digest, CacheHit: false, Posture: posture}, nil
}

// guardOCIPull validates a freshly pulled blob: the digest must match a pin
// (tamper guard) and the media type must match the caller's declared kind (the
// §15 D13 kind guard). An empty served media type is tolerated as the requested
// kind so a registry that omits the descriptor type still resolves.
func guardOCIPull(ref ociRef, digest, gotMediaType, wantMediaType, importRef, sourceID string) error {
	if ref.Digest != "" && digest != ref.Digest {
		return &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonContent, Err: fmt.Errorf("digest mismatch: pinned %s but registry served %s", ref.Digest, digest)}
	}
	if gotMediaType != "" && gotMediaType != wantMediaType {
		return &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonSchema, Err: fmt.Errorf("oci media type %q does not match required %q", gotMediaType, wantMediaType)}
	}
	return nil
}

// ociPull is the real OCI Distribution pull, not yet wired. The live wire
// protocol (manifest fetch, blob fetch, token auth) lands with pass-2 packages
// resolution; for now it deterministically reports a transport error so a
// misconfigured run fails loudly rather than silently, while the `puller` seam
// lets tests and the resolver drive the fetcher's caching/posture/media-type
// logic.
func ociPull(_ context.Context, ref ociRef, _ []byte) (ociBlob, error) {
	return ociBlob{}, fmt.Errorf("oci wire protocol not yet wired (registry=%s repo=%s); the live registry pull implements this", ref.Registry, ref.Repository)
}
