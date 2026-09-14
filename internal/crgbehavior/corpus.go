package crgbehavior

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
)

// DefaultCommitCount is the default size of the review-task corpus. Every
// pinned commit costs one isolated worktree and one full pinned-release build
// at gate time, so the corpus is a sample rather than a whole window.
const DefaultCommitCount = 25

// DefaultRef is the git ref the corpus window is taken from.
const DefaultRef = "origin/master"

// DefaultCommitWindow is how far back the corpus builder SCANS. Pinning is a
// sample of this window, so the window has to be wide enough that more than
// one language appears in it.
const DefaultCommitWindow = 250

// declExtractors maps a pinned-release language label to the patterns that
// recognize a declaration line in that language.
//
// The set is keyed by the RELEASE's own language labels, so which files a
// commit contributes and which identifiers they yield are both decided by the
// release's parser coverage rather than by a hardcoded guess. Previously the
// builder recognized only the Go and Python declaration forms: a TypeScript,
// Rust, Java or Ruby commit was pinned with an EMPTY identifier list and
// silently left the FTS search surface unexercised.
//
// The leading `[+-]?` is optional because these patterns are applied to FILE
// CONTENT on both sides of a commit, not to diff lines — see FileChange.
var declExtractors = map[string][]*regexp.Regexp{
	"go": {
		regexp.MustCompile(`^[+-]?\s*(?:func|type)\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`),
	},
	"python": {
		regexp.MustCompile(`^[+-]?\s*(?:async\s+)?(?:def|class)\s+([A-Za-z_]\w*)`),
	},
	"javascript": jsDeclPatterns(),
	"typescript": jsDeclPatterns(),
	"tsx":        jsDeclPatterns(),
	"vue":        jsDeclPatterns(),
	"svelte":     jsDeclPatterns(),
	"rust": {
		regexp.MustCompile(`^[+-]?\s*(?:pub(?:\([^)]*\))?\s+)?(?:(?:async|unsafe|const|default)\s+)*(?:fn|struct|enum|trait|union|type|macro_rules!)\s+([A-Za-z_]\w*)`),
	},
	"java":   jvmDeclPatterns(),
	"kotlin": jvmDeclPatterns(),
	"scala":  jvmDeclPatterns(),
	"csharp": jvmDeclPatterns(),
	"ruby": {
		regexp.MustCompile(`^[+-]?\s*(?:def|class|module)\s+(?:self\.)?([A-Za-z_]\w*)`),
	},
	"c":   cDeclPatterns(),
	"cpp": cDeclPatterns(),
	"objc": {
		regexp.MustCompile(`^[+-]?\s*(?:@interface|@implementation|@protocol)\s+([A-Za-z_]\w*)`),
	},
	"php": {
		regexp.MustCompile(`^[+-]?\s*(?:(?:abstract|final|public|private|protected|static|readonly)\s+)*(?:function|class|interface|trait|enum)\s+&?([A-Za-z_]\w*)`),
	},
	"swift": {
		regexp.MustCompile(`^[+-]?\s*(?:(?:@\w+|public|private|internal|fileprivate|open|final|static|class|override|mutating)\s+)*(?:func|class|struct|enum|protocol|actor|extension)\s+([A-Za-z_]\w*)`),
	},
	"bash": {
		regexp.MustCompile(`^[+-]?\s*(?:function\s+)?([A-Za-z_]\w*)\s*\(\s*\)`),
	},
	"lua":  luaDeclPatterns(),
	"luau": luaDeclPatterns(),
	"elixir": {
		regexp.MustCompile(`^[+-]?\s*(?:defmodule|defmacrop?|defp?)\s+([A-Za-z_][\w.]*)`),
	},
	"zig": {
		regexp.MustCompile(`^[+-]?\s*(?:pub\s+)?(?:export\s+)?(?:fn|const)\s+([A-Za-z_]\w*)`),
	},
	"julia": {
		regexp.MustCompile(`^[+-]?\s*(?:function|struct|macro|module|abstract\s+type)\s+([A-Za-z_]\w*)`),
	},
	"dart": {
		regexp.MustCompile(`^[+-]?\s*(?:abstract\s+)?(?:class|mixin|enum|extension)\s+([A-Za-z_]\w*)`),
	},
	"solidity": {
		regexp.MustCompile(`^[+-]?\s*(?:function|contract|library|interface|struct|enum|modifier|event)\s+([A-Za-z_]\w*)`),
	},
	"powershell": {
		regexp.MustCompile(`^[+-]?\s*(?i:function|filter|class)\s+([A-Za-z_][\w-]*)`),
	},
	"perl": {
		regexp.MustCompile(`^[+-]?\s*(?:sub|package)\s+([A-Za-z_][\w:]*)`),
	},
	"gdscript": {
		regexp.MustCompile(`^[+-]?\s*(?:func|class|signal)\s+([A-Za-z_]\w*)`),
	},
	"r": {
		regexp.MustCompile(`^[+-]?\s*([A-Za-z_.][\w.]*)\s*(?:<-|=)\s*function\s*\(`),
	},
	"sql": {
		regexp.MustCompile(`(?i)^[+-]?\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMP(?:ORARY)?\s+)?(?:TABLE|VIEW|FUNCTION|PROCEDURE|TRIGGER|INDEX)\s+(?:IF\s+NOT\s+EXISTS\s+)?["` + "`" + `']?([A-Za-z_]\w*)`),
	},
	"hcl": {
		regexp.MustCompile(`^[+-]?\s*(?:resource|module|variable|output|data|provider|locals)\s+"([A-Za-z_][\w-]*)"`),
	},
	"verilog": {
		regexp.MustCompile(`^[+-]?\s*(?:module|function|task|interface|package)\s+(?:automatic\s+)?([A-Za-z_]\w*)`),
	},
	"rescript": {
		regexp.MustCompile(`^[+-]?\s*(?:let|type|module|external)\s+(?:rec\s+)?([A-Za-z_]\w*)`),
	},
	"vbnet": {
		regexp.MustCompile(`(?i)^[+-]?\s*(?:(?:public|private|protected|friend|shared|overrides)\s+)*(?:Class|Module|Structure|Interface|Enum|Sub|Function)\s+([A-Za-z_]\w*)`),
	},
}

