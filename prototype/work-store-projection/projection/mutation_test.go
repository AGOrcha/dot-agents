package projection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

// brokenSerializeTasks is a deliberately MUTATED serializer that drops the Notes
// field (simulating "the serializer's field handling broke"). The mutation
// tests below assert that the H-roundtrip / H-reingest proofs CATCH this — if
// they passed against a broken serializer, they would be coverage theater.
func brokenSerializeTasks(tf *TaskFile) ([]byte, error) {
	type brokenTask struct {
		ID                   string   `yaml:"id"`
		Title                string   `yaml:"title"`
		Status               string   `yaml:"status"`
		DependsOn            []string `yaml:"depends_on"`
		Blocks               []string `yaml:"blocks"`
		Owner                string   `yaml:"owner"`
		WriteScope           []string `yaml:"write_scope"`
		VerificationRequired bool     `yaml:"verification_required"`
		// Notes intentionally OMITTED — the mutation.
		AppType string `yaml:"app_type,omitempty"`
	}
	type brokenFile struct {
		SchemaVersion int          `yaml:"schema_version"`
		PlanID        string       `yaml:"plan_id"`
		Tasks         []brokenTask `yaml:"tasks"`
	}
	bf := brokenFile{SchemaVersion: tf.SchemaVersion, PlanID: tf.PlanID}
	for _, t := range tf.Tasks {
		bf.Tasks = append(bf.Tasks, brokenTask{
			ID: t.ID, Title: t.Title, Status: t.Status, DependsOn: t.DependsOn,
			Blocks: t.Blocks, Owner: t.Owner, WriteScope: t.WriteScope,
			VerificationRequired: t.VerificationRequired, AppType: t.AppType,
		})
	}
	return yaml.Marshal(bf)
}

// TestMutationSerializerDropsNotesIsCaught proves the grading is mutation-
// sensitive: a serializer that drops Notes turns a byte-identical (or any
// lossless) file into a detectable loss. If this test were to find NO files
// where the broken serializer degrades fidelity, the proofs would be vacuous.
func TestMutationSerializerDropsNotesIsCaught(t *testing.T) {
	root := realPlansRoot(t)
	caught := 0
	for _, dir := range planDirs(t, root) {
		if mutationCaughtForFile(t, filepath.Join(dir, "TASKS.yaml")) {
			caught++
		}
	}
	if caught == 0 {
		t.Fatal("no files exercised the notes-mutation — proof is vacuous")
	}
	t.Logf("mutation sensitivity: notes-dropping serializer caught on %d real files", caught)
}

// mutationCaughtForFile returns true when a TASKS.yaml with non-empty notes
// has its notes-dropping mutation DETECTED (and the correct serializer does
// not lose data). Files without notes return false (not applicable).
func mutationCaughtForFile(t *testing.T, p string) bool {
	t.Helper()
	if !fileExists(p) {
		return false
	}
	orig, _ := os.ReadFile(p)
	tf, err := ParseTasks(orig)
	if err != nil || !anyTaskHasNotes(tf) {
		return false
	}
	good, _ := SerializeTasks(tf)
	goodTf, _ := ParseTasks(good)
	if !tasksModelEqual(tf, goodTf) {
		t.Errorf("%s: CORRECT serializer already loses data — proof baseline broken", p)
	}
	bad, _ := brokenSerializeTasks(tf)
	badTf, _ := ParseTasks(bad)
	if tasksModelEqual(tf, badTf) {
		t.Errorf("%s: broken (notes-dropping) serializer NOT caught — proof is not mutation-sensitive", p)
		return false
	}
	return true
}

func anyTaskHasNotes(tf *TaskFile) bool {
	for _, task := range tf.Tasks {
		if strings.TrimSpace(task.Notes) != "" {
			return true
		}
	}
	return false
}

// TestMutationKeyReorderIsChurn proves the CHURN proof is mutation-sensitive:
// the negative-control naive serializer (key reorder) is detected as churn vs
// the canonical one. If reordering keys produced zero diff, H-churn would be
// meaningless.
func TestMutationKeyReorderIsChurn(t *testing.T) {
	root := realPlansRoot(t)
	for _, dir := range planDirs(t, root) {
		p := filepath.Join(dir, "TASKS.yaml")
		if !fileExists(p) {
			continue
		}
		orig, _ := os.ReadFile(p)
		tf, err := ParseTasks(orig)
		if err != nil || len(tf.Tasks) == 0 {
			continue
		}
		canon, _ := SerializeTasks(tf)
		naive, _ := NaiveSerializeTasks(tf)
		if string(canon) == string(naive) {
			continue // some trivial files may coincide; skip
		}
		if DiffLineCount(canon, naive) == 0 {
			t.Errorf("%s: key-reorder mutation produced zero churn — churn metric is blind", p)
		}
		return // one representative non-trivial file is enough
	}
}
