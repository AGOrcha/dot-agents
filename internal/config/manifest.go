package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// manifest.go defines the kind:manifest unit model for the
// distributable-config-manifest spec (L2). A manifest is the ONE distributable,
// scope-attachable artifact that bundles, by REFERENCE, the canonical sources to
// pull, the layering policy/profiles that bind, and (optionally) the project-set
// to manage — the inputs `init --from` reproduces a whole setup from (D1/D2/R2).
//
// It is a §15 unit, not a bespoke file format (D1): addressed by the absolute ref
// <source>:<name>, locked in .agentsrc.lock, resolved/locked through the SAME
// source/scope/lock + inputs_digest machinery as every other unit. It is a
// DISTINCT kind from a profile (F2): a profile is a per-context resolution
// fragment; a manifest is a distributable bundle + entrypoint that NAMES which
// units bind. Resolution does NOT fork the engines — it composes the §15
// source/scope resolution with the L1 selector-merge engine (profile_resolver.go)
// and adds ZERO resolution semantics of its own (D2). Authority is SOURCE-derived
// (D4): a public/untrusted manifest cannot self-grant authority — the §15
// no-self-blessing invariant is reused verbatim, not re-implemented.

// UnitKindManifest is the §15 unit kind for a distributable config manifest (R1).
// It is recognized alongside layer/artifact/project-set/profile so the resolver
// and lock model fail-closed on an unknown kind rather than mis-resolving a
// manifest. Like project-set it is synced, scope-attachable, lockable config.
const UnitKindManifest = "manifest"

// ManifestSpec is the AUTHORED payload of a kind:manifest unit (R2). It carries
// only REFERENCES — never re-defined copies — of the units it binds (D2/D6) and
// NO secrets, NO self-declared authority (authority is source-derived, D4/F6).
// This lenient typed shape is what AgentsRC round-trips; the loud fail-closed
// validation (manifest->manifest edge, self-blessing authority, force-allow) runs
// at resolve time in decodeManifest (R10), mirroring the layering_policy split.
type ManifestSpec struct {
	// Sources are the canonical source refs the manifest pulls
	// (<source>:<path>@<version>, §15 D2). They are NAMES only — a private source
	// carries no credential (F6/NEW-FORK-B): auth is threaded ambient-first at
	// init --from, never embedded here.
	Sources []string `json:"sources,omitempty"`
	// Binds are the refs of the layering_policy + profile units that bind (R2b).
	// Empty ⇒ the manifest binds whatever policy the scope chain resolves; a
	// non-empty set narrows the bound fragments to exactly those refs.
	Binds []string `json:"binds,omitempty"`
	// ProjectSet is the OPTIONAL ref of a kind:project-set unit (R2c/D6). The
	// project-set IS the home-config portable identity registry (built by L3); the
	// manifest only references it. Empty ⇒ a manifest with no project-set, which is
	// valid and resolves (item 3).
	ProjectSet string `json:"project_set,omitempty"`
}

// ConfigManifest is one RESOLVED kind:manifest unit: its authored spec plus the
// loader-stamped identity (Ref) and SOURCE-derived authority (Scope, Decision 1).
// Scope is NEVER read from the unit's own contents — the loader stamps it from
// ref.source → source-registry → scope — so a public/untrusted manifest cannot
// self-grant authority (D4, the §15 no-self-blessing invariant).
type ConfigManifest struct {
	// Ref is the absolute unit ref <source>:<name> — the identity (§15 D2).
	Ref string
	// Scope is the SOURCE-derived authority scope (Decision 1), stamped by the
	// loader, never authored on the unit.
	Scope AuthorityScope
	// Spec is the authored, validated payload.
	Spec ManifestSpec
}

// manifestForbiddenFields are top-level keys a manifest may NOT carry, each mapped
// to the loud reason its presence is a validation error rather than a silently
// dropped input (R10). They enforce the two hard invariants: no manifest→manifest
// composition edge (D3) and no self-declared authority / no force-allow (D4).
var manifestForbiddenFields = map[string]string{
	"extends":          "manifests do not extend or inherit other manifests (no manifest->manifest edge); composition is selector-merge across the scope chain",
	"inherits":         "manifests do not extend or inherit other manifests (no manifest->manifest edge); composition is selector-merge across the scope chain",
	"composes":         "manifests do not compose other manifests by reference; composition is selector-merge across the scope chain",
	"authority":        "manifest authority is source-derived (ref.source -> source-registry -> scope), never self-declared",
	"scope":            "manifest authority is source-derived (ref.source -> source-registry -> scope), never self-declared",
	"authority_grants": "a manifest cannot self-grant authority; authority is source-derived",
	"force_allow":      "there is no force-allow: a lower scope can never punch a capability through a higher deny",
}

