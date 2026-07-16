package config

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
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
// Callers that use the result as a filesystem path MUST first gate the digest
// through looksLikeSha256Digest — otherwise a hostile "sha256:../../etc"
// digest (e.g. from a `pinned:` version spec) would become a traversal segment.
func digestDir(digest string) string {
	if i := strings.IndexByte(digest, ':'); i >= 0 {
		return digest[i+1:]
	}
	return digest
}

// looksLikeSha256Digest reports whether digest is a well-formed
// "sha256:<64 lowercase hex>" string. It is the canonicalize-once gate that
// keeps an attacker-influenced digest (e.g. a "pinned:sha256:../../etc"
// version spec) from ever becoming a cache-path component: digestDir would
// otherwise turn "../../etc" into a directory-traversal segment when joined
// under the packages cache root.
func looksLikeSha256Digest(digest string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(digest, prefix) {
		return false
	}
	hexPart := digest[len(prefix):]
	if len(hexPart) != 64 {
		return false
	}
	for i := 0; i < len(hexPart); i++ {
		c := hexPart[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
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
	// Refuse to turn a malformed/attacker-controlled digest into a cache path
	// (traversal guard); an unparseable digest is simply a miss.
	if !looksLikeSha256Digest(digest) {
		return nil, false
	}
	// Confine the read to the packages cache root and apply the same discipline
	// as the local tree reader: no-follow Lstat + fstat identity + size cap +
	// bounded copy, so a huge or symlinked cache entry is neither followed nor
	// fully allocated before the digest is verified.
	root, err := os.OpenRoot(packagesCacheRoot())
	if err != nil {
		return nil, false
	}
	defer func() { _ = root.Close() }()
	data, ok := readConfinedCacheBlob(root, filepath.Join(digestDir(digest), "artifact.blob"), DefaultBundleLimits().MaxFileBytes)
	if !ok {
		return nil, false
	}
	if artifactDigest(data) != digest {
		return nil, false
	}
	return data, true
}

// readConfinedCacheBlob reads rel confined under root (the packages cache),
// mirroring the local-tree read discipline: it Lstats (no-follow) and rejects
// a symlinked or non-regular cache entry, rejects an over-cap size BEFORE
// opening, verifies the opened fd's identity against the Lstat via os.SameFile
// (defeating a symlink swapped in after the Lstat), and bounds the copy to
// maxBytes+1. A huge or symlinked cache file is therefore never followed or
// fully allocated before its digest is verified. Any anomaly is a cache miss.
func readConfinedCacheBlob(root *os.Root, rel string, maxBytes int64) ([]byte, bool) {
	li, err := root.Lstat(rel)
	if err != nil {
		return nil, false
	}
	if li.Mode()&fs.ModeSymlink != 0 || !li.Mode().IsRegular() {
		return nil, false
	}
	if li.Size() > maxBytes {
		return nil, false
	}
	fh, err := root.Open(rel)
	if err != nil {
		return nil, false
	}
	defer func() { _ = fh.Close() }()
	fi, err := fh.Stat()
	if err != nil {
		return nil, false
	}
	if !fi.Mode().IsRegular() || !os.SameFile(li, fi) || fi.Size() > maxBytes {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(fh, maxBytes+1))
	if err != nil {
		return nil, false
	}
	if int64(len(data)) > maxBytes {
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
// cache. Per H8, the publish is a same-dir temp+rename so a concurrent reader
// (readCachedArtifact) never observes a partially-written file — closing the
// torn-read window a plain truncate-and-write leaves open. The write is
// confined beneath the cache root through an os.Root handle (Finding 3): the
// digest subdir is the only attacker-influenceable path component, so a
// symlink/junction pre-created or raced at that component cannot redirect the
// write outside the cache.
func writeCachedArtifact(digest string, data []byte) error {
	// A cache blob is only ever addressed by a well-formed content digest;
	// refuse to write under a malformed digest that could escape the cache root.
	if !looksLikeSha256Digest(digest) {
		return fmt.Errorf("refusing to cache artifact under malformed digest %q", digest)
	}
	return writeConfinedPackagesCacheFile(digestDir(digest), "artifact.blob", data)
}

// ociTypeSidecarName is the per-digest sidecar recording the OCI type metadata
// (manifest artifactType + layer media type) that a fresh packages/artifact
// pull validated before caching the blob. Its PRESENCE (alongside a matching
// content-addressed blob) is the trust signal that a cached blob was admitted
// as an OCI artifact-bundle; a blob seeded into the shared cache by another
// source type (git/local/http) or by direct seeding carries no sidecar and is
// therefore never trusted as an OCI artifact on a cache hit (Finding 2 / H6).
const ociTypeSidecarName = "oci-type.json"

// ociTypeSidecar is the on-disk shape of the OCI type sidecar.
type ociTypeSidecar struct {
	ArtifactType string `json:"artifactType"`
	MediaType    string `json:"mediaType"`
}

// writeOCITypeSidecar records the validated OCI type metadata for a cached
// artifact blob, confined beneath the packages cache root exactly like the
// blob write. It is written only after guardOCIArtifactType has accepted a
// fresh pull, so a present sidecar always attests a validated artifact-bundle.
func writeOCITypeSidecar(digest, artifactType, mediaType string) error {
	if !looksLikeSha256Digest(digest) {
		return fmt.Errorf("refusing to cache oci type metadata under malformed digest %q", digest)
	}
	payload, err := json.Marshal(ociTypeSidecar{ArtifactType: artifactType, MediaType: mediaType})
	if err != nil {
		return fmt.Errorf("marshaling oci type sidecar: %w", err)
	}
	return writeConfinedPackagesCacheFile(digestDir(digest), ociTypeSidecarName, payload)
}

// readOCITypeSidecar loads the OCI type sidecar for a digest, confined beneath
// the packages cache root with the same no-follow + bounded-read discipline as
// readCachedArtifact. A missing, oversized, symlinked, or malformed sidecar is
// simply absent (ok=false), never an error the caller must special-case.
func readOCITypeSidecar(digest string) (ociTypeSidecar, bool) {
	if !looksLikeSha256Digest(digest) {
		return ociTypeSidecar{}, false
	}
	root, err := os.OpenRoot(packagesCacheRoot())
	if err != nil {
		return ociTypeSidecar{}, false
	}
	defer func() { _ = root.Close() }()
	data, ok := readConfinedCacheBlob(root, filepath.Join(digestDir(digest), ociTypeSidecarName), 4096)
	if !ok {
		return ociTypeSidecar{}, false
	}
	var sc ociTypeSidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		return ociTypeSidecar{}, false
	}
	return sc, true
}

// readCachedOCIArtifact returns the cached blob for digest ONLY when it is
// backed by a valid OCI type sidecar declaring the artifact-bundle media type
// at BOTH the manifest (artifactType) and layer (mediaType) levels (H6 on
// cache hits, Finding 2). A cached blob with no sidecar, or one whose recorded
// types do not match, is reported as a MISS so the caller re-resolves the
// manifest and re-validates the types before trusting the bytes — a blob
// seeded through any non-OCI route is never materialized as an OCI artifact
// without the OCI type contract being met.
func readCachedOCIArtifact(digest string) ([]byte, bool) {
	data, ok := readCachedArtifact(digest)
	if !ok {
		return nil, false
	}
	sc, ok := readOCITypeSidecar(digest)
	if !ok || sc.ArtifactType != ociArtifactMediaType || sc.MediaType != ociArtifactMediaType {
		return nil, false
	}
	return data, true
}

// writeConfinedPackagesCacheFile writes data to <packagesCacheRoot>/<relDir>/
// <name> via a same-dir temp+rename performed entirely through an os.Root
// confined beneath the cache root. The cache ROOT path is derived from
// AgentsHome (trusted) and created unconfined; every step BELOW it — the
// digest subdir create, the temp create/write, and the atomic rename — goes
// through the root handle, so no attacker-controlled component (a symlink or
// Windows junction pre-created at the digest dir) can redirect the write
// outside the cache (Finding 3). A digest dir that is itself a symlink is
// rejected outright as defense in depth over os.Root's escape check.
func writeConfinedPackagesCacheFile(relDir, name string, data []byte) error {
	if err := fsops.MkdirAll(packagesCacheRoot(), 0o755); err != nil {
		return fmt.Errorf("creating package cache root: %w", err)
	}
	root, err := os.OpenRoot(packagesCacheRoot())
	if err != nil {
		return fmt.Errorf("opening package cache root: %w", err)
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll(relDir, 0o755); err != nil {
		return fmt.Errorf("creating package cache dir: %w", err)
	}
	if li, err := root.Lstat(relDir); err != nil {
		return fmt.Errorf("stat package cache dir: %w", err)
	} else if li.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write through a symlinked package cache dir %q", relDir)
	}
	tmpName, err := randomTempName(name)
	if err != nil {
		return err
	}
	tmpRel := filepath.Join(relDir, tmpName)
	fh, err := root.OpenFile(tmpRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("creating package cache temp: %w", err)
	}
	if _, err := fh.Write(data); err != nil {
		_ = fh.Close()
		_ = root.Remove(tmpRel)
		return fmt.Errorf("writing package cache temp: %w", err)
	}
	if err := fh.Close(); err != nil {
		_ = root.Remove(tmpRel)
		return fmt.Errorf("closing package cache temp: %w", err)
	}
	if err := root.Rename(tmpRel, filepath.Join(relDir, name)); err != nil {
		_ = root.Remove(tmpRel)
		return fmt.Errorf("renaming package cache file: %w", err)
	}
	return nil
}

// randomTempName derives a collision-resistant, hidden temp filename for a
// same-dir atomic write, so concurrent writers of the same digest never race
// on a shared temp path.
func randomTempName(base string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating package cache temp name: %w", err)
	}
	return "." + base + ".tmp-" + hex.EncodeToString(b[:]), nil
}

// --- oci fetcher -----------------------------------------------------------

// ociBlob is the result of a single OCI blob pull: the blob bytes, the two
// distinct digests the manifest resolution surfaces (H5: the layer-descriptor
// digest of the payload vs the manifest's own digest — these are DIFFERENT
// objects and must never be conflated), and the blob's media type (from the
// resolving manifest's config/layer descriptor). The media type is what keeps
// `kind` meaningful now that any source serves any kind (config-distribution-
// model §15 D13): an artifact pull and a layer pull share this pull but guard
// on different media types.
type ociBlob struct {
	Data []byte
	// Digest is the manifest's LAYER/BLOB-DESCRIPTOR digest — the registry's
	// declared digest of Data (the payload/layer). It is NOT trusted as a label:
	// pullOCIContent ALWAYS recomputes sha256(Data) and, when this is non-empty,
	// requires it to match before the payload is type-checked, untarred, or
	// cached (H5 integrity). This is the only integrity anchor on a tag pull
	// (no pin) and is enforced on pinned pulls too. Empty is tolerated only for
	// a registry that omits the descriptor digest.
	Digest string
	// ManifestDigest is the manifest's OWN digest — the value a `pinned:sha256:`
	// ref addresses (H5: "pinned addresses the manifest"). It is a DIFFERENT
	// object than the layer blob, so a pin is validated against THIS, not the
	// payload digest (verifyOCIPin). Empty until the live wire protocol that
	// fetches+hashes the manifest is wired; while empty, a pin falls back to the
	// content-addressed payload digest so the offline cache round-trips.
	ManifestDigest string
	MediaType      string
	// ArtifactType is the OCI 1.1 manifest-level `artifactType` field (distinct
	// from MediaType, which is the layer/blob descriptor's own media type).
	// H6 (package-artifact-install spec §3A) requires a packages/artifact pull
	// to validate BOTH independently against the artifact-bundle media type —
	// a registry that is consistent at one level but wrong/omitted at the
	// other must still fail closed. Empty for a layer pull (fetcher_oci_layer.go
	// does not require it) and for any puller/test fixture that predates H6.
	ArtifactType string
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
// spec (spec §5) becomes a digest pin; any other spec is treated as a literal
// tag, EXCEPT a spec that looks like a still-deferred SemVer range (spec §6),
// which is rejected up front by classifyOCIVersionSpec (oci_resolve.go) rather
// than silently sent to the registry as a nonsense tag name.
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
	tag, digest, err := classifyOCIVersionSpec(parts.VersionSpec)
	if err != nil {
		return ociRef{}, err
	}
	ref.Tag, ref.Digest = tag, digest
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
	// H6 — a fresh pull's declared types are the only signal available; a cache
	// hit has already been validated by readCachedOCIArtifact against its OCI
	// type sidecar inside pullOCIContent (Finding 2), so guardOCIArtifactType
	// is the fresh-pull-only leg of the same contract. This is deliberately
	// stricter than the shared media-type check pullOCIContent applies (which
	// stays tolerant of an empty served media type for the `extends`/config-
	// layer OCI path — fetcher_oci_layer.go — where that tolerance is still
	// relied on): a packages/artifact pull rejects empty/missing/mismatched
	// types at BOTH the manifest and the blob/descriptor level, before the
	// payload is ever cached or untarred.
	if !pulled.CacheHit {
		if err := guardOCIArtifactType(pulled, importRef, parts.SourceID); err != nil {
			return FetchedArtifact{}, err
		}
	}
	// CRITICAL (t3 review #5) — an OCI artifact-bundle pull is always
	// tree-shaped (`+tar+gzip`); without this, FetchedArtifact.Bundle stayed
	// nil for every OCI ref and the pass-2 driver rejected it as "not a
	// directory-shaped bundle". Route both a fresh pull and a digest-pinned
	// cache hit through the same H1 fail-closed normalizer the git/local/http
	// fetchers use (MaybeUntarBundle), so an OCI ref materializes exactly like
	// any other content layout. Data/Digest continue to address the fetched
	// bytes exactly as before — Bundle is an additional, derived view.
	bundle, err := MaybeUntarBundle(pulled.Data, DefaultBundleLimits())
	if err != nil {
		return FetchedArtifact{}, &ImportError{Ref: importRef, SourceID: parts.SourceID, Reason: ReasonContent, Err: fmt.Errorf("oci artifact bundle: %w", err)}
	}
	// Persist the validated blob AND its OCI type sidecar only on a fresh pull:
	// a cache hit was already served from a sidecar-validated entry, and
	// rewriting it with the (empty) cache-hit type metadata would corrupt the
	// sidecar. The sidecar is written after the blob so a present sidecar always
	// implies a fully-written, digest-matching, type-validated blob (Finding 2).
	if !pulled.CacheHit {
		if err := writeCachedArtifact(pulled.Digest, pulled.Data); err != nil {
			return FetchedArtifact{}, err
		}
		if err := writeOCITypeSidecar(pulled.Digest, pulled.ArtifactType, pulled.MediaType); err != nil {
			return FetchedArtifact{}, err
		}
	}
	return FetchedArtifact{
		Data:      pulled.Data,
		Digest:    pulled.Digest,
		CacheHit:  pulled.CacheHit,
		Posture:   pulled.Posture,
		KeyInputs: CacheKeyInputs{OCIDigest: pulled.Digest},
		Bundle:    bundle,
	}, nil
}

// guardOCIArtifactType is the H6 strict guard applied only to a FRESH OCI
// packages/artifact pull (package-artifact-install spec §3A H6): the
// manifest-level artifactType AND the blob/descriptor media type must BOTH
// independently declare the artifact-bundle type, with no empty-type
// tolerance — a registry that omits either, or serves the wrong type at
// either level, fails before the payload is cached or untarred. "No new
// UnitKind is introduced" (H6): the lock's kind stays "artifact" for every
// family; this guards the OCI wire-level type declarations, not the lock
// shape.
func guardOCIArtifactType(pulled ociContent, importRef, sourceID string) error {
	if pulled.ArtifactType == "" {
		return &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonSchema, Err: fmt.Errorf("oci manifest is missing the required artifactType %q", ociArtifactMediaType)}
	}
	if pulled.ArtifactType != ociArtifactMediaType {
		return &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonSchema, Err: fmt.Errorf("oci manifest artifactType %q does not match required %q", pulled.ArtifactType, ociArtifactMediaType)}
	}
	if pulled.MediaType == "" {
		return &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonSchema, Err: fmt.Errorf("oci layer descriptor is missing the required media type %q", ociArtifactMediaType)}
	}
	if pulled.MediaType != ociArtifactMediaType {
		return &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonSchema, Err: fmt.Errorf("oci layer media type %q does not match required %q", pulled.MediaType, ociArtifactMediaType)}
	}
	return nil
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
	// MediaType and ArtifactType carry the raw blob descriptor media type and
	// manifest-level artifactType from a FRESH pull (both "" on a cache hit,
	// which has no manifest to re-read). The layer fetcher (fetcher_oci_layer.go)
	// ignores them, preserving its existing tolerant contract; the packages
	// fetcher (below) applies its own stricter H6 guard on them.
	MediaType    string
	ArtifactType string
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
	// before any network work (offline fast path, spec §8). For the packages/
	// artifact path the cache hit MUST be backed by a validated OCI type sidecar
	// (readCachedOCIArtifact, Finding 2): a blob seeded into the shared cache by
	// another source type carries no sidecar and is treated as a miss so the
	// manifest is re-resolved and its types re-validated. The config-layer path
	// has its own media-type contract and reads the blob directly.
	if ref.Digest != "" {
		cached, ok := readCachedPinnedOCIBlob(ref.Digest, wantMediaType)
		if ok {
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
	// H5 (BLOCKER: digest conflation) — the content digest is ALWAYS recomputed
	// over the fetched payload, never taken as-is from a registry-reported label.
	payloadDigest := artifactDigest(blob.Data)
	// H5 integrity (BLOCKER, Finding 1) — the manifest's layer/blob DESCRIPTOR
	// declares the digest of the payload; when present it is ALWAYS compared to
	// the recomputed payload digest, before type-check/untar/cache, for BOTH tag
	// and pinned pulls. This is the only integrity anchor a tag pull has — the
	// earlier shape discarded blob.Digest and only compared against a pin, so a
	// MITM serving tampered bytes under a valid tag with correct media labels
	// flowed straight through. A registry that omits the descriptor digest is
	// tolerated here; the packages path still requires the type metadata below.
	if blob.Digest != "" && payloadDigest != blob.Digest {
		return ociContent{}, &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonContent, Err: fmt.Errorf("layer digest mismatch: manifest declared %s but payload hashes to %s", blob.Digest, payloadDigest)}
	}
	// H5 pin (Finding 1) — a `pinned:sha256:` ref addresses the MANIFEST digest,
	// a different object than the payload/layer blob, so the pin is validated
	// against the manifest digest (verifyOCIPin), never conflated with the
	// payload digest. Until the live manifest-fetch wire protocol is wired the
	// manifest digest is unknown, so verifyOCIPin falls back to the recomputed
	// payload digest — preserving the content-addressed offline-cache contract.
	if err := verifyOCIPin(ref.Digest, blob.ManifestDigest, payloadDigest, importRef, sourceID); err != nil {
		return ociContent{}, err
	}
	// D13 kind guard: the shared media-type tolerance (an empty served type is
	// accepted as the requested kind so a registry that omits the descriptor
	// type still resolves) — the `extends`/config-layer path relies on this;
	// the packages path layers guardOCIArtifactType (H6, non-tolerant) on top.
	if blob.MediaType != "" && blob.MediaType != wantMediaType {
		return ociContent{}, &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonSchema, Err: fmt.Errorf("oci media type %q does not match required %q", blob.MediaType, wantMediaType)}
	}
	if err := verifySignature(posture, payloadDigest, false); err != nil {
		return ociContent{}, err
	}
	return ociContent{Data: blob.Data, Digest: payloadDigest, CacheHit: false, Posture: posture, MediaType: blob.MediaType, ArtifactType: blob.ArtifactType}, nil
}

