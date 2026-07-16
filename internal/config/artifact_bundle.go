package config

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// This file is the OCI publish PRODUCER (package-artifact-install spec D9 +
// the BLOCKER #4 owner ruling: the publish verb lands on the canonical
// unified resource CRUD surface — `da config publish`, commands/config/
// publish.go — not a revived `da packages` family). It is the counterpart of
// the consumer half already built in fetcher_oci.go / bundle_safety.go:
//
//   - PackTree is the producer mirror of UntarBundle: it walks a resource
//     tree on disk into a normalized Bundle through the SAME H1 fail-closed
//     NormalizeBundle choke point the fetch/materialize side uses (reusing
//     localRootWalker from fetcher_local_artifact.go — one normalizer, one
//     walker, shared by producer and consumer, H1), then serializes it as the
//     SAME typed `+tar+gzip` artifact-bundle blob (ociArtifactMediaType) the
//     consumer decodes.
//   - ociPushLive is the real OCI Distribution push (manifest + config +
//     layer blobs), reusing t7's ociAuthHeaderForRef (oci_auth.go) for auth so
//     the resolved credential is scoped per request exactly like the pull
//     path (H12).
//   - ociPullLive is the real OCI Distribution pull that fetcher_oci.go's
//     ociPull wires to, replacing the not-yet-wired transport-error stub.
//     Populating ociBlob.ManifestDigest here closes the tracked
//     oci-pin-manifest-digest-deadcode residual — see verifyOCIPin's updated
//     comment in fetcher_oci.go for how the now-live manifest digest is used
//     without breaking the payload-digest-keyed offline cache contract.

// PublishResult is what a successful PublishTree reports: the pushed
// manifest's own digest (H5 — the object the OCI wire protocol addresses,
// distinct from the payload) and the layer/payload digest — the content
// digest the shared packages cache and lock key on uniformly across every
// source type (git/http/local/oci). A `pinned:sha256:<digest>` version-spec
// for a subsequent `packages` pull should use LayerDigest: that is what
// readCachedPinnedOCIBlob/writeCachedArtifact key on, so pinning it is what
// makes the round trip a cache hit (R8 byte-parity, DC2-oci).
type PublishResult struct {
	ManifestDigest string
	LayerDigest    string
}

// PackTree packs the resource tree rooted at dirPath into a normalized Bundle
// (H1) and serializes it as a `+tar+gzip` artifact-bundle blob — the producer
// counterpart of UntarBundle/MaybeUntarBundle. It reuses localRootWalker (the
// same confined, symlink-defeating walker the local packages fetcher uses) so
// a publish source tree is walked under the identical fail-closed contract a
// consumed tree is validated against; producer and consumer share one
// normalizer, never two parallel implementations that could drift (H1).
func PackTree(dirPath string, limits BundleLimits) (Bundle, []byte, error) {
	limits = limits.orDefault()
	root, err := os.OpenRoot(dirPath)
	if err != nil {
		return Bundle{}, nil, fmt.Errorf("opening resource tree %s: %w", dirPath, err)
	}
	defer func() { _ = root.Close() }()
	rootInfo, err := root.Lstat(".")
	if err != nil {
		return Bundle{}, nil, fmt.Errorf("stat resource tree %s: %w", dirPath, err)
	}
	if rootInfo.Mode()&fs.ModeSymlink != 0 {
		return Bundle{}, nil, fmt.Errorf("resource tree root %s is a symlink; symlinks are not permitted", dirPath)
	}
	if !rootInfo.IsDir() {
		return Bundle{}, nil, fmt.Errorf("resource tree root %s is not a directory", dirPath)
	}
	bundle, err := NormalizeBundle(localRootWalker(root, ".", rootInfo, limits), limits)
	if err != nil {
		return Bundle{}, nil, fmt.Errorf("packing resource tree %s: %w", dirPath, err)
	}
	blob, err := tarGzBundle(bundle)
	if err != nil {
		return Bundle{}, nil, fmt.Errorf("serializing artifact bundle for %s: %w", dirPath, err)
	}
	return bundle, blob, nil
}

