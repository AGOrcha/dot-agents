package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/commands"
)

func TestRootHelpIncludesExamples(t *testing.T) {
	root := commands.NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)

	if err := root.Help(); err != nil {
		t.Fatalf("Help: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"Examples:",
		"da install --generate",
		"da status --audit",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("root help missing %q:\n%s", want, out)
		}
	}
}
