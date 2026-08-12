package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// Profile kinds for resolve-prompt: each is a stage in the scope-mergeable
// stage_profiles map a composed prompt is resolved from.
const (
	profileKindExecutor     = "executor"
	profileKindVerifier     = "verifier"
	profileKindReviewer     = "reviewer"
	profileKindOrchestrator = "orchestrator"
)

// profileStages is the ordered set of valid --kind values; each names a stage
// key under stage_profiles.
var profileStages = []string{profileKindExecutor, profileKindVerifier, profileKindReviewer, profileKindOrchestrator}

// composedPromptView is the base-first, scope-resolved composition of a
// verifier/reviewer profile's prompt_files. The compose order is the profile's
// declared prompt_files order (base → per-type → repo overlay); each entry is
// resolved independently across the scope search path. This is the seam the
// orchestrator/ISP calls when dispatching a verifier or reviewer so the worker
// gets the same merged prompt every other surface resolves.
type composedPromptView struct {
	Kind        string                `json:"kind"`         // stage: executor | verifier | reviewer | orchestrator
	Slug        string                `json:"slug"`         // profile slug (verifier_type, reviewer lens, …)
	Matched     bool                  `json:"matched"`      // a profile entry exists in the merged config
	Model       string                `json:"model"`        // concrete OMP model identifier
	ModelFamily string                `json:"model_family"` // semantic family for diversity gates
	Entries     []composedPromptEntry `json:"entries"`
}

// composedPromptEntry is one resolved prompt_files reference, preserving the
// profile's declared (base-first) order. Source and Hint are additive (omitempty):
// an existing consumer reading ref/resolved/scope/exists is unaffected.
type composedPromptEntry struct {
	Ref      string `json:"ref"`      // the canonical prompt_files entry (source-qualified refs render as "source:path[@version]")
	Resolved string `json:"resolved"` // the file it resolved to (display path), empty if unresolved
	Scope    string `json:"scope"`    // literal | repo-local | shared-home | source | unresolved
	Exists   bool   `json:"exists"`
	// Source is the config source id a source-qualified entry is pinned to,
	// empty for a local-path entry.
	Source string `json:"source,omitempty"`
	// Hint is the operator-facing next step when the entry could not be resolved
	// (e.g. a source-qualified prompt that was never synced or whose cache was
	// pruned). Empty when the entry resolved.
	Hint string `json:"hint,omitempty"`
}

func validateProfileKind(kind string) error {
	for _, s := range profileStages {
		if kind == s {
			return nil
		}
	}
	return fmt.Errorf("--kind must be one of %s, got %q", strings.Join(profileStages, ", "), kind)
}

// decodeProfilePromptFiles reads stage_profiles.<stage>.<slug>.prompt_files from
// the effective (scope-merged) config. The second return reports whether a
// profile entry for slug exists at all (so an empty prompt_files on a real
// profile is distinguishable from a missing profile). Each prompt_files entry is
// either a bare string (a local path, or a "source-id:path[@version]" ref) or the
// canonical source-aware object {source, path, version}; BOTH forms are carried
// through as a typed config.PromptFileRef so the source/version provenance
// survives to resolution — flattening to a bare path here is what made every
// source-qualified prompt unresolvable.
func decodeProfilePromptFiles(raw map[string]any, stage, slug string) ([]config.PromptFileRef, bool) {
	stages, ok := raw["stage_profiles"].(map[string]any)
	if !ok {
		return nil, false
	}
	profiles, ok := stages[stage].(map[string]any)
	if !ok {
		return nil, false
	}
	prof, ok := profiles[slug].(map[string]any)
	if !ok {
		return nil, false
	}
	pf, ok := prof["prompt_files"].([]any)
	if !ok {
		return nil, true // profile exists, no prompt_files declared
	}
	out := make([]config.PromptFileRef, 0, len(pf))
	for _, e := range pf {
		if ref, ok := promptRefEntry(e); ok {
			out = append(out, ref)
		}
	}
	return out, true
}

// decodeProfileModelRoute reads the explicit OMP model route from the same
// effective profile object as prompt_files. Unknown family values are preserved:
// cross-family checks compare identity and do not require a closed vendor list.
func decodeProfileModelRoute(raw map[string]any, stage, slug string) (model, family string) {
	stages, ok := raw["stage_profiles"].(map[string]any)
	if !ok {
		return "", ""
	}
	profiles, ok := stages[stage].(map[string]any)
	if !ok {
		return "", ""
	}
	prof, ok := profiles[slug].(map[string]any)
	if !ok {
		return "", ""
	}
	model, _ = prof["model"].(string)
	family, _ = prof["model_family"].(string)
	return strings.TrimSpace(model), strings.TrimSpace(family)
}

