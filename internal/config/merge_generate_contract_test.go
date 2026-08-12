package config

import (
	"reflect"
	"testing"
)

// TestMergeGenerateAgentsRC_DeclarationsWinUnlessForced pins the overwrite
// contract for the scan-detectable declarations. Default: the fresh scan fills
// an ABSENT field only, so a committed declaration survives regeneration.
// Force (`--force-generate`): a scan that detected something replaces it. In
// NEITHER mode may a scan that detected nothing delete a declaration.
func TestMergeGenerateAgentsRC_DeclarationsWinUnlessForced(t *testing.T) {
	tests := []struct {
		name string
		// existing/generated are the settings declaration on each side; nil
		// means the field is absent.
		existing  *bool
		generated *bool
		force     bool
		want      *bool
	}{
		{
			name:      "explicit false survives a positive scan by default",
			existing:  pbool(false),
			generated: pbool(true),
			want:      pbool(false),
		},
		{
			name:      "explicit false is replaced under force",
			existing:  pbool(false),
			generated: pbool(true),
			force:     true,
			want:      pbool(true),
		},
		{
			name:      "absent is filled from the scan by default",
			existing:  nil,
			generated: pbool(true),
			want:      pbool(true),
		},
		{
			name:      "an empty scan never deletes a declaration by default",
			existing:  pbool(true),
			generated: nil,
			want:      pbool(true),
		},
		{
			name:      "an empty scan never deletes a declaration under force either",
			existing:  pbool(true),
			generated: nil,
			force:     true,
			want:      pbool(true),
		},
		{
			name:      "absent on both sides stays absent",
			existing:  nil,
			generated: nil,
			want:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := mergeSettingsDeclaration(tc.existing, tc.generated, tc.force)
			assertOptionalBool(t, "Settings", out.Settings, tc.want)
		})
	}
}

// TestMergeGenerateAgentsRC_HooksAndMCPFollowTheSameRule covers the
// StringsOrBool half of the declaration rule, including the case that made the
// old contract dangerous: a repo that disabled hooks outright while the scan
// still finds hook bundles on disk.
func TestMergeGenerateAgentsRC_HooksAndMCPFollowTheSameRule(t *testing.T) {
	existing := &AgentsRC{
		Hooks: sob(StringsOrBool{}),
		MCP:   sob(StringsOrBool{Names: []string{"committed"}}),
	}
	generated := &AgentsRC{
		Hooks: sob(StringsOrBool{Names: []string{"PreToolUse"}}),
		MCP:   sob(StringsOrBool{All: true}),
	}

	kept := MergeGenerateAgentsRC(existing, generated)
	if len(sobNames(kept.Hooks)) != 0 || sobAll(kept.Hooks) {
		t.Errorf("default: committed hooks-off must survive, got names=%v all=%v", sobNames(kept.Hooks), sobAll(kept.Hooks))
	}
	if !reflect.DeepEqual(sobNames(kept.MCP), []string{"committed"}) {
		t.Errorf("default: committed mcp list must survive, got %v", sobNames(kept.MCP))
	}

	forced := MergeGenerateAgentsRC(existing, generated, MergeGenerateOptions{Force: true})
	if !reflect.DeepEqual(sobNames(forced.Hooks), []string{"PreToolUse"}) {
		t.Errorf("force: scan must replace hooks, got %v", sobNames(forced.Hooks))
	}
	if !sobAll(forced.MCP) {
		t.Error("force: scan must replace mcp")
	}
}

// TestMergeGenerateAgentsRC_AuthorOwnedFieldsSurviveRegeneration is the
// regression test for the structural half of the change. The merge used to
// base on the GENERATED manifest, so every author-owned field the generator
// does not produce was dropped on each `da install --generate` unless someone
// had added a bespoke preservation clause for it. Basing on existing makes
// preservation the default, which is what these fields — none of which the
// generator emits — are proving.
func TestMergeGenerateAgentsRC_AuthorOwnedFieldsSurviveRegeneration(t *testing.T) {
	existing := &AgentsRC{
		Version:              2,
		Project:              "fixture",
		Extends:              []LayerRef{{Ref: "acme:base.json"}},
		Packages:             []PackageRef{{Ref: "acme:tool@1.2.3"}},
		Features:             map[string]string{"beta": "on"},
		KG:                   &AgentsRCKG{Backend: "postgres"},
		WorkTracking:         &AgentsRCWorkTracking{ReadFrom: "master"},
		GitignoreProjections: pbool(false),
	}
	generated := &AgentsRC{
		Version:      1,
		Project:      "fixture",
		Skills:       []string{"scanned"},
		WorkTracking: &AgentsRCWorkTracking{Backend: WorkTrackingBackendGitRef},
	}

	out := MergeGenerateAgentsRC(existing, generated)

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"Extends", out.Extends, existing.Extends},
		{"Packages", out.Packages, existing.Packages},
		{"Features", out.Features, existing.Features},
		{"KG", out.KG, existing.KG},
		{"GitignoreProjections", out.GitignoreProjections, existing.GitignoreProjections},
		// work_tracking is generate-time BOOTSTRAP, not a rescan: a committed
		// backend choice must not be reset by the generator's default.
		{"WorkTracking", out.WorkTracking, existing.WorkTracking},
		// version must not regress to the generator's v1 default.
		{"Version", out.Version, 2},
		// the scan-derived set still replaces.
		{"Skills", out.Skills, []string{"scanned"}},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s: got %#v, want %#v", c.field, c.got, c.want)
		}
	}
}

// TestMergeGenerateAgentsRC_AbsentScalarsFillFromScan pins the bootstrap half:
// an incomplete or v1 manifest is still completed in place from the scan.
func TestMergeGenerateAgentsRC_AbsentScalarsFillFromScan(t *testing.T) {
	existing := &AgentsRC{}
	generated := &AgentsRC{
		Schema:       "https://agorcha.dev/schemas/agentsrc.schema.json",
		Version:      1,
		Project:      "derived",
		RepoID:       "github.com/acme/derived",
		WorkTracking: &AgentsRCWorkTracking{Backend: WorkTrackingBackendGitRef},
	}

	out := MergeGenerateAgentsRC(existing, generated)

	if out.Schema != generated.Schema {
		t.Errorf("Schema: got %q, want %q", out.Schema, generated.Schema)
	}
	if out.Version != 1 {
		t.Errorf("Version: got %d, want 1", out.Version)
	}
	if out.Project != "derived" {
		t.Errorf("Project: got %q, want derived", out.Project)
	}
	if out.RepoID != "github.com/acme/derived" {
		t.Errorf("RepoID: got %q, want github.com/acme/derived", out.RepoID)
	}
	if out.WorkTracking == nil || out.WorkTracking.Backend != WorkTrackingBackendGitRef {
		t.Errorf("WorkTracking: got %#v, want the generated git-ref bootstrap", out.WorkTracking)
	}
}

// mergeSettingsDeclaration merges two manifests that differ only in `settings`.
func mergeSettingsDeclaration(existing, generated *bool, force bool) *AgentsRC {
	opts := []MergeGenerateOptions{{Force: force}}
	return MergeGenerateAgentsRC(&AgentsRC{Settings: existing}, &AgentsRC{Settings: generated}, opts...)
}

// assertOptionalBool compares an optional bool against want, treating nil as
// "the key is absent".
func assertOptionalBool(t *testing.T, field string, got, want *bool) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s: got %v, want absent", field, *got)
	case want != nil && got == nil:
		t.Errorf("%s: got absent, want %v", field, *want)
	case want != nil && *got != *want:
		t.Errorf("%s: got %v, want %v", field, *got, *want)
	}
}
