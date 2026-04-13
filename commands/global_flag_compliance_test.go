package commands

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunWorkflowStatus_JSONUsesFlagsJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	prevJSON := Flags.JSON
	Flags.JSON = true
	defer func() { Flags.JSON = prevJSON }()

	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	errRun := runWorkflowStatus()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	os.Stdout = oldOut

	if errRun != nil {
		t.Fatalf("runWorkflowStatus: %v", errRun)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("expected JSON: %v\n%s", err, string(out))
	}
	if _, ok := payload["project"]; !ok {
		t.Fatalf("JSON missing project: %s", string(out))
	}
}

func TestSyncPullCmd_RejectsGlobalDryRun(t *testing.T) {
	prev := Flags.DryRun
	Flags.DryRun = true
	defer func() { Flags.DryRun = prev }()

	cmd := newSyncPullCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error when Flags.DryRun is set")
	}
	if !strings.Contains(err.Error(), "sync pull") || !strings.Contains(err.Error(), "dry-run") {
		t.Fatalf("unexpected error: %v", err)
	}
}