// jsDeclPatterns are the ECMAScript-family declaration forms, including the
// `const name = (…) =>` binding that carries most modern module surface.
func jsDeclPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`^[+-]?\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)`),
		regexp.MustCompile(`^[+-]?\s*(?:export\s+)?(?:declare\s+)?(?:abstract\s+)?(?:class|interface|enum|type|namespace)\s+([A-Za-z_$][\w$]*)`),
		regexp.MustCompile(`^[+-]?\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=]+)?=\s*(?:async\s*)?(?:\([^)]*\)\s*(?::[^=]+)?=>|function\b)`),
	}
}

// jvmDeclPatterns cover the JVM-family and C# declaration forms: the
// keyword-introduced type declarations, plus the modifier-prefixed method form
// those languages use instead of a `func` keyword.
func jvmDeclPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`^[+-]?\s*(?:(?:@\w+|public|private|protected|internal|open|sealed|abstract|final|static|override|suspend|data|inline|partial|case|implicit)\s+)*(?:class|interface|enum|record|object|trait|struct|fun|def)\s+([A-Za-z_]\w*)`),
		regexp.MustCompile(`^[+-]?\s*(?:(?:@\w+|public|private|protected|internal|static|final|abstract|override|virtual|async)\s+)+[\w<>\[\],.?]+\s+([A-Za-z_]\w*)\s*\(`),
	}
}

