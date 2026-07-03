// Package gencore is the shared engine of the R4 eval task generator. Every
// per-language generator (internal/eval/gen/{golang,typescript,python}) is a
// thin adapter that supplies a [Profile] and delegates all control flow here:
// template selection, KG seed search, difficulty derivation, spec assembly,
// and prompt rendering live in this package exactly once.
//
// The engine synthesises versioned eval.TaskSpecs from the Tree-sitter
// knowledge graph via three templates:
//
//   - impl-pure-fn: implement a function so existing tests pass
//   - refactor-extract: extract helpers from a complex function
//   - add-test-coverage: write tests for an uncovered function
//
// A [Profile] describes only what genuinely varies between languages (the
// eval.Language, task-ID token, file/test naming, verification commands, and
// the language-specific prompt fragments). Adding a language is a new Profile
// plus a two-line constructor wrapper, not another copy of the engine.
package gencore

import (
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/eval"
)

// Profile carries everything that varies between the per-language generators.
// The shared [Generator] owns all generation control flow; a language package
// supplies a Profile plus thin New/Register wrappers around [New]/[Register].
type Profile struct {
	// Language is the eval.Language this profile produces tasks for.
	Language eval.Language
	// IDToken is the short language token embedded in a task ID, e.g. "go"
	// yields task IDs like "kg-go-impl-...".
	IDToken string
	// ErrPrefix prefixes this generator's error messages, e.g. "gogen".
	ErrPrefix string
	// DisplayName is the human language name used in the "no <name> symbols
	// found in graph" diagnostic, e.g. "Go", "TypeScript", "Python".
	DisplayName string

	// NoTestEditFragment is the constraint line (with its trailing newline)
	// forbidding edits to existing test files. The glob is language-specific,
	// e.g. "- Do not modify any existing *_test.go file.\n".
	NoTestEditFragment string
	// MustSatisfyCmd is the verification command shown in a prompt's
	// "must satisfy" line, minus its target argument, e.g. "go test -race".
	MustSatisfyCmd string
	// TestFileNoun names where the agent writes new tests in the
	// add-test-coverage prompt: "package" for Go/Python, "directory" for TS.
	TestFileNoun string

	// TestFilePath maps an implementation file to its conventional test file,
	// e.g. "pkg/foo/foo.go" -> "pkg/foo/foo_test.go".
	TestFilePath func(implPath string) string
	// VerifyTarget derives the argument shared by a prompt's "must satisfy"
	// line and TestCmd: a Go package pattern, TS test glob, or Python dir.
	VerifyTarget func(implPath string) string
	// BuildCmd builds the language's build/type-check command for implPath.
	BuildCmd func(implPath string) []string
	// TestCmd builds the language's test command for implPath.
	TestCmd func(implPath string) []string
}

// SlashDir returns the slash-normalised directory of implPath, collapsing the
// root and empty cases to ".". It is the shared path primitive each language
// profile's path derivations (package pattern, test glob, pytest dir) build on,
// so that normalisation lives here once rather than in every adapter.
func SlashDir(implPath string) string {
	dir := filepath.ToSlash(filepath.Dir(implPath))
	if dir == "" || dir == "." {
		return "."
	}
	return dir
}
