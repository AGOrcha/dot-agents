package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMapGlobalRelToDest(t *testing.T) {
	cases := []struct {
		rel  string
		want string
	}{
		{".claude/settings.json", "settings/global/claude-code.json"},
		{".cursor/settings.json", "settings/global/cursor.json"},
		{".cursor/mcp.json", "mcp/global/mcp.json"},
		{".claude/CLAUDE.md", "rules/global/agents.md"},
		{".codex/config.toml", "settings/global/codex.toml"},
		{".cursor/hooks.json", "hooks/global/cursor.json"},
		{".unknown", ""},
	}

	for _, c := range cases {
		got := mapGlobalRelToDest(c.rel)
		if got != c.want {
			t.Fatalf("mapGlobalRelToDest(%q)=%q, want %q", c.rel, got, c.want)
		}
	}
}

func TestMapResourceRelToDestHooks(t *testing.T) {
	project := "my-project"
	cases := []struct {
		rel  string
		want string
	}{
		{relCursorHooksJSON, agentsHooksPrefix + project + "/cursor.json"},
		{".github/hooks/pre-tool.json", agentsHooksPrefix + project + "/pre-tool.json"},
		{".github/hooks/post-save.json", agentsHooksPrefix + project + "/post-save.json"},
	}

	for _, c := range cases {
		got := mapResourceRelToDest(project, c.rel)
		if got != c.want {
			t.Fatalf("mapResourceRelToDest(%q, %q)=%q, want %q", project, c.rel, got, c.want)
		}
	}
}

func TestFilesDifferent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(a, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("different"), 0644); err != nil {
		t.Fatal(err)
	}

	same, err := filesDifferent(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Fatalf("expected equal files")
	}

	diff, err := filesDifferent(a, c)
	if err != nil {
		t.Fatal(err)
	}
	if !diff {
		t.Fatalf("expected different files")
	}
}
