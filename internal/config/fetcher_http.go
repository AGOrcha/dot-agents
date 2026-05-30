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

func (f *httpArtifactFetcher) FetchArtifact(src Source, parts PackageRefParts) (FetchedArtifact, error) {
	posture := PostureFromSource(src)
	url := artifactURL(src, parts)
	if !strings.HasPrefix(strings.ToLower(url), "https://") {
		return FetchedArtifact{}, &ImportError{
			Ref:      parts.SourceID + ":" + parts.ArtifactPath,
			SourceID: parts.SourceID,
			Reason:   ReasonSchema,
			Err:      fmt.Errorf("http package source url must be https: %q", url),
		}
	}

	// A digest-pinned version is content-addressed, so the cache is checked
	// before any request (offline fast path, spec §8).
	pinned, isPinned := digestFromVersionSpec(parts.VersionSpec)
	if isPinned {
		if cached, ok := readCachedArtifact(pinned); ok {
			if err := verifySignature(posture, pinned, false); err != nil {
				return FetchedArtifact{}, err
			}
			return FetchedArtifact{Data: cached, Digest: pinned, CacheHit: true, Posture: posture}, nil
		}
	}

	client := f.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonTransport, Err: fmt.Errorf("building request for %s: %w", url, err)}
	}
	if tok := authString(src.Auth, "token"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonTransport, Err: fmt.Errorf("http get %s: %w", url, err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonAuth, Err: fmt.Errorf("http get %s: status %d", url, resp.StatusCode)}
	}
	if resp.StatusCode == http.StatusNotFound {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonNotFound, Err: fmt.Errorf("http get %s: status %d", url, resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonTransport, Err: fmt.Errorf("http get %s: status %d", url, resp.StatusCode)}
	}
	data, err := readAllLimited(resp.Body)
	if err != nil {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonContent, Err: fmt.Errorf("reading %s: %w", url, err)}
	}
	digest := artifactDigest(data)
	if isPinned && digest != pinned {
		return FetchedArtifact{}, &ImportError{Ref: parts.SourceID + ":" + parts.ArtifactPath, SourceID: parts.SourceID, Reason: ReasonContent, Err: fmt.Errorf("digest mismatch: pinned %s but server served %s", pinned, digest)}
	}
	if err := verifySignature(posture, digest, false); err != nil {
		return FetchedArtifact{}, err
	}
	if err := writeCachedArtifact(digest, data); err != nil {
		return FetchedArtifact{}, err
	}
	return FetchedArtifact{Data: data, Digest: digest, CacheHit: false, Posture: posture}, nil
}
