package home

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// kg-ideate conformance surface. These consts are hoisted so the repeated
// path segments and frontmatter keys are not duplicated string literals
// (Sonar S1192) across the resolver helpers below.
const (
	kgIdeateRel     = "skills/global/kg-ideate"
	skillFileName   = "SKILL.md"
	instructionsSeg = "instructions"
	mdExt           = ".md"
	tierKey         = "tier:"
	callsKey        = "calls:"
	tierCompound    = "compound"
	frontmatterMark = "---"
)

// loadRefPattern captures the backtick-quoted `instructions/<x>.md` or
// `templates/<y>.md` path in a `Load → ...` directive. The rendered arrow is
// the Unicode U+2192; whitespace around it is tolerated.
var loadRefPattern = regexp.MustCompile("Load\\s*→\\s*`((?:instructions|templates)/[^`]+)`")

// TestKGIdeateCallsAndLoadRefsResolve asserts the whole shipped kg-ideate
// tree (the compound + its four molecules) has ZERO dangling references:
//
//   - every frontmatter `calls:` entry resolves — a compound call to a sibling
//     molecule's SKILL.md, a molecule call to its `instructions/<call>.md`;
//   - every `Load → `+"`"+`instructions/X.md`+"`"+` / `+"`"+`templates/Y.md`+"`"+`
//     directive in any SKILL.md or instructions/*.md resolves under its skill dir.
//
// Materializes the shipped starter via CopyMissingStarterAssets (mirroring
// copy_test.go) so a path change in the embedded tree is exercised, and
// carries the same loud sentinel: if the walk finds no SKILL.md the test
// fails rather than silently passing.
func TestKGIdeateCallsAndLoadRefsResolve(t *testing.T) {
	tmp := t.TempDir()
	if err := CopyMissingStarterAssets(tmp); err != nil {
		t.Fatalf("CopyMissingStarterAssets: %v", err)
	}
	root := filepath.Join(tmp, filepath.FromSlash(kgIdeateRel))

	skillFiles, loadRefFiles, err := collectKGIdeateFiles(root)
	if err != nil {
		t.Fatalf("walk kg-ideate tree: %v", err)
	}
	if len(skillFiles) == 0 {
		// Sentinel (mirrors TestStarterVerifierSurfaceCrossReference): a path
		// change to the embedded tree must not silently no-op this test.
		t.Fatal("no SKILL.md found under kg-ideate; embedded path may have changed")
	}

	failures := checkAllCalls(skillFiles)
	failures = append(failures, checkAllLoadRefs(loadRefFiles)...)
	if len(failures) > 0 {
		t.Fatalf("kg-ideate has %d dangling call/ref(s):\n  - %s",
			len(failures), strings.Join(failures, "\n  - "))
	}
}

// collectKGIdeateFiles walks root once and returns the SKILL.md paths (which
// carry frontmatter calls) and the superset of files that may carry Load →
// directives (SKILL.md + instructions/*.md).
func collectKGIdeateFiles(root string) (skillFiles, loadRefFiles []string, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == skillFileName {
			skillFiles = append(skillFiles, path)
			loadRefFiles = append(loadRefFiles, path)
			return nil
		}
		if isInstructionFile(path) {
			loadRefFiles = append(loadRefFiles, path)
		}
		return nil
	})
	return skillFiles, loadRefFiles, err
}

// isInstructionFile reports whether path is a markdown file directly under an
// `instructions/` directory.
func isInstructionFile(path string) bool {
	return filepath.Base(filepath.Dir(path)) == instructionsSeg && strings.HasSuffix(path, mdExt)
}