// promptRefEntry decodes one raw prompt_files entry into a typed
// config.PromptFileRef: a bare string becomes {Path: <string>} (the source, if
// any, is classified later against the declared source set), and a
// {source, path, version} object keeps all three fields. A blank/pathless or
// otherwise malformed entry yields ok=false and is dropped.
func promptRefEntry(e any) (config.PromptFileRef, bool) {
	var ref config.PromptFileRef
	switch v := e.(type) {
	case string:
		ref.Path = v
	case map[string]any:
		ref.Source, _ = v["source"].(string)
		ref.Path, _ = v["path"].(string)
		ref.Version, _ = v["version"].(string)
	default:
		return config.PromptFileRef{}, false
	}
	ref.Source = strings.TrimSpace(ref.Source)
	ref.Path = strings.TrimSpace(ref.Path)
	ref.Version = strings.TrimSpace(ref.Version)
	if ref.Path == "" {
		return config.PromptFileRef{}, false
	}
	return ref, true
}

// promptResolveEnv is the read-only environment one composition resolves
// against: the local search roots plus the two offline inputs a source-qualified
// prompt needs — the effective config's declared sources (which decide whether a
// bare "a:b" string is a source ref at all) and the lock's units (which pin the
// digest its cached bytes live under). Both are gathered ONCE per composition.
type promptResolveEnv struct {
	projectPath string
	agentsHome  string
	sources     map[string]config.Source
	units       map[string]config.LockedUnit
}

// resolvePromptRef resolves a single prompt_files entry, preserving the caller's
// (base-first) list order. A SOURCE-QUALIFIED entry (a typed {source,…} object,
// or a bare string whose prefix before ':' names a declared source) resolves from
// the lock + content cache: the pinned prompt unit's digest locates the cached
// bytes `da config sync` fetched, yielding scope "source". It is strictly offline
// — a never-synced or cache-pruned prompt is unresolved WITH a sync hint, never a
// fetch trigger, which is the contract `da workflow resolve-prompt` inherits from
// ResolveLocked.
//
// A LOCAL entry keeps the historical scope search path (see
// resolveLocalPromptRef).
func resolvePromptRef(env promptResolveEnv, ref config.PromptFileRef) composedPromptEntry {
	unit, qualified := config.PromptUnitRefFor(ref, env.sources)
	if !qualified {
		return resolveLocalPromptRef(env.projectPath, env.agentsHome, ref.Path)
	}
	e := composedPromptEntry{Ref: unit.Key(), Source: unit.SourceID}
	if path, ok := config.LockedPromptFile(env.units, unit); ok {
		e.Resolved = config.DisplayPath(path)
		e.Scope = "source"
		e.Exists = true
		return e
	}
	e.Scope = "unresolved"
	e.Hint = fmt.Sprintf("prompt %q is not pinned in %s or its cached bytes are missing; run `da config sync`", unit.Key(), config.AgentsLockFile)
	return e
}

// resolveLocalPromptRef resolves a local (non source-qualified) prompt path
// across the scope search path. Per-file precedence, highest first:
//  1. an absolute path that exists                          -> literal
//  2. <projectPath>/<entry> exists (a full .agents/... ref) -> repo-local
//  3. <projectPath>/.agents/prompts/<entry> exists          -> repo-local
//  4. <agentsHome>/prompts/<entry> exists                   -> shared-home (product/user/org/team)
//  5. none                                                  -> unresolved (ref kept for visibility)
//
// Repo-local committed files win over the shared-home (product/starter) copy, so
// a project can override the product base by placing a same-named file under
// .agents/prompts/; the shared home holds the materialized starter baseline.
func resolveLocalPromptRef(projectPath, agentsHome, entry string) composedPromptEntry {
	e := composedPromptEntry{Ref: entry}
	try := func(p, scope string) bool {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			e.Resolved = config.DisplayPath(p)
			e.Scope = scope
			e.Exists = true
			return true
		}
		return false
	}
	if filepath.IsAbs(entry) {
		if try(entry, "literal") {
			return e
		}
	}
	if try(filepath.Join(projectPath, entry), "repo-local") {
		return e
	}
	if try(filepath.Join(projectPath, ".agents", "prompts", entry), "repo-local") {
		return e
	}
	if strings.TrimSpace(agentsHome) != "" {
		if try(filepath.Join(agentsHome, "prompts", entry), "shared-home") {
			return e
		}
	}
	e.Scope = "unresolved"
	return e
}

