package config

// cache_keys.go owns the per-source cache-key model (config-distribution-model
// §7A.4 / D4 / D6, R6) — the uv-style "what makes a resolved unit's cached
// content stale" policy.
//
// Two layers live here:
//
//  1. The DEFAULT content key per source kind. uv keys a cache entry on the
//     facts that actually pin a source's content:
//       - git   -> the resolved commit (an immutable content pin);
//       - local -> the resolved commit PLUS working-tree content, so authoring
//         before a commit still registers as a distinct key (§7A.4 / D6);
//       - http  -> the upstream ETag / Last-Modified validator, falling back to
//         a content digest when the server sends neither;
//       - oci   -> the manifest digest (OCI is strictly content-addressed).
//
//  2. The per-source OVERRIDE (Source.cache_keys). A source may pin its key on
//     explicit inputs instead of (or in addition to) the kind default: a
//     {file}/glob of tracked files, a {git:{commit,tags}} selector, named {env}
//     vars, or a {dir}-presence marker. This mirrors uv's `cache-keys` setting.
//
// Plus two FORCE ESCAPES (R6): a per-unit `--refresh` (a runtime resolution
// flag, threaded in via CacheKeyOptions) and a config-declared
// `always_revalidate` marker on the source. Either makes the effective key the
// always-revalidate sentinel, so the resolver re-checks upstream unconditionally
// regardless of the recorded digest.
//
// This file is PURE policy + data: it computes the content key the resolver and
// staleness layers compare against the lock. It does not fetch, hash files, hit
// the network, or touch a clock — those facts are supplied by the caller as
// CacheKeyInputs, exactly as staleness.go takes a UnitDigestFunc seam.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// SourceKind enumerates the four source kinds whose cache-key defaults differ
// (config-distribution-model §4). It mirrors the Source.Type values; a dedicated
// type keeps the switch in DefaultCacheKey exhaustive and self-documenting.
type SourceKind string

const (
	// SourceKindGit keys on the resolved commit (an immutable content pin).
	SourceKindGit SourceKind = "git"
	// SourceKindLocal keys on the resolved commit PLUS working-tree content, so
	// uncommitted authoring is a distinct key (§7A.4 / D6).
	SourceKindLocal SourceKind = "local"
	// SourceKindHTTP keys on the upstream ETag/Last-Modified validator, else a
	// content digest.
	SourceKindHTTP SourceKind = "http"
	// SourceKindOCI keys on the manifest digest (content-addressed).
	SourceKindOCI SourceKind = "oci"
)

// SourceKindOf maps a Source.Type string to its SourceKind so the resolver and
// fetchers can derive the cache-key default without re-spelling the switch. An
// unrecognized type is returned verbatim as a SourceKind; DefaultCacheKey's
// default branch then keys it on the content digest, so a future source type
// still gets a usable (if coarse) key rather than a panic.
func SourceKindOf(sourceType string) SourceKind {
	switch sourceType {
	case "git":
		return SourceKindGit
	case "local":
		return SourceKindLocal
	case "http":
		return SourceKindHTTP
	case "oci":
		return SourceKindOCI
	default:
		return SourceKind(sourceType)
	}
}

// cacheKeyPrefix tags a normalized cache key with the kind that produced it, so
// two kinds can never alias on the same underlying value (e.g. a git commit and
// a local commit with identical hex). It mirrors staleness.go's scopeSlot
// namespacing.
const cacheKeyPrefix = "cachekey/"

// AlwaysRevalidate is the sentinel effective cache key returned when a force
// escape is in play (`--refresh` or the source's always_revalidate marker). It
// is intentionally not derivable from any real input, so it never matches a
// recorded lock digest — the resolver therefore always re-checks upstream.
const AlwaysRevalidate = cacheKeyPrefix + "always-revalidate"

