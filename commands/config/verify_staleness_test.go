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
