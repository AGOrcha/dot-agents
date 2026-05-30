package eval

import (
	"strings"
	"testing"
)

func validSpec() TaskSpec {
	return TaskSpec{
		TaskSpecVersion: CurrentTaskSpecVersion,
		TaskID:          "kg-go-impl-001",
		Language:        LanguageGo,
		Difficulty:      DifficultyMedium,
		DifficultySignals: map[string]int{
			"cyclomatic_complexity": 7,
			"edge_count":            12,
			"involved_symbols":      4,
		},
		GeneratedFrom: GeneratedFrom{
			Kind:       KindKGTemplate,
			TemplateID: "impl-pure-fn",
			KGQuery:    &KGQuery{Intent: "code_context", SeedSymbol: "pkg/foo.Bar"},
		},
		Prompt:            "Implement the function Bar(...)",
		SolutionArtifacts: []SolutionArtifact{{Path: "pkg/foo/bar.go", Role: "target"}},
		Verification: Verification{
			BuildCmd:       []string{"go", "build", "./..."},
			TestCmd:        []string{"go", "test", "./pkg/foo/..."},
			TimeoutSeconds: 120,
		},
	}
}

func TestLanguageValid(t *testing.T) {
	tests := []struct {
		lang Language
		want bool
	}{
		{LanguageGo, true},
		{LanguagePython, true},
		{LanguageTypeScript, true},
		{Language("rust"), false},
		{Language(""), false},
	}
	for _, tc := range tests {
		if got := tc.lang.Valid(); got != tc.want {
			t.Errorf("Language(%q).Valid() = %v, want %v", tc.lang, got, tc.want)
		}
	}
}

func TestDifficultyValid(t *testing.T) {
	tests := []struct {
		d    Difficulty
		want bool
	}{
		{DifficultyEasy, true},
		{DifficultyMedium, true},
		{DifficultyHard, true},
		{Difficulty("trivial"), false},
		{Difficulty(""), false},
	}
	for _, tc := range tests {
		if got := tc.d.Valid(); got != tc.want {
			t.Errorf("Difficulty(%q).Valid() = %v, want %v", tc.d, got, tc.want)
		}
	}
}

func TestGeneratedKindValid(t *testing.T) {
	tests := []struct {
		k    GeneratedKind
		want bool
	}{
		{KindKGTemplate, true},
		{KindBenchmarkSeed, true},
		{GeneratedKind("handwritten"), false},
		{GeneratedKind(""), false},
	}
	for _, tc := range tests {
		if got := tc.k.Valid(); got != tc.want {
			t.Errorf("GeneratedKind(%q).Valid() = %v, want %v", tc.k, got, tc.want)
		}
	}
}

func TestTaskSpecValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*TaskSpec)
		wantErr string
	}{
		{"valid", func(*TaskSpec) {}, ""},
		{"wrong version", func(s *TaskSpec) { s.TaskSpecVersion = 99 }, "unsupported task_spec_version"},
		{"zero version", func(s *TaskSpec) { s.TaskSpecVersion = 0 }, "unsupported task_spec_version"},
		{"empty task_id", func(s *TaskSpec) { s.TaskID = "" }, "task_id is required"},
		{"whitespace task_id", func(s *TaskSpec) { s.TaskID = "   " }, "task_id is required"},
		{"invalid language", func(s *TaskSpec) { s.Language = "cobol" }, "invalid language"},
		{"invalid difficulty", func(s *TaskSpec) { s.Difficulty = "trivial" }, "invalid difficulty"},
		{"invalid kind", func(s *TaskSpec) { s.GeneratedFrom.Kind = "nope" }, "invalid generated_from.kind"},
		{"empty prompt", func(s *TaskSpec) { s.Prompt = "" }, "prompt is required"},
		{"whitespace prompt", func(s *TaskSpec) { s.Prompt = "\n\t " }, "prompt is required"},
		{"nil test_cmd", func(s *TaskSpec) { s.Verification.TestCmd = nil }, "test_cmd is required"},
		{"empty test_cmd", func(s *TaskSpec) { s.Verification.TestCmd = []string{} }, "test_cmd is required"},
		{"negative timeout", func(s *TaskSpec) { s.Verification.TimeoutSeconds = -1 }, "timeout_seconds must be non-negative"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpec()
			tc.mutate(&s)
			err := s.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestTaskSpecRoundTrip(t *testing.T) {
	orig := validSpec()
	data, err := orig.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML() error = %v", err)
	}
	got, err := ParseTaskSpec(data)
	if err != nil {
		t.Fatalf("ParseTaskSpec() error = %v", err)
	}
	// Re-marshal and compare bytes for full structural fidelity.
	data2, err := got.MarshalYAML()
	if err != nil {
		t.Fatalf("re-MarshalYAML() error = %v", err)
	}
	if string(data) != string(data2) {
		t.Fatalf("round-trip mismatch:\nfirst:\n%s\nsecond:\n%s", data, data2)
	}
	if got.TaskID != orig.TaskID || got.Language != orig.Language || got.Difficulty != orig.Difficulty {
		t.Errorf("scalar fields differ after round-trip: %+v", got)
	}
	if got.GeneratedFrom.KGQuery == nil || got.GeneratedFrom.KGQuery.SeedSymbol != "pkg/foo.Bar" {
		t.Errorf("kg_query lost in round-trip: %+v", got.GeneratedFrom)
	}
	if got.DifficultySignals["cyclomatic_complexity"] != 7 {
		t.Errorf("difficulty_signals lost in round-trip: %+v", got.DifficultySignals)
	}
}

func TestMarshalYAMLDeterministic(t *testing.T) {
	s := validSpec()
	first, err := s.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML() error = %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := s.MarshalYAML()
		if err != nil {
			t.Fatalf("MarshalYAML() error = %v", err)
		}
		if string(first) != string(again) {
			t.Fatalf("MarshalYAML not deterministic on iter %d", i)
		}
	}
}

func TestParseTaskSpecErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{"malformed yaml", "task_spec_version: [", "decode task spec"},
		{"unknown field", "task_spec_version: 1\nbogus_field: x\n", "decode task spec"},
		{"fails validation", "task_spec_version: 1\ntask_id: x\nlanguage: cobol\ndifficulty: easy\ngenerated_from:\n  kind: kg_template\nprompt: do it\nverification:\n  test_cmd: [go, test]\n", "invalid language"},
		{"wrong version rejected", "task_spec_version: 2\ntask_id: x\nlanguage: go\ndifficulty: easy\ngenerated_from:\n  kind: kg_template\nprompt: do it\nverification:\n  test_cmd: [go, test]\n", "unsupported task_spec_version"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTaskSpec([]byte(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ParseTaskSpec() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestParseTaskSpecValid(t *testing.T) {
	data := "task_spec_version: 1\ntask_id: kg-py-001\nlanguage: python\ndifficulty: hard\ngenerated_from:\n  kind: kg_template\nprompt: implement it\nverification:\n  test_cmd: [pytest]\n"
	spec, err := ParseTaskSpec([]byte(data))
	if err != nil {
		t.Fatalf("ParseTaskSpec() error = %v", err)
	}
	if spec.Language != LanguagePython || spec.Difficulty != DifficultyHard {
		t.Errorf("unexpected parse: %+v", spec)
	}
}

func TestSignalKeys(t *testing.T) {
	s := validSpec()
	keys := s.SignalKeys()
	want := []string{"cyclomatic_complexity", "edge_count", "involved_symbols"}
	if len(keys) != len(want) {
		t.Fatalf("SignalKeys() = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("SignalKeys()[%d] = %q, want %q", i, keys[i], want[i])
		}
	}

	empty := TaskSpec{}
	if got := empty.SignalKeys(); len(got) != 0 {
		t.Errorf("SignalKeys() on empty = %v, want []", got)
	}
}