// cDeclPatterns cover C/C++ aggregate and function declaration forms.
func cDeclPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`^[+-]?\s*(?:template\s*<[^>]*>\s*)?(?:struct|class|union|enum|namespace)\s+([A-Za-z_]\w*)`),
		regexp.MustCompile(`^[+-]?\s*(?:(?:static|inline|extern|const|constexpr|virtual|explicit|unsigned|signed)\s+)*[A-Za-z_][\w:<>,\s*&]*?\b([A-Za-z_]\w*)\s*\([^;]*\)\s*(?:const\s*)?\{?\s*$`),
	}
}

// luaDeclPatterns cover the Lua/Luau function declaration forms.
func luaDeclPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`^[+-]?\s*(?:local\s+)?function\s+([A-Za-z_][\w.:]*)`),
		regexp.MustCompile(`^[+-]?\s*local\s+([A-Za-z_]\w*)\s*=\s*function\b`),
	}
}

// ExtractorCoverage splits the release's languages into those the corpus
// builder can extract declaration identifiers for and those it cannot. The
// uncovered set is reported rather than hidden: a commit in an uncovered
// language still contributes changed files, but it can never contribute an FTS
// search input, and that has to be visible when judging surface coverage.
func ExtractorCoverage(rel Release) (covered, uncovered []string) {
	for _, lang := range rel.IndexedLanguages() {
		if len(declExtractors[lang]) > 0 {
			covered = append(covered, lang)
			continue
		}
		uncovered = append(uncovered, lang)
	}
	return covered, uncovered
}

// BuildManifest derives a review-task corpus from real repository history.
//
// It scans a WINDOW of recent non-merge commits, keeps those that touched a
// file the pinned release indexes, and pins `count` of them. Selection is
// language-coverage-first: the newest commit for every language in the window
// is pinned before the remainder is filled newest-first.
//
// That curation is load-bearing, not cosmetic. Each pinned commit costs a full
// release build at gate time, so the corpus is necessarily a sample — and a
// newest-N sample of this repository is overwhelmingly single-language, which
// is exactly how a run ends up reporting a verdict while never exercising the
// release's parser, FTS or flow behavior for any other language. The rule
// maximizes exercised surfaces; it cannot hide a divergence, because a pinned
// commit is never dropped for diverging.
func BuildManifest(repoRoot, ref string, count, window int, rel Release) (Manifest, error) {
	repo, err := openRepo(repoRoot)
	if err != nil {
		return Manifest{}, err
	}
	return buildManifest(repo, ref, count, window, rel)
}

// buildManifest is BuildManifest over an injected repository seam.
func buildManifest(r repoReader, ref string, count, window int, rel Release) (Manifest, error) {
	if ref == "" {
		ref = DefaultRef
	}
	if count <= 0 {
		count = DefaultCommitCount
	}
	if window <= 0 {
		window = defaultWindow(count)
	}
	head, err := r.Resolve(ref)
	if err != nil {
		return Manifest{}, err
	}
	hashes, err := r.Window(ref, window)
	if err != nil {
		return Manifest{}, err
	}
	candidates, err := tasksFor(r, hashes, rel)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		SchemaVersion: ManifestSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		GeneratedFrom: ref,
		Head:          head.String(),
		Window:        window,
		Release:       rel.Version,
		Tasks:         selectTasks(candidates, count),
	}, nil
}

// defaultWindow scans well past the target corpus size so language coverage
// has something to select from.
func defaultWindow(count int) int {
	if w := count * 10; w > DefaultCommitWindow {
		return w
	}
	return DefaultCommitWindow
}

// selectTasks pins count tasks, newest-first, after first reserving the newest
// task for each language the window covers. The result keeps the window's
// newest-first order so the pinned corpus reads chronologically.
func selectTasks(candidates []Task, count int) []Task {
	if count >= len(candidates) {
		return candidates
	}
	pinned := make(map[int]bool, count)
	seen := map[string]bool{}
	for i, t := range candidates {
		if len(pinned) >= count {
			break
		}
		if introducesLanguage(t, seen) {
			pinned[i] = true
		}
	}
	for i := range candidates {
		if len(pinned) >= count {
			break
		}
		pinned[i] = true
	}
	out := make([]Task, 0, len(pinned))
	for i, t := range candidates {
		if pinned[i] {
			out = append(out, t)
		}
	}
	return out
}

