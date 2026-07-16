package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// profile_lockfile.go is the §15 lock contribution for kind:profile units (R2).
// A profile is a first-class §15 unit identified by its absolute ref
// <source>:<name>; like a layer or an artifact it is recorded in .agentsrc.lock
// with a content DIGEST so a later resolve can confirm the inputs are unchanged
// WITHOUT re-deriving them — the team-reproducibility guarantee (§2.4): same
// source-set ⇒ same locked profile units ⇒ identical effective bundle. This file
// owns no parallel lock machinery: it produces LockedUnit entries the existing
// WriteUnitsLock writer persists into the shared "units" section, exactly as
// every other unit kind is locked.
//
// Two digest axes, kept distinct:
//
//   - the per-UNIT content digest here (ProfileUnitDigest) pins each fragment's
//     authored inputs — the §15 staleness driver (a fragment edit moves it);
//   - the per-RESOLUTION digest (ResolvedProfile.Digest, Decision 7) pins an
//     effective bundle for a context. The lock carries the units; the resolution
//     digest is reproducible from them.

// profileDigestPrefix is the content-hash scheme tag carried on a locked profile
// unit's Digest, matching the "sha256:…" convention every other §15 unit uses.
const profileDigestPrefix = "sha256:"

// profileUnitDigestPayload is the canonical, field-stable shape hashed into a
// profile unit's content digest. Every input that changes the fragment's
// resolved contribution is covered — identity (ref), merge discipline (kind),
// source-derived authority (scope), value-merge position (order), applicability
// (selector), provenance (authored), and the payload (bundle) — so the digest
// moves whenever any of them change, and never when only map-iteration order
// would (json.Marshal sorts map keys).
type profileUnitDigestPayload struct {
	Ref      string          `json:"ref"`
	Kind     ProfileKind     `json:"kind"`
	Scope    AuthorityScope  `json:"scope"`
	Order    int             `json:"order"`
	Selector ProfileSelector `json:"selector"`
	Authored bool            `json:"authored"`
	Bundle   map[string]any  `json:"bundle"`
}

// ProfileUnitDigest computes the content digest of one profile unit — a stable
// "sha256:<hex>" hash over its identity, authority, value-position, selector,
// provenance, and bundle. It is deterministic: the same fragment always hashes
// to the same digest, and two fragments differing in any covered field hash
// differently, so the lock detects a fragment edit.
func ProfileUnitDigest(p ConfigProfile) string {
	bundle := p.Bundle
	if bundle == nil {
		bundle = map[string]any{}
	}
	raw, err := json.Marshal(profileUnitDigestPayload{
		Ref:      p.Ref,
		Kind:     p.Kind,
		Scope:    p.Scope,
		Order:    p.Order,
		Selector: p.Selector,
		Authored: p.Authored,
		Bundle:   bundle,
	})
	if err != nil {
		// The payload is plain JSON values (no channels/funcs/cycles), so marshal
		// cannot fail; the empty return mirrors digestProfile's impossible-error path.
		return ""
	}
	sum := sha256.Sum256(raw)
	return profileDigestPrefix + hex.EncodeToString(sum[:])
}

// ProfileLockUnits projects a ProfileSet's profiles into §15 LockedUnit entries
// (kind: profile), keyed by the profile's NAMESPACED key, each carrying the unit
// content digest and the fetch timestamp. Keying by Key() (not the raw Ref) keeps
// an authored and a synthesized fragment that share a ref as TWO distinct lock
// entries instead of one collapsing the other — the identity collision the
// authored namespace exists to prevent. The result is deterministic in content:
// the digest is content-derived and the map is key-stable, so a re-run over the
// same set yields byte-identical entries regardless of input order.
// The fetchedAt parameter is retained for signature stability (the resolver
// threads its clock seam through here) but is no longer recorded: the lock is
// content-addressed and carries no per-unit timestamp (see LockedUnit).
func ProfileLockUnits(set ProfileSet, _ time.Time) map[string]LockedUnit {
	out := make(map[string]LockedUnit, len(set.Profiles))
	for _, p := range sortProfilesByKey(set.Profiles) {
		out[p.Key()] = LockedUnit{
			Kind:   UnitKindProfile,
			Digest: ProfileUnitDigest(p),
		}
	}
	return out
}

// MergeProfileUnits folds a ProfileSet's profile units into an existing units map
// (the lock's "units" section) without disturbing sibling kinds, returning a new
// map. It is the additive bridge the lock writer uses to record profile units
// alongside layer/artifact/manifest entries in one .agentsrc.lock.
func MergeProfileUnits(units map[string]LockedUnit, set ProfileSet, fetchedAt time.Time) map[string]LockedUnit {
	out := make(map[string]LockedUnit, len(units)+len(set.Profiles))
	for ref, u := range units {
		out[ref] = u
	}
	for ref, u := range ProfileLockUnits(set, fetchedAt) {
		out[ref] = u
	}
	return out
}

