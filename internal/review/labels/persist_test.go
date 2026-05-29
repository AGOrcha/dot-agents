package labels

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func fixedNow() time.Time { return time.Date(2026, 5, 28, 9, 0, 0, 0, time.UTC) }

func sampleAdd() AddInput {
	return AddInput{
		Actor:      "reviewer@example.com",
		Role:       RoleReviewer,
		Structured: validStructured(),
		FreeText:   "initial review",
		Now:        fixedNow(),
	}
}

func TestIterationLabelsPath(t *testing.T) {
	got := IterationLabelsPath("/tmp/log", 7)
	want := filepath.Join("/tmp/log", "iter-7.labels.yaml")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestReadSidecarMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	sc, err := ReadSidecar(dir, 4)
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if sc.Iteration != 4 || sc.SchemaVersion != LabelSchemaVersion {
		t.Fatalf("unexpected empty sidecar: %+v", sc)
	}
	if sc.Labels == nil || len(sc.Labels) != 0 {
		t.Fatalf("Labels should be empty non-nil, got %#v", sc.Labels)
	}
}

func TestAddReadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	lbl, err := Add(dir, 5, sampleAdd())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if lbl.ID == "" || lbl.Iteration != 5 {
		t.Fatalf("bad label: %+v", lbl)
	}
	if !lbl.CreatedAt.Equal(fixedNow()) || !lbl.UpdatedAt.Equal(fixedNow()) {
		t.Fatalf("timestamps not set: %+v", lbl)
	}

	got, err := Get(dir, 5, lbl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EffectiveFreeText() != "initial review" {
		t.Fatalf("roundtrip free text = %q", got.EffectiveFreeText())
	}
	if !got.CreatedAt.Equal(lbl.CreatedAt) {
		t.Fatalf("CreatedAt roundtrip mismatch: %v vs %v", got.CreatedAt, lbl.CreatedAt)
	}
	if got.EffectiveStructured() != validStructured() {
		t.Fatalf("structured roundtrip mismatch: %+v", got.EffectiveStructured())
	}
}

func TestAddNowDefaultsToClock(t *testing.T) {
	dir := t.TempDir()
	in := sampleAdd()
	in.Now = time.Time{}
	before := time.Now().UTC().Add(-time.Second)
	lbl, err := Add(dir, 1, in)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if lbl.CreatedAt.Before(before) {
		t.Fatalf("CreatedAt %v predates call %v", lbl.CreatedAt, before)
	}
}

func TestAddMultipleLabelsPerIteration(t *testing.T) {
	dir := t.TempDir()
	if _, err := Add(dir, 2, sampleAdd()); err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	second := sampleAdd()
	second.Actor = "reviewer2@example.com"
	if _, err := Add(dir, 2, second); err != nil {
		t.Fatalf("Add 2: %v", err)
	}
	list, err := List(dir, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 labels, got %d", len(list))
	}
	if list[0].ID == list[1].ID {
		t.Fatalf("ids collided: %s", list[0].ID)
	}
}

