package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// verify_staleness_test.go covers Fix #3 of the §7A units-lock wiring
// (section-7a-units-lock-wiring): the config-staleness check on the primary
// verify contract. A flat/local-only project that now carries a lockfile reports
// its tracked inputs_digest (pass); a project whose local scope changed since the
// last resolve reports drift (warn); a never-resolved project reports the missing
// lock (warn). The check is content-hash driven — no clock.

// TestVerifyStaleness_FreshPasses: after a real resolve the lock's inputs_digest
// matches the current scopes, so the staleness check passes and surfaces the
// tracked digest — the "local-only project shows its inputs_digest, not nothing
// to verify" property §7A wires in.
func TestVerifyStaleness_FreshPasses(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"repo_id":"github.com/acme/flat","skills":["s"]}`, "")

	// A real resolve writes the §7A units lock (inputs_digest + empty units).
	if _, err := cfg.NewLayeredResolver().Resolve(project); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	checks := verifyStaleness(project)
	if len(checks) != 1 {
		t.Fatalf("expected 1 staleness check, got %+v", checks)
	}
	c := checks[0]
	if c.Name != "config-staleness" || c.Status != verifyPass {
		t.Fatalf("expected passing config-staleness check, got %+v", c)
	}
	// The tracked inputs_digest is surfaced (abbreviated sha256:…).
	if c.Detail == "" {
		t.Errorf("fresh check should surface the tracked inputs_digest, detail empty")
	}
}

// TestVerifyStaleness_DriftWarns: editing a local scope (the repo-local manifest)
// after a resolve changes the recomputed inputs_digest, so the staleness check
// warns about local-scope drift. The detail names both the recorded and current
// digests so the operator knows the lock is behind.
func TestVerifyStaleness_DriftWarns(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"repo_id":"github.com/acme/flat","skills":["s"]}`, "")

	if _, err := cfg.NewLayeredResolver().Resolve(project); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Mutate the repo-local scope AFTER the resolve so inputs_digest drifts.
	manifest := filepath.Join(project, cfg.AgentsRCFile)
	if err := os.WriteFile(manifest, []byte(`{"version":2,"repo_id":"github.com/acme/flat","skills":["s","added"]}`), 0o644); err != nil {
		t.Fatalf("mutate manifest: %v", err)
	}

	checks := verifyStaleness(project)
	if len(checks) != 1 {
		t.Fatalf("expected 1 staleness check, got %+v", checks)
	}
	c := checks[0]
	if c.Status != verifyWarn {
		t.Fatalf("drift must warn, got %+v", c)
	}
	if !strings.Contains(c.Detail, "local config scopes changed") || !strings.Contains(c.Detail, "da config sync") {
		t.Errorf("drift detail should explain the change and the fix, got %q", c.Detail)
	}
}

// TestVerifyStaleness_NoLockWarns: a project that was never resolved has no
// inputs_digest recorded, so the check warns to run sync to create the lock
// rather than silently passing.
func TestVerifyStaleness_NoLockWarns(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"repo_id":"github.com/acme/flat"}`, "")
	// No resolve: no .agentsrc.lock exists.

	checks := verifyStaleness(project)
	if len(checks) != 1 {
		t.Fatalf("expected 1 staleness check, got %+v", checks)
	}
	c := checks[0]
	if c.Status != verifyWarn {
		t.Fatalf("missing lock must warn, got %+v", c)
	}
	if !strings.Contains(c.Detail, "no inputs_digest recorded") {
		t.Errorf("missing-lock detail should say no inputs_digest recorded, got %q", c.Detail)
	}
}

// TestVerifyStaleness_DeclaredSetOnlyWarnsWithIdenticalDigests reproduces the
// originally-reported symptom end to end through verifyStaleness
// (config-verify-staleness-digest): a project using config-transitive-layering
// warns immediately after a clean resolve, with the recorded and recomputed
// inputs_digest byte-IDENTICAL, because a transitively-pulled layer used to be
// counted against the manifest's own declared extends set. The fix must (a)
// still warn — the lock genuinely doesn't reflect a directly-declared layer
// that's missing — and (b) name the declared-set reason instead of fabricating
// a digest-diff sentence for two digests that are exactly equal.
func TestVerifyStaleness_DeclaredSetOnlyWarnsWithIdenticalDigests(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"repo_id":"github.com/acme/flat","extends":["acme:org/base.json","acme:org/svc.json"]}`, "")

	digest, err := cfg.ComputeInputsDigest(project, "")
	if err != nil {
		t.Fatalf("ComputeInputsDigest: %v", err)
	}
	// Simulate a lock written by a resolve that also pulled in a transitive
	// layer, but is MISSING one of the two directly-declared layers (e.g. an
	// interrupted sync) — a genuine declared-set drift the manifest bytes don't
	// reflect, so inputs_digest stays exactly what ComputeInputsDigest reports.
	err = cfg.WriteUnitsLock(project, cfg.UnitsLock{
		InputsDigest: digest,
		Units: map[string]cfg.LockedUnit{
			"acme:org/base.json@a1":  {Kind: cfg.UnitKindLayer, Digest: "sha256:d1"},
			"other:org/base.json@t1": {Kind: cfg.UnitKindLayer, Digest: "sha256:d2", Transitive: true},
		},
	})
	if err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}

	checks := verifyStaleness(project)
	if len(checks) != 1 {
		t.Fatalf("expected 1 staleness check, got %+v", checks)
	}
	c := checks[0]
	if c.Status != verifyWarn {
		t.Fatalf("declared-set drift must warn, got %+v", c)
	}
	if strings.Contains(c.Detail, "lock sha256") || strings.Contains(c.Detail, ", now sha256") {
		t.Errorf("detail must not fabricate a digest-diff sentence when the digests are identical, got %q", c.Detail)
	}
	if !strings.Contains(c.Detail, "declared") {
		t.Errorf("detail should name the declared-set reason, got %q", c.Detail)
	}
}

