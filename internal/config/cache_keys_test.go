package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── DefaultCacheKey: per-kind defaults (config-distribution-model §7A.4 / D6) ──

// TestDefaultCacheKey_PerKind exercises every kind's default derivation in one
// table: git keys on the commit, local on commit (+worktree when dirty), http on
// ETag→Last-Modified→digest, oci on the manifest digest, and an unknown kind on
// the content digest. Each case asserts both the kind tag and the load-bearing
// value are present (positive) and that a sentinel from a different branch is NOT
// (negative).
func TestDefaultCacheKey_PerKind(t *testing.T) {
	cases := []struct {
		name        string
		kind        SourceKind
		in          CacheKeyInputs
		wantSubstr  string
		wantTag     string
		notExpected string
	}{
		{
			name:        "git resolved commit",
			kind:        SourceKindGit,
			in:          CacheKeyInputs{ResolvedCommit: "abc123"},
			wantSubstr:  "abc123",
			wantTag:     "git:",
			notExpected: "etag=",
		},
		{
			name:        "local clean keys on commit only",
			kind:        SourceKindLocal,
			in:          CacheKeyInputs{ResolvedCommit: "deadbeef"},
			wantSubstr:  "deadbeef",
			wantTag:     "local:",
			notExpected: "+", // no worktree extension when clean
		},
		{
			name:        "local dirty without content hash uses dirty marker",
			kind:        SourceKindLocal,
			in:          CacheKeyInputs{ResolvedCommit: "deadbeef", WorktreeDirty: true},
			wantSubstr:  "deadbeef+dirty",
			wantTag:     "local:",
			notExpected: "etag=",
		},
		{
			name:        "local dirty with content hash uses precise hash",
			kind:        SourceKindLocal,
			in:          CacheKeyInputs{ResolvedCommit: "deadbeef", WorktreeDirty: true, WorktreeContentHash: "wt99"},
			wantSubstr:  "deadbeef+wt99",
			wantTag:     "local:",
			notExpected: "+dirty",
		},
		{
			name:        "http prefers ETag",
			kind:        SourceKindHTTP,
			in:          CacheKeyInputs{ETag: "W/\"e1\"", LastModified: "Mon", ContentDigest: "d1"},
			wantSubstr:  "etag=W/\"e1\"",
			wantTag:     "http:",
			notExpected: "lastmod=",
		},
		{
			name:        "http falls back to Last-Modified",
			kind:        SourceKindHTTP,
			in:          CacheKeyInputs{LastModified: "Mon, 01 Jan 2024", ContentDigest: "d1"},
			wantSubstr:  "lastmod=Mon, 01 Jan 2024",
			wantTag:     "http:",
			notExpected: "etag=",
		},
		{
			name:        "http falls back to content digest",
			kind:        SourceKindHTTP,
			in:          CacheKeyInputs{ContentDigest: "sha256:cafe"},
			wantSubstr:  "digest=sha256:cafe",
			wantTag:     "http:",
			notExpected: "etag=",
		},
		{
			name:        "oci manifest digest",
			kind:        SourceKindOCI,
			in:          CacheKeyInputs{OCIDigest: "sha256:0c1"},
			wantSubstr:  "sha256:0c1",
			wantTag:     "oci:",
			notExpected: "etag=",
		},
		{
			name:        "unknown kind falls back to content digest",
			kind:        SourceKind("ftp"),
			in:          CacheKeyInputs{ContentDigest: "sha256:fa11"},
			wantSubstr:  "sha256:fa11",
			wantTag:     "ftp:",
			notExpected: "git:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultCacheKey(tc.kind, tc.in)
			if !strings.HasPrefix(got, cacheKeyPrefix) {
				t.Fatalf("key %q missing cache-key prefix %q", got, cacheKeyPrefix)
			}
			if !strings.Contains(got, tc.wantTag) {
				t.Errorf("key %q missing kind tag %q", got, tc.wantTag)
			}
			if !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("key %q missing expected value %q", got, tc.wantSubstr)
			}
			if tc.notExpected != "" && strings.Contains(got, tc.notExpected) {
				t.Errorf("key %q must not contain %q", got, tc.notExpected)
			}
		})
	}
}

