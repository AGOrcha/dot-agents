package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