func TestEditAppendsHistoryNonDestructive(t *testing.T) {
	dir := t.TempDir()
	lbl, err := Add(dir, 3, sampleAdd())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	editTime := fixedNow().Add(time.Hour)
	updated, err := EditLabel(dir, 3, lbl.ID, EditInput{
		Actor:      "reviewer@example.com",
		Role:       RoleReviewer,
		Structured: Structured{Correctness: 1, ScopeJudgement: ScopePartial, Hallucination: HallucinationMinor},
		FreeText:   "changed my mind",
		Now:        editTime,
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if len(updated.History) != 2 {
		t.Fatalf("history len = %d, want 2", len(updated.History))
	}
	if updated.History[0].FreeText != "initial review" {
		t.Fatalf("original edit lost: %q", updated.History[0].FreeText)
	}
	if !updated.UpdatedAt.Equal(editTime) {
		t.Fatalf("UpdatedAt not bumped: %v", updated.UpdatedAt)
	}
	if !updated.CreatedAt.Equal(fixedNow()) {
		t.Fatalf("CreatedAt mutated: %v", updated.CreatedAt)
	}
	if updated.EffectiveFreeText() != "changed my mind" {
		t.Fatalf("effective text = %q", updated.EffectiveFreeText())
	}
}

func TestEditNowDefaultsToClock(t *testing.T) {
	dir := t.TempDir()
	lbl, _ := Add(dir, 3, sampleAdd())
	before := time.Now().UTC().Add(-time.Second)
	updated, err := EditLabel(dir, 3, lbl.ID, EditInput{
		Actor:      "reviewer@example.com",
		Role:       RoleReviewer,
		Structured: validStructured(),
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if updated.UpdatedAt.Before(before) {
		t.Fatalf("UpdatedAt %v predates call", updated.UpdatedAt)
	}
}

func TestEditAdminCanEditOthers(t *testing.T) {
	dir := t.TempDir()
	lbl, _ := Add(dir, 3, sampleAdd())
	_, err := EditLabel(dir, 3, lbl.ID, EditInput{
		Actor:      "admin@example.com",
		Role:       RoleAdmin,
		Structured: validStructured(),
		FreeText:   "admin correction",
		Now:        fixedNow().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("admin edit should succeed: %v", err)
	}
}

func TestEditUnauthorized(t *testing.T) {
	dir := t.TempDir()
	lbl, _ := Add(dir, 3, sampleAdd())
	_, err := EditLabel(dir, 3, lbl.ID, EditInput{
		Actor:      "other@example.com",
		Role:       RoleReviewer,
		Structured: validStructured(),
		Now:        fixedNow().Add(time.Minute),
	})
	if !errors.Is(err, ErrUnauthorizedEdit) {
		t.Fatalf("want ErrUnauthorizedEdit, got %v", err)
	}
}

func TestEditNotFound(t *testing.T) {
	dir := t.TempDir()
	_, _ = Add(dir, 3, sampleAdd())
	_, err := EditLabel(dir, 3, "nope", EditInput{Actor: "x", Role: RoleAdmin, Structured: validStructured()})
	if !errors.Is(err, ErrLabelNotFound) {
		t.Fatalf("want ErrLabelNotFound, got %v", err)
	}
}

func TestEditInvalidStructuredRejected(t *testing.T) {
	dir := t.TempDir()
	lbl, _ := Add(dir, 3, sampleAdd())
	_, err := EditLabel(dir, 3, lbl.ID, EditInput{
		Actor:      "reviewer@example.com",
		Role:       RoleReviewer,
		Structured: Structured{Correctness: 9, ScopeJudgement: ScopeOnTarget, Hallucination: HallucinationNone},
		Now:        fixedNow().Add(time.Minute),
	})
	if !errors.Is(err, ErrCorrectnessRange) {
		t.Fatalf("want ErrCorrectnessRange, got %v", err)
	}
	// The bad edit must not have been persisted.
	got, _ := Get(dir, 3, lbl.ID)
	if len(got.History) != 1 {
		t.Fatalf("rejected edit leaked into history: %d entries", len(got.History))
	}
}

func TestGetNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Get(dir, 1, "missing")
	if !errors.Is(err, ErrLabelNotFound) {
		t.Fatalf("want ErrLabelNotFound, got %v", err)
	}
}

func TestListEmpty(t *testing.T) {
	dir := t.TempDir()
	list, err := List(dir, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("want empty, got %d", len(list))
	}
}

func TestWriteSidecarRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	bad := Sidecar{Iteration: -1, SchemaVersion: LabelSchemaVersion}
	if _, err := WriteSidecar(dir, bad); !errors.Is(err, ErrNegativeIteration) {
		t.Fatalf("want ErrNegativeIteration, got %v", err)
	}
}

func TestWriteSidecarWritesFile(t *testing.T) {
	dir := t.TempDir()
	sc := Sidecar{Iteration: 1, SchemaVersion: LabelSchemaVersion, Labels: []Label{}}
	path, err := WriteSidecar(dir, sc)
	if err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}
}

// TestColonSpaceSafeFreeText verifies that free text containing a YAML
// "key: value" sequence roundtrips literally and does not corrupt the file
// (schema-usage colon-space rule). The yaml marshaller block-scalars or
// quotes such strings automatically; this asserts the contract end to end.
func TestColonSpaceSafeFreeText(t *testing.T) {
	dir := t.TempDir()
	tricky := "Implemented two-lens: phase 1 review gate\nnotes: still: nested colons"
	in := sampleAdd()
	in.FreeText = tricky
	lbl, err := Add(dir, 8, in)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := Get(dir, 8, lbl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EffectiveFreeText() != tricky {
		t.Fatalf("colon-space text corrupted:\n got %q\nwant %q", got.EffectiveFreeText(), tricky)
	}
	// And the raw file must still parse as a valid sidecar.
	if _, err := ReadSidecar(dir, 8); err != nil {
		t.Fatalf("sidecar with colon-space text failed to re-read: %v", err)
	}
}

func TestReadSidecarCorruptYAML(t *testing.T) {
	dir := t.TempDir()
	path := IterationLabelsPath(dir, 1)
	if err := os.WriteFile(path, []byte("\tnot: [valid"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := ReadSidecar(dir, 1); err == nil {
		t.Fatal("want parse error, got nil")
	}
}

func TestReadSidecarInvalidContent(t *testing.T) {
	dir := t.TempDir()
	// Well-formed YAML but an invalid label (bad role) must be rejected.
	sc := Sidecar{
		Iteration:     1,
		SchemaVersion: LabelSchemaVersion,
		Labels: []Label{{
			ID:            "x",
			Iteration:     1,
			Actor:         "a",
			Role:          "bogus",
			SchemaVersion: LabelSchemaVersion,
			History:       []Edit{{Actor: "a", Role: "bogus", Structured: validStructured()}},
		}},
	}
	data, _ := yaml.Marshal(sc)
	path := IterationLabelsPath(dir, 1)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := ReadSidecar(dir, 1); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("want ErrInvalidRole, got %v", err)
	}
}

func TestReadSidecarNilLabelsNormalized(t *testing.T) {
	dir := t.TempDir()
	path := IterationLabelsPath(dir, 1)
	if err := os.WriteFile(path, []byte("iteration: 1\nschema_version: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	sc, err := ReadSidecar(dir, 1)
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if sc.Labels == nil {
		t.Fatal("Labels should be normalized to non-nil")
	}
}

func TestAddAdminOverridePersists(t *testing.T) {
	dir := t.TempDir()
	in := sampleAdd()
	in.Role = RoleAdmin
	in.Actor = "admin@example.com"
	in.AdminOverride = true
	lbl, err := Add(dir, 9, in)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, _ := Get(dir, 9, lbl.ID)
	if !got.AdminOverride {
		t.Fatal("AdminOverride not persisted")
	}
}

func TestReadSidecarPropagatesReadError(t *testing.T) {
	// A directory in place of the sidecar file forces a non-NotExist read error.
	dir := t.TempDir()
	path := IterationLabelsPath(dir, 1)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := ReadSidecar(dir, 1)
	if err == nil || strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want read error, got %v", err)
	}
}

// dirAtSidecar plants a directory where iter-N.labels.yaml should be so any
// read of that sidecar fails with a non-NotExist error. This drives the
// read-error propagation paths in Add/EditLabel/Get/List.
func dirAtSidecar(t *testing.T, iteration int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(IterationLabelsPath(dir, iteration), 0o755); err != nil {
		t.Fatalf("mkdir sidecar: %v", err)
	}
	return dir
}

func TestCRUDPropagateReadError(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		if _, err := Add(dirAtSidecar(t, 1), 1, sampleAdd()); err == nil {
			t.Fatal("want read error")
		}
	})
	t.Run("EditLabel", func(t *testing.T) {
		if _, err := EditLabel(dirAtSidecar(t, 1), 1, "id", EditInput{Actor: "a", Role: RoleAdmin, Structured: validStructured()}); err == nil {
			t.Fatal("want read error")
		}
	})
	t.Run("Get", func(t *testing.T) {
		if _, err := Get(dirAtSidecar(t, 1), 1, "id"); err == nil {
			t.Fatal("want read error")
		}
	})
	t.Run("List", func(t *testing.T) {
		if _, err := List(dirAtSidecar(t, 1), 1); err == nil {
			t.Fatal("want read error")
		}
	})
}

// withWriteFileAtomic overrides the atomic-write seam to drive the persist
// error branches deterministically. The granular temp/write/close/rename
// failures of the atomic primitive itself are owned and tested by the fsops
// package; here we only need WriteSidecar's callers to surface a write error.
func withWriteFileAtomic(t *testing.T, fn func(path string, data []byte, perm os.FileMode) error) {
	t.Helper()
	orig := writeFileAtomic
	writeFileAtomic = fn
	t.Cleanup(func() { writeFileAtomic = orig })
}

// TestAddWritePropagatesError verifies Add surfaces a write failure, covering
// Add's WriteSidecar error path.
func TestAddWritePropagatesError(t *testing.T) {
	withWriteFileAtomic(t, func(string, []byte, os.FileMode) error {
		return errors.New("no write")
	})
	if _, err := Add(t.TempDir(), 1, sampleAdd()); err == nil {
		t.Fatal("want write error from Add")
	}
}

// TestEditLabelWritePropagatesError verifies EditLabel surfaces a write
// failure on the persist step (after a successful seed Add).
func TestEditLabelWritePropagatesError(t *testing.T) {
	dir := t.TempDir()
	lbl, err := Add(dir, 1, sampleAdd())
	if err != nil {
		t.Fatalf("seed Add: %v", err)
	}
	withWriteFileAtomic(t, func(string, []byte, os.FileMode) error {
		return errors.New("no write")
	})
	_, err = EditLabel(dir, 1, lbl.ID, EditInput{
		Actor: "reviewer@example.com", Role: RoleReviewer, Structured: validStructured(), Now: fixedNow().Add(time.Minute),
	})
	if err == nil {
		t.Fatal("want write error from EditLabel")
	}
}

// TestWriteSidecarPropagatesWriteError verifies WriteSidecar surfaces an atomic
// write failure from the fsops seam.
func TestWriteSidecarPropagatesWriteError(t *testing.T) {
	withWriteFileAtomic(t, func(string, []byte, os.FileMode) error {
		return errors.New("disk full")
	})
	_, err := WriteSidecar(t.TempDir(), Sidecar{Iteration: 1, SchemaVersion: LabelSchemaVersion, Labels: []Label{}})
	if err == nil {
		t.Fatal("want write error")
	}
}

func TestAddRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	in := sampleAdd()
	in.Structured.Correctness = 99 // out of range → label.Validate fails inside Add
	if _, err := Add(dir, 1, in); !errors.Is(err, ErrCorrectnessRange) {
		t.Fatalf("want ErrCorrectnessRange, got %v", err)
	}
}

func TestAddEntropyFailurePropagates(t *testing.T) {
	orig := randReader
	randReader = failReader{}
	defer func() { randReader = orig }()
	if _, err := Add(t.TempDir(), 1, sampleAdd()); err == nil {
		t.Fatal("want entropy error from Add")
	}
}

// TestWriteYAMLAtomicRenameFailure drives the rename-failure branch: the temp
// file is created in the dir, but the destination path is a non-empty
// directory, so os.Rename fails.
func TestWriteYAMLAtomicRenameFailure(t *testing.T) {
	dir := t.TempDir()
	dest := IterationLabelsPath(dir, 1)
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}
	// Put a child in the dest dir so rename-over fails on all platforms.
	if err := os.WriteFile(filepath.Join(dest, "child"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed child: %v", err)
	}
	sc := Sidecar{Iteration: 1, SchemaVersion: LabelSchemaVersion, Labels: []Label{}}
	if _, err := WriteSidecar(dir, sc); err == nil {
		t.Fatal("want rename error")
	}
}
