package eval

import (
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// CurrentTaskSpecVersion is the TaskSpec schema version this build produces.
// v1 is the initial schema (R4 spec decision D4.5).
const CurrentTaskSpecVersion = 1

// Language identifies the programming language a task targets. Per R4
// decision D4.3 the v1 harness covers Go, Python, and TypeScript; the type is
// a string so a future language is an additive constant, not a breaking
// change.
type Language string

// Supported v1 languages.
const (
	LanguageGo         Language = "go"
	LanguagePython     Language = "python"
	LanguageTypeScript Language = "typescript"
)

// Valid reports whether l is a recognized v1 language.
func (l Language) Valid() bool {
	switch l {
	case LanguageGo, LanguagePython, LanguageTypeScript:
		return true
	default:
		return false
	}
}

// Difficulty is the reproducible, KG-derived difficulty band of a task. The
// band is computed downstream from difficulty signals (node/edge counts, a
// cyclomatic-complexity proxy) so re-running the generator on the same KG
// state yields the same band (R4 requirement R2).
type Difficulty string

// Difficulty bands.
const (
	DifficultyEasy   Difficulty = "easy"
	DifficultyMedium Difficulty = "medium"
	DifficultyHard   Difficulty = "hard"
)

// Valid reports whether d is a recognized difficulty band.
func (d Difficulty) Valid() bool {
	switch d {
	case DifficultyEasy, DifficultyMedium, DifficultyHard:
		return true
	default:
		return false
	}
}

// GeneratedKind names the provenance of a task. v1 generates from the
// Tree-sitter knowledge graph (KindKGTemplate); KindBenchmarkSeed is reserved
// for the v2 benchmark-seed adapter that emits the same TaskSpec shape.
type GeneratedKind string

// Generation provenance kinds.
const (
	KindKGTemplate    GeneratedKind = "kg_template"
	KindBenchmarkSeed GeneratedKind = "benchmark_seed"
)

// Valid reports whether k is a recognized generation kind.
func (k GeneratedKind) Valid() bool {
	switch k {
	case KindKGTemplate, KindBenchmarkSeed:
		return true
	default:
		return false
	}
}

// KGQuery records the knowledge-graph query a kg_template task was framed
// around. It is metadata only — this package issues no queries.
type KGQuery struct {
	Intent     string `yaml:"intent,omitempty"`
	SeedSymbol string `yaml:"seed_symbol,omitempty"`
}

// GeneratedFrom records how a task was produced so a run is reproducible and
// auditable (R4 requirement R10).
type GeneratedFrom struct {
	Kind       GeneratedKind `yaml:"kind"`
	TemplateID string        `yaml:"template_id,omitempty"`
	KGQuery    *KGQuery      `yaml:"kg_query,omitempty"`
}

// SolutionArtifact names a file the task expects to exist or be modified and
// its role (e.g. "target").
type SolutionArtifact struct {
	Path string `yaml:"path"`
	Role string `yaml:"role,omitempty"`
}

// Verification holds the commands the harness runs after the agent finishes.
// The test command is hidden from the agent (R4 decision D4.7); these fields
// are data only — this package executes nothing.
type Verification struct {
	BuildCmd       []string `yaml:"build_cmd,omitempty"`
	TestCmd        []string `yaml:"test_cmd"`
	TimeoutSeconds int      `yaml:"timeout_seconds,omitempty"`
}

// TaskSpec is the versioned, language-agnostic description of a single
// evaluable programming task. It is the central contract of the R4 harness:
// generators produce it, sandboxes provision against it, verifiers consume
// its verification commands, and the scoring bridge records it.
//
// TaskSpec round-trips through YAML via the canonical field tags so the
// on-disk sidecar (.agents/eval/runs/<run-id>/taskspec.yaml) matches the
// in-memory shape exactly.
type TaskSpec struct {
	TaskSpecVersion   int                `yaml:"task_spec_version"`
	TaskID            string             `yaml:"task_id"`
	Language          Language           `yaml:"language"`
	Difficulty        Difficulty         `yaml:"difficulty"`
	DifficultySignals map[string]int     `yaml:"difficulty_signals,omitempty"`
	GeneratedFrom     GeneratedFrom      `yaml:"generated_from"`
	Prompt            string             `yaml:"prompt"`
	SolutionArtifacts []SolutionArtifact `yaml:"solution_artifacts,omitempty"`
	Verification      Verification       `yaml:"verification"`
}

// Validate checks structural invariants the harness depends on. It does not
// validate that referenced files or symbols exist — that is a downstream,
// I/O-bearing concern.
func (t *TaskSpec) Validate() error {
	if t.TaskSpecVersion != CurrentTaskSpecVersion {
		return fmt.Errorf("eval: unsupported task_spec_version %d (this build produces v%d)", t.TaskSpecVersion, CurrentTaskSpecVersion)
	}
	if strings.TrimSpace(t.TaskID) == "" {
		return fmt.Errorf("eval: task_id is required")
	}
	if !t.Language.Valid() {
		return fmt.Errorf("eval: invalid language %q", t.Language)
	}
	if !t.Difficulty.Valid() {
		return fmt.Errorf("eval: invalid difficulty %q", t.Difficulty)
	}
	if !t.GeneratedFrom.Kind.Valid() {
		return fmt.Errorf("eval: invalid generated_from.kind %q", t.GeneratedFrom.Kind)
	}
	if strings.TrimSpace(t.Prompt) == "" {
		return fmt.Errorf("eval: prompt is required")
	}
	if len(t.Verification.TestCmd) == 0 {
		return fmt.Errorf("eval: verification.test_cmd is required")
	}
	if t.Verification.TimeoutSeconds < 0 {
		return fmt.Errorf("eval: verification.timeout_seconds must be non-negative, got %d", t.Verification.TimeoutSeconds)
	}
	return nil
}

// MarshalYAML serializes the spec to canonical YAML bytes. Map keys in
// difficulty_signals are emitted in sorted order so the same spec always
// produces byte-identical output (reproducibility, R4 requirement R2/R10).
func (t *TaskSpec) MarshalYAML() ([]byte, error) {
	return yaml.Marshal(t)
}

// ParseTaskSpec decodes a TaskSpec from YAML bytes and validates it. Strict
// decoding rejects unknown fields so a stale-version sidecar cannot be
// silently misread.
func ParseTaskSpec(data []byte) (*TaskSpec, error) {
	var spec TaskSpec
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("eval: decode task spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &spec, nil
}

// SignalKeys returns the difficulty-signal keys in sorted order. It is a
// convenience for callers (verifier, dashboard) that need stable iteration.
func (t *TaskSpec) SignalKeys() []string {
	keys := make([]string, 0, len(t.DifficultySignals))
	for k := range t.DifficultySignals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
