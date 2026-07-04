package gencore

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/kgquery"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Template IDs every language generator supports (the R4 v1 templates).
const (
	TemplateImplPureFn      = "impl-pure-fn"
	TemplateRefactorExtract = "refactor-extract"
	TemplateAddTestCoverage = "add-test-coverage"
)

// Engine tuning constants, shared across all languages.
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

// Generator is the shared, language-parameterised task generator. It satisfies
// eval.Generator; a language package supplies a [Profile] and thin New/Register
// wrappers so all generation control flow lives in this one place.
type Generator struct {
	querier  *kgquery.Querier
	profile  Profile
	repoRoot string // root KG file paths are relativized against (see normalize.go)
}

// New constructs a Generator for profile p backed by querier. It errors on a
// nil querier or a Profile with nil required function fields, tagging the
// message with the profile's error prefix. The repository root used to
// relativize KG file paths is resolved from the process working directory
// (mirroring commands/eval resolveRepoDir); newForRoot injects a fixed root.
func New(querier *kgquery.Querier, p Profile) (*Generator, error) {
	return newForRoot(querier, p, resolveRepoRoot())
}

// newForRoot is New with an explicit repository root. New resolves the root from
// the working directory; tests inject a fixed root to exercise the absolute-path
// (code-review-graph) normalization deterministically.
func newForRoot(querier *kgquery.Querier, p Profile, repoRoot string) (*Generator, error) {
	if querier == nil {
		return nil, fmt.Errorf("%s: querier is required", p.ErrPrefix)
	}
	if err := validateProfile(p); err != nil {
		return nil, fmt.Errorf("%s: %w", p.ErrPrefix, err)
	}
	return &Generator{querier: querier, profile: p, repoRoot: repoRoot}, nil
}

// validateProfile returns an error if any required function field of p is nil.
// Nil function fields cause a runtime panic when the engine first calls them
// (in buildSpec and the prompt builders); catching them at construction time
// produces an actionable error instead of a crash.
func validateProfile(p Profile) error {
	if p.TestFilePath == nil {
		return fmt.Errorf("Profile.TestFilePath is required")
	}
	if p.VerifyTarget == nil {
		return fmt.Errorf("Profile.VerifyTarget is required")
	}
	if p.BuildCmd == nil {
		return fmt.Errorf("Profile.BuildCmd is required")
	}
	if p.TestCmd == nil {
		return fmt.Errorf("Profile.TestCmd is required")
	}
	return nil
}

// Register constructs a Generator for profile p and adds it to r. It is a
// convenience wrapper for the common wiring pattern.
func Register(r *eval.Registry, querier *kgquery.Querier, p Profile) error {
	g, err := New(querier, p)
	if err != nil {
		return err
	}
	return r.Register(g)
}

// Language returns the profile's language, fulfilling the eval.Generator
// contract.
func (g *Generator) Language() eval.Language { return g.profile.Language }

// Generate synthesises one TaskSpec. The returned spec passes TaskSpec.Validate.
func (g *Generator) Generate(ctx context.Context, opts eval.GenerateOptions) (*eval.TaskSpec, error) {
	tid, err := g.selectTemplateID(opts)
	if err != nil {
		return nil, err
	}
	return g.synthesize(ctx, tid, opts)
}

// selectTemplateID resolves which template to use. An empty TemplateID picks
// the default; an unrecognised ID is rejected.
func (g *Generator) selectTemplateID(opts eval.GenerateOptions) (string, error) {
	if opts.TemplateID == "" {
		return defaultTemplateID, nil
	}
	switch opts.TemplateID {
	case TemplateImplPureFn, TemplateRefactorExtract, TemplateAddTestCoverage:
		return opts.TemplateID, nil
	default:
		return "", fmt.Errorf("%s: unknown template %q", g.profile.ErrPrefix, opts.TemplateID)
	}
}

// synthesize finds a suitable seed and builds the TaskSpec for template tid. It
// normalizes the seed's KG paths and symbol names to a repo-relative,
// ingestion-independent form before assembly, then guards the emitted spec so no
// absolute path can escape into a package pattern, artifact, or prompt.
func (g *Generator) synthesize(ctx context.Context, tid string, opts eval.GenerateOptions) (*eval.TaskSpec, error) {
	r, err := g.findSeed(ctx, opts.Difficulty)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", g.profile.ErrPrefix, err)
	}
	r, err = normalizeSeed(r, g.repoRoot)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", g.profile.ErrPrefix, err)
	}
	spec := g.buildSpec(tid, r)
	if err := assertSpecRelative(spec); err != nil {
		return nil, fmt.Errorf("%s: %w", g.profile.ErrPrefix, err)
	}
	return spec, nil
}

