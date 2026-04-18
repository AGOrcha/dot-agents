package workflow

import (
	"encoding/json"
	"io"
	"os"
	"testing"
)

func TestRunWorkflowStatus_JSONUsesFlagsJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	oldwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

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
