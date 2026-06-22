package config

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// This file adds the fuller http source type: the http-as-packages path
// (config-distribution-model §4 — `http` is valid for both tiers, "Layer files
// or OCI-compatible HTTP endpoint"). The existing layer-tier httpFetcher (in
// fetcher.go) GETs a layer.json content-addressed by sha256 hex; the artifact
// path here pulls a package blob over HTTPS with auth pass-through and digest
// addressing, returning a FetchedArtifact so it composes with the same pass-2
// (p6) resolution and signing-posture stub as the oci path.

// httpArtifactFetcher pulls a tier-2 package artifact over HTTPS. HTTPS is
// enforced (no plaintext transport), an optional bearer token from the source's
// opaque auth block is attached, and a "pinned:sha256:..." version spec is
// verified against the computed digest. It satisfies PackageFetcher so
// SelectPackageFetcher can return it for `http` package sources.
type httpArtifactFetcher struct {
	// client is a test seam; nil uses a default client with a timeout.
	client *http.Client
}

// httpStatusErrFmt is the shared format for HTTP status-code import errors.
const httpStatusErrFmt = "http get %s: status %d"

// artifactURL builds the absolute HTTPS URL for an http package artifact:
// <source-url>/<artifact-path>/<version-spec>. The version spec is appended as
// a path segment (a tag or a digest) so distinct versions are distinct URLs and
// the cache stays content-addressed.
func artifactURL(src Source, parts PackageRefParts) string {
	base := strings.TrimRight(src.URL, "/")
	path := strings.Trim(parts.ArtifactPath, "/")
	ver := strings.TrimLeft(parts.VersionSpec, "/")
	return base + "/" + path + "/" + ver
}

// newArtifactImportError builds an ImportError for an http artifact pull,
// centralizing the repeated Ref/SourceID wiring.
func newArtifactImportError(parts PackageRefParts, reason ImportFailReason, err error) *ImportError {
	return &ImportError{
		Ref:      parts.SourceID + ":" + parts.ArtifactPath,
		SourceID: parts.SourceID,
		Reason:   reason,
		Err:      err,
	}
}

// reasonForStatus maps a non-OK HTTP status to its import-error reason, or ""
// when the status is OK and the body should be read.
func reasonForStatus(status int) ImportFailReason {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ReasonAuth
	case status == http.StatusNotFound:
		return ReasonNotFound
	case status != http.StatusOK:
		return ReasonTransport
	default:
		return ""
	}
}

// readCachedPinnedArtifact returns a cache-hit FetchedArtifact when the version
// spec is digest-pinned and the blob is already cached. ok is false when the
// caller must fall through to a network fetch.
func (f *httpArtifactFetcher) readCachedPinnedArtifact(posture SigningPosture, pinned string, isPinned bool) (FetchedArtifact, bool, error) {
	if !isPinned {
		return FetchedArtifact{}, false, nil
	}
	cached, ok := readCachedArtifact(pinned)
	if !ok {
		return FetchedArtifact{}, false, nil
	}
	if err := verifySignature(posture, pinned, false); err != nil {
		return FetchedArtifact{}, true, err
	}
	// A digest-pinned cache hit served without contacting the registry has no
	// fresh validators; the pinned digest is the content key (§7A.4 http default
	// falls back to the content digest when no ETag/Last-Modified is observed).
	return FetchedArtifact{Data: cached, Digest: pinned, CacheHit: true, Posture: posture, KeyInputs: CacheKeyInputs{ContentDigest: pinned}}, true, nil
}

// doRequest issues the authenticated GET and maps any transport or status-code
// failure to an ImportError. On success it returns the open response; the caller
// owns closing the body.
func (f *httpArtifactFetcher) doRequest(src Source, parts PackageRefParts, url string) (*http.Response, error) {
	client := f.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, newArtifactImportError(parts, ReasonTransport, fmt.Errorf("building request for %s: %w", url, err))
	}
	if tok := authString(src.Auth, "token"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, newArtifactImportError(parts, ReasonTransport, fmt.Errorf("http get %s: %w", url, err))
	}
	if reason := reasonForStatus(resp.StatusCode); reason != "" {
		_ = resp.Body.Close()
		return nil, newArtifactImportError(parts, reason, fmt.Errorf(httpStatusErrFmt, url, resp.StatusCode))
	}
	return resp, nil
}

func (f *httpArtifactFetcher) FetchArtifact(src Source, parts PackageRefParts) (FetchedArtifact, error) {
	posture := PostureFromSource(src)
	url := artifactURL(src, parts)
	if !strings.HasPrefix(strings.ToLower(url), "https://") {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonSchema, fmt.Errorf("http package source url must be https: %q", url))
	}

	// A digest-pinned version is content-addressed, so the cache is checked
	// before any request (offline fast path, spec §8).
	pinned, isPinned := digestFromVersionSpec(parts.VersionSpec)
	if art, ok, err := f.readCachedPinnedArtifact(posture, pinned, isPinned); ok {
		return art, err
	}

	resp, err := f.doRequest(src, parts, url)
	if err != nil {
		return FetchedArtifact{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := readAllLimited(resp.Body)
	if err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("reading %s: %w", url, err))
	}
	digest := artifactDigest(data)
	if isPinned && digest != pinned {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("digest mismatch: pinned %s but server served %s", pinned, digest))
	}
	if err := verifySignature(posture, digest, false); err != nil {
		return FetchedArtifact{}, err
	}
	if err := writeCachedArtifact(digest, data); err != nil {
		return FetchedArtifact{}, err
	}
	// Capture the upstream validators the http cache-key default prefers
	// (config-distribution-model §7A.4: ETag, else Last-Modified, else digest), so
	// the package resolver can derive an effective key that re-checks on a
	// validator change even when the digest-addressed blob is still cached.
	keyInputs := CacheKeyInputs{
		ETag:          resp.Header.Get("ETag"),
		LastModified:  resp.Header.Get("Last-Modified"),
		ContentDigest: digest,
	}
	return FetchedArtifact{Data: data, Digest: digest, CacheHit: false, Posture: posture, KeyInputs: keyInputs}, nil
}