// findSeed picks the first seed whose derived difficulty matches want (any
// difficulty when want is empty). Seeds are evaluated in deterministic
// qualified-name order guaranteed by SeedSymbols.
func (g *Generator) findSeed(ctx context.Context, want eval.Difficulty) (seedResult, error) {
	seeds, err := g.querier.SeedSymbols(ctx, g.profile.Language, seedLimit)
	if err != nil {
		return seedResult{}, fmt.Errorf("seed symbols: %w", err)
	}
	if len(seeds) == 0 {
		return seedResult{}, fmt.Errorf("no %s symbols found in graph", g.profile.DisplayName)
	}
	return pickMatchingSeed(ctx, g.querier, seeds, want)
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
	for _, seed := range seeds {
		r, err := deriveFromSeed(ctx, q, seed)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if matchesDifficulty(r.band, want) {
			return r, nil
		}
	}
	// If any seed failed to derive, surface that KG failure rather than a
	// no-match — even when another seed derived cleanly. A storage/query fault
	// could have hidden a seed that would have matched, so a partial failure
	// under a difficulty constraint must not masquerade as a genuine empty
	// result. (firstErr is also non-nil in the all-fail case.)
	if firstErr != nil {
		return seedResult{}, firstErr
	}
	// Every seed derived cleanly but none matched the requested difficulty:
	// a genuine empty result. (Only reachable with a non-empty want — an
	// empty want accepts the first derived seed above.)
	return seedResult{}, fmt.Errorf("no seed matches difficulty %q", want)
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
func (g *Generator) buildSpec(tid string, r seedResult) *eval.TaskSpec {
	implPath := r.seed.FilePath
	return &eval.TaskSpec{
		TaskSpecVersion:   eval.CurrentTaskSpecVersion,
		TaskID:            g.taskID(tid, r.seed.QualifiedName),
		Language:          g.profile.Language,
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
		Prompt:            g.renderPrompt(tid, r),
		SolutionArtifacts: []eval.SolutionArtifact{{Path: g.solutionArtifactPath(tid, implPath), Role: "target"}},
		Verification: eval.Verification{
			BuildCmd:       g.profile.BuildCmd(implPath),
			TestCmd:        g.profile.TestCmd(implPath),
			TimeoutSeconds: defaultTimeout,
		},
	}
}

// solutionArtifactPath names the file the template's prompt directs the agent
// to WRITE — the file a downstream verifier/scorer must look for the diff in.
// The two "modify the implementation" templates target the seed's own file;
// add-test-coverage directs the agent to a sibling test file, so its expected
// artifact must be that test file, not the implementation. A mismatch here
// makes the harness score the wrong file and mis-judge every run.
func (g *Generator) solutionArtifactPath(tid, implPath string) string {
	if tid == TemplateAddTestCoverage {
		return g.profile.TestFilePath(implPath)
	}
	return implPath
}

// taskID builds a deterministic task identifier from the profile's language
// token, the template short-form, and the sanitised seed qualified name.
func (g *Generator) taskID(tid, qualifiedName string) string {
	return "kg-" + g.profile.IDToken + "-" + templateShort(tid) + "-" + sanitizeQN(qualifiedName)
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
func (g *Generator) renderPrompt(tid string, r seedResult) string {
	switch tid {
	case TemplateImplPureFn:
		return g.promptImplPureFn(r)
	case TemplateRefactorExtract:
		return g.promptRefactorExtract(r)
	default: // TemplateAddTestCoverage
		return g.promptAddTestCoverage(r)
	}
}

// Prompt fragments shared across the three task templates (hoisted to consts so
// the format strings stay DRY — sonar S1192). The "%s" placeholders left in the
// builders below are filled with profile-supplied, language-specific text.
const (
	promptNearbySymbols   = "Nearby symbols (within %d hops): %s\n\n"
	promptConstraintsHdr  = "Constraints:\n"
	promptMustSatisfyLine = "- The solution must satisfy: %s %s"
)

// promptImplPureFn builds a prompt asking the agent to implement a function.
func (g *Generator) promptImplPureFn(r seedResult) string {
	return fmt.Sprintf(
		"Implement the function `%s` in `%s` so that the existing tests pass.\n\n"+
			promptNearbySymbols+
			promptConstraintsHdr+
			"%s"+
			promptMustSatisfyLine,
		r.seed.QualifiedName,
		r.seed.FilePath,
		neighborhoodDepth,
		neighborList(r.nbhd),
		g.profile.NoTestEditFragment,
		g.profile.MustSatisfyCmd,
		g.profile.VerifyTarget(r.seed.FilePath),
	)
}

// promptRefactorExtract builds a prompt asking the agent to extract helpers
// from a complex function.
func (g *Generator) promptRefactorExtract(r seedResult) string {
	return fmt.Sprintf(
		"Refactor `%s` in `%s` by extracting one or more well-named helper functions.\n\n"+
			"Complexity signals: cyclomatic=%d, fan-out=%d callee(s), span=%d line(s).\n"+
			promptNearbySymbols+
			promptConstraintsHdr+
			"%s"+
			promptMustSatisfyLine,
		r.seed.QualifiedName,
		r.seed.FilePath,
		r.complexity.Cyclomatic,
		r.complexity.FanOut,
		r.complexity.SpanLines,
		neighborhoodDepth,
		neighborList(r.nbhd),
		g.profile.NoTestEditFragment,
		g.profile.MustSatisfyCmd,
		g.profile.VerifyTarget(r.seed.FilePath),
	)
}

// promptAddTestCoverage builds a prompt asking the agent to write tests. It
// names the exact test file the agent should create or extend — the same file
// buildSpec records as the expected solution artifact — so the prompt's write
// target and the spec's artifact never disagree.
func (g *Generator) promptAddTestCoverage(r seedResult) string {
	return fmt.Sprintf(
		"Add test coverage for `%s` (implemented in `%s`).\n\n"+
			"Call-graph context: %d caller(s), %d callee(s).\n"+
			promptNearbySymbols+
			promptConstraintsHdr+
			"- Create or extend the test file `%s` in the same %s.\n"+
			"- Do not modify the implementation file `%s`.\n"+
			promptMustSatisfyLine,
		r.seed.QualifiedName,
		r.seed.FilePath,
		r.complexity.FanIn,
		r.complexity.FanOut,
		neighborhoodDepth,
		neighborList(r.nbhd),
		g.profile.TestFilePath(r.seed.FilePath),
		g.profile.TestFileNoun,
		r.seed.FilePath,
		g.profile.MustSatisfyCmd,
		g.profile.VerifyTarget(r.seed.FilePath),
	)
}

// neighborList returns a comma-separated list of up to maxNeighborNames symbols
// in nbhd that are not the root, for use in prompts.
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
