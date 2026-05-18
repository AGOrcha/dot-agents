package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompiledVerificationResultSchema(t *testing.T) {
	sch, err := compiledVerificationResultSchema()
	if err != nil {
		t.Fatalf("compiledVerificationResultSchema: %v", err)
	}
	if sch == nil {
		t.Error("expected non-nil schema")
	}

	sch2, err := compiledVerificationResultSchema()
	if err != nil {
		t.Fatal(err)
	}
	if sch != sch2 {
		t.Error("expected sync.Once cached schema")
	}
}

func TestValidateVerificationResultDoc_Nil(t *testing.T) {
	if err := validateVerificationResultDoc(nil); err == nil {
		t.Error("expected error for nil doc")
	}
}

func TestValidateVerificationResultDoc_Valid(t *testing.T) {
	doc := &VerificationResultDoc{
		SchemaVersion: 1,
		TaskID:        "task-001",
		ParentPlanID:  "plan-001",
		VerifierType:  "unit",
		Status:        "pass",
		Summary:       "tests pass",
		RecordedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := validateVerificationResultDoc(doc); err != nil {
		t.Errorf("expected valid doc to pass: %v", err)
	}
}

func TestValidateVerificationResultDoc_Invalid(t *testing.T) {
	doc := &VerificationResultDoc{}
	if err := validateVerificationResultDoc(doc); err == nil {
		t.Error("expected validation failure for empty doc")
	}
}

func TestVerificationResultFilePath(t *testing.T) {
	p, err := verificationResultFilePath("/proj", "task-1", "unit")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "task-1") || !strings.HasSuffix(p, "unit.result.yaml") {
		t.Errorf("path = %q", p)
	}
	if _, err := verificationResultFilePath("/proj", "", "unit"); err == nil {
		t.Error("expected error for empty task")
	}
	if _, err := verificationResultFilePath("/proj", "t", ""); err == nil {
		t.Error("expected error for empty verifier_type")
	}
	if _, err := verificationResultFilePath("/proj", "t", "BAD"); err == nil {
		t.Error("expected error for invalid verifier_type")
	}
}

func TestValidVerificationVerifierTypeStem(t *testing.T) {
	cases := map[string]bool{
		"unit":     true,
		"api":      true,
		"unit-99":  true,
		"unit_99":  true,
		"":         false,
		"1unit":    false,
		"Unit":     false,
		"unit/bad": false,
		"unit!":    false,
	}
	for in, want := range cases {
		if got := validVerificationVerifierTypeStem(in); got != want {
			t.Errorf("validVerificationVerifierTypeStem(%q)=%v want %v", in, got, want)
		}
	}
}

func TestWriteVerificationResultYAML(t *testing.T) {
	dir := t.TempDir()
	doc := &VerificationResultDoc{
		SchemaVersion: 1,
		TaskID:        "task-1",
		ParentPlanID:  "plan-1",
		VerifierType:  "unit",
		Status:        "pass",
		Summary:       "ok",
		RecordedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeVerificationResultYAML(dir, doc); err != nil {
		t.Fatalf("writeVerificationResultYAML: %v", err)
	}
	want := filepath.Join(dir, ".agents", "active", "verification", "task-1", "unit.result.yaml")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected file at %s: %v", want, err)
	}

	if err := writeVerificationResultYAML(dir, nil); err == nil {
		t.Error("nil doc should error")
	}
	bad := &VerificationResultDoc{TaskID: "t", VerifierType: ""}
	if err := writeVerificationResultYAML(dir, bad); err == nil {
		t.Error("missing verifier_type should error")
	}
}

func TestWriteVerificationResultYAML_SchemaInvalid(t *testing.T) {

	doc := &VerificationResultDoc{
		SchemaVersion: 1, TaskID: "t", ParentPlanID: "p",
		VerifierType: "unit",
		RecordedAt:   "2026-05-12T00:00:00Z",
	}
	err := writeVerificationResultYAML(t.TempDir(), doc)
	if err == nil {
		t.Fatal("expected schema error")
	}
}

func TestWriteVerificationResultYAML_YAMLMarshalError_Wrapped(t *testing.T) {
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := writeVerificationResultYAML(t.TempDir(), newValidVerificationResultDoc())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

func TestValidateVerificationResultDoc_Valid_Push8(t *testing.T) {
	if err := validateVerificationResultDoc(newValidVerificationResultDoc()); err != nil {
		t.Fatalf("valid doc rejected: %v", err)
	}
}
