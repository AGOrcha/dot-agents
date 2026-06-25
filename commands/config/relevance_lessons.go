package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

const (
	lessonDirName          = ".agents/lessons"
	lessonFileName         = "LESSON.md"
	lessonFrontmatterFence = "---"
	lessonFieldAppType     = "app_type"
	lessonFieldDescription = "description"
	lessonFieldMetadata    = "metadata"
	lessonFieldName        = "name"
	lessonFieldPackage     = "package"
	lessonFieldPackages    = "packages"
	lessonFieldPath        = "path"
	lessonFieldPaths       = "paths"
	lessonFieldTags        = "tags"
	lessonWildcardAll      = "all"
	lessonWildcardAny      = "any"
	lessonWildcardStar     = "*"
	lessonPathSeparator    = "/"
	lessonScoreAppType     = 100
	lessonScoreWildcard    = 1
	lessonScorePathOverlap = 20
	lessonScoreTagOverlap  = 5
)

type lessonScope struct {
	paths    []string
	packages []string
}

type lessonDoc struct {
	name        string
	description string
	path        string
	appTypes    []string
	tags        []string
	paths       []string
	packages    []string
}

type lessonCandidate struct {
	item            lessonResult
	score           int
	pathHits        int
	tagHits         int
	appTypeSpecific bool
}

func buildLessonsFacet(opts *runRelevanceOptions, appType string) (*lessonsFacet, error) {
	scope, err := lessonScopeFromOptions(opts)
	if err != nil {
		return nil, err
	}
	lessons, err := loadLessonDocs(opts.cwd)
	if err != nil {
		return nil, err
	}
	candidates := rankLessons(lessons, appType, scope)
	items := make([]lessonResult, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, candidate.item)
	}
	return &lessonsFacet{Items: items}, nil
}

func lessonScopeFromOptions(opts *runRelevanceOptions) (lessonScope, error) {
	scope := lessonScope{
		paths:    normalizeUniqueStrings(opts.paths),
		packages: normalizeUniqueStrings(opts.packages),
	}
	if opts.task == "" {
		return scope, nil
	}
	paths, err := lookupTaskWriteScope(opts.cwd, opts.task)
	if err != nil {
		return lessonScope{}, err
	}
	scope.paths = normalizeUniqueStrings(append(scope.paths, paths...))
	return scope, nil
}

func lookupTaskWriteScope(projectPath, selector string) ([]string, error) {
	plan, task, err := splitTaskSelector(selector)
	if err != nil {
		return nil, err
	}
	var tasksDoc struct {
		Tasks []struct {
			ID         string   `yaml:"id"`
			WriteScope []string `yaml:"write_scope"`
		} `yaml:"tasks"`
	}
	tasksPath := filepath.Join(projectPath, ".agents", "workflow", "plans", plan, "TASKS.yaml")
	if err := readYAMLFile(tasksPath, &tasksDoc); err != nil {
		return nil, fmt.Errorf("reading tasks for plan %q: %w", plan, err)
	}
	for _, t := range tasksDoc.Tasks {
		if t.ID == task {
			return t.WriteScope, nil
		}
	}
	return nil, fmt.Errorf("task %q not found in plan %q", task, plan)
}

func loadLessonDocs(projectPath string) ([]lessonDoc, error) {
	lessonsDir := filepath.Join(projectPath, lessonDirName)
	entries, err := os.ReadDir(lessonsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading lessons directory: %w", err)
	}
	lessons := make([]lessonDoc, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		doc, ok, err := readLessonDoc(projectPath, entry.Name())
		if err != nil {
			return nil, err
		}
		if ok {
			lessons = append(lessons, doc)
		}
	}
	return lessons, nil
}