// introducesLanguage reports whether a task is the first to cover one of its
// languages, marking every language it covers as seen.
func introducesLanguage(t Task, seen map[string]bool) bool {
	fresh := false
	for _, lang := range t.Languages {
		if !seen[lang] {
			seen[lang] = true
			fresh = true
		}
	}
	return fresh
}

// tasksFor builds one review task per commit, dropping commits that touched no
// file the release indexes. A corpus that can never supply an FTS search input
// is rejected outright: it would leave a required surface permanently
// unexercised, and that is a corpus defect, not a gate result.
func tasksFor(r repoReader, hashes []plumbing.Hash, rel Release) ([]Task, error) {
	tasks := make([]Task, 0, len(hashes))
	withIdentifiers := 0
	for _, hash := range hashes {
		task, ok, err := taskFor(r, hash, rel)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if len(task.Identifiers) > 0 {
			withIdentifiers++
		}
		tasks = append(tasks, task)
	}
	switch {
	case len(tasks) == 0:
		return nil, fmt.Errorf("crgbehavior: no commit in the window touched a file %s %s indexes",
			PackageName, rel.Version)
	case withIdentifiers == 0:
		_, uncovered := ExtractorCoverage(rel)
		return nil, fmt.Errorf("crgbehavior: no commit in the window changed a declaration the builder can extract "+
			"(languages without an extractor: %s) — the corpus could never exercise the %s surface",
			strings.Join(uncovered, ", "), SurfaceFTSSearch)
	}
	return tasks, nil
}

// taskFor builds the review task for one commit: its release-indexed changed
// files, the release's language labels for them, and the declarations the
// commit added or removed. ok is false when the commit touched no file the
// release indexes.
func taskFor(r repoReader, hash plumbing.Hash, rel Release) (Task, bool, error) {
	changes, err := r.Changes(hash)
	if err != nil {
		return Task{}, false, err
	}
	files, languages, identifiers := scanChanges(changes, rel)
	if len(files) == 0 {
		return Task{}, false, nil
	}
	subject, err := r.Subject(hash)
	if err != nil {
		return Task{}, false, err
	}
	return Task{
		Commit:       hash.String(),
		Subject:      subject,
		ChangedFiles: files,
		Identifiers:  identifiers,
		Languages:    languages,
	}, true, nil
}

// scanChanges keeps the release-indexed changed files and derives each one's
// added-or-removed declarations with the extractor for ITS OWN language.
func scanChanges(changes []FileChange, rel Release) (files, languages, identifiers []string) {
	fileSet, langSet, identSet := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, change := range changes {
		language := rel.LanguageOf(change.Path)
		if language == "" {
			continue
		}
		fileSet[change.Path], langSet[language] = true, true
		for _, name := range changedDeclarations(change, language) {
			identSet[name] = true
		}
	}
	return sortedSet(fileSet), sortedSet(langSet), sortedSet(identSet)
}

// changedDeclarations is the symmetric difference of the declaration sets on
// the two sides of one changed file — exactly "the declarations this commit
// added or removed", computed from content rather than from diff hunks.
func changedDeclarations(change FileChange, language string) []string {
	before, after := declaredNames(change.Before, language), declaredNames(change.After, language)
	changed := map[string]bool{}
	for name := range after {
		if !before[name] {
			changed[name] = true
		}
	}
	for name := range before {
		if !after[name] {
			changed[name] = true
		}
	}
	return sortedSet(changed)
}

// declaredNames returns every identifier the lines declare in language.
func declaredNames(lines []string, language string) map[string]bool {
	patterns := declExtractors[language]
	if len(patterns) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, line := range lines {
		for _, re := range patterns {
			if m := re.FindStringSubmatch(line); m != nil {
				out[m[1]] = true
			}
		}
	}
	return out
}