// tarGzBundle serializes a normalized Bundle (already H1-validated, already
// path-sorted by NormalizeBundle) into a deterministic `+tar+gzip` blob: a
// fixed ModTime/Uid/Gid on every header and Entries' stable sort order mean
// two identical resource trees pack to structurally identical archives
// regardless of the source filesystem's native directory order or file
// timestamps.
func tarGzBundle(b Bundle) ([]byte, error) {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("creating gzip writer: %w", err)
	}
	tw := tar.NewWriter(gz)
	for _, e := range b.Entries {
		hdr := &tar.Header{
			Name: e.Path,
			Mode: int64(e.Mode.Perm()),
		}
		if e.IsDir {
			hdr.Typeflag = tar.TypeDir
			hdr.Name += "/"
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(e.Data))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("writing tar header for %q: %w", e.Path, err)
		}
		if !e.IsDir {
			if _, err := tw.Write(e.Data); err != nil {
				return nil, fmt.Errorf("writing tar content for %q: %w", e.Path, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("closing tar writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("closing gzip writer: %w", err)
	}
	return buf.Bytes(), nil
}

// FindSource returns the declared source with the given id, or (Source{},
// false). Exported so commands/config/publish.go (a different package) can
// resolve a packages ref's source-id to its Source without internal/config
// exposing its unexported indexSources helper.
func FindSource(sources []Source, id string) (Source, bool) {
	for _, s := range sources {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// PublishTree packs the resource tree at dirPath and pushes it to src (which
// must be an `oci` source) at parts' artifact path, tagged with parts'
// version-spec — the producer entry point `da config publish` drives. It
// reuses parseOCIRef (fetcher_oci.go) so the ref resolution logic is
// identical on both sides of the wire, and ociAuthHeaderForRef (t7,
// oci_auth.go) for auth, so a resolved credential is scoped to each push
// request exactly like a pull (H12 — never persisted, logged, or returned).
func PublishTree(ctx context.Context, src Source, parts PackageRefParts, dirPath string) (PublishResult, error) {
	if src.Type != "oci" {
		return PublishResult{}, fmt.Errorf("publish requires an oci source, got %q", src.Type)
	}
	ref, err := parseOCIRef(src, parts)
	if err != nil {
		return PublishResult{}, fmt.Errorf("resolving oci ref: %w", err)
	}
	if ref.Digest != "" {
		return PublishResult{}, fmt.Errorf("publish target must be an explicit tag, not a pinned:sha256: digest")
	}
	if ref.Tag == "" {
		return PublishResult{}, fmt.Errorf("publish requires an explicit tag in the version-spec (got %q)", parts.VersionSpec)
	}
	_, blob, err := PackTree(dirPath, DefaultBundleLimits())
	if err != nil {
		return PublishResult{}, fmt.Errorf("packing %s: %w", dirPath, err)
	}
	pushed, err := ociPushLive(ctx, ref, src.Auth, blob, ociArtifactMediaType, ociArtifactMediaType)
	if err != nil {
		return PublishResult{}, newRedactedError(fmt.Errorf("publishing %s to %s/%s:%s: %w", dirPath, ref.Registry, ref.Repository, ref.Tag, err))
	}
	return PublishResult{ManifestDigest: pushed.ManifestDigest, LayerDigest: pushed.LayerDigest}, nil
}

// --- OCI Distribution wire protocol (push + pull) ---------------------------

// ociRegistryHTTPClient is the HTTP client for the OCI Distribution registry
// transport (manifest/blob push + pull), distinct from ociTokenHTTPClient
// (oci_auth.go's token-endpoint exchange): a registry may legitimately
// redirect a blob GET to a CDN/blob-store, whereas a token endpoint must not
// (rejectTokenEndpointRedirect), so the two clients carry different redirect
// policies and live separately.
var ociRegistryHTTPClient = &http.Client{Timeout: 60 * time.Second}

// ociManifestMediaType is the OCI 1.1 image-manifest media type this client
// pushes/expects. ociEmptyConfigMediaType is the OCI 1.1 "artifact with no
// meaningful config" convention: da publishes a 2-byte `{}` config blob under
// this media type so a spec-conformant registry (which requires a config
// descriptor) accepts the manifest without inventing artifact-specific config
// schema.
const (
	ociManifestMediaType    = "application/vnd.oci.image.manifest.v1+json"
	ociEmptyConfigMediaType = "application/vnd.oci.empty.v1+json"
	// ociMaxManifestBytes bounds a fetched manifest document — generous for a
	// single-config/single-layer artifact manifest, but never unbounded.
	ociMaxManifestBytes = 4 << 20
)

// ociManifestDescriptor is one OCI content descriptor (config or layer) inside
// a pushed/fetched manifest.
type ociManifestDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// ociManifestDoc is the OCI 1.1 image-manifest document this client
// pushes/parses: schemaVersion 2, the artifact-bundle artifactType at the
// manifest level (H6), an empty config descriptor, and exactly one layer.
type ociManifestDoc struct {
	SchemaVersion int                     `json:"schemaVersion"`
	MediaType     string                  `json:"mediaType"`
	ArtifactType  string                  `json:"artifactType,omitempty"`
	Config        ociManifestDescriptor   `json:"config"`
	Layers        []ociManifestDescriptor `json:"layers"`
}

// ociPushed is what a successful ociPushLive reports: the manifest's own
// digest (self-computed over the exact bytes pushed — H5's "never trust a
// registry-reported label" discipline applies to the producer side too) and
// the layer/payload digest.
type ociPushed struct {
	ManifestDigest string
	LayerDigest    string
}

// ociBaseURL derives the registry's HTTP(S) base URL from ref.Registry. A
// registry host already carrying an explicit scheme (a test registry
// constructed as "http://127.0.0.1:port") is used as-is; a bare
// localhost/loopback host defaults to plain HTTP — the conventional
// "insecure by default" carve-out every OCI client applies to a local
// dev/test registry; every other host defaults to HTTPS, so a production
// registry is never spoken to in the clear.
func ociBaseURL(registry string) string {
	if strings.HasPrefix(registry, "http://") || strings.HasPrefix(registry, "https://") {
		return strings.TrimSuffix(registry, "/")
	}
	if isLoopbackOCIRegistry(registry) {
		return "http://" + registry
	}
	return "https://" + registry
}

// isLoopbackOCIRegistry reports whether registry's host (optionally carrying
// a ":port") is a loopback address.
func isLoopbackOCIRegistry(registry string) bool {
	host := registry
	if h, _, err := net.SplitHostPort(registry); err == nil {
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// ociAuthenticatedRequest performs one OCI Distribution HTTP call, applying
// the standard two-round-trip auth pattern: it first resolves an auth header
// with no challenge (covering an anonymous registry or a provider that needs
// none), sends the request, and — only on a 401 carrying a WWW-Authenticate
// challenge — re-resolves the auth header against that challenge and retries
// once. body is re-sent verbatim on the retry (it is buffered, never a
// one-shot stream), so a push's blob/manifest bytes survive the retry.
func ociAuthenticatedRequest(ctx context.Context, method, rawURL string, body []byte, headers map[string]string, auth json.RawMessage, ref ociRef) (*http.Response, error) {
	send := func(authHeader string) (*http.Response, error) {
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
		if err != nil {
			return nil, fmt.Errorf("building oci request %s %s: %w", method, rawURL, err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if body != nil {
			req.ContentLength = int64(len(body))
		}
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		return ociRegistryHTTPClient.Do(req)
	}

	authHeader, err := ociAuthHeaderForRef(ctx, auth, ref, "")
	if err != nil {
		return nil, err
	}
	resp, err := send(authHeader)
	if err != nil {
		return nil, fmt.Errorf("oci request %s %s: %w", method, rawURL, err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	challenge := resp.Header.Get("Www-Authenticate")
	_ = resp.Body.Close()
	if challenge == "" {
		return resp, nil
	}
	authHeader2, err := ociAuthHeaderForRef(ctx, auth, ref, challenge)
	if err != nil {
		return nil, err
	}
	return send(authHeader2)
}

// resolveOCILocation resolves a blob-upload session's (possibly relative)
// Location header against base, per the OCI Distribution spec.
//
// H12 — the Location is REGISTRY-CONTROLLED and the caller attaches the push
// credential to the resolved URL. A malicious/compromised registry can return
// an absolute Location on an attacker origin; because the credentialed PUT is
// an explicitly-built request (not a followed redirect), Go's cross-host
// Authorization stripping does not apply. So the resolved endpoint MUST be on
// the SAME origin as the configured registry — reject cross-origin, an
// https->http downgrade, and embedded userinfo before the credential is ever
// attached. A relative Location resolved against base is same-origin by
// construction; only an absolute Location can escape.
func resolveOCILocation(base, loc string) (string, error) {
	u, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("parsing upload location %q: %w", loc, err)
	}
	baseU, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parsing registry base %q: %w", base, err)
	}
	resolved := u
	if !u.IsAbs() {
		resolved = baseU.ResolveReference(u)
	}
	if resolved.User != nil {
		return "", fmt.Errorf("refusing blob-upload location with embedded userinfo (origin %s)", ociOriginString(resolved))
	}
	if !sameOCIOrigin(baseU, resolved) {
		return "", fmt.Errorf("refusing cross-origin blob-upload location %s: not the configured registry origin %s", ociOriginString(resolved), ociOriginString(baseU))
	}
	return resolved.String(), nil
}

// sameOCIOrigin reports whether a and b share scheme + host + effective port
// (applying the scheme's default port), i.e. the same web origin. Used to
// pin a registry-controlled blob-upload Location to the configured registry
// before a credential is attached (H12).
func sameOCIOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && ociHostPort(a) == ociHostPort(b)
}

// ociHostPort returns host:port with the scheme's default port applied when
// the URL omits an explicit port.
func ociHostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return u.Hostname() + ":" + port
}

// ociOriginString is a credential-safe scheme://host[:port] rendering of a URL
// for error text — it deliberately drops path, query (which may carry an OCI
// upload-session _state token), and userinfo.
func ociOriginString(u *url.URL) string {
	return u.Scheme + "://" + ociHostPort(u)
}

// appendDigestQuery adds/overwrites the "digest" query parameter on rawURL,
// preserving any existing query parameters (a blob-upload session's Location
// commonly already carries a "_state" token).
func appendDigestQuery(rawURL, digest string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing url %q: %w", rawURL, err)
	}
	q := u.Query()
	q.Set("digest", digest)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ociPushBlob pushes data as a single OCI blob via the standard two-step
// upload (POST to open a session, PUT the full body with a `digest` query to
// complete it) and returns its content digest. The digest is always the
// locally recomputed sha256 of data — never a value read back from the
// registry — the same "never trust a label" discipline H5 applies to a pull.
func ociPushBlob(ctx context.Context, ref ociRef, auth json.RawMessage, data []byte) (string, error) {
	digest := artifactDigest(data)
	base := ociBaseURL(ref.Registry)
	startURL := base + "/v2/" + ref.Repository + "/blobs/uploads/"
	resp, err := ociAuthenticatedRequest(ctx, http.MethodPost, startURL, nil, nil, auth, ref)
	if err != nil {
		return "", fmt.Errorf("starting blob upload: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("starting blob upload at %s: unexpected status %s", startURL, resp.Status)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("blob upload response from %s missing Location header", startURL)
	}
	putURL, err := resolveOCILocation(base, loc)
	if err != nil {
		return "", fmt.Errorf("resolving blob upload location: %w", err)
	}
	putURL, err = appendDigestQuery(putURL, digest)
	if err != nil {
		return "", fmt.Errorf("building blob upload completion url: %w", err)
	}
	resp2, err := ociAuthenticatedRequest(ctx, http.MethodPut, putURL, data, map[string]string{"Content-Type": "application/octet-stream"}, auth, ref)
	if err != nil {
		return "", fmt.Errorf("completing blob upload: %w", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusCreated {
		// Drop the query (may carry an OCI upload-session _state token) from
		// error text — redactSecrets only scrubs registered credentials.
		return "", fmt.Errorf("completing blob upload at %s: unexpected status %s", urlWithoutQuery(putURL), resp2.Status)
	}
	return digest, nil
}

// urlWithoutQuery strips the query string (and userinfo) from a URL for
// credential-safe error text; on a parse error it falls back to the substring
// before the first "?".
func urlWithoutQuery(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		u.RawQuery = ""
		u.User = nil
		return u.String()
	}
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		return rawURL[:i]
	}
	return rawURL
}

// ociPushLive is the real OCI Distribution push: an empty config blob (OCI
// 1.1 "no meaningful config" convention), the layer blob, then a manifest
// referencing both — reusing ociAuthHeaderForRef (t7) for auth on every
// request. It returns the manifest's own digest (self-computed over the exact
// bytes pushed) alongside the layer/payload digest.
func ociPushLive(ctx context.Context, ref ociRef, auth json.RawMessage, layerData []byte, layerMediaType, artifactType string) (ociPushed, error) {
	emptyConfig := []byte("{}")
	configDigest, err := ociPushBlob(ctx, ref, auth, emptyConfig)
	if err != nil {
		return ociPushed{}, fmt.Errorf("pushing config blob: %w", err)
	}
	layerDigest, err := ociPushBlob(ctx, ref, auth, layerData)
	if err != nil {
		return ociPushed{}, fmt.Errorf("pushing layer blob: %w", err)
	}
	manifest := ociManifestDoc{
		SchemaVersion: 2,
		MediaType:     ociManifestMediaType,
		ArtifactType:  artifactType,
		Config:        ociManifestDescriptor{MediaType: ociEmptyConfigMediaType, Digest: configDigest, Size: int64(len(emptyConfig))},
		Layers:        []ociManifestDescriptor{{MediaType: layerMediaType, Digest: layerDigest, Size: int64(len(layerData))}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return ociPushed{}, fmt.Errorf("encoding manifest: %w", err)
	}
	tagOrDigest := ref.Tag
	if tagOrDigest == "" {
		tagOrDigest = ref.Digest
	}
	if tagOrDigest == "" {
		return ociPushed{}, fmt.Errorf("oci push: ref has neither tag nor digest (registry=%s repo=%s)", ref.Registry, ref.Repository)
	}
	manifestURL := ociBaseURL(ref.Registry) + "/v2/" + ref.Repository + "/manifests/" + tagOrDigest
	resp, err := ociAuthenticatedRequest(ctx, http.MethodPut, manifestURL, manifestBytes, map[string]string{"Content-Type": ociManifestMediaType}, auth, ref)
	if err != nil {
		return ociPushed{}, fmt.Errorf("pushing manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return ociPushed{}, fmt.Errorf("pushing manifest to %s: unexpected status %s", manifestURL, resp.Status)
	}
	return ociPushed{ManifestDigest: artifactDigest(manifestBytes), LayerDigest: layerDigest}, nil
}

// ociPullLive is the real OCI Distribution pull that fetcher_oci.go's ociPull
// wires to: GET the manifest by tag or digest, recompute its digest locally
// (ManifestDigest — closing the tracked oci-pin-manifest-digest-deadcode
// residual, which observed this field was never populated because ociPull was
// an unwired stub), then GET the first layer's blob. Every request goes
// through ociAuthenticatedRequest, so auth is resolved via t7's
// ociAuthHeaderForRef per request exactly like a push.
func ociPullLive(ctx context.Context, ref ociRef, auth []byte) (ociBlob, error) {
	rawAuth := json.RawMessage(auth)
	base := ociBaseURL(ref.Registry)
	tagOrDigest := ref.Tag
	if ref.Digest != "" {
		tagOrDigest = ref.Digest
	}
	if tagOrDigest == "" {
		return ociBlob{}, fmt.Errorf("oci pull: ref has neither tag nor digest (registry=%s repo=%s)", ref.Registry, ref.Repository)
	}
	manifestURL := base + "/v2/" + ref.Repository + "/manifests/" + tagOrDigest
	resp, err := ociAuthenticatedRequest(ctx, http.MethodGet, manifestURL, nil, map[string]string{"Accept": ociManifestMediaType}, rawAuth, ref)
	if err != nil {
		return ociBlob{}, fmt.Errorf("fetching manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ociBlob{}, fmt.Errorf("fetching manifest %s: unexpected status %s", manifestURL, resp.Status)
	}
	manifestBytes, err := io.ReadAll(io.LimitReader(resp.Body, ociMaxManifestBytes+1))
	if err != nil {
		return ociBlob{}, fmt.Errorf("reading manifest: %w", err)
	}
	if int64(len(manifestBytes)) > ociMaxManifestBytes {
		return ociBlob{}, fmt.Errorf("manifest at %s exceeds %d byte cap", manifestURL, ociMaxManifestBytes)
	}
	// H5 — the manifest digest is ALWAYS the locally recomputed sha256 of the
	// exact bytes served, never a Docker-Content-Digest response header (a
	// server-reported label, exactly the kind of value H5 forbids trusting).
	manifestDigest := artifactDigest(manifestBytes)
	var manifest ociManifestDoc
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return ociBlob{}, fmt.Errorf("decoding manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return ociBlob{}, fmt.Errorf("manifest at %s declares no layers", manifestURL)
	}
	layer := manifest.Layers[0]
	blobURL := base + "/v2/" + ref.Repository + "/blobs/" + layer.Digest
	resp2, err := ociAuthenticatedRequest(ctx, http.MethodGet, blobURL, nil, nil, rawAuth, ref)
	if err != nil {
		return ociBlob{}, fmt.Errorf("fetching blob: %w", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		return ociBlob{}, fmt.Errorf("fetching blob %s: unexpected status %s", blobURL, resp2.Status)
	}
	maxBytes := DefaultBundleLimits().MaxBytes
	data, err := io.ReadAll(io.LimitReader(resp2.Body, maxBytes+1))
	if err != nil {
		return ociBlob{}, fmt.Errorf("reading blob: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return ociBlob{}, fmt.Errorf("blob %s exceeds %d byte cap", blobURL, maxBytes)
	}
	return ociBlob{
		Data:           data,
		Digest:         layer.Digest,
		ManifestDigest: manifestDigest,
		MediaType:      layer.MediaType,
		ArtifactType:   manifest.ArtifactType,
	}, nil
}