func readLessonDoc(projectPath, lessonName string) (lessonDoc, bool, error) {
	path := filepath.Join(projectPath, lessonDirName, lessonName, lessonFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return lessonDoc{}, false, nil
	}
	if err != nil {
		return lessonDoc{}, false, fmt.Errorf("reading lesson %q: %w", lessonName, err)
	}
	rel, err := filepath.Rel(projectPath, path)
	if err != nil {
		rel = path
	}
	doc, err := parseLessonDoc(filepath.ToSlash(rel), lessonName, string(data))
	if err != nil {
		return lessonDoc{}, false, fmt.Errorf("parsing lesson %q: %w", lessonName, err)
	}
	return doc, true, nil
}

func parseLessonDoc(relPath, dirName, content string) (lessonDoc, error) {
	frontmatter, body, err := lessonFrontmatter(content)
	if err != nil {
		return lessonDoc{}, err
	}
	doc := lessonDoc{
		name:        firstNonEmpty(lessonScalar(frontmatter, lessonFieldName), dirName),
		description: firstNonEmpty(lessonScalar(frontmatter, lessonFieldDescription), lessonDescriptionFromBody(body)),
		path:        relPath,
		appTypes:    lessonMergedList(frontmatter, lessonFieldAppType),
		tags:        lessonMergedList(frontmatter, lessonFieldTags),
		paths:       append(lessonMergedList(frontmatter, lessonFieldPath), lessonMergedList(frontmatter, lessonFieldPaths)...),
		packages:    append(lessonMergedList(frontmatter, lessonFieldPackage), lessonMergedList(frontmatter, lessonFieldPackages)...),
	}
	if len(doc.tags) == 0 {
		doc.tags = lessonTokens(doc.name + " " + doc.description)
	}
	doc.paths = normalizeUniqueStrings(doc.paths)
	doc.packages = normalizeUniqueStrings(doc.packages)
	return doc, nil
}

func lessonFrontmatter(content string) (*yaml.Node, string, error) {
	prefix := lessonFrontmatterFence + "\n"
	if !strings.HasPrefix(content, prefix) {
		return nil, content, nil
	}
	rest := strings.TrimPrefix(content, prefix)
	idx := strings.Index(rest, "\n"+lessonFrontmatterFence)
	if idx < 0 {
		return nil, content, fmt.Errorf("frontmatter fence is not closed")
	}
	raw := rest[:idx]
	body := strings.TrimPrefix(rest[idx+len("\n"+lessonFrontmatterFence):], "\n")
	var node yaml.Node
	if strings.TrimSpace(raw) == "" {
		return &node, body, nil
	}
	if err := yaml.Unmarshal([]byte(raw), &node); err != nil {
		return nil, content, err
	}
	return &node, body, nil
}

func lessonScalar(node *yaml.Node, key string) string {
	value := lessonMapValue(lessonDocumentMap(node), key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(value.Value)
}

func lessonMergedList(node *yaml.Node, key string) []string {
	root := lessonDocumentMap(node)
	values := lessonStringList(lessonMapValue(root, key))
	metadata := lessonMapValue(root, lessonFieldMetadata)
	values = append(values, lessonStringList(lessonMapValue(metadata, key))...)
	return normalizeUniqueStrings(values)
}

func lessonDocumentMap(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func lessonMapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func lessonStringList(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		return lessonSequenceStrings(node.Content)
	}
	if node.Kind == yaml.ScalarNode {
		return splitLessonScalar(node.Value)
	}
	return nil
}

func lessonSequenceStrings(nodes []*yaml.Node) []string {
	values := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == yaml.ScalarNode {
			values = append(values, splitLessonScalar(node.Value)...)
		}
	}
	return values
}

func splitLessonScalar(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' })
	return normalizeUniqueStrings(parts)
}

func rankLessons(lessons []lessonDoc, appType string, scope lessonScope) []lessonCandidate {
	candidates := make([]lessonCandidate, 0, len(lessons))
	for _, lesson := range lessons {
		candidate, ok := lessonCandidateFor(lesson, appType, scope)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return lessonCandidateLess(candidates[i], candidates[j])
	})
	return candidates
}

