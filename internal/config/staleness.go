package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// staleness.go computes content-hash staleness for the unified units lockfile
// (config-distribution-model §7A.3 / §7A.4). Staleness is a cheap, local,
// clock-free decision: it is driven by content-hash *driver events*, never a
// TTL. A committed lock is stale iff one of three inputs actually changed:
//
//  1. inputs_digest mismatch — the whole-normalized hash of ALL local config
//     scopes (user-local + project-local overlay + repo-local committed) no
//     longer matches the digest recorded in the lock;
//  2. the declared `extends`/`packages` set changed — a unit ref was added or
//     removed from the manifest since the lock was written; or
//  3. a recorded unit digest no longer matches — a previously-resolved unit's
//     content hash drifted from what the lock recorded.
//
// None of these touch the network or a clock. The `cache_ttl` /
// `last_checked_at` axis is orthogonal: it never invalidates the lock, it only
// powers a doctor/explain review-nudge (see lockstatus.go).

// AgentsRCLocalFile is the project-local overlay manifest (§7A.1): the user's
// personal, machine-local, per-project layer (the `.git/config` analog), stored
// gitignored alongside the committed manifest. It is one of the local scopes
// folded into inputs_digest.
const AgentsRCLocalFile = ".agentsrc.local.json"

// inputsDigestPrefix is the algorithm tag prefixed onto a computed
// inputs_digest, matching the "sha256:…" convention the lock uses for unit
// digests so the two are visually consistent.
const inputsDigestPrefix = "sha256:"

// localScopeManifests returns the absolute paths of the local config scopes
// folded into inputs_digest, in a stable order (user-local, project-local
// overlay, repo-local committed). The user-local path honors the resolver's
// test seam when set, falling back to <AgentsHome>/.agentsrc.json.
//
// Order is fixed (not sorted by path) so the digest is reproducible regardless
// of where the scopes live on disk; the bytes hashed below already namespace
// each scope by its slot, so two scopes can never alias.
func localScopeManifests(projectPath, userLocalPath string) []string {
	userPath := userLocalPath
	if userPath == "" {
		userPath = filepath.Join(AgentsHome(), AgentsRCFile)
	}
	return []string{
		userPath,
		filepath.Join(projectPath, AgentsRCLocalFile),
		filepath.Join(projectPath, AgentsRCFile),
	}
}

// scopeContent is one local scope's normalized contribution to inputs_digest:
// its stable slot key and the whole-normalized bytes of its manifest (empty
// when the scope file is absent — absence is a stable, hashable state).
type scopeContent struct {
	Slot  string `json:"slot"`
	Bytes string `json:"bytes"`
}

// ComputeInputsDigest hashes all local config scopes whole-normalized (§7A.3),
// returning a "sha256:…" digest. Each scope's manifest is re-marshaled through
// canonical JSON so cosmetic edits (key order, whitespace) do not register as a
// content change; a missing scope hashes as empty. The result is the value
// compared against the lock's recorded inputs_digest to detect scope drift.
//
// userLocalPath is the resolver's user-local manifest seam (empty ⇒ default
// <AgentsHome>/.agentsrc.json), so staleness honors the same test override the
// resolver uses.
func ComputeInputsDigest(projectPath, userLocalPath string) (string, error) {
	scopes := make([]scopeContent, 0, 3)
	for i, path := range localScopeManifests(projectPath, userLocalPath) {
		norm, err := normalizedManifestBytes(path)
		if err != nil {
			return "", err
		}
		scopes = append(scopes, scopeContent{Slot: scopeSlot(i), Bytes: norm})
	}
	// A []scopeContent of plain strings always marshals (mirrors the
	// impossible-marshal convention in agentslock.setVersion); any real failure
	// surfaces upstream from the manifest read/decode above.
	payload, _ := json.Marshal(scopes)
	sum := sha256.Sum256(payload)
	return inputsDigestPrefix + hex.EncodeToString(sum[:]), nil
}

// scopeSlot names a scope by its fixed index so each scope's bytes are
// namespaced in the hashed payload; identical content in different scopes still
// produces distinct digests.
func scopeSlot(i int) string {
	switch i {
	case 0:
		return "user-local"
	case 1:
		return "project-local"
	default:
		return "repo-local"
	}
}

