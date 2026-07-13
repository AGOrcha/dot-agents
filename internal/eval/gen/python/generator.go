// Package pygen is the Python-language adapter of the shared R4 task generator.
// It supplies a gencore.Profile describing Python's file conventions and
// verification commands (byte-compile via `python -m py_compile`, run tests via
// `python -m pytest`); every generation control-flow decision lives in
// internal/eval/gen/gencore. Wire a Python generator with
// gencore.Register(reg, querier, pygen.Profile) (or gencore.New for callers
// that manage registration themselves).
package pygen

import (
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/gen/gencore"
)

// Verification command tokens and file-naming fragments for Python.
const (
	pythonCmd   = "python"
	moduleFlag  = "-m"
	pyCompile   = "py_compile"
	pytestPkg   = "pytest"
	verboseFlag = "-v"
	pyExtension = ".py"
)

// Profile is the Python language profile for the shared generator engine.
var Profile = gencore.Profile{
	Language:           eval.LanguagePython,
	IDToken:            "py",
	ErrPrefix:          "pygen",
	DisplayName:        "Python",
	NoTestEditFragment: "- Do not modify any existing test_*.py file.\n",
	MustSatisfyCmd:     "python -m pytest -v",
	TestFileNoun:       "package",
	TestFilePath:       testFilePath,
	VerifyTarget:       gencore.SlashDir,
	BuildCmd:           buildCmd,
	TestCmd:            testCmd,
}

// testFilePath maps a Python implementation file to its conventional pytest
// test file in the same directory (e.g. "pkg/foo/utils.py" → "pkg/foo/test_utils.py").
func testFilePath(implPath string) string {
	name := strings.TrimSuffix(filepath.Base(implPath), pyExtension)
	testBase := "test_" + name + pyExtension
	dir := gencore.SlashDir(implPath)
	if dir == "." {
		return testBase
	}
	return dir + "/" + testBase
}

// buildCmd byte-compiles the implementation file to catch syntax errors.
func buildCmd(implPath string) []string {
	return []string{pythonCmd, moduleFlag, pyCompile, implPath}
}

// testCmd runs pytest against implPath's directory.
func testCmd(implPath string) []string {
	return []string{pythonCmd, moduleFlag, pytestPkg, verboseFlag, gencore.SlashDir(implPath)}
}