// TestDefaultCacheKey_EmptyValueTagged confirms an absent fact still yields a
// stable kind-tagged "absent" key rather than a bare prefix — so a source with
// no resolved facts never collides with another kind's empty key.
func TestDefaultCacheKey_EmptyValueTagged(t *testing.T) {
	got := DefaultCacheKey(SourceKindGit, CacheKeyInputs{})
	want := cacheKeyPrefix + "git:absent"
	if got != want {
		t.Errorf("empty git key = %q, want %q", got, want)
	}
}

// ── EffectiveCacheKey: force escapes (R6) ─────────────────────────────────────

// TestEffectiveCacheKey_ForceEscapes asserts both force escapes — the per-unit
// --refresh runtime flag and the config-declared always_revalidate marker —
// collapse the effective key to the AlwaysRevalidate sentinel, overriding both
// selectors and the kind default. The negative leg confirms a non-escaped call
// does NOT produce the sentinel.
func TestEffectiveCacheKey_ForceEscapes(t *testing.T) {
	in := CacheKeyInputs{ResolvedCommit: "abc"}

	if got := EffectiveCacheKey(SourceKindGit, nil, in, CacheKeyOptions{Refresh: true}); got != AlwaysRevalidate {
		t.Errorf("--refresh: got %q, want sentinel %q", got, AlwaysRevalidate)
	}

	keys := &CacheKeys{AlwaysRevalidate: true}
	if got := EffectiveCacheKey(SourceKindGit, keys, in, CacheKeyOptions{}); got != AlwaysRevalidate {
		t.Errorf("always_revalidate marker: got %q, want sentinel %q", got, AlwaysRevalidate)
	}

	// Force escape supersedes a declared selector too.
	keysWithSelector := &CacheKeys{AlwaysRevalidate: true, Files: []string{"a.txt"}}
	if got := EffectiveCacheKey(SourceKindGit, keysWithSelector, in, CacheKeyOptions{}); got != AlwaysRevalidate {
		t.Errorf("always_revalidate over selector: got %q, want sentinel", got)
	}

	// Negative: no escape, no override → kind default, never the sentinel.
	if got := EffectiveCacheKey(SourceKindGit, nil, in, CacheKeyOptions{}); got == AlwaysRevalidate {
		t.Errorf("non-escaped call must not yield the always-revalidate sentinel: %q", got)
	}
}

// ── EffectiveCacheKey: per-source override selectors ──────────────────────────

// TestEffectiveCacheKey_FallsBackToDefaultWhenNoSelectors confirms that a
// non-nil but selector-less CacheKeys (and a nil one) both fall through to the
// kind default rather than producing a composite override key.
func TestEffectiveCacheKey_FallsBackToDefaultWhenNoSelectors(t *testing.T) {
	in := CacheKeyInputs{ResolvedCommit: "abc"}
	want := DefaultCacheKey(SourceKindGit, in)

	if got := EffectiveCacheKey(SourceKindGit, nil, in, CacheKeyOptions{}); got != want {
		t.Errorf("nil keys: got %q, want default %q", got, want)
	}
	if got := EffectiveCacheKey(SourceKindGit, &CacheKeys{}, in, CacheKeyOptions{}); got != want {
		t.Errorf("empty keys: got %q, want default %q", got, want)
	}
}

// TestEffectiveCacheKey_FileSelector keys the source on declared files: a
// changed file content yields a different key (positive), and identical content
// yields an identical key. A file missing from FileContents is recorded as
// "absent" and still differs from a present-content key.
func TestEffectiveCacheKey_FileSelector(t *testing.T) {
	keys := &CacheKeys{Files: []string{"a.txt", "b.txt"}}

	base := EffectiveCacheKey(SourceKindLocal, keys, CacheKeyInputs{
		FileContents: map[string]string{"a.txt": "h1", "b.txt": "h2"},
	}, CacheKeyOptions{})
	same := EffectiveCacheKey(SourceKindLocal, keys, CacheKeyInputs{
		FileContents: map[string]string{"b.txt": "h2", "a.txt": "h1"},
	}, CacheKeyOptions{})
	changed := EffectiveCacheKey(SourceKindLocal, keys, CacheKeyInputs{
		FileContents: map[string]string{"a.txt": "h1", "b.txt": "DIFFERENT"},
	}, CacheKeyOptions{})
	missing := EffectiveCacheKey(SourceKindLocal, keys, CacheKeyInputs{
		FileContents: map[string]string{"a.txt": "h1"}, // b.txt absent
	}, CacheKeyOptions{})

	if base != same {
		t.Errorf("identical file content must yield identical key: %q vs %q", base, same)
	}
	if base == changed {
		t.Errorf("changed file content must yield a different key")
	}
	if base == missing {
		t.Errorf("absent file must yield a different key than present content")
	}
}