// CacheKeys is the per-source override of the default content key
// (config-distribution-model §7A.4, the uv `cache-keys` analog). All fields are
// optional and use omitempty so a source that relies on the kind default
// round-trips byte-for-byte with no cache_keys object emitted.
//
// When any selector field is set, the effective key is derived from those
// selectors instead of the kind default (the file/glob list, the git commit/tag
// selectors, the named env vars, and the dir-presence markers are combined into
// one composite key). AlwaysRevalidate is the config-declared force escape: it
// supersedes every selector and the default.
type CacheKeys struct {
	// Files lists tracked file paths or globs whose content pins the key — a
	// change to any matched file is a new key (uv `cache-keys = [{file=...}]`).
	Files []string `json:"file,omitempty"`
	// Git selects which git facts (commit and/or tags) pin the key. Nil means
	// the git facts do not participate in the override.
	Git *CacheKeyGit `json:"git,omitempty"`
	// Env lists environment variable names whose values pin the key — a changed
	// value (e.g. a credential or feature flag) is a new key.
	Env []string `json:"env,omitempty"`
	// Dir lists directory paths whose presence pins the key — a {dir} marker is
	// "exists" / "absent", catching a source that appears or disappears.
	Dir []string `json:"dir,omitempty"`
	// AlwaysRevalidate is the config-declared force escape (§7A.4 / R6): when
	// true the effective key is the AlwaysRevalidate sentinel, so the resolver
	// re-checks upstream on every resolve regardless of the recorded digest.
	AlwaysRevalidate bool `json:"always_revalidate,omitempty"`
}

// CacheKeyGit selects which git facts pin the cache key when CacheKeys.Git is
// set. At least one of Commit/Tags is expected; both false is a no-op selector
// (it contributes nothing, equivalent to omitting the git block).
type CacheKeyGit struct {
	// Commit pins the key on the resolved commit hash (the git default fact).
	Commit bool `json:"commit,omitempty"`
	// Tags pins the key on the set of tags pointing at the resolved commit, so a
	// re-tag (without a commit change) is a new key.
	Tags bool `json:"tags,omitempty"`
}

// IsZero reports whether the cache-keys override carries no selectors and no
// force escape — i.e. the source falls back entirely to the kind default. A nil
// receiver is zero. Used by callers to decide whether to emit the object at all.
func (c *CacheKeys) IsZero() bool {
	if c == nil {
		return true
	}
	return len(c.Files) == 0 &&
		c.Git == nil &&
		len(c.Env) == 0 &&
		len(c.Dir) == 0 &&
		!c.AlwaysRevalidate
}

// CacheKeyInputs carries the resolved facts the caller (the resolver / fetcher)
// supplies for one source so cache-key derivation stays pure: this file never
// runs git, never reads a file, never hits the network. Each field is the
// already-resolved fact for its kind; selectors and defaults read only from
// here. Absent facts are the zero value (empty string / nil), which derive a
// stable "absent" contribution rather than an error.
type CacheKeyInputs struct {
	// ResolvedCommit is the resolved git commit hash for a git or local source.
	ResolvedCommit string
	// WorktreeDirty reports whether a local source's working tree has
	// uncommitted changes (§7A.4: local keys on commit PLUS working-tree state).
	WorktreeDirty bool
	// WorktreeContentHash is an optional precise hash of the local working
	// tree's content; when set it makes the local key sensitive to the exact
	// uncommitted bytes, not just the dirty flag. Empty falls back to the dirty
	// marker alone.
	WorktreeContentHash string
	// Tags are the git tags pointing at ResolvedCommit, used by the {git:{tags}}
	// selector. Order-insensitive (sorted before hashing).
	Tags []string
	// ETag is the http source's upstream ETag validator (the preferred http key).
	ETag string
	// LastModified is the http source's Last-Modified validator (used when no
	// ETag is sent).
	LastModified string
	// ContentDigest is the fallback content hash for an http source whose server
	// sent neither ETag nor Last-Modified.
	ContentDigest string
	// OCIDigest is the oci manifest digest (the content-addressed oci key).
	OCIDigest string
	// FileContents maps a Source.cache_keys file/glob entry to its resolved
	// content hash, for the {file} selector. The caller hashes the matched
	// files; this layer only folds the hashes into the composite key.
	FileContents map[string]string
	// EnvValues maps a Source.cache_keys env name to its current value, for the
	// {env} selector. A missing var is recorded as absent.
	EnvValues map[string]string
	// DirPresent maps a Source.cache_keys dir entry to whether it exists, for
	// the {dir} selector.
	DirPresent map[string]bool
}

