// Package tsgen is the TypeScript-language task generator for the R4 eval harness.
// It implements eval.Generator and synthesises versioned TaskSpecs from the
// Tree-sitter knowledge graph via three templates:
//
//   - impl-pure-fn: implement a function so existing tests pass
//   - refactor-extract: extract helpers from a complex function
//   - add-test-coverage: write tests for an uncovered function
//
// TypeScript tasks include a transpile step (tsc --noEmit) and run tests
// via the Node.js built-in test runner (node --test). Register the generator
// by calling Register(registry, querier) during harness wiring; New is
// available for callers that manage registration themselves.
package tsgen

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/kgquery"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Template IDs the TypeScript generator supports.
const (
	TemplateImplPureFn      = "impl-pure-fn"
	TemplateRefactorExtract = "refactor-extract"
	TemplateAddTestCoverage = "add-test-coverage"
)

// Verification command components.
const (
	tscCmd      = "tsc"
	noEmitFlag  = "--noEmit"
	nodeCmd     = "node"
	testFlag    = "--test"
	tsExtension = ".ts"
	testInfix   = ".test"
)

// Generation configuration constants.
const (
	defaultTemplateID = TemplateImplPureFn
	seedLimit         = 50
	neighborhoodDepth = 2
	taskIDMaxLen      = 32
	defaultTimeout    = 120
	maxNeighborNames  = 5
)

// nonAlnum matches runs of characters that are not lowercase letters or digits.
var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// seedResult groups the KG-derived data for a single candidate seed symbol.
type seedResult struct {
	seed       graphstore.GraphNode
	nbhd       kgquery.Neighborhood
	complexity kgquery.Complexity
	band       eval.Difficulty
	signals    map[string]int
}

// Generator produces TypeScript eval.TaskSpecs from the knowledge graph.
// It satisfies the eval.Generator interface.
type Generator struct {
	querier *kgquery.Querier
}

// New constructs a Generator backed by querier. It errors on a nil querier.
func New(querier *kgquery.Querier) (*Generator, error) {
	if querier == nil {
		return nil, fmt.Errorf("tsgen: querier is required")
	}
	return &Generator{querier: querier}, nil
}

// Register constructs a Generator from querier and adds it to r. It is a
// convenience wrapper for the common wiring pattern.
func Register(r *eval.Registry, querier *kgquery.Querier) error {
	g, err := New(querier)
	if err != nil {
		return err
	}
	return r.Register(g)
}

// Language returns eval.LanguageTypeScript, fulfilling the eval.Generator contract.
func (g *Generator) Language() eval.Language { return eval.LanguageTypeScript }

// Generate synthesises one TaskSpec for TypeScript. The returned spec passes
// TaskSpec.Validate.
func (g *Generator) Generate(ctx context.Context, opts eval.GenerateOptions) (*eval.TaskSpec, error) {
	tid, err := selectTemplateID(opts)
	if err != nil {
		return nil, err
	}
	return synthesize(ctx, g.querier, tid, opts)
}

// selectTemplateID resolves which template to use. An empty TemplateID picks
// the default; an unrecognised ID is rejected.
func selectTemplateID(opts eval.GenerateOptions) (string, error) {
	if opts.TemplateID == "" {
		return defaultTemplateID, nil
	}
	switch opts.TemplateID {
	case TemplateImplPureFn, TemplateRefactorExtract, TemplateAddTestCoverage:
		return opts.TemplateID, nil
	default:
		return "", fmt.Errorf("tsgen: unknown template %q", opts.TemplateID)
	}
}

// synthesize finds a suitable seed and builds the TaskSpec for template tid.
func synthesize(ctx context.Context, q *kgquery.Querier, tid string, opts eval.GenerateOptions) (*eval.TaskSpec, error) {
	r, err := findSeed(ctx, q, opts.Difficulty)
	if err != nil {
		return nil, fmt.Errorf("tsgen: %w", err)
	}
	return buildSpec(tid, r), nil
}

// findSeed picks the first seed whose derived difficulty matches want (any
// difficulty when want is empty). Seeds are evaluated in deterministic
// qualified-name order guaranteed by SeedSymbols.
func findSeed(ctx context.Context, q *kgquery.Querier, want eval.Difficulty) (seedResult, error) {
	seeds, err := q.SeedSymbols(ctx, eval.LanguageTypeScript, seedLimit)
	if err != nil {
		return seedResult{}, fmt.Errorf("seed symbols: %w", err)
	}
	if len(seeds) == 0 {
		return seedResult{}, fmt.Errorf("no TypeScript symbols found in graph")
	}
	return pickMatchingSeed(ctx, q, seeds, want)
}