// TestEffectiveCacheKey_GitSelector covers the {git:{commit,tags}} selector:
// commit-only, tags-only, both, and a no-op selector (both false → falls back to
// the kind default because hasSelectors reports no real selector).
func TestEffectiveCacheKey_GitSelector(t *testing.T) {
	in := CacheKeyInputs{ResolvedCommit: "c1", Tags: []string{"v2", "v1"}}

	commitOnly := EffectiveCacheKey(SourceKindGit, &CacheKeys{Git: &CacheKeyGit{Commit: true}}, in, CacheKeyOptions{})
	tagsOnly := EffectiveCacheKey(SourceKindGit, &CacheKeys{Git: &CacheKeyGit{Tags: true}}, in, CacheKeyOptions{})
	both := EffectiveCacheKey(SourceKindGit, &CacheKeys{Git: &CacheKeyGit{Commit: true, Tags: true}}, in, CacheKeyOptions{})

	if commitOnly == tagsOnly || commitOnly == both || tagsOnly == both {
		t.Errorf("commit/tags/both selectors must produce distinct keys: %q %q %q", commitOnly, tagsOnly, both)
	}

	// Tag ordering must not change the key (sorted before hashing).
	reordered := EffectiveCacheKey(SourceKindGit, &CacheKeys{Git: &CacheKeyGit{Tags: true}},
		CacheKeyInputs{ResolvedCommit: "c1", Tags: []string{"v1", "v2"}}, CacheKeyOptions{})
	if tagsOnly != reordered {
		t.Errorf("tag ordering must not affect the key: %q vs %q", tagsOnly, reordered)
	}

	// A re-tag (same commit, different tag set) changes the key.
	retag := EffectiveCacheKey(SourceKindGit, &CacheKeys{Git: &CacheKeyGit{Tags: true}},
		CacheKeyInputs{ResolvedCommit: "c1", Tags: []string{"v3"}}, CacheKeyOptions{})
	if tagsOnly == retag {
		t.Errorf("a different tag set must yield a different key")
	}

	// No-op git selector (both false) is not a real selector → kind default.
	noop := EffectiveCacheKey(SourceKindGit, &CacheKeys{Git: &CacheKeyGit{}}, in, CacheKeyOptions{})
	if noop != DefaultCacheKey(SourceKindGit, in) {
		t.Errorf("empty git selector must fall back to the kind default: %q", noop)
	}
}

// TestEffectiveCacheKey_EnvSelector keys on env var values: a changed value is a
// new key, and an unset var ("absent") differs from any set value.
func TestEffectiveCacheKey_EnvSelector(t *testing.T) {
	keys := &CacheKeys{Env: []string{"TOKEN", "FLAG"}}

	set := EffectiveCacheKey(SourceKindHTTP, keys, CacheKeyInputs{
		EnvValues: map[string]string{"TOKEN": "t1", "FLAG": "on"},
	}, CacheKeyOptions{})
	changed := EffectiveCacheKey(SourceKindHTTP, keys, CacheKeyInputs{
		EnvValues: map[string]string{"TOKEN": "t2", "FLAG": "on"},
	}, CacheKeyOptions{})
	unset := EffectiveCacheKey(SourceKindHTTP, keys, CacheKeyInputs{
		EnvValues: map[string]string{"FLAG": "on"}, // TOKEN absent
	}, CacheKeyOptions{})

	if set == changed {
		t.Errorf("a changed env value must yield a different key")
	}
	if set == unset {
		t.Errorf("an unset env var must yield a different key than a set one")
	}
}

// TestEffectiveCacheKey_DirSelector keys on directory presence: present vs absent
// yield different keys.
func TestEffectiveCacheKey_DirSelector(t *testing.T) {
	keys := &CacheKeys{Dir: []string{"vendor", "node_modules"}}

	present := EffectiveCacheKey(SourceKindLocal, keys, CacheKeyInputs{
		DirPresent: map[string]bool{"vendor": true, "node_modules": true},
	}, CacheKeyOptions{})
	absent := EffectiveCacheKey(SourceKindLocal, keys, CacheKeyInputs{
		DirPresent: map[string]bool{"vendor": true}, // node_modules absent
	}, CacheKeyOptions{})

	if present == absent {
		t.Errorf("a dir flipping present→absent must yield a different key")
	}
}