// manifestSpecWire is the strict decode shape for a ManifestSpec: a named type
// WITHOUT ManifestSpec's UnmarshalJSON, so DisallowUnknownFields decoding cannot
// recurse back into the custom unmarshaler (the agentsRCCore aliasing convention).
type manifestSpecWire ManifestSpec

// decodeManifestSpec decodes and validates one manifest payload fail-closed,
// independent of identity/scope. A forbidden field (manifest->manifest edge,
// self-declared authority, force-allow), an unknown field, malformed JSON, or an
// unpinned/malformed ref is a loud error (R10/D3/D4). It is the SINGLE validation
// gate shared by both the raw resolve path (decodeManifest) and the typed AgentsRC
// load path (ManifestSpec.UnmarshalJSON), so the fail-closed guarantee holds
// across the whole lifecycle — not only at resolve (FIX C).
func decodeManifestSpec(raw json.RawMessage) (ManifestSpec, error) {
	if err := rejectForbiddenManifestFields(raw); err != nil {
		return ManifestSpec{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var wire manifestSpecWire
	if err := dec.Decode(&wire); err != nil {
		return ManifestSpec{}, fmt.Errorf("malformed manifest: %w", err)
	}
	spec := ManifestSpec(wire)
	if err := validateManifestSpec(spec); err != nil {
		return ManifestSpec{}, err
	}
	return spec, nil
}

// UnmarshalJSON decodes a manifest spec through the SAME fail-closed gate the
// resolve path uses (FIX C): a forbidden or unpinned field on the typed AgentsRC
// `manifests` map is rejected at load, never silently dropped before Save
// re-emits the typed field (the schema-usage.md typed-field/ExtraFields rule).
func (s *ManifestSpec) UnmarshalJSON(data []byte) error {
	spec, err := decodeManifestSpec(data)
	if err != nil {
		return err
	}
	*s = spec
	return nil
}

// decodeManifest decodes one manifest payload through the shared fail-closed gate
// and stamps its loader-supplied identity (ref) and SOURCE-derived authority
// (scope, D4) — never read from the payload itself.
func decodeManifest(raw json.RawMessage, ref string, scope AuthorityScope) (ConfigManifest, error) {
	spec, err := decodeManifestSpec(raw)
	if err != nil {
		return ConfigManifest{}, err
	}
	return ConfigManifest{Ref: ref, Scope: scope, Spec: spec}, nil
}

// rejectForbiddenManifestFields fails closed on any self-declared-authority or
// inheritance field BEFORE the strict decode, so the error names the specific
// invariant violated (D3/D4/R10) rather than a generic "unknown field". Keys are
// checked in sorted order so the surfaced violation is deterministic.
func rejectForbiddenManifestFields(raw json.RawMessage) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("malformed manifest: %w", err)
	}
	for _, k := range sortedRawKeys(probe) {
		if reason, bad := manifestForbiddenFields[k]; bad {
			return fmt.Errorf("manifest field %q is invalid: %s", k, reason)
		}
	}
	return nil
}

// validateManifestSpec checks every referenced ref fail-closed. Sources must be
// fully PINNED (<source>:<path>@<version>): an unpinned/mutable source ref hashed
// as a stable digest input is false reproducibility, so it is rejected loudly
// (FIX D / F5). Bind and project_set refs must be well-formed absolute refs
// (<source>:<name>); a typo'd ref must fail loudly, never resolve to nothing
// silently (R10).
func validateManifestSpec(spec ManifestSpec) error {
	for _, ref := range spec.Sources {
		if !validPinnedSourceRef(ref) {
			return fmt.Errorf("manifest source ref %q is not pinned (want <source>:<path>@<version>); an unpinned/mutable source breaks reproducibility", ref)
		}
	}
	if err := validateManifestRefs("bind", spec.Binds); err != nil {
		return err
	}
	if spec.ProjectSet != "" && !validManifestRef(spec.ProjectSet) {
		return fmt.Errorf("manifest project_set ref %q is malformed (want <source>:<name>)", spec.ProjectSet)
	}
	return nil
}

// validateManifestRefs validates a list of refs under a label used in the error.
func validateManifestRefs(label string, refs []string) error {
	for _, ref := range refs {
		if !validManifestRef(ref) {
			return fmt.Errorf("manifest %s ref %q is malformed (want <source>:<name>)", label, ref)
		}
	}
	return nil
}

// validManifestRef reports whether ref is a non-empty absolute ref carrying a
// non-empty source segment and a non-empty remainder (<source>:<name>).
func validManifestRef(ref string) bool {
	idx := strings.Index(ref, ":")
	return idx > 0 && idx < len(ref)-1
}

