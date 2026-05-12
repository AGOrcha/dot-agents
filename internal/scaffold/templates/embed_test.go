package templates

import (
	"strings"
	"testing"
)

func TestRenderSkillManifest(t *testing.T) {
	got, err := RenderSkillManifest("review-pr")
	if err != nil {
		t.Fatalf("RenderSkillManifest: %v", err)
	}
	for _, want := range []string{
		`name: "review-pr"`,
		"# Review Pr",
		"## When to Use",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("skill manifest missing %q:\n%s", want, got)
		}
	}
}

func TestRenderAgentManifest(t *testing.T) {
	got, err := RenderAgentManifest("doc-bot")
	if err != nil {
		t.Fatalf("RenderAgentManifest: %v", err)
	}
	for _, want := range []string{
		`name: "doc-bot"`,
		"# Doc Bot",
		"## Role",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("agent manifest missing %q:\n%s", want, got)
		}
	}
}