// checkAllCalls resolves every frontmatter `calls:` entry for each SKILL.md and
// returns a "<skill>: dangling call ..." line for each miss.
func checkAllCalls(skillFiles []string) []string {
	var failures []string
	for _, sf := range skillFiles {
		text, err := os.ReadFile(sf)
		if err != nil {
			failures = append(failures, describeReadErr(sf, err))
			continue
		}
		skillDir := filepath.Dir(sf)
		name := filepath.Base(skillDir)
		tier, calls := parseFrontmatterTierAndCalls(extractFrontmatter(string(text)))
		for _, call := range calls {
			if target, ok := resolveCall(skillDir, tier, call); !ok {
				failures = append(failures, fmt.Sprintf(
					"%s: dangling call %q (expected %s)", name, call, target))
			}
		}
	}
	return failures
}

// checkAllLoadRefs resolves every `Load → ...` reference in each candidate file
// and returns a "<skill>: dangling ref ..." line for each miss.
func checkAllLoadRefs(loadRefFiles []string) []string {
	var failures []string
	for _, f := range loadRefFiles {
		text, err := os.ReadFile(f)
		if err != nil {
			failures = append(failures, describeReadErr(f, err))
			continue
		}
		skillDir := skillDirForFile(f)
		name := filepath.Base(skillDir)
		for _, ref := range collectLoadRefs(string(text)) {
			if !refResolves(skillDir, ref) {
				failures = append(failures, fmt.Sprintf(
					"%s: dangling Load ref %q", name, ref))
			}
		}
	}
	return failures
}

// extractFrontmatter returns the text between the first two `---` fences, or
// "" if there is no closing fence.
func extractFrontmatter(text string) string {
	start := -1
	var fm []string
	for i, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == frontmatterMark {
			if start == -1 {
				start = i
				continue
			}
			return strings.Join(fm, "\n")
		}
		if start != -1 {
			fm = append(fm, line)
		}
	}
	return ""
}

// parseFrontmatterTierAndCalls does a line-based parse of a skill's frontmatter,
// returning its `tier:` value and the `calls:` block entries. The calls block
// runs from the `calls:` key until the next top-level key.
func parseFrontmatterTierAndCalls(fm string) (string, []string) {
	var tier string
	var calls []string
	inCalls := false
	for _, line := range strings.Split(fm, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, tierKey) {
			tier = strings.TrimSpace(strings.TrimPrefix(trimmed, tierKey))
			continue
		}
		if trimmed == callsKey {
			inCalls = true
			continue
		}
		if inCalls && strings.HasPrefix(trimmed, "- ") {
			calls = append(calls, strings.TrimSpace(trimmed[2:]))
			continue
		}
		inCalls = false
	}
	return tier, calls
}

// resolveCall returns the expected target path for a call and whether it exists.
// A compound call targets a sibling molecule's SKILL.md; a molecule call targets
// its own `instructions/<call>.md`.
func resolveCall(skillDir, tier, call string) (string, bool) {
	var target string
	if tier == tierCompound {
		target = filepath.Join(skillDir, call, skillFileName)
	} else {
		target = filepath.Join(skillDir, instructionsSeg, call+mdExt)
	}
	_, err := os.Stat(target)
	return target, err == nil
}

// collectLoadRefs returns every backtick-quoted Load → ref (slash-relative to
// the skill dir) found in text.
func collectLoadRefs(text string) []string {
	var refs []string
	for _, m := range loadRefPattern.FindAllStringSubmatch(text, -1) {
		refs = append(refs, m[1])
	}
	return refs
}

// refResolves reports whether a slash-relative Load ref exists under skillDir.
func refResolves(skillDir, ref string) bool {
	_, err := os.Stat(filepath.Join(skillDir, filepath.FromSlash(ref)))
	return err == nil
}

// skillDirForFile returns the owning skill directory for a walked file: the
// file's own directory for a SKILL.md, or the parent of `instructions/` for an
// instruction file (Load refs there are relative to the skill dir, not the
// instructions dir).
func skillDirForFile(path string) string {
	dir := filepath.Dir(path)
	if filepath.Base(dir) == instructionsSeg {
		return filepath.Dir(dir)
	}
	return dir
}

// describeReadErr formats a read failure keyed by the file's base name.
func describeReadErr(path string, err error) string {
	return fmt.Sprintf("%s: read error: %v", filepath.Base(path), err)
}