// validPinnedSourceRef reports whether ref is a fully PINNED source ref
// <source>:<path>@<version>: a non-empty source segment, a non-empty path, and a
// non-empty @version. An unpinned ref (no @version) is rejected so the
// transitive-pin digest only ever hashes immutable inputs (FIX D / F5).
func validPinnedSourceRef(ref string) bool {
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return false
	}
	rest := ref[idx+1:]
	at := strings.LastIndex(rest, "@")
	return at > 0 && at < len(rest)-1
}

// manifestRef builds the absolute unit ref <source>:<name> for a manifest
// declared in a layer; the source segment carries the provenance the digest pins
// (F5). It falls back to the bare name when the layer has no distinct source id.
func manifestRef(source, name string) string {
	if source == "" {
		return name
	}
	return source + ":" + name
}

// manifestsFromLayer derives every kind:manifest unit declared in one layer's raw
// object, stamping each with the layer's SOURCE-derived authority scope and its
// absolute ref. A `manifests` value that is not an object, or any manifest that
// fails validation, is a fail-closed error (R10). Names are processed in sorted
// order for deterministic output.
func manifestsFromLayer(raw map[string]any, scope AuthorityScope, source string) ([]ConfigManifest, error) {
	rawManifests, ok := raw["manifests"]
	if !ok {
		return nil, nil
	}
	obj, ok := rawManifests.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("manifests must be an object of <name> -> manifest")
	}
	out := make([]ConfigManifest, 0, len(obj))
	for _, name := range sortedAnyKeys(obj) {
		// obj[name] came from decoded JSON, so re-encoding cannot fail (the same
		// impossible-marshal convention as WriteUnitsLock / toBundle).
		data, _ := json.Marshal(obj[name])
		m, err := decodeManifest(data, manifestRef(source, name), scope)
		if err != nil {
			return nil, fmt.Errorf("manifest %q: %w", name, err)
		}
		out = append(out, m)
	}
	return out, nil
}

// ManifestSetFromSnapshot derives every declared manifest unit from a resolved
// snapshot, carrying each at its EFFECTIVE authority scope (base scope upgraded by
// any §15 source-authority grant the stack confers). It reuses effectiveLayerScopes
// — the §15 grant write-guard — so a self-blessing authority claim FAILS the
// resolve here (the no-self-blessing invariant), and a public/ungranted manifest
// stays AuthPublic. It is the manifest sibling of ProfileSetFromSnapshot.
func ManifestSetFromSnapshot(snap *Snapshot) ([]ConfigManifest, error) {
	scopes, err := effectiveLayerScopes(snap.Layers)
	if err != nil {
		return nil, err
	}
	var out []ConfigManifest
	for _, layer := range snap.Layers {
		if layer.Raw == nil {
			continue
		}
		ms, err := manifestsFromLayer(layer.Raw, scopes[layer.ID], layer.ID)
		if err != nil {
			return nil, fmt.Errorf("layer %q: %w", layer.ID, err)
		}
		out = append(out, ms...)
	}
	return out, nil
}

// ResolvedManifest is the resolution output for one manifest (item 2): its
// referenced source-set, the bound layering policy (resolved through the L1
// engine), the optional project-set ref, and the transitive-pin digest (F5).
type ResolvedManifest struct {
	// Ref is the manifest's absolute identity.
	Ref string `json:"ref"`
	// Scope is the source-derived authority the manifest resolved at.
	Scope AuthorityScope `json:"scope"`
	// Sources is the sorted referenced source-set the manifest pulls (R3 step 1).
	Sources []string `json:"sources"`
	// Policy is the bound layering policy + resolved fragment, produced by the L1
	// ResolveProfile engine — composed, not forked (R3 step 2 / DC6).
	Policy ResolvedProfile `json:"policy"`
	// ProjectSet is the referenced project-set ref, or "" when the manifest has
	// none (item 3 — a manifest without a project-set still resolves).
	ProjectSet string `json:"project_set,omitempty"`
	// HasProjectSet distinguishes "no project-set referenced" from a future empty
	// ref, so consumers (L3 init --from) branch explicitly rather than on "".
	HasProjectSet bool `json:"has_project_set"`
	// Digest is the transitive-pin digest over the manifest ref, the referenced
	// ref-set, and the resolved policy digest (F5): "same digest ⇒ identical
	// setup" (R4).
	Digest string `json:"digest"`
}

