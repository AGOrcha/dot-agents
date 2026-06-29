package platform

import (
	"path/filepath"
	"strings"
	"testing"

	scaffoldhooks "github.com/AGOrcha/dot-agents/internal/scaffold/hooks"
)

// session_handoff_hooks_test.go exercises the p7 session-handoff hook bundles
// (session-handoff-snapshot / session-handoff-recover) end to end: the real
// embedded HOOK.yaml manifests are installed, loaded through the canonical
// bundle loader, and rendered through the per-platform hook config renderers.
// The two hooks reuse the existing pre_compact / session_start event model —
// no new canonical event was needed — so these tests lock the WIRING (event
// mapping, the SessionStart `compact` source matcher, and the relative-command
// resolution) rather than a new mapper entry.

const (
	sessionHandoffSnapshotBundle = "session-handoff-snapshot"
	sessionHandoffRecoverBundle  = "session-handoff-recover"
)

// installSessionHandoffSpecs materializes the embedded global bundles into a
// temp agents home and returns the loaded snapshot + recover specs.
func installSessionHandoffSpecs(t *testing.T) (snapshot, recover HookSpec) {
	t.Helper()
	tmp := t.TempDir()
	globalRoot := filepath.Join(tmp, "hooks", "global")
	if err := scaffoldhooks.CopyMissingGlobalBundles(globalRoot); err != nil {
		t.Fatalf("CopyMissingGlobalBundles: %v", err)
	}
	specs, err := ListHookSpecs(tmp, "global")
	if err != nil {
		t.Fatalf("ListHookSpecs: %v", err)
	}
	var gotSnap, gotRecover *HookSpec
	for i := range specs {
		switch specs[i].Name {
		case sessionHandoffSnapshotBundle:
			gotSnap = &specs[i]
		case sessionHandoffRecoverBundle:
			gotRecover = &specs[i]
		}
	}
	if gotSnap == nil {
		t.Fatalf("bundle %q not installed/loaded", sessionHandoffSnapshotBundle)
	}
	if gotRecover == nil {
		t.Fatalf("bundle %q not installed/loaded", sessionHandoffRecoverBundle)
	}
	return *gotSnap, *gotRecover
}

// TestSessionHandoffBundlesLoadAsCanonicalSpecs asserts the manifests parse
// into the expected canonical HookSpec shape: snapshot fires on pre_compact
// with no matcher; recover fires on session_start narrowed to the `compact`
// source; both carry a relative ./*.sh command resolving under the bundle dir.
func TestSessionHandoffBundlesLoadAsCanonicalSpecs(t *testing.T) {
	snapshot, recover := installSessionHandoffSpecs(t)

	if snapshot.SourceKind != HookSourceCanonicalBundle {
		t.Errorf("snapshot SourceKind = %q, want canonical bundle", snapshot.SourceKind)
	}
	if snapshot.When != "pre_compact" {
		t.Errorf("snapshot when = %q, want pre_compact", snapshot.When)
	}
	if snapshot.MatchExpression != "" || len(snapshot.MatchTools) != 0 {
		t.Errorf("snapshot must be unmatched (fires for every compaction); got expr=%q tools=%v", snapshot.MatchExpression, snapshot.MatchTools)
	}
	if snapshot.Command != "./snapshot.sh" {
		t.Errorf("snapshot command = %q, want ./snapshot.sh", snapshot.Command)
	}
	if got := ResolveHookCommand(snapshot); !strings.HasSuffix(got, filepath.Join(sessionHandoffSnapshotBundle, "snapshot.sh")) {
		t.Errorf("snapshot resolved command = %q, want suffix %s/snapshot.sh", got, sessionHandoffSnapshotBundle)
	}

	if recover.When != "session_start" {
		t.Errorf("recover when = %q, want session_start", recover.When)
	}
	if recover.MatchExpression != "compact" {
		t.Errorf("recover match expression = %q, want compact (SessionStart source)", recover.MatchExpression)
	}
	if recover.Command != "./recover.sh" {
		t.Errorf("recover command = %q, want ./recover.sh", recover.Command)
	}
	if got := ResolveHookCommand(recover); !strings.HasSuffix(got, filepath.Join(sessionHandoffRecoverBundle, "recover.sh")) {
		t.Errorf("recover resolved command = %q, want suffix %s/recover.sh", got, sessionHandoffRecoverBundle)
	}
}

// TestSessionHandoffBundlesEnabledPlatforms locks the enabled_on contract:
// snapshot ships on claude/codex/cursor (every platform that maps pre_compact);
// recover ships on claude/codex only — the two platforms whose SessionStart
// surface documents the `compact` start source. Other platforms stay
// unaffected.
func TestSessionHandoffBundlesEnabledPlatforms(t *testing.T) {
	snapshot, recover := installSessionHandoffSpecs(t)

	cases := []struct {
		spec     HookSpec
		platform string
		want     bool
	}{
		{snapshot, "claude", true},
		{snapshot, "codex", true},
		{snapshot, "cursor", true},
		{snapshot, "copilot", false},
		{recover, "claude", true},
		{recover, "codex", true},
		{recover, "cursor", false},
		{recover, "copilot", false},
	}
	for _, tc := range cases {
		if got := hookEnabledOnPlatform(tc.spec, tc.platform); got != tc.want {
			t.Errorf("hookEnabledOnPlatform(%s, %s) = %v, want %v", tc.spec.Name, tc.platform, got, tc.want)
		}
	}
}

// TestSessionHandoffBundlesRenderForClaude asserts the Claude render path: the
// snapshot lands under PreCompact (matcher "*"), recover under SessionStart
// narrowed to the `compact` source, each pointing at the resolved script path.
func TestSessionHandoffBundlesRenderForClaude(t *testing.T) {
	snapshot, recover := installSessionHandoffSpecs(t)

	assertClaudeSettingsRenders(t, []HookSpec{snapshot}, map[string]string{
		"hooks.PreCompact.0.matcher":         "*",
		"hooks.PreCompact.0.hooks.0.type":    "command",
		"hooks.PreCompact.0.hooks.0.command": ResolveHookCommand(snapshot),
	})
	assertClaudeSettingsRenders(t, []HookSpec{recover}, map[string]string{
		"hooks.SessionStart.0.matcher":         "compact",
		"hooks.SessionStart.0.hooks.0.command": ResolveHookCommand(recover),
	})
}

// TestSessionHandoffBundlesRenderForCodex asserts the Codex render path. Codex
// whitelists matcher narrowing on both PreCompact and SessionStart, so the
// snapshot keeps the canonical "*" fallback and recover keeps the `compact`
// source narrowing.
func TestSessionHandoffBundlesRenderForCodex(t *testing.T) {
	snapshot, recover := installSessionHandoffSpecs(t)

	assertCodexConfigRenders(t, []HookSpec{snapshot}, map[string]string{
		"hooks.PreCompact.0.matcher":         "*",
		"hooks.PreCompact.0.hooks.0.command": ResolveHookCommand(snapshot),
	})
	assertCodexConfigRenders(t, []HookSpec{recover}, map[string]string{
		"hooks.SessionStart.0.matcher":         "compact",
		"hooks.SessionStart.0.hooks.0.command": ResolveHookCommand(recover),
	})
}
