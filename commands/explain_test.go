package commands

import (
	"testing"
)

func TestNewExplainCmd_ConstructsWithoutPanic(t *testing.T) {
	cmd := NewExplainCmd()
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Use != "explain [topic]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "explain [topic]")
	}
}

func TestRunExplain_OverviewTopic(t *testing.T) {
	cmd := NewExplainCmd()
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("da explain (overview): %v", err)
	}
}

func TestRunExplain_ManifestTopic(t *testing.T) {
	cmd := NewExplainCmd()
	cmd.SetArgs([]string{"manifest"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("da explain manifest: %v", err)
	}
}

func TestRunExplain_LinksTopic(t *testing.T) {
	cmd := NewExplainCmd()
	cmd.SetArgs([]string{"links"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("da explain links: %v", err)
	}
}

func TestRunExplain_PlatformsTopic(t *testing.T) {
	cmd := NewExplainCmd()
	cmd.SetArgs([]string{"platforms"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("da explain platforms: %v", err)
	}
}

func TestRunExplain_StructureTopic(t *testing.T) {
	cmd := NewExplainCmd()
	cmd.SetArgs([]string{"structure"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("da explain structure: %v", err)
	}
}

func TestRunExplain_UnknownTopicFallsBackToOverview(t *testing.T) {
	cmd := NewExplainCmd()
	cmd.SetArgs([]string{"unknown-topic"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("da explain unknown-topic: %v", err)
	}
}
