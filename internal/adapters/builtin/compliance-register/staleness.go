package complianceregister

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// staleField is the note-field key carrying the structured stale payload (§7.3).
const staleField = "stale"

// staleReason values mirror the scoped-KG §3.5 stale payload reasons (§7.3).
const (
	reasonEnvironmental = "environmental"
	reasonDerivation    = "derivation"
	reasonSource        = "source"
	reasonRevocation    = "revocation"
	reasonContradiction = "contradiction"
)

// ImpactRadius satisfies the registry.Adapter interface. It is the contract
// entry point the review pipeline calls; here it is the no-corpus form (the
// adapter has no ambient corpus), returning the changed ids unchanged. The
// corpus-driven blast-radius computation is RunImpact, which the named-query
// path and the hard test exercise against a loaded view.
func (a *Adapter) ImpactRadius(req registry.ImpactRequest) (registry.ImpactResult, error) {
	ids := make([]string, len(req.ChangedIDs))
	copy(ids, req.ChangedIDs)
	return registry.ImpactResult{IDs: ids}, nil
}

// ApplyDerivation implements the §7.3 environmental→derivation propagation with
// the O5-pinned semantics: when a policy carries an environmental stale tag,
// every control whose `derives_from` ref points at that policy is tagged
// `derivation`-stale. The derives_from ref is declared `derivation: true`
// (§13.2), and per O5 the code-graph derivation depth is one hop (note→note);
// here that is the single control→policy ref hop. It returns a new view with
// the derivation-stale controls merged in.
func (a *Adapter) ApplyDerivation(view sdk.NamespaceView) sdk.NamespaceView {
	stalePolicies := environmentallyStalePolicies(view)
	if len(stalePolicies) == 0 {
		return view
	}
	var tagged []sdk.Note
	for _, n := range view.Notes {
		if n.Type != "control" {
			continue
		}
		if pol, ok := derivesFromStalePolicy(n, stalePolicies); ok {
			tagged = append(tagged, tagDerivation(n, pol))
		}
	}
	return mergeTagged(view, tagged)
}

// environmentallyStalePolicies returns the set of policy ids carrying an
// environmental stale tag.
func environmentallyStalePolicies(view sdk.NamespaceView) map[string]bool {
	out := map[string]bool{}
	for _, n := range view.Notes {
		if n.Type == "policy" && staleReasonOf(n) == reasonEnvironmental {
			out[n.ID] = true
		}
	}
	return out
}

// derivesFromStalePolicy reports whether a control's derives_from ref points at
// an environmentally-stale policy, returning the policy id.
func derivesFromStalePolicy(control sdk.Note, stale map[string]bool) (string, bool) {
	if control.Fields == nil {
		return "", false
	}
	pol, ok := control.Fields["derives_from"].(string)
	if !ok || !stale[pol] {
		return "", false
	}
	return pol, true
}

// staleReasonOf reads a note's stale.reason, or "" when fresh.
func staleReasonOf(n sdk.Note) string {
	if n.Fields == nil {
		return ""
	}
	raw, ok := n.Fields[staleField].(map[string]any)
	if !ok {
		return ""
	}
	r, _ := raw["reason"].(string)
	return r
}

// tagDerivation returns a copy of control with a derivation stale payload
// referencing the upstream policy as provenance (§7.3 `because`).
func tagDerivation(control sdk.Note, policyID string) sdk.Note {
	fields := make(map[string]any, len(control.Fields)+1)
	for k, v := range control.Fields {
		fields[k] = v
	}
	fields[staleField] = map[string]any{
		"reason":  reasonDerivation,
		"because": []any{policyID},
	}
	return sdk.Note{ID: control.ID, Type: control.Type, Fields: fields}
}

// SourceContentHash computes the O5-pinned source-mutation signal: a note's
// content hash over its sorted field set. Per O5, source mutation fires on a
// content-hash CHANGE, NOT on any upsert — callers compare this hash before and
// after a write to decide whether the source_mutation driver fires.
func SourceContentHash(n sdk.Note) string {
	keys := make([]string, 0, len(n.Fields))
	for k := range n.Fields {
		if k == staleField {
			continue // stale tags are driver output, not source content
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	h.Write([]byte(n.Type + "\x00"))
	for _, k := range keys {
		fmt.Fprintf(h, "%s\x00%v\x00", k, n.Fields[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SourceMutationFires reports whether the source_mutation driver fires between
// two versions of a note. Per O5 it is keyed on content-hash change: an
// identical re-upsert does NOT fire.
func SourceMutationFires(before, after sdk.Note) bool {
	return SourceContentHash(before) != SourceContentHash(after)
}

// RevocationFires reports whether an explicit_revocation driver fires: a note
// carrying `revokes: <id>` revokes the named note (O5: revocation = new note
// `revokes: <id>`).
func RevocationFires(n sdk.Note) (string, bool) {
	if n.Fields == nil {
		return "", false
	}
	id, ok := n.Fields["revokes"].(string)
	return id, ok && id != ""
}