// normalizedManifestBytes reads the manifest at path and re-encodes it through
// canonical JSON so the digest is insensitive to key order and whitespace while
// remaining LOSSLESS in two ways the standard unmarshal-into-`any` path is not:
//
//   - Number precision is preserved. Decoding into `any` coerces every JSON
//     number to float64, so two manifests that differ only in an integer beyond
//     2^53 (e.g. a large id or timestamp) would hash identically — a silent
//     false-fresh that hides a real config change. We decode with UseNumber()
//     and emit each number's exact source text.
//   - Duplicate object keys are rejected, not silently last-wins-collapsed.
//     `{"a":1,"a":2}` is a malformed-but-parseable manifest whose meaning is
//     ambiguous; collapsing it is another false-fresh vector, so we error.
//
// A missing file yields "" (absence is a stable state, not an error); an
// unreadable or malformed file surfaces the error so staleness fails loudly
// rather than silently treating a broken manifest as empty.
func normalizedManifestBytes(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var buf bytes.Buffer
	if err := canonicalizeJSON(dec, &buf); err != nil {
		return "", fmt.Errorf("normalizing %s: %w", path, err)
	}
	// A single top-level value is expected; trailing tokens (e.g. two JSON
	// documents concatenated) are a malformed manifest.
	if dec.More() {
		return "", fmt.Errorf("normalizing %s: trailing data after top-level value", path)
	}
	return buf.String(), nil
}

// canonicalizeJSON reads exactly one JSON value from dec and writes its
// canonical form to out: object keys sorted, no insignificant whitespace, and
// numbers emitted verbatim from their source text (lossless). It recurses for
// nested objects/arrays. Duplicate keys within an object are an error.
func canonicalizeJSON(dec *json.Decoder, out *bytes.Buffer) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return canonicalizeObject(dec, out)
		case '[':
			return canonicalizeArray(dec, out)
		default:
			return fmt.Errorf("unexpected delimiter %q", t)
		}
	default:
		return writeScalar(tok, out)
	}
}

// canonicalizeObject canonicalizes the body of an object whose '{' was already
// consumed: it collects every key/value as canonical bytes, errors on a
// duplicate key, then emits the pairs sorted by key so the output is
// order-independent.
func canonicalizeObject(dec *json.Decoder, out *bytes.Buffer) error {
	type pair struct{ key, val string }
	pairs := []pair{}
	seen := map[string]bool{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key := keyTok.(string)
		if seen[key] {
			return fmt.Errorf("duplicate object key %q", key)
		}
		seen[key] = true

		var valBuf bytes.Buffer
		if err := canonicalizeJSON(dec, &valBuf); err != nil {
			return err
		}
		pairs = append(pairs, pair{key: key, val: valBuf.String()})
	}
	if _, err := dec.Token(); err != nil { // consume closing '}'
		return err
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })

	out.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			out.WriteByte(',')
		}
		writeJSONString(p.key, out)
		out.WriteByte(':')
		out.WriteString(p.val)
	}
	out.WriteByte('}')
	return nil
}

// canonicalizeArray canonicalizes the body of an array whose '[' was already
// consumed, preserving element order (arrays are ordered, so position is
// significant — unlike object keys).
func canonicalizeArray(dec *json.Decoder, out *bytes.Buffer) error {
	out.WriteByte('[')
	first := true
	for dec.More() {
		if !first {
			out.WriteByte(',')
		}
		first = false
		if err := canonicalizeJSON(dec, out); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // consume closing ']'
		return err
	}
	out.WriteByte(']')
	return nil
}

// writeScalar emits a non-delimiter token (string, json.Number, bool, or null)
// in canonical form. json.Number is written verbatim so integer precision is
// preserved exactly; everything else round-trips through json.Marshal.
func writeScalar(tok json.Token, out *bytes.Buffer) error {
	if num, ok := tok.(json.Number); ok {
		out.WriteString(num.String())
		return nil
	}
	if s, ok := tok.(string); ok {
		writeJSONString(s, out)
		return nil
	}
	// bool and nil: a fixed set of values that always marshal.
	raw, _ := json.Marshal(tok)
	out.Write(raw)
	return nil
}

// writeJSONString writes s as a JSON string literal. The encoder cannot fail on
// a Go string, so the error is discarded (mirrors the impossible-marshal
// convention).
func writeJSONString(s string, out *bytes.Buffer) {
	raw, _ := json.Marshal(s)
	out.Write(raw)
}

// StalenessReason classifies why a lock is considered stale. A fresh lock has
// no reasons; a stale lock carries one or more.
type StalenessReason string

