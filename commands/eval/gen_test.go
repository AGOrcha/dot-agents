package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"
)

// runGen over the fixture graph writes a valid, parseable Go TaskSpec to stdout.
func TestRunGenToStdout(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	var buf bytes.Buffer
	if err := runGen(context.Background(), &buf, genOptions{language: "go"}, false); err != nil {
		t.Fatalf("runGen: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "language: go") {
		t.Errorf("output missing language line:\n%s", out)
	}
	spec, err := evalcore.ParseTaskSpec([]byte(out))
	if err != nil {
		t.Fatalf("emitted spec does not round-trip through ParseTaskSpec: %v\n%s", err, out)
	}
	if spec.Language != evalcore.LanguageGo {
		t.Errorf("spec language = %q, want go", spec.Language)
	}
}

// With --json set, gen emits structured JSON (snake_case keys, parseable as
// JSON) instead of YAML.
func TestRunGenJSONToStdout(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	var buf bytes.Buffer
	if err := runGen(context.Background(), &buf, genOptions{language: "go"}, true); err != nil {
		t.Fatalf("runGen json: %v", err)
	}
	out := buf.String()
	// The YAML marker line must NOT appear; the JSON must decode and carry the
	// same snake_case field contract the YAML form uses.
	if strings.Contains(out, "language: go") {
		t.Errorf("json output leaked YAML formatting:\n%s", out)
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(out), &generic); err != nil {
		t.Fatalf("gen --json output is not valid JSON: %v\n%s", err, out)
	}
	if generic["language"] != "go" {
		t.Errorf("json language = %v, want go\n%s", generic["language"], out)
	}
	if _, ok := generic["task_spec_version"]; !ok {
		t.Errorf("json output missing snake_case task_spec_version key:\n%s", out)
	}
}

// --json --out writes JSON (not YAML) to the file.
func TestRunGenJSONToFile(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	out := filepath.Join(t.TempDir(), "task.json")
	var buf bytes.Buffer
	if err := runGen(context.Background(), &buf, genOptions{language: "go", out: out}, true); err != nil {
		t.Fatalf("runGen json --out: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("written --out file is not valid JSON: %v\n%s", err, data)
	}
	if generic["language"] != "go" {
		t.Errorf("written json language = %v, want go", generic["language"])
	}
}

// --out writes the spec atomically and prints a confirmation.
func TestRunGenToFile(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	out := filepath.Join(t.TempDir(), "task.yaml")
	var buf bytes.Buffer
	if err := runGen(context.Background(), &buf, genOptions{language: "go", out: out}, false); err != nil {
		t.Fatalf("runGen: %v", err)
	}
	if !strings.Contains(buf.String(), "Wrote TaskSpec") {
		t.Errorf("missing confirmation line: %q", buf.String())
	}
	mustExist(t, out)
}

func TestRunGenInvalidLanguage(t *testing.T) {
	if err := runGen(context.Background(), &bytes.Buffer{}, genOptions{language: ""}, false); err == nil {
		t.Fatal("empty language should error before opening the graph")
	}
}

// An invalid --difficulty is rejected up front with a clear "invalid difficulty"
// message — before any graph work — rather than surfacing as a "no seed matches
// difficulty" filter miss deep in the generator.
func TestRunGenInvalidDifficulty(t *testing.T) {
	// No openReader swap: validation must happen before the graph is opened, so
	// the production seam is never reached.
	err := runGen(context.Background(), &bytes.Buffer{}, genOptions{language: "go", difficulty: "bogus"}, false)
	if err == nil {
		t.Fatal("invalid difficulty should error before opening the graph")
	}
	if !strings.Contains(err.Error(), `invalid difficulty "bogus"`) ||
		!strings.Contains(err.Error(), "easy, medium, or hard") {
		t.Errorf("difficulty error not actionable: %v", err)
	}
}

// yamlToJSON surfaces a decode error for input that is not a YAML mapping,
// covering the conversion's failure path directly (the happy path is exercised
// by the gen --json tests above).
func TestYamlToJSONDecodeError(t *testing.T) {
	if _, err := yamlToJSON([]byte("::: not a mapping :::")); err == nil {
		t.Fatal("yamlToJSON should surface a YAML decode error for non-mapping input")
	}
}

func TestRunGenRegistryOpenError(t *testing.T) {
	swapOpenReader(t, func() (graphstore.CodeGraphReader, func() error, error) {
		return fixtureReader(), nil, errFixture
	})
	if err := runGen(context.Background(), &bytes.Buffer{}, genOptions{language: "go"}, false); err == nil {
		t.Fatal("runGen should surface the registry open error")
	}
}

// A language with no seeds in the fixture graph fails at the generate stage.
func TestRunGenNoSeedsForLanguage(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	if err := runGen(context.Background(), &bytes.Buffer{}, genOptions{language: "python"}, false); err == nil {
		t.Fatal("python over a go-only fixture should fail to find a seed")
	}
}

// generateAndWrite against a registry missing the language exercises the
// Lookup-miss branch directly.
func TestGenerateAndWriteNoGeneratorForLanguage(t *testing.T) {
	reg := evalcore.NewRegistry()
	err := generateAndWrite(context.Background(), &bytes.Buffer{}, reg, genOptions{language: "go"}, false)
	if err == nil || !strings.Contains(err.Error(), "no generator") {
		t.Fatalf("want no-generator error, got %v", err)
	}
}

// A write to a path whose parent dir does not exist fails in emitSpec.
func TestGenerateAndWriteFileError(t *testing.T) {
	reg, err := buildRegistry(fixtureReader())
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	badOut := filepath.Join(t.TempDir(), "missing-subdir", "task.yaml")
	err = generateAndWrite(context.Background(), &bytes.Buffer{}, reg, genOptions{language: "go", out: badOut}, false)
	if err == nil {
		t.Fatal("write into a nonexistent dir should error")
	}
}

// The --out confirmation write is not swallowed: the spec file is written, but a
// failing confirmation writer surfaces as an error.
func TestGenerateAndWriteConfirmError(t *testing.T) {
	reg, err := buildRegistry(fixtureReader())
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	out := filepath.Join(t.TempDir(), "task.yaml")
	err = generateAndWrite(context.Background(), failWriter{}, reg, genOptions{language: "go", out: out}, false)
	if err == nil {
		t.Fatal("confirmation-write failure should surface")
	}
	// The spec file itself was still written atomically before the confirmation.
	mustExist(t, out)
}

// Driving the assembled command through Execute with the real RunGen entry
// point covers RunGen + genOptionsFrom + flag wiring end-to-end.
func TestGenCommandExecute(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	cmd := newGenCmd(func(c *cobra.Command, _ []string) error { return RunGen(c, false) })
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--language", "go", "--difficulty", "easy", "--template", "impl-pure-fn"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gen Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "task_spec_version") {
		t.Errorf("gen Execute output missing spec body:\n%s", buf.String())
	}
}