// TestEffectiveCacheKey_CompositeSelectors confirms multiple selector kinds
// combine: dropping any one selector's contribution changes the composite key,
// proving each selector actually feeds the hash.
func TestEffectiveCacheKey_CompositeSelectors(t *testing.T) {
	full := &CacheKeys{
		Files: []string{"a.txt"},
		Git:   &CacheKeyGit{Commit: true},
		Env:   []string{"TOKEN"},
		Dir:   []string{"vendor"},
	}
	in := CacheKeyInputs{
		ResolvedCommit: "c1",
		FileContents:   map[string]string{"a.txt": "h1"},
		EnvValues:      map[string]string{"TOKEN": "t1"},
		DirPresent:     map[string]bool{"vendor": true},
	}
	base := EffectiveCacheKey(SourceKindLocal, full, in, CacheKeyOptions{})

	// Flip the env value: the composite must change, proving env participated.
	envChanged := EffectiveCacheKey(SourceKindLocal, full, CacheKeyInputs{
		ResolvedCommit: "c1",
		FileContents:   map[string]string{"a.txt": "h1"},
		EnvValues:      map[string]string{"TOKEN": "t2"},
		DirPresent:     map[string]bool{"vendor": true},
	}, CacheKeyOptions{})
	if base == envChanged {
		t.Errorf("composite key must change when an env value changes")
	}

	// The composite is still kind-tagged and fixed-width (sha256).
	if !strings.HasPrefix(base, cacheKeyPrefix+"local:"+inputsDigestPrefix) {
		t.Errorf("composite key %q missing local kind tag + sha256 body", base)
	}
}

// ── CacheKeys.IsZero ──────────────────────────────────────────────────────────

// TestCacheKeys_IsZero covers the zero/non-zero decision for every field plus
// the nil receiver — the predicate callers use to decide whether to emit the
// cache_keys object at all.
func TestCacheKeys_IsZero(t *testing.T) {
	var nilKeys *CacheKeys
	if !nilKeys.IsZero() {
		t.Errorf("nil *CacheKeys must be zero")
	}
	if !(&CacheKeys{}).IsZero() {
		t.Errorf("empty CacheKeys must be zero")
	}

	nonZero := []*CacheKeys{
		{Files: []string{"a"}},
		{Git: &CacheKeyGit{Commit: true}},
		{Env: []string{"X"}},
		{Dir: []string{"d"}},
		{AlwaysRevalidate: true},
	}
	for i, k := range nonZero {
		if k.IsZero() {
			t.Errorf("nonZero[%d] %+v must not be zero", i, k)
		}
	}
}

// ── JSON (de)serialization + round-trip ───────────────────────────────────────

// TestCacheKeys_MarshalRoundTrip asserts a populated CacheKeys round-trips
// through JSON with every field preserved and the documented wire keys emitted.
func TestCacheKeys_MarshalRoundTrip(t *testing.T) {
	orig := CacheKeys{
		Files:            []string{"a.txt", "b/*.md"},
		Git:              &CacheKeyGit{Commit: true, Tags: true},
		Env:              []string{"TOKEN"},
		Dir:              []string{"vendor"},
		AlwaysRevalidate: true,
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"file"`, `"git"`, `"commit"`, `"tags"`, `"env"`, `"dir"`, `"always_revalidate"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("marshaled cache_keys missing wire key %s: %s", key, data)
		}
	}

	var rt CacheKeys
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rt.Files) != 2 || rt.Files[0] != "a.txt" {
		t.Errorf("files round-trip lost: %+v", rt.Files)
	}
	if rt.Git == nil || !rt.Git.Commit || !rt.Git.Tags {
		t.Errorf("git selector round-trip lost: %+v", rt.Git)
	}
	if len(rt.Env) != 1 || rt.Env[0] != "TOKEN" || len(rt.Dir) != 1 || rt.Dir[0] != "vendor" {
		t.Errorf("env/dir round-trip lost: env=%v dir=%v", rt.Env, rt.Dir)
	}
	if !rt.AlwaysRevalidate {
		t.Errorf("always_revalidate round-trip lost")
	}
}

