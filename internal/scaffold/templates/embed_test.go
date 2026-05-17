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

func TestTitleFromNameEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Empty parts mid-string (consecutive separators) → empty part continue.
		{"consecutive-dashes", "foo--bar", "Foo Bar"},
		{"trailing-dash", "foo-bar-", "Foo Bar"},
		// Single-separator-only name → no parts → fall back to original name.
		{"only-dashes", "---", "---"},
		{"only-underscores", "___", "___"},
		{"empty-string", "", ""},
		{"single-word", "agent", "Agent"},
		{"mixed-separators", "a_b c-d", "A B C D"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := titleFromName(tc.in); got != tc.want {
				t.Errorf("titleFromName(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
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