func lessonCandidateFor(lesson lessonDoc, appType string, scope lessonScope) (lessonCandidate, bool) {
	appScore, specific, ok := lessonAppTypeScore(lesson.appTypes, appType)
	if !ok {
		return lessonCandidate{}, false
	}
	pathHits := lessonPathHits(scope.paths, lesson.paths) + lessonPathHits(scope.packages, lesson.packages)
	tagHits := lessonTagHits(scope, lesson.tags)
	scopeScore := pathHits*lessonScorePathOverlap + tagHits*lessonScoreTagOverlap
	if appScore == 0 && scopeScore == 0 {
		return lessonCandidate{}, false
	}
	return lessonCandidate{
		item:            lessonResult{Name: lesson.name, Description: lesson.description, Path: lesson.path},
		score:           appScore + scopeScore,
		pathHits:        pathHits,
		tagHits:         tagHits,
		appTypeSpecific: specific,
	}, true
}

func lessonAppTypeScore(appTypes []string, appType string) (score int, specific bool, ok bool) {
	if len(appTypes) == 0 {
		return 0, false, true
	}
	hasWildcard := false
	for _, declared := range appTypes {
		if strings.EqualFold(strings.TrimSpace(declared), strings.TrimSpace(appType)) && appType != "" {
			return lessonScoreAppType, true, true
		}
		if lessonWildcard(declared) {
			hasWildcard = true
		}
	}
	if hasWildcard {
		return lessonScoreWildcard, false, true
	}
	return 0, false, false
}

func lessonWildcard(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	return trimmed == lessonWildcardStar || trimmed == lessonWildcardAll || trimmed == lessonWildcardAny
}

func lessonPathHits(touched, declared []string) int {
	hits := 0
	for _, left := range touched {
		for _, right := range declared {
			if lessonPathOverlap(left, right) {
				hits++
			}
		}
	}
	return hits
}

func lessonPathOverlap(left, right string) bool {
	left = normalizeLessonPath(left)
	right = normalizeLessonPath(right)
	if left == "" || right == "" {
		return false
	}
	return left == right || strings.HasPrefix(left, right+lessonPathSeparator) || strings.HasPrefix(right, left+lessonPathSeparator)
}

func lessonTagHits(scope lessonScope, tags []string) int {
	scopeTokens := lessonScopeTokens(scope)
	tagTokens := lessonTokenSet(tags)
	hits := 0
	for token := range scopeTokens {
		if tagTokens[token] {
			hits++
		}
	}
	return hits
}

func lessonScopeTokens(scope lessonScope) map[string]bool {
	values := make([]string, 0, len(scope.paths)+len(scope.packages))
	values = append(values, scope.paths...)
	values = append(values, scope.packages...)
	return lessonTokenSet(values)
}

func lessonTokenSet(values []string) map[string]bool {
	tokens := map[string]bool{}
	for _, value := range values {
		for _, token := range lessonTokens(value) {
			tokens[token] = true
		}
	}
	return tokens
}

func lessonTokens(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(value), lessonTokenSeparator)
	return normalizeUniqueStrings(parts)
}

func lessonTokenSeparator(r rune) bool {
	return !(unicode.IsLetter(r) || unicode.IsDigit(r))
}

func lessonCandidateLess(left, right lessonCandidate) bool {
	if left.score != right.score {
		return left.score > right.score
	}
	if left.pathHits != right.pathHits {
		return left.pathHits > right.pathHits
	}
	if left.tagHits != right.tagHits {
		return left.tagHits > right.tagHits
	}
	if left.appTypeSpecific != right.appTypeSpecific {
		return left.appTypeSpecific
	}
	if left.item.Name != right.item.Name {
		return left.item.Name < right.item.Name
	}
	return left.item.Path < right.item.Path
}

func normalizeLessonPath(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	return strings.Trim(value, lessonPathSeparator)
}

func normalizeUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func lessonDescriptionFromBody(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return cleanLessonDescription(trimmed)
	}
	return ""
}

func cleanLessonDescription(line string) string {
	line = strings.TrimLeft(line, "#")
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "Lesson:")
	return strings.TrimSpace(line)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