const (
	// ReasonInputsDigest means the whole-normalized hash of the local config
	// scopes no longer matches the lock's recorded inputs_digest (§7A.3).
	ReasonInputsDigest StalenessReason = "inputs-digest-mismatch"
	// ReasonDeclaredSet means the declared `extends`/`packages` unit set changed
	// since the lock was written (a ref was added or removed).
	ReasonDeclaredSet StalenessReason = "declared-set-changed"
	// ReasonUnitDigest means a recorded unit's digest no longer matches the
	// digest recomputed for its current content.
	ReasonUnitDigest StalenessReason = "unit-digest-mismatch"
)

// StalenessResult is the renderable outcome of a clock-free staleness check.
// Fresh reports the common "nothing changed" case; Reasons enumerates every
// driver event that fired so doctor/explain can show precisely what drifted.
type StalenessResult struct {
	// Fresh is true when no driver event fired — the lock matches every local
	// input and may be used as-is (the `--frozen`/no-op path of EnsureResolved).
	Fresh bool
	// Reasons lists the distinct driver events that made the lock stale, in a
	// stable order. Empty when Fresh.
	Reasons []StalenessReason
	// ExpectedInputsDigest is the inputs_digest computed from the current local
	// scopes; doctor/explain surface it next to the lock's recorded value.
	ExpectedInputsDigest string
}

// IsStale reports whether any driver event fired (the inverse of Fresh).
func (r StalenessResult) IsStale() bool { return !r.Fresh }

// CacheKeyStale reports whether a unit's recorded content cache key is stale
// relative to its freshly-derived effective key (config-distribution-model
// §7A.4). It is the cache-key axis of staleness — orthogonal to the
// inputs_digest / declared-set / unit-digest driver events in Staleness above —
// and the single point the resolver consults to decide whether the SHA-addressed
// cache may be served or the upstream must be re-checked.
//
// A unit is cache-key-stale when:
//
//   - the effective key is the AlwaysRevalidate sentinel — a force escape
//     (`--refresh` or the source's always_revalidate marker) is in play, so the
//     cache is bypassed unconditionally regardless of what was recorded; or
//   - the recorded key differs from the effective key — the per-source
//     cache_keys override or the kind default now pins the content on a different
//     fact than the lock captured (e.g. a changed {env} value, a re-tag, or a
//     switched selector), so the cached content can no longer be trusted as
//     current.
//
// An empty recorded key (a pre-cache-key lock, or a unit resolved before this
// axis existed) is treated as stale against any concrete effective key so the
// first resolve under the new model re-checks and records a key. Two empty keys
// are fresh (nothing to compare yet).
func CacheKeyStale(recorded, effective string) bool {
	if effective == AlwaysRevalidate {
		return true
	}
	return recorded != effective
}

// UnitDigestFunc recomputes the current content digest for a resolved unit ref
// ("source:path@version"), reporting whether the unit's content is locally
// available to hash. It is the seam through which staleness checks the third
// driver event (recorded digest no longer matches) without this file reaching
// into the resolver, the cache, or the network: callers that have a cheap local
// digest source (e.g. the local source's working-tree hash) supply one; callers
// that only want the inputs_digest + declared-set checks pass nil.
type UnitDigestFunc func(ref string) (digest string, ok bool)

// Staleness performs the full clock-free staleness check (§7A.3) for a project
// against its committed lock. It compares the recorded inputs_digest, the
// declared `extends`/`packages` set, and (when recompute is non-nil) each
// recorded unit's digest, returning every driver event that fired.
//
// It is strictly read-only and never touches the network or a clock. recompute
// may be nil to skip the per-unit digest check (inputs_digest + declared-set
// only); userLocalPath threads the resolver's user-local seam into the digest
// computation.
func Staleness(projectPath, userLocalPath string, recompute UnitDigestFunc) (StalenessResult, error) {
	rc, err := LoadAgentsRC(projectPath)
	if err != nil {
		return StalenessResult{}, err
	}
	lock, err := ReadUnits(projectPath)
	if err != nil {
		return StalenessResult{}, err
	}
	expected, err := ComputeInputsDigest(projectPath, userLocalPath)
	if err != nil {
		return StalenessResult{}, err
	}

	res := StalenessResult{ExpectedInputsDigest: expected}
	if expected != lock.InputsDigest {
		res.Reasons = append(res.Reasons, ReasonInputsDigest)
	}
	if declaredSetChanged(*rc, lock.Units) {
		res.Reasons = append(res.Reasons, ReasonDeclaredSet)
	}
	if recompute != nil && unitDigestChanged(lock.Units, recompute) {
		res.Reasons = append(res.Reasons, ReasonUnitDigest)
	}

	res.Fresh = len(res.Reasons) == 0
	return res, nil
}

