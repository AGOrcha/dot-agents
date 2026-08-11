package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// writeProvenanceBundle writes a canonical hook bundle plus its script and
// returns the absolute bundle directory.
func writeProvenanceBundle(t *testing.T, agentsHome, scope, name, manifest string) string {
	t.Helper()
	dir := filepath.Join(agentsHome, "hooks", scope, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gate.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestHookProvenance_OwnsRenderedCommands is the core contract: whatever a
// renderer emits for a bundle must be recognized as belonging to that bundle,
// under whatever name its author chose. The bundle here is deliberately named
// nothing like the name the importer would derive from its rendered entries
// ("pre-compact-gate" / "stop-gate"), because name coincidence is exactly the
// dedupe that used to be relied on.
func TestHookProvenance_OwnsRenderedCommands(t *testing.T) {
	agentsHome := t.TempDir()
	dir := writeProvenanceBundle(t, agentsHome, "global", "isp-gate", `name: isp-gate
when_events:
  - pre_compact
  - stop
run:
  command: ./gate.sh
`)
	script := filepath.Join(dir, "gate.sh")

	specs, err := listCanonicalHookSpecs(agentsHome, "global")
	if err != nil {
		t.Fatalf("listCanonicalHookSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected one bundle, got %d", len(specs))
	}
	rendered := ResolveHookCommand(specs[0])
	if rendered != script {
		t.Fatalf("render resolves to %q, want the bundle script %q", rendered, script)
	}

	p := NewHookProvenance(agentsHome)
	owner, ok := p.Owner(rendered)
	if !ok || owner != "global/isp-gate" {
		t.Fatalf("rendered command must be owned by global/isp-gate, got owner=%q ok=%v", owner, ok)
	}
}

func TestHookProvenance_OwnerCases(t *testing.T) {
	agentsHome := t.TempDir()
	bundleDir := writeProvenanceBundle(t, agentsHome, "global", "isp-gate", `name: isp-gate
when: pre_compact
run:
  command: ./gate.sh
`)
	// A bundle whose command is not a path into its own directory at all:
	// only exact-command equality can recognize its render output.
	writeProvenanceBundle(t, agentsHome, "proj", "checkpoint", `name: checkpoint
when: post_tool_use
run:
  command: da workflow checkpoint --quiet
`)
	// A bundle with no command renders nothing and must claim nothing.
	writeProvenanceBundle(t, agentsHome, "global", "commandless", `name: commandless
when: stop
`)
	// A legacy single-file hook next to the bundle dirs must not break the scan.
	if err := os.WriteFile(filepath.Join(agentsHome, "hooks", "global", "legacy.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-directory entry directly under hooks/ is skipped as a scope.
	if err := os.WriteFile(filepath.Join(agentsHome, "hooks", "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(bundleDir, "gate.sh")
	p := NewHookProvenance(agentsHome)

	cases := []struct {
		name      string
		command   string
		wantOwner string
		wantOK    bool
	}{
		{
			name:      "absolute script path inside a bundle",
			command:   script,
			wantOwner: "global/isp-gate",
			wantOK:    true,
		},
		{
			name:      "interpreter prefix in front of a bundle script",
			command:   "bash " + script + " --verbose",
			wantOwner: "global/isp-gate",
			wantOK:    true,
		},
		{
			name:      "non-path command equal to a bundle's run.command",
			command:   "da workflow checkpoint --quiet",
			wantOwner: "proj/checkpoint",
			wantOK:    true,
		},
		{
			name:    "hand-authored command referencing nothing managed",
			command: "/usr/local/bin/my-own-hook.sh",
		},
		{
			name:    "relative command is never a render output",
			command: "./gate.sh",
		},
		{
			name:    "absolute path outside the hooks tree",
			command: filepath.Join(agentsHome, "skills", "x", "gate.sh"),
		},
		{
			name:    "path at the hooks root with no bundle segment",
			command: filepath.Join(agentsHome, "hooks", "global", "legacy.json"),
		},
		{
			name:    "empty command",
			command: "   ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner, ok := p.Owner(tc.command)
			if ok != tc.wantOK || owner != tc.wantOwner {
				t.Fatalf("Owner(%q) = (%q, %v), want (%q, %v)", tc.command, owner, ok, tc.wantOwner, tc.wantOK)
			}
		})
	}
}

// TestHookProvenance_DegradesToOwnsNothing pins the three ways the index can
// come up empty. Provenance never fails its caller: an import must still run
// (and, at worst, capture an entry it could have skipped) rather than abort.
func TestHookProvenance_DegradesToOwnsNothing(t *testing.T) {
	withBadScope := t.TempDir()
	writeProvenanceBundle(t, withBadScope, "global", "broken", "name: [unterminated\n")

	cases := []struct {
		name       string
		agentsHome string
	}{
		{name: "empty agents home", agentsHome: "  "},
		{name: "hooks bucket absent", agentsHome: t.TempDir()},
		{name: "scope with an unparseable manifest", agentsHome: withBadScope},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewHookProvenance(tc.agentsHome)
			if owner, ok := p.Owner("anything at all"); ok {
				t.Fatalf("expected no ownership claim, got %q", owner)
			}
			if owner, ok := p.Owner("/abs/path/gate.sh"); ok {
				t.Fatalf("expected no ownership claim for an absolute path, got %q", owner)
			}
		})
	}
}
