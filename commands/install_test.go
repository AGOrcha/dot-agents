package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstResourceCandidatePrefersProjectScopeOverGlobal(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "agentshome")
	project := "myproj"
	name := "docbot"
	projDir := filepath.Join(home, "agents", project, name)
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "AGENT.md"), []byte("# Docbot\n"), 0644); err != nil {
		t.Fatal(err)
	}

	candidate, srcRoot, found := firstResourceCandidate("agents", name, "AGENT.md", project, []string{home})
	if !found {
		t.Fatal("expected project-scoped agent to resolve")
	}
	if srcRoot != home {
		t.Fatalf("srcRoot = %q, want %q", srcRoot, home)
	}
	want := filepath.Join(home, "agents", project, name)
	if candidate != want {
		t.Fatalf("candidate = %q, want %q", candidate, want)
	}
}

func TestFirstResourceCandidateFallsBackToGlobal(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "agentshome")
	project := "myproj"
	name := "globalonly"
	globalDir := filepath.Join(home, "agents", "global", name)
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "AGENT.md"), []byte("# G\n"), 0644); err != nil {
		t.Fatal(err)
	}

	candidate, _, found := firstResourceCandidate("agents", name, "AGENT.md", project, []string{home})
	if !found {
		t.Fatal("expected global agent to resolve")
	}
	want := filepath.Join(home, "agents", "global", name)
	if candidate != want {
		t.Fatalf("candidate = %q, want %q", candidate, want)
	}
}