// composeProfilePrompt resolves the base-first composition for a verifier/reviewer
// profile from the effective (scope-merged) config.
func composeProfilePrompt(projectPath, agentsHome, kind, slug string) (composedPromptView, error) {
	if err := validateProfileKind(kind); err != nil {
		return composedPromptView{}, err
	}
	snap, err := appTypeSnapshot(projectPath)
	if err != nil {
		if isMissingManifestErr(err) {
			return composedPromptView{Kind: kind, Slug: slug}, nil
		}
		return composedPromptView{}, err
	}
	raw, err := snap.EffectiveRaw()
	if err != nil {
		return composedPromptView{}, err
	}
	entries, matched := decodeProfilePromptFiles(raw, kind, slug)
	model, family := decodeProfileModelRoute(raw, kind, slug)
	view := composedPromptView{Kind: kind, Slug: slug, Matched: matched, Model: model, ModelFamily: family}
	env, err := newPromptResolveEnv(projectPath, agentsHome, snap)
	if err != nil {
		return composedPromptView{}, err
	}
	for _, entry := range entries {
		view.Entries = append(view.Entries, resolvePromptRef(env, entry))
	}
	return view, nil
}

// newPromptResolveEnv gathers the per-composition resolution environment: the
// effective config's declared sources (from the same snapshot the profile was
// read out of) and the lock's units section. Reading the lock is offline and
// non-mutating; a project with no lockfile yields an empty units map, so a purely
// local composition behaves exactly as it did before source qualification existed.
func newPromptResolveEnv(projectPath, agentsHome string, snap *config.Snapshot) (promptResolveEnv, error) {
	env := promptResolveEnv{
		projectPath: projectPath,
		agentsHome:  agentsHome,
		sources:     config.SourceIndex(snap.Effective.Sources),
	}
	units, err := config.ReadUnits(projectPath)
	if err != nil {
		return promptResolveEnv{}, err
	}
	env.units = units.Units
	return env, nil
}

func newWorkflowResolvePromptCmd() *cobra.Command {
	var kind, slug string
	cmd := &cobra.Command{
		Use:   "resolve-prompt",
		Short: "Resolve a stage profile's composed (base-first, scope-resolved) prompt_files",
		Example: deps.ExampleBlock(
			"  da workflow resolve-prompt --kind verifier --slug cli-runner",
			"  da workflow resolve-prompt --kind reviewer --slug architecture-standards",
			"  da workflow resolve-prompt --kind executor --slug default",
			"  da --json workflow resolve-prompt --kind verifier --slug unit",
		),
		Args: deps.NoArgsWithHints("Run workflow resolve-prompt from inside the project repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowResolvePrompt(kind, slug)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "stage: executor, verifier, reviewer, or orchestrator (required)")
	cmd.Flags().StringVar(&slug, "slug", "", "profile slug (verifier_type, reviewer lens, …) (required)")
	return cmd
}

func runWorkflowResolvePrompt(kind, slug string) error {
	kind = strings.TrimSpace(kind)
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("--slug is required")
	}
	if err := validateProfileKind(kind); err != nil {
		return err
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	view, err := composeProfilePrompt(project.Path, config.AgentsHome(), kind, slug)
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
	renderComposedPrompt(view)
	return nil
}

func renderComposedPrompt(view composedPromptView) {
	ui.Header("Composed prompt")
	fmt.Fprintf(os.Stdout, "  kind    : %s\n", view.Kind)
	fmt.Fprintf(os.Stdout, "  slug    : %s\n", view.Slug)
	fmt.Fprintf(os.Stdout, "  matched : %t\n", view.Matched)
	fmt.Fprintf(os.Stdout, "  model   : %s\n", view.Model)
	fmt.Fprintf(os.Stdout, "  family  : %s\n", view.ModelFamily)
	fmt.Fprintln(os.Stdout)
	if !view.Matched {
		fmt.Fprintf(os.Stdout, "  no stage_profiles.%s entry for %q in the effective config.\n", view.Kind, view.Slug)
		return
	}
	if len(view.Entries) == 0 {
		fmt.Fprintln(os.Stdout, "  profile has no prompt_files.")
		return
	}
	fmt.Fprintln(os.Stdout, "  composition (base-first):")
	for i, e := range view.Entries {
		if e.Exists {
			fmt.Fprintf(os.Stdout, "    %d. %s  [%s -> %s]\n", i+1, e.Ref, e.Scope, e.Resolved)
			continue
		}
		fmt.Fprintf(os.Stdout, "    %d. %s  [%s]\n", i+1, e.Ref, e.Scope)
		if e.Hint != "" {
			fmt.Fprintf(os.Stdout, "       %s\n", e.Hint)
		}
	}
}
