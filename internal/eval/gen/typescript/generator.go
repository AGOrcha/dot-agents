// Package tsgen is the TypeScript-language adapter of the shared R4 task
// generator. It supplies a gencore.Profile describing TypeScript's file
// conventions and verification commands (transpile via `tsc --noEmit`, run
// tests via the Node.js built-in runner `node --test`); every generation
// control-flow decision lives in internal/eval/gen/gencore. Wire a TypeScript
// generator with gencore.Register(reg, querier, tsgen.Profile) (or gencore.New
// for callers that manage registration themselves).
package tsgen

import (
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/gen/gencore"
)

// Verification command tokens and file-naming fragments for TypeScript.
const (
	tscCmd      = "tsc"
	noEmitFlag  = "--noEmit"
	nodeCmd     = "node"
	testFlag    = "--test"
	tsExtension = ".ts"
	testInfix   = ".test"
)

// Profile is the TypeScript language profile for the shared generator engine.
var Profile = gencore.Profile{
	Language:           eval.LanguageTypeScript,
	IDToken:            "ts",
	ErrPrefix:          "tsgen",
	DisplayName:        "TypeScript",
	NoTestEditFragment: "- Do not modify any existing *.test.ts file.\n",
	MustSatisfyCmd:     "node --test",
	TestFileNoun:       "directory",
	TestFilePath:       testFilePath,
	VerifyTarget:       testGlobPattern,
	BuildCmd:           buildCmd,
	TestCmd:            testCmd,
}

// testFilePath maps a TypeScript implementation file to its conventional test
// file in the same directory (e.g. "src/foo/foo.ts" → "src/foo/foo.test.ts").
func testFilePath(implPath string) string {
	return strings.TrimSuffix(implPath, tsExtension) + testInfix + tsExtension
}

// testGlobPattern returns a glob for test files in implPath's directory
// (e.g. "src/foo/foo.ts" → "src/foo/*.test.ts"). This is the pattern passed to
// `node --test` to run all tests in the seed's source dir.
func testGlobPattern(implPath string) string {
	dir := gencore.SlashDir(implPath)
	if dir == "." {
		return "*" + testInfix + tsExtension
	}
	return dir + "/*" + testInfix + tsExtension
}

// buildCmd type-checks the project without emitting output; it is independent
// of implPath.
func buildCmd(string) []string {
	return []string{tscCmd, noEmitFlag}
}

// testCmd runs the tests in implPath's directory via the Node.js test runner.
func testCmd(implPath string) []string {
	return []string{nodeCmd, testFlag, testGlobPattern(implPath)}
}
