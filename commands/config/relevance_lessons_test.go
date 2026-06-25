package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const (
	lessonTestCommandsConfig = "commands/config"
	lessonTestConfigSpecific = "config-specific"
	lessonTestConfigGeneral  = "config-general"
	lessonTestConfigTag      = "config"
	lessonTestDocsConfig     = "docs-config"
	lessonTestDocsPath       = "docs/CONFIG_RELEVANCE.md"
	lessonTestFallback       = "fallback"
	lessonTestGoCLI          = "go-cli"
	lessonTestIdeation       = "ideation"
	lessonTestIdeationOnly   = "ideation-only"
	lessonTestPackageConfig  = "pkg-config"
	lessonTestPlan           = "lesson-plan"
	lessonTestSample         = "sample"
	lessonTestSampleDesc     = "Sample desc"
	lessonTestTask           = "lesson-task"
	lessonTestWriteScope     = "commands/config/relevance.go"
	lessonTestMissingTask    = "ghost-task"
	lessonTestSamplePath     = ".agents/lessons/sample/LESSON.md"
	lessonTestDirLesson      = "dir-lesson"
	lessonTestBadYAML        = "bad-yaml"
)

func seedLesson(t *testing.T, project, name, body string) {
	t.Helper()
	dir := filepath.Join(project, lessonDirName, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir lesson dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, lessonFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write lesson: %v", err)
	}
}

func seedLessonTask(t *testing.T, project string) {
	t.Helper()
	seedPlan(t, project, lessonTestPlan,
		"schema_version: 1\nid: "+lessonTestPlan+"\ndefault_app_type: ideation\n",
		"schema_version: 1\nplan_id: "+lessonTestPlan+"\ntasks:\n  - id: "+lessonTestTask+"\n    app_type: "+lessonTestGoCLI+"\n    write_scope:\n      - "+lessonTestWriteScope+"\n",
	)
}

func TestRunRelevance_LessonsJSONRanksByAppTypeAndScope(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedLessonTask(t, project)
	seedLessonFixtures(t, project)

	opts := mustRelevanceOptions(project)
	opts.filter = filterLessons
	opts.task = lessonTestPlan + "/" + lessonTestTask
	opts.jsonOut = true

	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance: %v", err)
	}
	var got relevanceResult
	if err := json.Unmarshal([]byte(relevanceOut(opts)), &got); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	assertLessonNames(t, got.Lessons, []string{lessonTestConfigSpecific, lessonTestConfigGeneral})
	if got.AppType != lessonTestGoCLI || got.AppTypeSource != "task" {
		t.Fatalf("selector mismatch: %+v", got)
	}
}

