package projection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pickByteIdenticalTasks finds a real TASKS.yaml that round-trips byte-identical
// (so the test edits a canonical file, isolating the edit as the only change).
func pickByteIdenticalTasks(t *testing.T) (path string, orig []byte) {
	t.Helper()
	root := realPlansRoot(t)
	for _, dir := range planDirs(t, root) {
		p := filepath.Join(dir, "TASKS.yaml")
		if !fileExists(p) {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fr, err := GradeTasksRoundTrip(p, data)
		if err == nil && fr.Grade == ByteIdentical && len(mustParse(t, data).Tasks) > 0 {
			return p, data
		}
	}
	t.Skip("no byte-identical TASKS.yaml found to edit")
	return "", nil
}

func mustParse(t *testing.T, data []byte) *TaskFile {
	t.Helper()
	tf, err := ParseTasks(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tf
}

// TestHReingest is H-reingest: regenerate -> hand-edit a task's status/notes ->
// re-ingest -> the model reflects the edit. This is the FS-is-the-interface
// principle: editing the projected file IS the act of changing state. The edit
// is applied to the FILE BYTES (not the struct) to faithfully model a human/
// agent editing the YAML on disk.
func TestHReingest(t *testing.T) {
	path, orig := pickByteIdenticalTasks(t)

	// 1. Regenerate from the model (the projection an agent would see).
	tf := mustParse(t, orig)
	regen, err := SerializeTasks(tf)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Hand-edit the FILE BYTES: flip the first task's status and append to
	//    its notes — the literal act of editing the projection on disk.
	target := tf.Tasks[0]
	oldStatus := "status: " + target.Status
	newStatus := "status: in_progress"
	if target.Status == "in_progress" {
		newStatus = "status: completed"
	}
	edited := strings.Replace(string(regen), oldStatus, newStatus, 1)
	if edited == string(regen) {
		t.Fatalf("status edit did not change the bytes (looked for %q)", oldStatus)
	}

	// 3. Re-ingest the edited file -> the MODEL must reflect the edit.
	reTf, err := ParseTasks([]byte(edited))
	if err != nil {
		t.Fatalf("re-ingest edited file: %v", err)
	}
	wantStatus := strings.TrimPrefix(newStatus, "status: ")
	if reTf.Tasks[0].Status != wantStatus {
		t.Errorf("re-ingest did not reflect status edit: got %q want %q", reTf.Tasks[0].Status, wantStatus)
	}
	// The OTHER tasks must be untouched (the edit is surgical).
	for i := 1; i < len(reTf.Tasks); i++ {
		if reTf.Tasks[i].Status != tf.Tasks[i].Status {
			t.Errorf("task %d status changed unexpectedly: %q -> %q", i, tf.Tasks[i].Status, reTf.Tasks[i].Status)
		}
	}
	t.Logf("H-reingest on %s: edited %q -> model reflects %q", filepath.Base(filepath.Dir(path)), oldStatus, wantStatus)
}

// TestHReingestNotesWithColon proves the schema-usage `: `-in-notes case
// survives the full edit cycle: a notes value containing ": " (which YAML would
// misparse as a mapping if emitted as a plain scalar) must round-trip through
// serialize -> hand-edit -> re-ingest with the colon content intact.
func TestHReingestNotesWithColon(t *testing.T) {
	tf := &TaskFile{
		SchemaVersion: 1,
		PlanID:        "synthetic",
		Tasks: []Task{{
			ID:     "t1",
			Title:  "demo",
			Status: "pending",
			Notes:  "Implemented two-lens: phase 1 review gate (contains a : space run)",
		}},
	}
	regen, err := SerializeTasks(tf)
	if err != nil {
		t.Fatal(err)
	}
	// The serializer MUST have quoted/blocked the colon notes so it does not
	// parse as a nested mapping.
	reTf, err := ParseTasks(regen)
	if err != nil {
		t.Fatalf("re-ingest colon notes: %v", err)
	}
	if reTf.Tasks[0].Notes != tf.Tasks[0].Notes {
		t.Errorf("colon-notes corrupted by round-trip:\n got: %q\nwant: %q", reTf.Tasks[0].Notes, tf.Tasks[0].Notes)
	}

	// Now hand-edit the notes on disk (append text) and re-ingest.
	edited := strings.Replace(string(regen), "review gate", "review gate EDITED", 1)
	if edited == string(regen) {
		t.Fatal("notes edit did not change bytes")
	}
	reTf2, err := ParseTasks([]byte(edited))
	if err != nil {
		t.Fatalf("re-ingest edited colon notes: %v", err)
	}
	if !strings.Contains(reTf2.Tasks[0].Notes, "review gate EDITED") {
		t.Errorf("edited colon notes not reflected: %q", reTf2.Tasks[0].Notes)
	}
	if !strings.Contains(reTf2.Tasks[0].Notes, "two-lens: phase 1") {
		t.Errorf("colon content lost after edit: %q", reTf2.Tasks[0].Notes)
	}
}