// TestStaleWarnDetail covers every StalenessReason combination staleWarnDetail
// renders, including the empty-reasons defensive fallback that is unreachable
// through verifyStaleness's own call path (Fresh is checked first) but must
// still degrade sanely if a future StalenessReason is ever left unhandled.
func TestStaleWarnDetail(t *testing.T) {
	cases := []struct {
		name     string
		reasons  []cfg.StalenessReason
		recorded string
		expected string
		want     []string // substrings that must appear
		mustNot  []string // substrings that must NOT appear
	}{
		{
			name:     "inputs digest mismatch names both digests",
			reasons:  []cfg.StalenessReason{cfg.ReasonInputsDigest},
			recorded: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			expected: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			want:     []string{"local config scopes changed", "sha256:aaaaaaaaaaaaaaaa", "sha256:bbbbbbbbbbbbbbbb", "da config sync"},
		},
		{
			name:     "declared set changed does not mention digests",
			reasons:  []cfg.StalenessReason{cfg.ReasonDeclaredSet},
			recorded: "sha256:same",
			expected: "sha256:same",
			want:     []string{"declared", "da config sync"},
			mustNot:  []string{"sha256:same"},
		},
		{
			name:     "both reasons join",
			reasons:  []cfg.StalenessReason{cfg.ReasonInputsDigest, cfg.ReasonDeclaredSet},
			recorded: "sha256:x",
			expected: "sha256:y",
			want:     []string{"local config scopes changed", "declared", "; "},
		},
		{
			name:    "empty reasons degrades to a generic message",
			reasons: nil,
			want:    []string{"local config changed since last resolve", "da config sync"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := staleWarnDetail(tc.reasons, tc.recorded, tc.expected)
			for _, s := range tc.want {
				if !strings.Contains(got, s) {
					t.Errorf("expected detail to contain %q, got %q", s, got)
				}
			}
			for _, s := range tc.mustNot {
				if strings.Contains(got, s) {
					t.Errorf("expected detail NOT to contain %q, got %q", s, got)
				}
			}
		})
	}
}

// TestAbbrevDigest covers the sha256-prefixed truncation, the short-value
// pass-through, and the non-prefixed fallback to abbrevSHA.
func TestAbbrevDigest(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "long sha256 digest truncates to 16 hex chars past the prefix",
			in:   "sha256:0123456789abcdef0123456789abcdef",
			want: "sha256:0123456789abcdef",
		},
		{
			name: "short sha256 digest passes through unchanged",
			in:   "sha256:abc",
			want: "sha256:abc",
		},
		{
			name: "non-prefixed value falls back to abbrevSHA",
			in:   "0123456789abcdef0123456789abcdef",
			want: abbrevSHA("0123456789abcdef0123456789abcdef"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := abbrevDigest(tc.in); got != tc.want {
				t.Errorf("abbrevDigest(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestVerifyStaleness_DoesNotFlipOK: drift is advisory — the staleness warning
// must not flip the report's OK boolean (CI on an editable local overlay should
// not hard-fail). The full report stays OK with the warn present.
func TestVerifyStaleness_DoesNotFlipOK(t *testing.T) {
	project := withRepoLayer(t, `{"version":2,"repo_id":"github.com/acme/flat"}`, "")
	// Never resolved → staleness warns, but the report should remain OK.

	report := buildVerifyReport(mustVerifyOptions(project, false, okProbe))
	if !report.OK {
		t.Fatalf("a staleness warning must not flip report OK, got OK=false: %+v", report.Checks)
	}
	c, ok := findCheck(report.Checks, "config-staleness")
	if !ok {
		t.Fatalf("config-staleness check missing from report: %+v", report.Checks)
	}
	if c.Status != verifyWarn {
		t.Errorf("config-staleness should warn for a never-resolved project, got %+v", c)
	}
}