func TestRunRelevance_LessonsHumanUsesPathAndPackageFlags(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedLesson(t, project, lessonTestDocsConfig, lessonFrontmatterBody(lessonTestDocsConfig, "Docs config lesson", lessonWildcardStar, []string{"docs"}, []string{lessonTestDocsPath}, nil))
	seedLesson(t, project, lessonTestPackageConfig, lessonFrontmatterBody(lessonTestPackageConfig, "Package config lesson", lessonWildcardStar, []string{lessonTestConfigTag}, nil, []string{lessonTestCommandsConfig}))

	opts := mustRelevanceOptions(project)
	opts.filter = filterLessons
	opts.appType = lessonTestGoCLI
	opts.paths = []string{lessonTestDocsPath}
	opts.packages = []string{lessonTestCommandsConfig}

	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance: %v", err)
	}
	out := relevanceOut(opts)
	for _, want := range []string{"lessons", lessonTestDocsConfig, lessonTestPackageConfig, "path        : .agents/lessons/docs-config/LESSON.md"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestParseLessonDoc_FrontmatterAndFallback(t *testing.T) {
	doc, err := parseLessonDoc(".agents/lessons/sample/LESSON.md", lessonTestSample, lessonFrontmatterBody(lessonTestSample, lessonTestSampleDesc, lessonWildcardAll, []string{"config,go"}, []string{lessonTestCommandsConfig}, []string{"github.com/AGOrcha/dot-agents/commands/config"}))
	if err != nil {
		t.Fatalf("parseLessonDoc: %v", err)
	}
	if doc.name != lessonTestSample || doc.description != lessonTestSampleDesc {
		t.Fatalf("identity mismatch: %+v", doc)
	}
	if len(doc.tags) != 2 || len(doc.paths) != 1 || len(doc.packages) != 1 {
		t.Fatalf("metadata mismatch: %+v", doc)
	}

	fallback, err := parseLessonDoc(".agents/lessons/fallback/LESSON.md", lessonTestFallback, "# Lesson: fallback heading\n\nBody")
	if err != nil {
		t.Fatalf("parse fallback: %v", err)
	}
	if fallback.name != lessonTestFallback || fallback.description != "fallback heading" {
		t.Fatalf("fallback mismatch: %+v", fallback)
	}
}

func TestLessonCandidateFor_AppTypeAndScopeRules(t *testing.T) {
	scope := lessonScope{paths: []string{lessonTestWriteScope}}
	exact := lessonDoc{name: lessonTestConfigSpecific, appTypes: []string{lessonTestGoCLI}, paths: []string{lessonTestCommandsConfig}}
	wildcard := lessonDoc{name: lessonTestConfigGeneral, appTypes: []string{lessonWildcardStar}, tags: []string{lessonTestConfigTag}}
	wildcardThenExact := lessonDoc{name: "mixed-app", appTypes: []string{lessonWildcardStar, lessonTestGoCLI}}
	wrongApp := lessonDoc{name: "wrong-app", appTypes: []string{lessonTestIdeation}, paths: []string{lessonTestCommandsConfig}}
	noSignal := lessonDoc{name: "no-signal"}

	assertLessonCandidate(t, exact, lessonTestGoCLI, scope, true)
	assertLessonCandidate(t, wildcard, lessonTestGoCLI, scope, true)
	assertLessonSpecificAppType(t, wildcardThenExact, lessonTestGoCLI)
	assertLessonCandidate(t, wrongApp, lessonTestGoCLI, scope, false)
	assertLessonCandidate(t, noSignal, lessonTestGoCLI, scope, false)
}

func seedLessonFixtures(t *testing.T, project string) {
	t.Helper()
	seedLesson(t, project, lessonTestConfigSpecific, lessonFrontmatterBody(lessonTestConfigSpecific, "Specific config lesson", lessonTestGoCLI, []string{lessonTestConfigTag}, []string{lessonTestCommandsConfig}, nil))
	seedLesson(t, project, lessonTestConfigGeneral, lessonFrontmatterBody(lessonTestConfigGeneral, "General config lesson", lessonWildcardStar, []string{lessonTestConfigTag}, []string{"commands"}, nil))
	seedLesson(t, project, lessonTestIdeationOnly, lessonFrontmatterBody(lessonTestIdeationOnly, "Wrong app lesson", lessonTestIdeation, []string{lessonTestConfigTag}, []string{lessonTestCommandsConfig}, nil))
	seedLesson(t, project, "unmatched", "# Lesson: unmatched heading\n\nNo metadata.")
}

func lessonFrontmatterBody(name, description, appType string, tags, paths, packages []string) string {
	return "---\n" +
		"name: " + strconv.Quote(name) + "\n" +
		"description: " + strconv.Quote(description) + "\n" +
		"app_type: " + strconv.Quote(appType) + "\n" +
		lessonYAMLList(lessonFieldTags, tags) +
		lessonYAMLList(lessonFieldPaths, paths) +
		lessonYAMLList(lessonFieldPackages, packages) +
		"---\n\n# " + description + "\n"
}

func lessonYAMLList(key string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(key + ":\n")
	for _, value := range values {
		b.WriteString("  - " + strconv.Quote(value) + "\n")
	}
	return b.String()
}

func assertLessonNames(t *testing.T, facet *lessonsFacet, want []string) {
	t.Helper()
	if facet == nil {
		t.Fatalf("lessons facet missing")
	}
	if len(facet.Items) != len(want) {
		t.Fatalf("lesson count %d want %d: %+v", len(facet.Items), len(want), facet.Items)
	}
	for i, name := range want {
		if facet.Items[i].Name != name {
			t.Fatalf("lesson[%d]=%q want %q: %+v", i, facet.Items[i].Name, name, facet.Items)
		}
	}
}

func assertLessonCandidate(t *testing.T, doc lessonDoc, appType string, scope lessonScope, want bool) {
	t.Helper()
	_, ok := lessonCandidateFor(doc, appType, scope)
	if ok != want {
		t.Fatalf("candidate %q ok=%t want %t", doc.name, ok, want)
	}
}

func assertLessonSpecificAppType(t *testing.T, doc lessonDoc, appType string) {
	t.Helper()
	candidate, ok := lessonCandidateFor(doc, appType, lessonScope{})
	if !ok || !candidate.appTypeSpecific {
		t.Fatalf("candidate %+v should be app_type-specific", candidate)
	}
}

// TestLookupTaskWriteScope_Errors drives the three failure branches a real
// `--task` selector hits: a malformed selector, a missing plan file, and a
// task id absent from an otherwise valid plan. Each asserts the exact wrapped
// error so a regression in the message or the wrapping fails the test.
func TestLookupTaskWriteScope_Errors(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedLessonTask(t, project)

	if _, err := lookupTaskWriteScope(project, "no-slash"); err == nil ||
		!strings.Contains(err.Error(), "must be <plan-id>/<task-id>") {
		t.Fatalf("malformed selector error = %v", err)
	}

	if _, err := lookupTaskWriteScope(project, "absent-plan/"+lessonTestTask); err == nil ||
		!strings.Contains(err.Error(), `reading tasks for plan "absent-plan"`) {
		t.Fatalf("missing plan error = %v", err)
	}

	scope, err := lookupTaskWriteScope(project, lessonTestPlan+"/"+lessonTestTask)
	if err != nil {
		t.Fatalf("resolve known task: %v", err)
	}
	if len(scope) != 1 || scope[0] != lessonTestWriteScope {
		t.Fatalf("write scope = %v want [%s]", scope, lessonTestWriteScope)
	}

	if _, err := lookupTaskWriteScope(project, lessonTestPlan+"/"+lessonTestMissingTask); err == nil ||
		!strings.Contains(err.Error(), `task "`+lessonTestMissingTask+`" not found in plan "`+lessonTestPlan+`"`) {
		t.Fatalf("missing task error = %v", err)
	}
}

// TestBuildLessonsFacet_PropagatesScopeError ensures the public facet builder
// surfaces a bad --task selector rather than swallowing it. If the production
// error propagation were dropped, the facet would return nil error and this
// test fails.
func TestBuildLessonsFacet_PropagatesScopeError(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	opts := mustRelevanceOptions(project)
	opts.task = lessonTestPlan + "/" + lessonTestMissingTask
	seedLessonTask(t, project)

	facet, err := buildLessonsFacet(opts, lessonTestGoCLI)
	if err == nil || !strings.Contains(err.Error(), "not found in plan") {
		t.Fatalf("buildLessonsFacet err = %v (facet %v)", err, facet)
	}
}

// TestBuildLessonsFacet_PropagatesLoadError ensures a lesson file that fails to
// parse (unclosed frontmatter) aborts the build with a parse error rather than
// producing a partial facet.
func TestBuildLessonsFacet_PropagatesLoadError(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedLesson(t, project, "broken", "---\nname: broken\nno closing fence")

	opts := mustRelevanceOptions(project)
	if _, err := buildLessonsFacet(opts, lessonTestGoCLI); err == nil ||
		!strings.Contains(err.Error(), `parsing lesson "broken"`) ||
		!strings.Contains(err.Error(), "frontmatter fence is not closed") {
		t.Fatalf("buildLessonsFacet err = %v", err)
	}
}

// TestLoadLessonDocs_SkipsNonDirAndMissingLesson confirms the directory walk
// ignores stray files at the lessons root and lesson directories that lack a
// LESSON.md, while still loading well-formed lessons.
func TestLoadLessonDocs_SkipsNonDirAndMissingLesson(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedLesson(t, project, lessonTestConfigSpecific,
		lessonFrontmatterBody(lessonTestConfigSpecific, "Specific config lesson", lessonTestGoCLI, []string{lessonTestConfigTag}, nil, nil))
	// A directory with no LESSON.md must be skipped (readLessonDoc returns ok=false).
	if err := os.MkdirAll(filepath.Join(project, lessonDirName, "empty-dir"), 0o755); err != nil {
		t.Fatalf("mkdir empty lesson dir: %v", err)
	}
	// A stray file at the lessons root must be skipped (not a directory).
	if err := os.WriteFile(filepath.Join(project, lessonDirName, "README.md"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	docs, err := loadLessonDocs(project)
	if err != nil {
		t.Fatalf("loadLessonDocs: %v", err)
	}
	if len(docs) != 1 || docs[0].name != lessonTestConfigSpecific {
		t.Fatalf("docs = %+v want only %s", docs, lessonTestConfigSpecific)
	}
}

// TestLoadLessonDocs_MissingDirIsEmpty confirms an absent lessons directory is a
// non-error empty result (the relevance command runs in repos with no lessons).
func TestLoadLessonDocs_MissingDirIsEmpty(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	docs, err := loadLessonDocs(project)
	if err != nil || docs != nil {
		t.Fatalf("loadLessonDocs on bare repo = (%+v, %v)", docs, err)
	}
}

// TestReadLessonDoc_MissingFileSkipped exercises the os.IsNotExist branch: a
// lesson directory whose LESSON.md was never written yields ok=false, nil error.
func TestReadLessonDoc_MissingFileSkipped(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	if err := os.MkdirAll(filepath.Join(project, lessonDirName, "no-file"), 0o755); err != nil {
		t.Fatalf("mkdir lesson dir: %v", err)
	}
	doc, ok, err := readLessonDoc(project, "no-file")
	if ok || err != nil {
		t.Fatalf("readLessonDoc missing = (%+v, %t, %v)", doc, ok, err)
	}
}

// TestReadLessonDoc_ReadErrorWrapped exercises the non-NotExist read failure:
// when LESSON.md is itself a directory, os.ReadFile fails and the error must be
// wrapped with the lesson name rather than silently skipped.
func TestReadLessonDoc_ReadErrorWrapped(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	// Create LESSON.md as a directory so ReadFile returns a non-NotExist error.
	if err := os.MkdirAll(filepath.Join(project, lessonDirName, lessonTestDirLesson, lessonFileName), 0o755); err != nil {
		t.Fatalf("mkdir LESSON.md dir: %v", err)
	}
	if _, _, err := readLessonDoc(project, lessonTestDirLesson); err == nil ||
		!strings.Contains(err.Error(), `reading lesson "`+lessonTestDirLesson+`"`) {
		t.Fatalf("readLessonDoc read error = %v", err)
	}
}

// TestLoadLessonDocs_ReadDirErrorWrapped exercises the non-NotExist directory
// read failure: when the lessons path is a file rather than a directory,
// os.ReadDir fails and the error must be wrapped, not swallowed as empty.
func TestLoadLessonDocs_ReadDirErrorWrapped(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	// Make .agents/lessons a file so ReadDir returns a non-NotExist error.
	if err := os.MkdirAll(filepath.Join(project, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir .agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, lessonDirName), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write lessons file: %v", err)
	}
	if _, err := loadLessonDocs(project); err == nil ||
		!strings.Contains(err.Error(), "reading lessons directory") {
		t.Fatalf("loadLessonDocs readdir error = %v", err)
	}
}

// TestReadLessonDoc_ParseErrorWrapped confirms a malformed lesson surfaces a
// parse error tagged with the lesson name (the directory-walk caller relies on
// this to point at the offending lesson).
func TestReadLessonDoc_ParseErrorWrapped(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedLesson(t, project, lessonTestBadYAML, "---\nname: [unterminated\n---\nbody")
	if _, _, err := readLessonDoc(project, lessonTestBadYAML); err == nil ||
		!strings.Contains(err.Error(), `parsing lesson "`+lessonTestBadYAML+`"`) {
		t.Fatalf("readLessonDoc parse error = %v", err)
	}
}

// TestParseLessonDoc_FrontmatterEdges drives the non-fenced, empty-frontmatter,
// invalid-yaml, and unclosed-fence shapes through the real parser and asserts
// the resolved doc / error for each.
func TestParseLessonDoc_FrontmatterEdges(t *testing.T) {
	// No frontmatter fence: name falls back to dir, description to first body line.
	doc, err := parseLessonDoc(lessonTestSamplePath, "plain", "# Lesson: plain heading\n\nrest")
	if err != nil {
		t.Fatalf("plain parse: %v", err)
	}
	if doc.name != "plain" || doc.description != "plain heading" {
		t.Fatalf("plain doc = %+v", doc)
	}
	// Body-derived tags are generated when frontmatter declares none.
	if len(doc.tags) == 0 {
		t.Fatalf("expected body-derived tags, got none: %+v", doc)
	}

	// Empty (whitespace-only) frontmatter block: still falls back to dir name +
	// body description, exercising the blank-raw short-circuit in lessonFrontmatter.
	empty, err := parseLessonDoc(lessonTestSamplePath, "blank", "---\n   \n---\nbody line")
	if err != nil {
		t.Fatalf("empty frontmatter parse: %v", err)
	}
	if empty.name != "blank" || empty.description != "body line" {
		t.Fatalf("empty frontmatter doc = %+v", empty)
	}

	// Invalid YAML inside a closed fence is a hard error.
	if _, err := parseLessonDoc(lessonTestSamplePath, "bad", "---\nname: [oops\n---\nbody"); err == nil {
		t.Fatalf("expected yaml error for invalid frontmatter")
	}

	// Unclosed fence is reported distinctly.
	if _, err := parseLessonDoc(lessonTestSamplePath, "open", "---\nname: x\nnever closed"); err == nil ||
		!strings.Contains(err.Error(), "frontmatter fence is not closed") {
		t.Fatalf("unclosed fence error = %v", err)
	}
}

// TestLessonStringList_NonStringKinds confirms scalar comma-splitting and that a
// mapping node (unsupported shape) yields no values rather than panicking.
func TestLessonStringList_NonStringKinds(t *testing.T) {
	scalar := parseLessonFrontmatter(t, "app_type: \"a, b ,b\"\n")
	got := lessonStringList(lessonMapValue(lessonDocumentMap(scalar), lessonFieldAppType))
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("scalar split = %v want [a b]", got)
	}

	mapping := parseLessonFrontmatter(t, "tags:\n  nested: value\n")
	if vals := lessonStringList(lessonMapValue(lessonDocumentMap(mapping), lessonFieldTags)); vals != nil {
		t.Fatalf("mapping node should yield nil, got %v", vals)
	}

	if vals := lessonStringList(nil); vals != nil {
		t.Fatalf("nil node should yield nil, got %v", vals)
	}
}

// TestLessonDocumentMap_NonDocumentNodes confirms the helper unwraps document
// nodes, passes mapping nodes through, and tolerates nil.
func TestLessonDocumentMap_NonDocumentNodes(t *testing.T) {
	if lessonDocumentMap(nil) != nil {
		t.Fatalf("nil node should map to nil")
	}
	doc := parseLessonFrontmatter(t, "name: x\n")
	mapping := lessonDocumentMap(doc)
	if mapping == nil || mapping == doc {
		t.Fatalf("document node should unwrap to its first child")
	}
	// A non-document mapping node passes straight through.
	if lessonDocumentMap(mapping) != mapping {
		t.Fatalf("mapping node should pass through unchanged")
	}
}

// TestLessonPathOverlap_Boundaries asserts the path-matching contract: exact
// match, directory-prefix in both directions, and that empty segments never
// match (the bug guard that would otherwise treat "" as a universal match).
func TestLessonPathOverlap_Boundaries(t *testing.T) {
	cases := []struct {
		name        string
		left, right string
		want        bool
	}{
		{"exact", lessonTestCommandsConfig, lessonTestCommandsConfig, true},
		{"left under right", lessonTestWriteScope, lessonTestCommandsConfig, true},
		{"right under left", "commands", lessonTestCommandsConfig, true},
		{"disjoint", lessonTestCommandsConfig, "internal/links", false},
		{"prefix not on boundary", "commands/configx", lessonTestCommandsConfig, false},
		{"empty left", "", lessonTestCommandsConfig, false},
		{"empty right", lessonTestCommandsConfig, "", false},
		// Both-empty must NOT match: without the guard, "" == "" would falsely
		// report overlap (the universal-match bug). Whitespace/slash-only paths
		// normalize to "" and must hit the same guard.
		{"both empty", "", "", false},
		{"both blank", "   ", "/", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lessonPathOverlap(tc.left, tc.right); got != tc.want {
				t.Fatalf("overlap(%q,%q)=%t want %t", tc.left, tc.right, got, tc.want)
			}
		})
	}
}

// TestLessonCandidateLess_Tiebreaks drives every tiebreak rung of the ordering
// (score, pathHits, tagHits, appTypeSpecific, name, path) so a reordering of the
// comparison ladder is caught.
func TestLessonCandidateLess_Tiebreaks(t *testing.T) {
	base := lessonCandidate{score: 10, pathHits: 2, tagHits: 1, appTypeSpecific: true,
		item: lessonResult{Name: "b", Path: "p/b"}}
	cases := []struct {
		name        string
		left, right lessonCandidate
		want        bool
	}{
		{"higher score wins", withScore(base, 11), base, true},
		{"lower score loses", withScore(base, 9), base, false},
		{"more path hits wins", withPathHits(base, 3), base, true},
		{"more tag hits wins", withTagHits(base, 2), withPathHits(base, 2), true},
		{"specific beats generic", base, withSpecific(base, false), true},
		{"name breaks ties", withName(base, "a"), base, true},
		{"path breaks final tie", withPath(base, "p/a"), base, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lessonCandidateLess(tc.left, tc.right); got != tc.want {
				t.Fatalf("less = %t want %t", got, tc.want)
			}
		})
	}
}