// TestCacheKeys_MarshalEmptyOmitsFields confirms a zero CacheKeys marshals to an
// empty object — no selector keys leak — so a source relying on the kind default
// emits nothing load-bearing.
func TestCacheKeys_MarshalEmptyOmitsFields(t *testing.T) {
	data, err := json.Marshal(CacheKeys{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "{}" {
		t.Errorf("empty CacheKeys must marshal to {}, got %s", data)
	}
}

// TestSource_CacheKeysOmittedWhenNil confirms a Source with no CacheKeys does NOT
// emit a "cache_keys" key (byte-stable v1/default-source contract), while a
// Source carrying an override does emit it.
func TestSource_CacheKeysOmittedWhenNil(t *testing.T) {
	plain, err := json.Marshal(Source{Type: "local"})
	if err != nil {
		t.Fatalf("marshal plain: %v", err)
	}
	if strings.Contains(string(plain), "cache_keys") {
		t.Errorf("default source must not emit cache_keys: %s", plain)
	}

	withKeys, err := json.Marshal(Source{
		Type:      "git",
		URL:       "https://example.com/r.git",
		CacheKeys: &CacheKeys{AlwaysRevalidate: true},
	})
	if err != nil {
		t.Fatalf("marshal withKeys: %v", err)
	}
	if !strings.Contains(string(withKeys), `"cache_keys"`) {
		t.Errorf("source with override must emit cache_keys: %s", withKeys)
	}

	// Full Source round-trip preserves the override.
	var rt Source
	if err := json.Unmarshal(withKeys, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rt.CacheKeys == nil || !rt.CacheKeys.AlwaysRevalidate {
		t.Errorf("source cache_keys round-trip lost: %+v", rt.CacheKeys)
	}
}

// TestSource_CacheKeysSchemaValidates confirms a manifest carrying a populated
// source.cache_keys object validates against the shipped schema (the schema must
// allow the new key under additionalProperties:false on the source object).
func TestSource_CacheKeysSchemaValidates(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	manifest := map[string]any{
		"version":  2,
		"hooks":    false,
		"mcp":      false,
		"settings": false,
		"sources": []any{
			map[string]any{
				"type": "git",
				"url":  "https://example.com/r.git",
				"cache_keys": map[string]any{
					"file":              []any{"a.txt"},
					"git":               map[string]any{"commit": true, "tags": true},
					"env":               []any{"TOKEN"},
					"dir":               []any{"vendor"},
					"always_revalidate": true,
				},
			},
		},
	}
	if err := sch.Validate(manifest); err != nil {
		t.Fatalf("source with cache_keys must validate against schema: %v", err)
	}
}

// TestCacheKeys_UnmarshalRejectsTypeMismatch confirms a wrongly-typed selector
// value (a string where a bool is expected) is a hard unmarshal error, not a
// silently-dropped field — so a malformed cache_keys block fails loudly at load
// rather than resolving with an unintended default.
func TestCacheKeys_UnmarshalRejectsTypeMismatch(t *testing.T) {
	bad := []byte(`{"always_revalidate":"not-a-bool"}`)
	var c CacheKeys
	if err := json.Unmarshal(bad, &c); err == nil {
		t.Fatal("expected unmarshal error for non-bool always_revalidate")
	}

	badGit := []byte(`{"git":{"commit":"yes"}}`)
	var c2 CacheKeys
	if err := json.Unmarshal(badGit, &c2); err == nil {
		t.Fatal("expected unmarshal error for non-bool git.commit")
	}
}

// TestSource_CacheKeysSchemaRejectsUnknownKey confirms additionalProperties:false
// on the cache_keys object rejects an undeclared selector — guarding against
// silent struct↔schema drift if a future field is added to one side only.
func TestSource_CacheKeysSchemaRejectsUnknownKey(t *testing.T) {
	sch := compileAgentsRCSchema(t)
	bad := map[string]any{
		"version":  2,
		"hooks":    false,
		"mcp":      false,
		"settings": false,
		"sources": []any{
			map[string]any{
				"type":       "local",
				"cache_keys": map[string]any{"bogus_selector": true},
			},
		},
	}
	if err := sch.Validate(bad); err == nil {
		t.Fatal("expected validation error for unknown cache_keys selector")
	}
}