// CacheKeyOptions threads per-unit resolution-time force escapes into key
// derivation — the runtime half of R6 (the config half is
// CacheKeys.AlwaysRevalidate). It parallels EnsureResolved's opts struct rather
// than a package-level flag.
type CacheKeyOptions struct {
	// Refresh is the per-unit `--refresh` force escape: when true the effective
	// key is AlwaysRevalidate, so this resolve re-checks upstream unconditionally
	// even if nothing else changed.
	Refresh bool
}

// DefaultCacheKey derives the content key for a source kind from the resolved
// inputs, with NO per-source override applied (config-distribution-model §7A.4 /
// D6). It is the kind-default half of EffectiveCacheKey, exported so callers that
// only need the default (e.g. a doctor "what would the default be" preview) can
// compute it directly.
//
// Each kind keys on the fact that pins its content:
//
//	git   -> resolved commit
//	local -> resolved commit + working-tree state (dirty marker or content hash)
//	http  -> ETag, else Last-Modified, else content digest
//	oci   -> manifest digest
//
// An unknown kind keys on the content digest as a safe, content-addressed
// fallback rather than panicking, so a future source type still gets a usable
// (if coarse) key.
func DefaultCacheKey(kind SourceKind, in CacheKeyInputs) string {
	switch kind {
	case SourceKindGit:
		return tag(kind, in.ResolvedCommit)
	case SourceKindLocal:
		return tag(kind, localDefaultKey(in))
	case SourceKindHTTP:
		return tag(kind, httpDefaultKey(in))
	case SourceKindOCI:
		return tag(kind, in.OCIDigest)
	default:
		return tag(kind, in.ContentDigest)
	}
}

// localDefaultKey builds the local-source default: the resolved commit, extended
// with working-tree state when the tree is dirty (§7A.4 / D6). A precise
// WorktreeContentHash is preferred; absent that, the literal dirty marker keeps
// the key distinct from the clean-commit key so uncommitted authoring still
// re-triggers a re-check.
func localDefaultKey(in CacheKeyInputs) string {
	if !in.WorktreeDirty {
		return in.ResolvedCommit
	}
	worktree := in.WorktreeContentHash
	if worktree == "" {
		worktree = "dirty"
	}
	return in.ResolvedCommit + "+" + worktree
}

// httpDefaultKey applies the http validator preference order: ETag (the strong
// validator) wins, then Last-Modified, then a content digest when the server
// sent neither (§7A.4). Each branch is tagged so an ETag value can never alias a
// Last-Modified value that happens to share text.
func httpDefaultKey(in CacheKeyInputs) string {
	switch {
	case in.ETag != "":
		return "etag=" + in.ETag
	case in.LastModified != "":
		return "lastmod=" + in.LastModified
	default:
		return "digest=" + in.ContentDigest
	}
}

// EffectiveCacheKey is the single resolution point for a source's content key
// (config-distribution-model §7A.4 / R6). It composes, in priority order:
//
//  1. FORCE ESCAPES — a per-unit `--refresh` (opts.Refresh) or the source's
//     config-declared always_revalidate marker yields AlwaysRevalidate, which
//     never matches a recorded digest so the resolver always re-checks.
//  2. PER-SOURCE OVERRIDE — when CacheKeys carries selectors (file/glob, git
//     commit/tags, env, dir), the key is derived from those selectors.
//  3. KIND DEFAULT — otherwise DefaultCacheKey(kind, in).
//
// The result is a stable, namespaced string suitable for recording in / comparing
// against the lock. It is deterministic: identical inputs always yield identical
// keys, and selector ordering never affects the result (file/env/dir/tags are
// sorted before hashing).
func EffectiveCacheKey(kind SourceKind, keys *CacheKeys, in CacheKeyInputs, opts CacheKeyOptions) string {
	if opts.Refresh || (keys != nil && keys.AlwaysRevalidate) {
		return AlwaysRevalidate
	}
	if keys != nil && hasSelectors(keys) {
		return overrideCacheKey(kind, keys, in)
	}
	return DefaultCacheKey(kind, in)
}

// hasSelectors reports whether the override declares at least one input selector
// (the always_revalidate force escape is handled separately, before this is
// consulted). When false the caller falls back to the kind default.
func hasSelectors(keys *CacheKeys) bool {
	return len(keys.Files) > 0 ||
		(keys.Git != nil && (keys.Git.Commit || keys.Git.Tags)) ||
		len(keys.Env) > 0 ||
		len(keys.Dir) > 0
}