// TestLessonDescriptionFromBody confirms the first non-blank line is cleaned of
// heading markers and the "Lesson:" prefix, and that an empty body yields "".
func TestLessonDescriptionFromBody(t *testing.T) {
	if got := lessonDescriptionFromBody("\n\n## Lesson: real summary\nmore"); got != "real summary" {
		t.Fatalf("description = %q want %q", got, "real summary")
	}
	if got := lessonDescriptionFromBody("   \n\t\n"); got != "" {
		t.Fatalf("blank body description = %q want empty", got)
	}
}

// TestFirstNonEmpty confirms the first trimmed-nonblank value wins and an
// all-blank list yields "".
func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", " kept ", "later"); got != "kept" {
		t.Fatalf("firstNonEmpty = %q want %q", got, "kept")
	}
	if got := firstNonEmpty("", "   "); got != "" {
		t.Fatalf("all-blank firstNonEmpty = %q want empty", got)
	}
}

func parseLessonFrontmatter(t *testing.T, raw string) *yaml.Node {
	t.Helper()
	node, _, err := lessonFrontmatter(lessonFrontmatterFence + "\n" + raw + lessonFrontmatterFence + "\n")
	if err != nil {
		t.Fatalf("lessonFrontmatter(%q): %v", raw, err)
	}
	return node
}

func withScore(c lessonCandidate, score int) lessonCandidate   { c.score = score; return c }
func withPathHits(c lessonCandidate, hits int) lessonCandidate { c.pathHits = hits; return c }
func withTagHits(c lessonCandidate, hits int) lessonCandidate  { c.tagHits = hits; return c }
func withSpecific(c lessonCandidate, v bool) lessonCandidate   { c.appTypeSpecific = v; return c }
func withName(c lessonCandidate, name string) lessonCandidate  { c.item.Name = name; return c }
func withPath(c lessonCandidate, path string) lessonCandidate  { c.item.Path = path; return c }