// ResolveManifest resolves one manifest unit on the §15 + L1 substrate (item 2).
// It does NOT fork resolution: the bound layering policy is produced by the L1
// ResolveProfile engine over the profiles/policies the manifest binds, and the
// source-set + project-set ref are carried through unchanged. The transitive-pin
// digest (F5) covers the manifest ref, the referenced ref-set, AND the resolved
// policy digest — so it moves whenever a referenced ref OR a referenced unit's
// resolved version changes, giving "same manifest digest ⇒ identical setup" (R4).
//
// A fatal L1 authority violation (force-allow, self-blessing, overlapping lock)
// propagates as an error fail-closed, exactly as the L1 engine raises it — the
// manifest reuses that invariant rather than re-checking it.
func ResolveManifest(m ConfigManifest, set ProfileSet, ctx ProfileContext) (ResolvedManifest, error) {
	policy, err := ResolveProfile(bindProfileSet(set, m.Spec.Binds), ctx)
	if err != nil {
		return ResolvedManifest{}, err
	}
	sources := append([]string{}, m.Spec.Sources...)
	sort.Strings(sources)
	return ResolvedManifest{
		Ref:           m.Ref,
		Scope:         m.Scope,
		Sources:       sources,
		Policy:        policy,
		ProjectSet:    m.Spec.ProjectSet,
		HasProjectSet: m.Spec.ProjectSet != "",
		Digest:        manifestDigest(m, policy.Digest),
	}, nil
}

// bindProfileSet narrows a profile set to the fragments the manifest binds by ref
// (R2b). An empty bind list binds the whole in-scope set. Layering POLICIES are
// always kept regardless of the bind list: policy authority binds by SCOPE under
// the §15 law (a member cannot escape an org lock by binding only its own
// profiles), so filtering them would be an authority-escape hole (D4/R6).
func bindProfileSet(set ProfileSet, binds []string) ProfileSet {
	if len(binds) == 0 {
		return set
	}
	want := make(map[string]bool, len(binds))
	for _, ref := range binds {
		want[ref] = true
	}
	bound := ProfileSet{Policies: set.Policies}
	for _, p := range set.Profiles {
		if want[p.Ref] {
			bound.Profiles = append(bound.Profiles, p)
		}
	}
	return bound
}

// manifestDigest hashes the manifest's pinned inputs — its ref, its RESOLVED
// authority scope, its sorted referenced source + bind refs, its project-set ref,
// and the resolved policy digest — into the transitive-pin digest (F5). Including
// the resolved scope means an authority/grant change that alters the manifest's
// resolved authority moves the pin (FIX D): the scope is part of the resolved
// setup, so omitting it would let authority drift under a "pinned" digest. Sorting
// makes the ref-set order-independent; folding the policy digest in makes it move
// when a referenced unit's RESOLVED version changes, not only when a ref STRING
// changes.
func manifestDigest(m ConfigManifest, policyDigest string) string {
	sources := append([]string{}, m.Spec.Sources...)
	sort.Strings(sources)
	binds := append([]string{}, m.Spec.Binds...)
	sort.Strings(binds)
	payload := struct {
		Ref          string         `json:"ref"`
		Scope        AuthorityScope `json:"scope"`
		Sources      []string       `json:"sources"`
		Binds        []string       `json:"binds"`
		ProjectSet   string         `json:"project_set"`
		PolicyDigest string         `json:"policy_digest"`
	}{Ref: m.Ref, Scope: m.Scope, Sources: sources, Binds: binds, ProjectSet: m.Spec.ProjectSet, PolicyDigest: policyDigest}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

// ResolveManifestFromSnapshot derives the manifest + profile sets from a resolved
// snapshot, locates the manifest by absolute ref, and resolves it against the
// snapshot's scope chain — the single readback path that resolves a manifest on
// the live §15 + L1 substrate (composes both engines, forks neither). A ref that
// names no declared manifest is a loud not-found error, never a silent empty
// resolve.
func ResolveManifestFromSnapshot(snap *Snapshot, ref, role, appType, stage, harness string) (ResolvedManifest, error) {
	manifests, err := ManifestSetFromSnapshot(snap)
	if err != nil {
		return ResolvedManifest{}, err
	}
	m, ok := findManifest(manifests, ref)
	if !ok {
		return ResolvedManifest{}, fmt.Errorf("no manifest %q declared in the resolved config", ref)
	}
	set, err := ProfileSetFromSnapshot(snap)
	if err != nil {
		return ResolvedManifest{}, err
	}
	ctx := ProfileContext{
		Role:       role,
		AppType:    appType,
		Stage:      stage,
		Harness:    harness,
		ScopeChain: SnapshotScopeChain(snap),
	}
	return ResolveManifest(m, set, ctx)
}

// findManifest returns the manifest with the given absolute ref, if present.
func findManifest(manifests []ConfigManifest, ref string) (ConfigManifest, bool) {
	for _, m := range manifests {
		if m.Ref == ref {
			return m, true
		}
	}
	return ConfigManifest{}, false
}

// sortedRawKeys returns the keys of a raw-message map in deterministic order.
func sortedRawKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