// ProfileUnitsForSnapshot derives the kind:profile lock contribution directly from
// a resolved snapshot (R2): it re-derives the profile set the resolver already
// produces (ProfileSetFromSnapshot, the same bridge da config explain uses) and
// projects it to LockedUnit entries. It is the ONE call the lock-generation caller
// makes to feed UnitsLock.ProfileUnits, so the profile units recorded in the lock
// are exactly the fragments the engine resolves — never a re-derived divergence. A
// malformed policy / authored profile fails closed (the same error the resolve
// path raises), never a silent empty contribution.
func ProfileUnitsForSnapshot(snap *Snapshot, fetchedAt time.Time) (map[string]LockedUnit, error) {
	set, err := ProfileSetFromSnapshot(snap)
	if err != nil {
		return nil, err
	}
	return ProfileLockUnits(set, fetchedAt), nil
}

// ProfileLockStatus classifies one profile unit's reproducibility state against
// the lock.
type ProfileLockStatus string

const (
	// ProfileLockOK means the current fragment's content digest matches the lock —
	// the prior resolution reproduces from the lock without re-deriving the unit.
	ProfileLockOK ProfileLockStatus = "ok"
	// ProfileLockMissing means a current fragment has no entry in the lock (a new
	// profile that was never locked).
	ProfileLockMissing ProfileLockStatus = "missing-from-lock"
	// ProfileLockStale means the locked entry's digest no longer matches the
	// current fragment (the fragment was edited since it was locked).
	ProfileLockStale ProfileLockStatus = "stale"
	// ProfileLockExtra means the lock carries a profile unit no longer present in
	// the current set (a removed fragment whose lock entry lingers).
	ProfileLockExtra ProfileLockStatus = "extra-in-lock"
)

// ProfileLockDelta is one profile unit's reproducibility record: its namespaced
// key, the classification, and the two digests so a caller renders a one-line
// diagnostic.
type ProfileLockDelta struct {
	// Key is the profile's namespaced addressable key (authored profiles carry the
	// "authored:" prefix), matching how the lock units are keyed.
	Key        string
	Status     ProfileLockStatus
	LockDigest string
	LiveDigest string
}

// ProfileLockReproducibility compares a ProfileSet against the profile units
// recorded in a lock and reports, per ref, whether the resolution reproduces from
// the lock (R2). A lock whose profile units all report ProfileLockOK certifies
// that the effective bundle is reproducible WITHOUT re-resolving — the §15
// content-hash staleness model applied to profiles. Non-profile lock entries are
// ignored. Records are keyed by the profile's NAMESPACED key (so an authored and
// a synthesized fragment sharing a ref are tracked independently, never aliased)
// and returned sorted by key for deterministic rendering.
func ProfileLockReproducibility(locked map[string]LockedUnit, set ProfileSet) []ProfileLockDelta {
	live := map[string]string{}
	for _, p := range set.Profiles {
		live[p.Key()] = ProfileUnitDigest(p)
	}
	lockedProfiles := map[string]string{}
	for key, u := range locked {
		if u.Kind == UnitKindProfile {
			lockedProfiles[key] = u.Digest
		}
	}

	seen := map[string]bool{}
	var out []ProfileLockDelta
	for key, liveDigest := range live {
		seen[key] = true
		lockDigest, ok := lockedProfiles[key]
		switch {
		case !ok:
			out = append(out, ProfileLockDelta{Key: key, Status: ProfileLockMissing, LiveDigest: liveDigest})
		case lockDigest != liveDigest:
			out = append(out, ProfileLockDelta{Key: key, Status: ProfileLockStale, LockDigest: lockDigest, LiveDigest: liveDigest})
		default:
			out = append(out, ProfileLockDelta{Key: key, Status: ProfileLockOK, LockDigest: lockDigest, LiveDigest: liveDigest})
		}
	}
	for key, lockDigest := range lockedProfiles {
		if !seen[key] {
			out = append(out, ProfileLockDelta{Key: key, Status: ProfileLockExtra, LockDigest: lockDigest})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ProfileLockReproducible reports whether every current profile unit is locked
// with a matching digest (no missing, stale, or extra entries) — the boolean
// "this resolution reproduces from the lock" summary (R2).
func ProfileLockReproducible(deltas []ProfileLockDelta) bool {
	for _, d := range deltas {
		if d.Status != ProfileLockOK {
			return false
		}
	}
	return true
}

// sortProfilesByKey returns the profiles sorted by their NAMESPACED key so any
// key-keyed projection (the lock map, the reproducibility report) is
// order-independent of the input and keeps authored/synthesized refs distinct.
func sortProfilesByKey(profiles []ConfigProfile) []ConfigProfile {
	out := append([]ConfigProfile{}, profiles...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}
