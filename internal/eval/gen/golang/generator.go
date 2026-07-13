// Package gogen is the Go-language adapter of the shared R4 task generator. It
// supplies [Profile] describing Go's file conventions, verification commands,
// and prompt fragments; every generation control-flow decision lives in
// internal/eval/gen/gencore. Wire a Go generator with
// gencore.Register(reg, querier, gogen.Profile) (or gencore.New for callers
// that manage registration themselves).
package gogen

import (
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/gen/gencore"
)

// Verification command tokens for Go.
const (
	goCmd    = "go"
	buildSub = "build"
	testSub  = "test"
	raceFlag = "-race"
)

// Profile is the Go language profile for the shared generator engine.
var Profile = gencore.Profile{
	Language:           eval.LanguageGo,
	IDToken:            "go",
	ErrPrefix:          "gogen",
	DisplayName:        "Go",
	NoTestEditFragment: "- Do not modify any existing *_test.go file.\n",
	MustSatisfyCmd:     "go test -race",
	TestFileNoun:       "package",
	TestFilePath:       testFilePath,
	VerifyTarget:       pkgPattern,
	BuildCmd:           buildCmd,
	TestCmd:            testCmd,
}

// testFilePath maps a Go implementation file to its conventional test file in
// the same package (e.g. "pkg/foo/foo.go" → "pkg/foo/foo_test.go").
func testFilePath(implPath string) string {
	return strings.TrimSuffix(implPath, ".go") + "_test.go"
}

// pkgPattern returns the Go package pattern for the directory containing
// implPath (e.g. "internal/eval/foo.go" → "./internal/eval/...").
func pkgPattern(implPath string) string {
	dir := gencore.SlashDir(implPath)
	if dir == "." {
		return "./..."
	}
	return "./" + dir + "/..."
}

// buildCmd is the Go build command scoped to implPath's package.
func buildCmd(implPath string) []string {
	return []string{goCmd, buildSub, pkgPattern(implPath)}
}

// testCmd is the race-enabled Go test command scoped to implPath's package.
func testCmd(implPath string) []string {
	return []string{goCmd, testSub, raceFlag, pkgPattern(implPath)}
}
