package platform

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// ImportArtifactReason classifies why ImportArtifactCandidates flagged a
// bundle. Ambiguous candidates carry no owner: callers must skip them rather
// than guess which bundle they duplicate.
type ImportArtifactReason string

const (
	// ImportArtifactReasonCommandOwned: the bundle's own run.command is an
	// absolute path that HookProvenance attributes to a DIFFERENT existing
	// bundle — exactly the shape #533 traced back to the render/import
	// feedback loop (an import run capturing da's own rendered output).
	ImportArtifactReasonCommandOwned ImportArtifactReason = "command_owned"
	// ImportArtifactReasonDuplicateManifest: the bundle's manifest fields
	// (everything but name/scope/location) and any local sidecar script are
	// byte-identical to another existing bundle's — a copy, not an
	// independent authoring.
	ImportArtifactReasonDuplicateManifest ImportArtifactReason = "duplicate_manifest"
	// ImportArtifactReasonAmbiguous: two detection signals disagreed on the
	// owner, or the duplicate-manifest signal matched more than one other
	// bundle. There is no single bundle this one can be safely attributed
	// to, so it must never be auto-pruned.
	ImportArtifactReasonAmbiguous ImportArtifactReason = "ambiguous"
)

// ImportArtifactCandidate is one canonical hook bundle ImportArtifactCandidates
// flagged as a probable import-artifact capture: a bundle that exists only
// because a prior import run re-captured another bundle's rendered output
// under a different derived name (see #533's HookProvenance doc comment).
type ImportArtifactCandidate struct {
	Scope      string
	Name       string
	BundleDir  string
	OwnerScope string // empty when Reason == ImportArtifactReasonAmbiguous
	OwnerName  string // empty when Reason == ImportArtifactReasonAmbiguous
	Reason     ImportArtifactReason
	Detail     string
}

// Owner formats "<scope>/<name>" for the bundle this candidate is a capture
// of. Empty for an ambiguous candidate.
func (c ImportArtifactCandidate) Owner() string {
	if c.OwnerScope == "" && c.OwnerName == "" {
		return ""
	}
	return c.OwnerScope + "/" + c.OwnerName
}