// readCachedPinnedOCIBlob resolves a digest-pinned offline cache hit for
// pullOCIContent. The packages/artifact path (wantMediaType is the artifact-
// bundle type) requires the cached blob to be backed by a validated OCI type
// sidecar (readCachedOCIArtifact, Finding 2 / H6-on-cache-hit); every other
// caller (the config-layer path) reads the digest-verified blob directly, as
// before. A miss (including a sidecar-less artifact blob) returns ok=false so
// the caller falls through to a fresh manifest resolution.
func readCachedPinnedOCIBlob(digest, wantMediaType string) ([]byte, bool) {
	if wantMediaType == ociArtifactMediaType {
		return readCachedOCIArtifact(digest)
	}
	return readCachedArtifact(digest)
}

// verifyOCIPin validates a `pinned:sha256:` ref against the digest it
// addresses. Per H5 a pin addresses the MANIFEST digest, so when the puller
// resolved a manifest digest the pin is checked against THAT. When no manifest
// digest is available (the offline content-addressed cache fast path, or a
// puller that does not surface it) the pin falls back to the recomputed
// payload/content digest — the value the cache is keyed by and the lock
// records — so a pinned offline hit still round-trips. The layer-descriptor
// integrity check (payload vs blob.Digest) is separate and always runs; this
// only guards the pin, and only when a pin is present.
func verifyOCIPin(pin, manifestDigest, payloadDigest, importRef, sourceID string) error {
	if pin == "" {
		return nil
	}
	want := manifestDigest
	if want == "" {
		want = payloadDigest
	}
	if pin != want {
		return &ImportError{Ref: importRef, SourceID: sourceID, Reason: ReasonContent, Err: fmt.Errorf("digest mismatch: pinned %s but resolved %s", pin, want)}
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