// overrideCacheKey derives the composite key from the declared selectors. Each
// selector contributes a stable, labeled segment; the segments are hashed
// together so the override key is fixed-width and namespaced by kind. A selector
// whose backing input is absent contributes a deterministic "absent" segment
// (e.g. an unset env var), so the absence is itself part of the key.
func overrideCacheKey(kind SourceKind, keys *CacheKeys, in CacheKeyInputs) string {
	var segs []string
	segs = append(segs, fileSegments(keys.Files, in.FileContents)...)
	segs = append(segs, gitSegments(keys.Git, in)...)
	segs = append(segs, envSegments(keys.Env, in.EnvValues)...)
	segs = append(segs, dirSegments(keys.Dir, in.DirPresent)...)
	return tag(kind, hashSegments(segs))
}

// fileSegments folds each declared file/glob entry and its resolved content hash
// into a labeled segment. Entries are sorted so declaration order does not
// change the key. A file with no recorded hash contributes "absent".
func fileSegments(files []string, contents map[string]string) []string {
	return mapSegments("file", files, func(f string) string {
		if h, ok := contents[f]; ok {
			return h
		}
		return "absent"
	})
}

// gitSegments folds the selected git facts (commit and/or tags) into labeled
// segments. Tags are sorted so a re-ordered tag list does not change the key. A
// nil selector contributes nothing.
func gitSegments(sel *CacheKeyGit, in CacheKeyInputs) []string {
	if sel == nil {
		return nil
	}
	var segs []string
	if sel.Commit {
		segs = append(segs, "git:commit="+in.ResolvedCommit)
	}
	if sel.Tags {
		tags := append([]string(nil), in.Tags...)
		sort.Strings(tags)
		segs = append(segs, "git:tags="+strings.Join(tags, ","))
	}
	return segs
}

// envSegments folds each declared env var name and its current value into a
// labeled segment. Names are sorted for determinism; an unset var contributes
// "absent" so its absence is part of the key.
func envSegments(names []string, values map[string]string) []string {
	return mapSegments("env", names, func(n string) string {
		if v, ok := values[n]; ok {
			return v
		}
		return "absent"
	})
}

// dirSegments folds each declared dir-presence marker into a labeled segment:
// "present" when the dir exists, "absent" otherwise. Paths are sorted for
// determinism.
func dirSegments(dirs []string, present map[string]bool) []string {
	return mapSegments("dir", dirs, func(d string) string {
		if present[d] {
			return "present"
		}
		return "absent"
	})
}

// mapSegments builds "<label>:<key>=<value>" segments for a sorted copy of keys,
// resolving each value through valueFor. It is the shared shape behind the file,
// env, and dir selectors so sorting + labeling is written once.
func mapSegments(label string, keys []string, valueFor func(string) string) []string {
	if len(keys) == 0 {
		return nil
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	segs := make([]string, 0, len(sorted))
	for _, k := range sorted {
		segs = append(segs, label+":"+k+"="+valueFor(k))
	}
	return segs
}

// hashSegments joins the already-stable segments with a separator that cannot
// appear unescaped in a JSON-string segment value and returns a "sha256:…"
// digest, so the composite override key is fixed-width regardless of how many
// selectors fed it. An empty segment set hashes deterministically too.
func hashSegments(segs []string) string {
	joined := strings.Join(segs, "\x00")
	sum := sha256.Sum256([]byte(joined))
	return inputsDigestPrefix + hex.EncodeToString(sum[:])
}

// tag namespaces a derived key value by its source kind so two kinds can never
// produce the same effective key from coincidentally-equal values. An empty
// value still produces a stable, kind-tagged "absent" key rather than a bare
// prefix.
func tag(kind SourceKind, value string) string {
	if value == "" {
		value = "absent"
	}
	return cacheKeyPrefix + string(kind) + ":" + value
}

// MarshalJSON omits an all-zero cache-keys object so a source relying on the kind
// default round-trips with no cache_keys key at all (byte-stable v1/v2 contract).
// A non-zero override marshals through the field-tagged shape.
func (c CacheKeys) MarshalJSON() ([]byte, error) {
	type wire CacheKeys
	return json.Marshal(wire(c))
}
