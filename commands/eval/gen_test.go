package eval

import (
	"bytes"
	"context"
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
	if err := runGen(context.Background(), &buf, genOptions{language: "go"}); err != nil {
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

// --out writes the spec atomically and prints a confirmation.
func TestRunGenToFile(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	out := filepath.Join(t.TempDir(), "task.yaml")
	var buf bytes.Buffer
	if err := runGen(context.Background(), &buf, genOptions{language: "go", out: out}); err != nil {
		t.Fatalf("runGen: %v", err)
	}
	if !strings.Contains(buf.String(), "Wrote TaskSpec") {
		t.Errorf("missing confirmation line: %q", buf.String())
	}
	mustExist(t, out)
}

func TestRunGenInvalidLanguage(t *testing.T) {
	if err := runGen(context.Background(), &bytes.Buffer{}, genOptions{language: ""}); err == nil {
		t.Fatal("empty language should error before opening the graph")
	}
}

func TestRunGenRegistryOpenError(t *testing.T) {
	swapOpenReader(t, func() (graphstore.CodeGraphReader, func() error, error) {
		return fixtureReader(), nil, errFixture
	})
	if err := runGen(context.Background(), &bytes.Buffer{}, genOptions{language: "go"}); err == nil {
		t.Fatal("runGen should surface the registry open error")
	}
}

// A language with no seeds in the fixture graph fails at the generate stage.
func TestRunGenNoSeedsForLanguage(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	if err := runGen(context.Background(), &bytes.Buffer{}, genOptions{language: "python"}); err == nil {
		t.Fatal("python over a go-only fixture should fail to find a seed")
	}
}

// generateAndWrite against a registry missing the language exercises the
// Lookup-miss branch directly.
func TestGenerateAndWriteNoGeneratorForLanguage(t *testing.T) {
	reg := evalcore.NewRegistry()
	err := generateAndWrite(context.Background(), &bytes.Buffer{}, reg, genOptions{language: "go"})
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
	err = generateAndWrite(context.Background(), &bytes.Buffer{}, reg, genOptions{language: "go", out: badOut})
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
	err = generateAndWrite(context.Background(), failWriter{}, reg, genOptions{language: "go", out: out})
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
	cmd := newGenCmd(func(c *cobra.Command, _ []string) error { return RunGen(c) })
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
