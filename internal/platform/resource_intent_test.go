package platform

import (
	"strings"
	"testing"
)

func TestResourceIntentValidateAcceptsRFCShape(t *testing.T) {
	intent := ResourceIntent{
		IntentID:    "skills.proj.reviewer.repo-agents-skills",
		Project:     "proj",
		Bucket:      "skills",
		LogicalName: "reviewer",
		TargetPath:  ".agents/skills/reviewer",
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "skills",
			RelativePath: "reviewer",
			Kind:         ResourceSourceCanonicalDir,
			Origin:       "agents",
		},
		Shape:         ResourceShapeDirectDir,
		Transport:     ResourceTransportSymlink,
		Materializer:  "scoped-dir-symlink",
		ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
		PrunePolicy:   ResourcePruneTarget,
		Provenance: ResourceProvenance{
			Emitter:   "codex",
			Operation: "refresh",
			Detail:    "shared skill mirror",
		},
		Precedence:  10,
		MarkerFiles: []string{"SKILL.md"},
		EnabledOn:   []string{"claude", "codex", "copilot"},
		ReviewHint:  "shared mirror convergence",
	}

	if err := intent.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
}

func TestResourceIntentEffectiveConflictKeyDefaultsToTargetPath(t *testing.T) {
	intent := ResourceIntent{TargetPath: ".agents/skills/reviewer"}
	if got := intent.EffectiveConflictKey(); got != intent.TargetPath {
		t.Fatalf("EffectiveConflictKey() = %q, want %q", got, intent.TargetPath)
	}

	intent.ConflictKey = "shared:.agents/skills/reviewer"
	if got := intent.EffectiveConflictKey(); got != intent.ConflictKey {
		t.Fatalf("EffectiveConflictKey() = %q, want %q", got, intent.ConflictKey)
	}
}

func TestResourceIntentValidateRejectsInvalidShapeTransportPairs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ResourceIntent)
		want string
	}{
		{
			name: "direct shape cannot write",
			edit: func(intent *ResourceIntent) {
				intent.Shape = ResourceShapeDirectDir
				intent.Transport = ResourceTransportWrite
			},
			want: `shape "direct_dir" cannot use transport "write"`,
		},
		{
			name: "render shape requires write",
			edit: func(intent *ResourceIntent) {
				intent.Shape = ResourceShapeRenderSingle
				intent.Transport = ResourceTransportSymlink
			},
			want: `shape "render_single" requires transport "write"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			intent := validResourceIntent()
			tc.edit(&intent)

			err := intent.Validate()
			if err == nil {
				t.Fatal("Validate() returned nil error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %q, want substring %q", err, tc.want)
			}
		})
	}
}

func TestResourceSourceRefValidateAndCanonicalPath(t *testing.T) {
	ref := ResourceSourceRef{
		Scope:        "proj",
		Bucket:       "hooks",
		RelativePath: "lint/HOOK.yaml",
		Kind:         ResourceSourceCanonicalBundle,
		Origin:       "copilot",
	}

	if err := ref.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}

	got := ref.CanonicalPath("/tmp/.agents")
	want := "/tmp/.agents/hooks/proj/lint/HOOK.yaml"
	if got != want {
		t.Fatalf("CanonicalPath() = %q, want %q", got, want)
	}
}

func TestResourceIntentValidateRequiresFields(t *testing.T) {
	intent := validResourceIntent()
	intent.IntentID = ""

	err := intent.Validate()
	if err == nil {
		t.Fatal("Validate() returned nil error")
	}
	if !strings.Contains(err.Error(), "intent_id is required") {
		t.Fatalf("Validate() error = %q, want missing intent_id", err)
	}
}

func validResourceIntent() ResourceIntent {
	return ResourceIntent{
		IntentID:    "skills.proj.reviewer.repo-agents-skills",
		Project:     "proj",
		Bucket:      "skills",
		LogicalName: "reviewer",
		TargetPath:  ".agents/skills/reviewer",
		Ownership:   ResourceOwnershipSharedRepo,
		SourceRef: ResourceSourceRef{
			Scope:        "proj",
			Bucket:       "skills",
			RelativePath: "reviewer",
			Kind:         ResourceSourceCanonicalDir,
		},
		Shape:         ResourceShapeDirectDir,
		Transport:     ResourceTransportSymlink,
		Materializer:  "scoped-dir-symlink",
		ReplacePolicy: ResourceReplaceIfManaged,
		PrunePolicy:   ResourcePruneTarget,
	}
}