// pickMatchingSeed iterates seeds in order and returns the first whose derived
// difficulty equals want. When want is empty any successfully derived seed is
// accepted.
//
// Exhausting the loop has two outcomes that must not be conflated:
//   - at least one seed derived cleanly but none matched want → a legitimate
//     empty result ("no seed matches difficulty").
//   - every candidate seed failed to derive (a KG read/query/storage error) →
//     an infrastructure failure; the first such error is returned wrapped so
//     it is not masked as an ordinary no-match (the #262 swallow-class).
//
// When want is empty the first successfully derived seed returns immediately,
// so the no-match branch is only reachable with a non-empty want.
func pickMatchingSeed(ctx context.Context, q *kgquery.Querier, seeds []graphstore.GraphNode, want eval.Difficulty) (seedResult, error) {
	var firstErr error
	derivedAny := false
	for _, seed := range seeds {
		r, err := deriveFromSeed(ctx, q, seed)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		derivedAny = true
		if matchesDifficulty(r.band, want) {
			return r, nil
		}
	}
	// A seed derived cleanly but none matched the requested difficulty: a
	// genuine empty result, not a failure. (Only reachable with a non-empty
	// want — an empty want accepts the first derived seed above.)
	if derivedAny {
		return seedResult{}, fmt.Errorf("no seed matches difficulty %q", want)
	}
	// No seed derived at all — every candidate failed with an error. Surface
	// the first failure rather than reporting a no-match, so a real KG
	// infrastructure fault is not swallowed. firstErr is non-nil here:
	// findSeed guarantees len(seeds) > 0 and every seed took the error branch.
	return seedResult{}, firstErr
}

// matchesDifficulty reports whether band satisfies the want constraint.
// A zero want accepts any band.
func matchesDifficulty(band, want eval.Difficulty) bool {
	return want == "" || band == want
}

// deriveFromSeed runs the neighborhood and complexity queries for seed and
// derives its difficulty band and signals map.
func deriveFromSeed(ctx context.Context, q *kgquery.Querier, seed graphstore.GraphNode) (seedResult, error) {
	nbhd, err := q.NeighborhoodFor(ctx, seed.QualifiedName, neighborhoodDepth)
	if err != nil {
		return seedResult{}, fmt.Errorf("neighborhood of %q: %w", seed.QualifiedName, err)
	}
	c, err := q.ComplexityProxy(ctx, seed.QualifiedName)
	if err != nil {
		return seedResult{}, fmt.Errorf("complexity of %q: %w", seed.QualifiedName, err)
	}
	inputs := eval.DifficultyInputs{
		NodeCount:  len(nbhd.Nodes),
		EdgeCount:  len(nbhd.Edges),
		Cyclomatic: c.Cyclomatic,
	}
	band, signals, err := eval.DeriveDifficulty(inputs)
	if err != nil {
		return seedResult{}, fmt.Errorf("derive difficulty: %w", err)
	}
	return seedResult{
		seed:       seed,
		nbhd:       nbhd,
		complexity: c,
		band:       band,
		signals:    signals,
	}, nil
}

// buildSpec assembles a valid TaskSpec from the template ID and seed result.
func buildSpec(tid string, r seedResult) *eval.TaskSpec {
	testGlob := testGlobPattern(r.seed.FilePath)
	return &eval.TaskSpec{
		TaskSpecVersion:   eval.CurrentTaskSpecVersion,
		TaskID:            taskID(tid, r.seed.QualifiedName),
		Language:          eval.LanguageTypeScript,
		Difficulty:        r.band,
		DifficultySignals: r.signals,
		GeneratedFrom: eval.GeneratedFrom{
			Kind:       eval.KindKGTemplate,
			TemplateID: tid,
			KGQuery: &eval.KGQuery{
				Intent:     "code_context",
				SeedSymbol: r.seed.QualifiedName,
			},
		},
		Prompt:            renderPrompt(tid, r),
		SolutionArtifacts: []eval.SolutionArtifact{{Path: solutionArtifactPath(tid, r.seed.FilePath), Role: "target"}},
		Verification: eval.Verification{
			BuildCmd:       []string{tscCmd, noEmitFlag},
			TestCmd:        []string{nodeCmd, testFlag, testGlob},
			TimeoutSeconds: defaultTimeout,
		},
	}
}

// solutionArtifactPath names the file the template's prompt directs the agent
// to WRITE — the file a downstream verifier/scorer must look for the diff in.
// The two "modify the implementation" templates target the seed's own file;
// add-test-coverage directs the agent to a sibling .test.ts file, so its
// expected artifact must be that test file, not the implementation. A mismatch
// here makes the harness score the wrong file and mis-judge every run.
func solutionArtifactPath(tid, implPath string) string {
	if tid == TemplateAddTestCoverage {
		return testFilePath(implPath)
	}
	return implPath
}

// testFilePath maps a TypeScript implementation file to its conventional test
// file in the same directory (e.g. "src/foo/foo.ts" → "src/foo/foo.test.ts").
func testFilePath(implPath string) string {
	base := strings.TrimSuffix(implPath, tsExtension)
	return base + testInfix + tsExtension
}

