package config

import (
	"fmt"
	"sort"
)

// verify_prompts.go is the OFFLINE verification surface for source-qualified
// prompt units (prompt_units.go), the prompt-tier analogue of verify_layers.go.
//
// A prompt unit is pinned in the lock but its bytes live in the shared
// content-addressed cache under ~/.agents. That cache is machine-local and
// prunable (`da config cache prune`, a manual `rm -rf`, a new machine restored
// from a synced home), so the lock can be perfectly valid while the bytes it
// points at are gone. Before this check the gap surfaced only when
// `da workflow resolve-prompt` reported the prompt unresolved mid-dispatch;
// `da config verify` now catches it up front, with the same
// "run `da config sync`" hint the resolve path emits.

// PromptUnitStatus is the offline verification result for one lock-pinned prompt
// unit: what it points at, whether its cached bytes are present, and — where the
// digest is a content hash the local cache can re-derive — whether those bytes
// still match the pinned digest. It performs NO fetch and no lock mutation.
type PromptUnitStatus struct {
	// Key is the lock unit key, "<source-id>:<prompt-path>[@version]".
	Key string `json:"key"`
	// SourceID is the config source the prompt is fetched from.
	SourceID string `json:"source_id,omitempty"`
	// Path is the prompt path relative to the source root.
	Path string `json:"path,omitempty"`
	// Version is the declared source-relative pin, when the ref carries one.
	Version string `json:"version,omitempty"`
	// Digest is the resolved SHA/content hash recorded in the lock.
	Digest string `json:"digest,omitempty"`
	// Cached reports whether the cached bytes exist at the locked digest.
	Cached bool `json:"cached"`
	// CachePath is where those bytes belong (present or not), so an operator can
	// see exactly which entry a warning is about.
	CachePath string `json:"cache_path,omitempty"`
	// Problem is empty when the unit verifies; otherwise a short, actionable
	// reason (no digest, missing bytes, digest mismatch).
	Problem string `json:"problem,omitempty"`
}

// OK reports whether this prompt unit verified cleanly offline.
func (s PromptUnitStatus) OK() bool { return s.Problem == "" }

// VerifyPromptUnits cross-checks every `kind: prompt` unit pinned in the
// project's lock against the on-disk prompt cache, WITHOUT any fetch or lock
// mutation. Results are returned in stable key order.
//
// It enumerates the LOCK rather than the effective config's stage_profiles on
// purpose: the lock is what an offline `da workflow resolve-prompt` reads, so
// verifying exactly that set reports precisely the prompts a dispatch could fail
// to resolve — including prompts contributed by an imported team layer.
//
// Returns an empty slice (no error) when nothing is pinned; a malformed lock
// surfaces an error.
func VerifyPromptUnits(projectPath string) ([]PromptUnitStatus, error) {
	lock, err := ReadUnits(projectPath)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(lock.Units))
	for key, unit := range lock.Units {
		if unit.Kind == UnitKindPrompt {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]PromptUnitStatus, 0, len(keys))
	for _, key := range keys {
		out = append(out, verifyOnePromptUnit(key, lock.Units[key]))
	}
	return out, nil
}

// verifyOnePromptUnit checks one pinned prompt unit's cached bytes.
func verifyOnePromptUnit(key string, unit LockedUnit) PromptUnitStatus {
	st := PromptUnitStatus{Key: key, Digest: unit.Digest}
	parts, err := ParseLayerRef(key)
	if err != nil {
		st.Problem = "invalid prompt unit key: " + err.Error()
		return st
	}
	ref := PromptUnitRef{SourceID: parts.SourceID, Path: parts.LayerPath, Version: parts.Version}
	st.SourceID, st.Path, st.Version = ref.SourceID, ref.Path, ref.Version
	if unit.Digest == "" {
		st.Problem = "pinned without a digest (run `da config sync`)"
		return st
	}
	st.CachePath = CachedPromptPath(ref, unit.Digest)
	data, ok := readCachedUnit(promptTarget(ref), unit.Digest)
	if !ok {
		st.Problem = fmt.Sprintf("cached bytes for digest %s are missing (run `da config sync`)", shortSHA(unit.Digest))
		return st
	}
	st.Cached = true
	st.Problem = promptDigestProblem(unit.Digest, data)
	return st
}

// promptDigestProblem re-derives the content hash of the cached bytes and
// compares it against the pinned digest, returning a problem string on a
// mismatch and "" otherwise.
//
// Only a BARE 64-hex digest is checked: that is exactly what the http and local
// fetchers record (contentHash of the bytes), so the comparison is meaningful. A
// git digest is a 40-hex COMMIT — it identifies the revision the file came from,
// not the file's content — and an "sha256:<hex>" digest is an OCI manifest digest
// whose relationship to the blob bytes is the registry's contract, not ours. For
// those, presence at the digest-addressed path is the strongest offline
// statement available, and claiming a mismatch would be a false alarm.
func promptDigestProblem(digest string, data []byte) string {
	if !isBareSha256Hex(digest) {
		return ""
	}
	if got := contentHash(data); got != digest {
		return fmt.Sprintf("cached bytes no longer match the locked digest %s (found %s) — run `da config sync`",
			shortSHA(digest), shortSHA(got))
	}
	return ""
}

// isBareSha256Hex reports whether digest is 64 lowercase hex characters — the
// content-hash shape the http/local fetchers record (as opposed to a 40-hex git
// commit or a prefixed "sha256:<hex>" OCI digest).
func isBareSha256Hex(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for i := 0; i < len(digest); i++ {
		c := digest[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
