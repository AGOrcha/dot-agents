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
	Kind    string                `json:"kind"`    // stage: executor | verifier | reviewer | orchestrator
	Slug    string                `json:"slug"`    // profile slug (verifier_type, reviewer lens, …)
	Matched bool                  `json:"matched"` // a profile entry exists in the merged config
	Entries []composedPromptEntry `json:"entries"`
}

// composedPromptEntry is one resolved prompt_files reference, preserving the
// profile's declared (base-first) order.
type composedPromptEntry struct {
	Ref      string `json:"ref"`      // the raw prompt_files entry
	Resolved string `json:"resolved"` // the file it resolved to (display path), empty if unresolved
	Scope    string `json:"scope"`    // literal | repo-local | shared-home | unresolved
	Exists   bool   `json:"exists"`
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
// either a bare string (legacy local path) or a source-aware object
// {source, path, version}; both forms contribute their path here (source
// resolution is a separate concern from local composition).
func decodeProfilePromptFiles(raw map[string]any, stage, slug string) ([]string, bool) {
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
	out := make([]string, 0, len(pf))
	for _, e := range pf {
		if p := strings.TrimSpace(promptRefPath(e)); p != "" {
			out = append(out, p)
		}
	}
	return out, true
}

// promptRefPath extracts the path from a prompt_files entry in either form: a
// bare string, or a {source, path, version} object (object form contributes its
// "path"). Anything else yields "".
func promptRefPath(e any) string {
	switch v := e.(type) {
	case string:
		return v
	case map[string]any:
		if p, ok := v["path"].(string); ok {
			return p
		}
	}
	return ""
}

// resolvePromptRef resolves a single prompt_files entry across the scope search
// path, preserving the caller's (base-first) list order. Per-file precedence,
// highest first:
//  1. an absolute path that exists                          -> literal
//  2. <projectPath>/<entry> exists (a full .agents/... ref) -> repo-local
//  3. <projectPath>/.agents/prompts/<entry> exists          -> repo-local
//  4. <agentsHome>/prompts/<entry> exists                   -> shared-home (product/user/org/team)
//  5. none                                                  -> unresolved (ref kept for visibility)
//
// Repo-local committed files win over the shared-home (product/starter) copy, so
// a project can override the product base by placing a same-named file under
// .agents/prompts/; the shared home holds the materialized starter baseline.
func resolvePromptRef(projectPath, agentsHome, entry string) composedPromptEntry {
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
	view := composedPromptView{Kind: kind, Slug: slug, Matched: matched}
	for _, entry := range entries {
		view.Entries = append(view.Entries, resolvePromptRef(projectPath, agentsHome, entry))
	}
	return view, nil
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
		} else {
			fmt.Fprintf(os.Stdout, "    %d. %s  [%s]\n", i+1, e.Ref, e.Scope)
		}
	}
}