// testGlobPattern returns a glob pattern for test files in the same directory
// as filePath (e.g. "src/foo/foo.ts" → "src/foo/*.test.ts"). This is the
// pattern passed to `node --test` to run all tests in the seed's source dir.
func testGlobPattern(filePath string) string {
	dir := filepath.ToSlash(filepath.Dir(filePath))
	if dir == "" || dir == "." {
		return "*" + testInfix + tsExtension
	}
	return dir + "/*" + testInfix + tsExtension
}

// taskID builds a deterministic task identifier from the template short-form
// and the sanitised qualified name of the seed symbol.
func taskID(tid, qualifiedName string) string {
	return "kg-ts-" + templateShort(tid) + "-" + sanitizeQN(qualifiedName)
}

// templateShort maps a template ID to a concise token used in task IDs.
func templateShort(tid string) string {
	switch tid {
	case TemplateImplPureFn:
		return "impl"
	case TemplateRefactorExtract:
		return "refactor"
	default: // TemplateAddTestCoverage
		return "testcov"
	}
}

// sanitizeQN converts a qualified name to a lowercase, URL-safe token
// truncated to taskIDMaxLen characters.
func sanitizeQN(qn string) string {
	lower := strings.ToLower(qn)
	safe := nonAlnum.ReplaceAllString(lower, "-")
	safe = strings.Trim(safe, "-")
	if len(safe) > taskIDMaxLen {
		safe = safe[:taskIDMaxLen]
	}
	return safe
}

// renderPrompt dispatches to the per-template prompt builder.
func renderPrompt(tid string, r seedResult) string {
	switch tid {
	case TemplateImplPureFn:
		return promptImplPureFn(r)
	case TemplateRefactorExtract:
		return promptRefactorExtract(r)
	default: // TemplateAddTestCoverage
		return promptAddTestCoverage(r)
	}
}

// Prompt fragments shared across the three task templates (hoisted to consts
// so the format strings stay DRY — sonar S1192).
const (
	promptNearbySymbols  = "Nearby symbols (within %d hops): %s\n\n"
	promptConstraintsHdr = "Constraints:\n"
	promptNoTestEdit     = "- Do not modify any existing *.test.ts file.\n"
	promptMustSatisfy    = "- The solution must satisfy: node --test %s"
)

// promptImplPureFn builds a prompt asking the agent to implement a function.
func promptImplPureFn(r seedResult) string {
	return fmt.Sprintf(
		"Implement the function `%s` in `%s` so that the existing tests pass.\n\n"+
			promptNearbySymbols+
			promptConstraintsHdr+
			promptNoTestEdit+
			promptMustSatisfy,
		r.seed.QualifiedName,
		r.seed.FilePath,
		neighborhoodDepth,
		neighborList(r.nbhd),
		testGlobPattern(r.seed.FilePath),
	)
}

// promptRefactorExtract builds a prompt asking the agent to extract helpers
// from a complex function.
func promptRefactorExtract(r seedResult) string {
	return fmt.Sprintf(
		"Refactor `%s` in `%s` by extracting one or more well-named helper functions.\n\n"+
			"Complexity signals: cyclomatic=%d, fan-out=%d callee(s), span=%d line(s).\n"+
			promptNearbySymbols+
			promptConstraintsHdr+
			promptNoTestEdit+
			promptMustSatisfy,
		r.seed.QualifiedName,
		r.seed.FilePath,
		r.complexity.Cyclomatic,
		r.complexity.FanOut,
		r.complexity.SpanLines,
		neighborhoodDepth,
		neighborList(r.nbhd),
		testGlobPattern(r.seed.FilePath),
	)
}

// promptAddTestCoverage builds a prompt asking the agent to write tests. It
// names the exact test file the agent should create or extend — the same file
// buildSpec records as the expected solution artifact — so the prompt's write
// target and the spec's artifact never disagree.
func promptAddTestCoverage(r seedResult) string {
	return fmt.Sprintf(
		"Add test coverage for `%s` (implemented in `%s`).\n\n"+
			"Call-graph context: %d caller(s), %d callee(s).\n"+
			promptNearbySymbols+
			promptConstraintsHdr+
			"- Create or extend the test file `%s` in the same directory.\n"+
			"- Do not modify the implementation file `%s`.\n"+
			promptMustSatisfy,
		r.seed.QualifiedName,
		r.seed.FilePath,
		r.complexity.FanIn,
		r.complexity.FanOut,
		neighborhoodDepth,
		neighborList(r.nbhd),
		testFilePath(r.seed.FilePath),
		r.seed.FilePath,
		testGlobPattern(r.seed.FilePath),
	)
}

// neighborList returns a comma-separated list of up to maxNeighborNames
// symbols in nbhd that are not the root, for use in prompts.
func neighborList(nbhd kgquery.Neighborhood) string {
	var names []string
	for _, n := range nbhd.Nodes {
		if n.QualifiedName == "" || n.QualifiedName == nbhd.Root.QualifiedName {
			continue
		}
		names = append(names, n.QualifiedName)
		if len(names) >= maxNeighborNames {
			break
		}
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