// ImportArtifactCandidates scans every canonical hook bundle under agentsHome
// (every scope: global and every managed project) and reports each one that
// looks like an import-artifact capture of another bundle's rendered output,
// per two signals built on top of #533's provenance mechanism:
//
//  1. command_owned — the bundle's own manifest `run.command` is an absolute
//     path that lives inside a DIFFERENT existing bundle's own directory,
//     per HookProvenance's directory-containment check (ownerOfPath, the
//     #533 authority's own verified sub-component, reused directly here —
//     see commandOwnedByOtherBundle's doc comment for why the exact-command-
//     string half of Owner is deliberately NOT reused: two real production
//     dry runs proved it produces circular false attributions). A relative
//     command (`./gate.sh`, a bare tool name) can only ever resolve inside
//     the bundle's OWN directory — see ResolveHookCommand — so this signal
//     only fires on the exact shape import capture leaves behind: a
//     rendered, already-absolute command recorded verbatim in a manifest
//     that isn't the bundle it came from.
//  2. duplicate_manifest — the bundle's manifest content (every field but
//     Name/Scope/SourcePath) and any local sidecar script it references are
//     byte-identical to another existing bundle's. This catches a capture
//     whose own command still resolves inside its own directory (so signal
//     1 cannot see it) because the whole bundle is a copy, script included.
//
// A bundle whose two signals disagree on the owner, or whose duplicate-
// manifest match is not unique, is reported with Reason ==
// ImportArtifactReasonAmbiguous and no owner — callers must never delete an
// ambiguous candidate.
func ImportArtifactCandidates(agentsHome string) ([]ImportArtifactCandidate, error) {
	specs, err := allCanonicalHookSpecs(agentsHome)
	if err != nil {
		return nil, err
	}

	var out []ImportArtifactCandidate
	for _, spec := range specs {
		if c, ok := classifyImportArtifact(specs, spec); ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// classifyImportArtifact runs both detection signals for one bundle and
// reconciles them into a single verdict.
//
// command_owned is checked FIRST and, when it fires, wins outright — even if
// the duplicate-manifest signal is itself ambiguous. This matters on real
// production homes: a multi-event bundle's render produces several sibling
// artifact captures (pre-compact-gate, pre-compact-gate-2, -3, …) that are
// ALL byte-identical to EACH OTHER, not just to the real bundle. Checking
// dupAmbiguous first would report every one of those siblings as
// "ambiguous, matches more than one bundle" and skip all of them, even
// though command_owned already gives each one a clean, individually
// unambiguous directory-containment answer. Duplicate-content ambiguity
// only overrides an ALSO-confident (non-ambiguous) command_owned verdict
// when the two signals actively disagree on the owner — see the second
// case below.
func classifyImportArtifact(specs []HookSpec, spec HookSpec) (ImportArtifactCandidate, bool) {
	cmdOwner, cmdDetail, cmdOK := commandOwnedByOtherBundle(specs, spec)
	dupOwner, dupDetail, dupOK, dupAmbiguous := duplicateCaptureOfOtherBundle(specs, spec)

	switch {
	case cmdOK && dupOK && !dupAmbiguous && cmdOwner != dupOwner:
		return ambiguousCandidate(spec, fmt.Sprintf(
			"command ownership points to %s but manifest content matches %s",
			cmdOwner, dupOwner,
		)), true
	case cmdOK:
		return ownedCandidate(spec, cmdOwner, ImportArtifactReasonCommandOwned, cmdDetail), true
	case dupAmbiguous:
		return ambiguousCandidate(spec, "manifest and script content match more than one existing bundle"), true
	case dupOK:
		return ownedCandidate(spec, dupOwner, ImportArtifactReasonDuplicateManifest, dupDetail), true
	default:
		return ImportArtifactCandidate{}, false
	}
}

func ownedCandidate(spec HookSpec, owner string, reason ImportArtifactReason, detail string) ImportArtifactCandidate {
	ownerScope, ownerName, _ := strings.Cut(owner, "/")
	return ImportArtifactCandidate{
		Scope:      spec.Scope,
		Name:       spec.Name,
		BundleDir:  filepath.Dir(spec.SourcePath),
		OwnerScope: ownerScope,
		OwnerName:  ownerName,
		Reason:     reason,
		Detail:     detail,
	}
}

func ambiguousCandidate(spec HookSpec, detail string) ImportArtifactCandidate {
	return ImportArtifactCandidate{
		Scope:     spec.Scope,
		Name:      spec.Name,
		BundleDir: filepath.Dir(spec.SourcePath),
		Reason:    ImportArtifactReasonAmbiguous,
		Detail:    detail,
	}
}

// allCanonicalHookSpecs lists every canonical bundle across every scope
// directory under agentsHome/hooks/, mirroring NewHookProvenance's own scope
// enumeration so both agree on what "every bundle" means.
func allCanonicalHookSpecs(agentsHome string) ([]HookSpec, error) {
	agentsHome = strings.TrimSpace(agentsHome)
	if agentsHome == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Join(agentsHome, "hooks"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []HookSpec
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		specs, err := listCanonicalHookSpecs(agentsHome, entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, specs...)
	}
	return out, nil
}

// commandOwnedByOtherBundle implements ImportArtifactReasonCommandOwned. See
// the ImportArtifactCandidates doc comment for why the absolute-path guard
// is load-bearing: it is what keeps two independently authored bundles that
// happen to invoke the same bare tool from flagging each other.
//
// Attribution here uses ONLY directory containment (HookProvenance's
// ownerOfPath, reused directly — same package, same method, no reimplemented
// matching logic), deliberately never the exact-command-string equality
// Owner also considers. An absolute path is under exactly one bundle's own
// directory, so directory containment is unconditionally trustworthy no
// matter how many OTHER bundles' commands happen to repeat the same string.
// Exact-string equality has no such guarantee: two real production dry runs
// surfaced why trusting it produced a data-loss risk —
//
//  1. When the target script's own bundle directory has no HOOK.yaml (an
//     orphaned script, its manifest deleted or never existed), two sibling
//     captures that both merely hold that same orphaned absolute string have
//     no real owner between them. Excluding self and falling through to an
//     exact-string map lookup made each one, in turn, find the OTHER as its
//     sole remaining match — a circular, factually wrong mutual attribution
//     (A "owned by" B and B "owned by" A, BOTH reported removable).
//  2. That circularity is not fixable by only requiring a UNIQUE match
//     either: with exactly two bundles sharing a string, each one's own
//     self-excluded view always sees exactly one other, so a per-candidate
//     uniqueness check still trips the same trap symmetrically.
//
// duplicateCaptureOfOtherBundle already owns the "these bundles are
// interchangeable copies" question, with a principled global match count and
// mtime tiebreaker — the identical-content case this signal would otherwise
// try to shortcut. commandOwnedByOtherBundle sticks to the one signal that
// cannot produce a false positive.
func commandOwnedByOtherBundle(specs []HookSpec, spec HookSpec) (owner, detail string, ok bool) {
	raw := strings.TrimSpace(spec.Command)
	if raw == "" {
		return "", "", false
	}
	rawFields := strings.Fields(raw)
	if !filepath.IsAbs(rawFields[0]) {
		return "", "", false
	}
	// ResolveHookCommand never rewrites an already-absolute first token (see
	// its own doc comment), so command is guaranteed non-empty here given
	// raw already passed the blank check above.
	command := strings.TrimSpace(ResolveHookCommand(spec))
	prov := dirProvenanceExcluding(specs, spec.Scope, spec.Name)
	for _, field := range strings.Fields(command) {
		if owner, ok := prov.ownerOfPath(field); ok {
			return owner, fmt.Sprintf("command %q resolves into bundle %s", command, owner), true
		}
	}
	return "", "", false
}

// dirProvenanceExcluding builds a HookProvenance directory index over specs,
// omitting the (scope, name) bundle under test. Omitting self is required:
// without it, a bundle's own directory always "contains" its own resolved
// command trivially. The commands map is left empty deliberately —
// commandOwnedByOtherBundle never consults it (see that function's doc
// comment) — so this mirrors only the half of HookProvenance.indexScope's
// indexing this signal actually uses.
func dirProvenanceExcluding(specs []HookSpec, scope, name string) HookProvenance {
	p := HookProvenance{commands: map[string]string{}, dirs: map[string]string{}}
	for _, s := range specs {
		if s.Scope == scope && s.Name == name {
			continue
		}
		p.dirs[filepath.Clean(filepath.Dir(s.SourcePath))] = s.Scope + "/" + s.Name
	}
	return p
}

// duplicateCaptureOfOtherBundle implements ImportArtifactReasonDuplicateManifest.
// ok=false, ambiguous=false means no other bundle matches; ambiguous=true
// means either more than one other bundle matches, or exactly one does but
// direction cannot be established, and no single owner can be reported.
//
// Byte-identical content alone is inherently symmetric — from content alone
// there is no way to tell which of two identical bundles is the original and
// which is the capture. The tiebreaker is HOOK.yaml modification time: a
// capture is written by an import run AFTER the bundle it copied, so the
// STRICTLY NEWER file of the pair is reported as the candidate and the
// older one as its owner. When the pair's mtimes are equal (indistinguishable)
// the match is reported ambiguous rather than guessed.
func duplicateCaptureOfOtherBundle(specs []HookSpec, spec HookSpec) (owner, detail string, ok, ambiguous bool) {
	var matches []HookSpec
	for _, other := range specs {
		if other.Scope == spec.Scope && other.Name == spec.Name {
			continue
		}
		if hookSpecContentEqual(spec, other) && hookBundleScriptBytesEqual(spec, other) {
			matches = append(matches, other)
		}
	}
	if len(matches) > 1 {
		return "", "", false, true
	}
	if len(matches) == 0 {
		return "", "", false, false
	}
	m := matches[0]
	newer, determined := hookManifestIsNewer(spec, m)
	if !determined {
		return "", "", false, true
	}
	if !newer {
		// spec is the older (presumed original) half of the pair; the newer
		// half is reported as the candidate when IT is visited instead.
		return "", "", false, false
	}
	return m.Scope + "/" + m.Name, fmt.Sprintf(
		"manifest and script content are byte-identical to the older bundle %s/%s", m.Scope, m.Name,
	), true, false
}

// hookManifestIsNewer reports whether spec's HOOK.yaml was modified strictly
// after other's. determined=false when either mtime is unreadable or the two
// are equal (same-instant writes, e.g. an aggressive test fixture), in which
// case the caller must not guess a direction.
func hookManifestIsNewer(spec, other HookSpec) (newer, determined bool) {
	specInfo, err := os.Stat(spec.SourcePath)
	if err != nil {
		return false, false
	}
	otherInfo, err := os.Stat(other.SourcePath)
	if err != nil {
		return false, false
	}
	specTime, otherTime := specInfo.ModTime(), otherInfo.ModTime()
	if specTime.Equal(otherTime) {
		return false, false
	}
	return specTime.After(otherTime), true
}

// hookSpecContentEqual reports whether a and b carry identical hook content
// — every field except the identity/location ones (Name, Scope, SourcePath,
// SourceBucket, SourceKind) that necessarily differ between two distinct
// bundle directories.
func hookSpecContentEqual(a, b HookSpec) bool {
	type contentView struct {
		When              string
		WhenEvents        []string
		MatchTools        []string
		MatchExpression   string
		Command           string
		TimeoutMS         int
		EnabledOn         []string
		RequiredOn        []string
		Description       string
		PlatformOverrides map[string]HookPlatformOverride
	}
	av := contentView{a.When, a.WhenEvents, a.MatchTools, a.MatchExpression, a.Command, a.TimeoutMS, a.EnabledOn, a.RequiredOn, a.Description, a.PlatformOverrides}
	bv := contentView{b.When, b.WhenEvents, b.MatchTools, b.MatchExpression, b.Command, b.TimeoutMS, b.EnabledOn, b.RequiredOn, b.Description, b.PlatformOverrides}
	return reflect.DeepEqual(av, bv)
}

// hookBundleScriptBytesEqual reports whether a and b's local sidecar scripts
// (the file a bundle-relative command token resolves to, if any) are
// byte-identical. Two bundles that reference no local script at all compare
// equal on this axis (manifest equality alone stands); a mismatch between
// "has a local script" and "doesn't" is never equal.
func hookBundleScriptBytesEqual(a, b HookSpec) bool {
	aFile, aOK := bundleRelativeScriptFile(a)
	bFile, bOK := bundleRelativeScriptFile(b)
	if aOK != bOK {
		return false
	}
	if !aOK {
		return true
	}
	aBytes, err := os.ReadFile(aFile)
	if err != nil {
		return false
	}
	bBytes, err := os.ReadFile(bFile)
	if err != nil {
		return false
	}
	return bytes.Equal(aBytes, bBytes)
}

// bundleRelativeScriptFile returns the absolute path of the local sidecar
// script spec's command references, if any. Mirrors ResolveHookCommand's own
// "is this a bundle-relative file" test (explicit ./ or ../ prefix, or a bare
// token that names an existing regular file beside the manifest).
func bundleRelativeScriptFile(spec HookSpec) (string, bool) {
	command := strings.TrimSpace(spec.Command)
	if command == "" {
		return "", false
	}
	first := strings.Fields(command)[0]
	explicitRelative := strings.HasPrefix(first, "./") || strings.HasPrefix(first, "../")
	if !explicitRelative && !bundleRelativeHookCommandFile(spec, first) {
		return "", false
	}
	return filepath.Clean(filepath.Join(filepath.Dir(spec.SourcePath), first)), true
}