// declaredSetChanged reports whether the manifest's declared unit set (the
// `extends` layers plus `packages` artifacts) differs from the set the lock
// recorded. Both sides are compared on their "source:path" identity (the
// version/version-spec suffix is stripped), so a declaration's resolved-version
// churn is left to the digest check while add/remove of a unit registers here.
func declaredSetChanged(rc AgentsRC, units map[string]LockedUnit) bool {
	declared := declaredUnitRefs(rc)
	locked := lockedUnitRefs(units)
	if len(declared) != len(locked) {
		return true
	}
	for ref := range declared {
		if !locked[ref] {
			return true
		}
	}
	return false
}

// declaredUnitRefs collects the manifest's declared unit identities — every
// `extends` layer and every `packages` artifact — as a set keyed by
// "source:path" (the declared version/version-spec suffix is stripped so the
// set matches resolved lock keys, which carry a concrete resolved version).
func declaredUnitRefs(rc AgentsRC) map[string]bool {
	refs := make(map[string]bool, len(rc.Extends)+len(rc.Packages))
	for _, e := range rc.Extends {
		refs[declaredRefOf(e.Ref)] = true
	}
	for _, p := range rc.Packages {
		refs[declaredRefOf(p.Ref)] = true
	}
	return refs
}

// lockedUnitRefs reduces the lock's resolved unit keys ("source:path@version")
// to the declared-ref form by stripping the resolved-version suffix, so the set
// comparison in declaredSetChanged matches manifest refs (which may omit or
// pin a version) against resolved lock keys.
func lockedUnitRefs(units map[string]LockedUnit) map[string]bool {
	refs := make(map[string]bool, len(units))
	for key, u := range units {
		// Only layer (from `extends`) and artifact (from `packages`) units are
		// DECLARED in the manifest, so only they belong in the declared-set
		// comparison. Derived units the resolver folds into the lock — kind:profile
		// (R2 stage profiles), project-set, descriptor — are never declared; counting
		// them here permanently trips ReasonDeclaredSet (their ref is not in the
		// manifest's declared set), forcing a re-resolve on EVERY run. That re-stamps
		// fetched_at/last_checked_at on every unit and breaks the H9 frozen-no-op
		// contract (a lock-clean re-install must not churn the lock).
		if u.Kind != UnitKindLayer && u.Kind != UnitKindArtifact {
			continue
		}
		// A transitively-pulled layer (config-transitive-layering: locked because
		// ANOTHER layer's own extends named it, not because this project's
		// manifest did) has no top-level declared identity either — the same
		// false-positive shape as the derived-unit case just above. Counting it
		// here trips ReasonDeclaredSet on every run for any project using
		// transitive layering, even immediately after a clean `da config sync`
		// (regression: config-verify-staleness-digest).
		if u.Transitive {
			continue
		}
		refs[declaredRefOf(key)] = true
	}
	return refs
}

// declaredRefOf returns the declared-ref prefix of a resolved lock key by
// trimming the "@<resolved-version>" suffix. A key with no "@" is returned
// unchanged. The split is on the LAST "@" so a path containing "@" is preserved
// up to the version delimiter.
func declaredRefOf(key string) string {
	if i := lastIndexByte(key, '@'); i >= 0 {
		return key[:i]
	}
	return key
}

// lastIndexByte returns the index of the last occurrence of b in s, or -1.
// (strings.LastIndexByte avoided to keep the import set minimal and the helper
// trivially inlinable.)
func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// unitDigestChanged reports whether any recorded unit's content digest drifted
// from what the lock holds. recompute supplies the current digest for a ref; a
// unit whose content is not locally available (ok=false) is skipped — its
// absence is a cache/install concern surfaced elsewhere, not a content-drift
// driver event. Keys are visited in sorted order so the check is deterministic.
func unitDigestChanged(units map[string]LockedUnit, recompute UnitDigestFunc) bool {
	for _, ref := range sortedUnitKeys(units) {
		current, ok := recompute(ref)
		if !ok {
			continue
		}
		if current != units[ref].Digest {
			return true
		}
	}
	return false
}

// sortedUnitKeys returns the unit map's keys in lexical order so digest checks
// and any rendering over the units are deterministic.
func sortedUnitKeys(units map[string]LockedUnit) []string {
	keys := make([]string, 0, len(units))
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
