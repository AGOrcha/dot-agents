package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// Facet filter values for `da config relevance --filter`. The design
// (.agents/proposals/skill-relevance-filter.md §5) keeps one command evolvable
// across facets via --filter rather than a verb per facet, so new facets slot
// in here without a new top-level command.
const (
	filterUnits    = "units"
	filterTopology = "topology"
	filterLenses   = "lenses"
	filterAll      = "all"
)

// validFilters is the closed set accepted by --filter. Sorted form is used in
// the usage hint so the error message lists the choices deterministically.
var validFilters = []string{filterAll, filterLenses, filterTopology, filterUnits}

// relevanceResult is the stable JSON shape emitted by
// `da config relevance --json`. It reports the resolution context (resolved
// app_type, stage, the selector that won) alongside only the requested facet,
// so scripts can pin the envelope while the facet payload varies by --filter.
//
// STRUCTURE CONTRACT (one consistent shape for every consumer): the resolution
// context — AppType, AppTypeSource, Stage, Filter, and Matched — lives at the
// ENVELOPE top level because it is cross-facet (with --filter all, one
// resolution either matched a profile or did not, regardless of which of the
// three facets is sliced). The facet payloads — Units, Topology, Lenses — nest
// under their own key. Matched is therefore deliberately NOT a field of any
// facet: folding it into Topology/Units/Lenses would duplicate it across facets
// and split one piece of state into three. Consumers must read the ENVELOPE
// (matched + the facet), never a bare facet object, to recover whether the
// resolution matched. The ultracode-wave-engine mirrors this nesting exactly.
type relevanceResult struct {
	// AppType is the app_type the profile was resolved for, after the
	// task -> plan-default -> --app-type precedence in resolveAppType.
	AppType string `json:"app_type"`
	// AppTypeSource names where the resolved app_type came from: "task",
	// "plan-default", "flag", or "none" (no selector resolved an app_type).
	AppTypeSource string `json:"app_type_source"`
	// Stage is the stage the units facet was sliced for (empty when not
	// requested or not applicable to the chosen filter).
	Stage string `json:"stage,omitempty"`
	// Filter echoes the facet that was rendered.
	Filter string `json:"filter"`
	// Matched reports whether the resolved app_type had a profile entry in the
	// effective execution_profile layer. False means defaults/empty facets were
	// rendered (an unlisted app_type is never an error — it just has no overrides).
	Matched bool `json:"matched"`
	// Units is the relevance facet (facet 1), present when --filter is units/all.
	Units *unitsFacet `json:"units,omitempty"`
	// Topology is facet 2, present when --filter is topology/all.
	Topology *topologyFacet `json:"topology,omitempty"`
	// Lenses is facet 3, present when --filter is lenses/all.
	Lenses *lensesFacet `json:"lenses,omitempty"`
}

// unitsFacet is the per-stage core/situational/noise classification. When a
// --stage is supplied, ByStage holds exactly that stage; otherwise every stage
// declared for the app_type is rendered.
type unitsFacet struct {
	// DefaultClass is the class assigned to any unit not explicitly listed
	// (situational unless overridden) — surfaced so the operator can see that
	// nothing unlisted is silently dropped.
	DefaultClass string `json:"default_class"`
	// ByStage maps a stage name to its resolved classes. Stages are sorted in
	// the human render for determinism; the JSON map carries them all.
	ByStage map[string]cfg.RelevanceClasses `json:"by_stage"`
	// WorkingSet maps a stage name to the resolved working set the profile
	// actually yields once noise is suppressed: the kept (core + situational)
	// units and the suppressed (noise) units, in input order. This is the live
	// product of ExecutionProfile.SuppressNoise — the design's reversible-view
	// filter (§2) made visible, so the command shows what the noise class does
	// rather than just listing it.
	WorkingSet map[string]workingSetView `json:"working_set"`
}

// workingSetView is the rendered form of a resolved cfg.WorkingSet for one
// stage: the effective working set after suppression plus the suppressed units
// it set aside. Suppression is a reversible view, so both lists are reported.
type workingSetView struct {
	// Kept is the effective working set (core + situational), in input order.
	Kept []string `json:"kept"`
	// Suppressed is the noise-classed units filtered out of the working set, in
	// input order. Retained (not deleted) so the view is reversible.
	Suppressed []string `json:"suppressed"`
}

// topologyFacet is the executor:verifier:reviewer fan-out for the app_type.
type topologyFacet struct {
	Executors            int      `json:"executors"`
	VerifiersPerExecutor int      `json:"verifiers_per_executor"`
	Reviewers            string   `json:"reviewers,omitempty"`
	VerifierSequence     []string `json:"verifier_sequence,omitempty"`
}

// lensesFacet is the review-lens config for the app_type.
type lensesFacet struct {
	LensSet         []string `json:"lens_set,omitempty"`
	LensConcurrency string   `json:"lens_concurrency,omitempty"`
}

// runRelevanceOptions captures one invocation's flag state. stdout/stderr/cwd
// are injected so the run path is table-drivable without going through cobra.
type runRelevanceOptions struct {
	filter  string
	appType string
	stage   string
	task    string
	// recompute switches the command from the inspector path to the explicit
	// driver-event path (design §5: `da config relevance --recompute [--write]`).
	// It is a flag rather than a subcommand so the public surface matches the
	// design contract exactly.
	recompute bool
	// write is honored only with --recompute: it emits a PROPOSED
	// execution_profile layer diff (never auto-applied).
	write   bool
	jsonOut bool
	stdout  io.Writer
	stderr  io.Writer
	cwd     string
}

func newRelevanceCmd(deps Deps) *cobra.Command {
	opts := &runRelevanceOptions{}
	cmd := &cobra.Command{
		Use:   "relevance",
		Short: "Resolve a task's execution profile (units, topology, lenses) by app_type",
		Long: `Resolve the effective execution_profile layer for a task and print the
requested facet. The execution profile bundles three independently
scope-overridable facets per app_type (design
.agents/proposals/skill-relevance-filter.md §2):

  units     facet 1 — per-stage core/situational/noise unit relevance
  topology  facet 2 — the executor:verifier:reviewer fan-out + verifier_sequence
  lenses    facet 3 — the review-lens set + concurrency

--filter slices the facet you want (default "all") so the command stays
evolvable as new facets land. The app_type is selected by, in precedence order:
  1. the named --task's own app_type (TASKS.yaml app_type override),
  2. that plan's default_app_type (PLAN.yaml),
  3. the --app-type flag.
An app_type with no profile entry is not an error — defaults/empty facets are
rendered (nothing unlisted is silently dropped). --json emits a stable envelope
documented on the relevanceResult type in this package.

--recompute switches to the explicit driver-event path (design §5/§6): it reads
the scored iteration corpus (.agents/active/iteration-log/iter-N.yaml +
iter-N.score.yaml) and proposes core/situational/noise class changes per unit
from a contribution signal, plus a gaps list of cited-but-unclassified units.
With --write it emits a PROPOSED execution_profile layer diff for a human to
accept — it never auto-applies, and it has no clock (explicit-only, config-v2
D4). The recompute envelope is documented on the recomputeResult type.`,
		Example: exampleBlock(
			"  da config relevance --filter units --app-type go-cli --stage review",
			"  da config relevance --filter topology --task config-relevance-profiles/t2-config-relevance-resolver",
			"  da config relevance --filter lenses --app-type ideation",
			"  da config relevance --recompute --app-type go-cli",
			"  da config relevance --recompute --app-type go-cli --write",
			"  da config relevance --json",
		),
		Args: deps.MaximumNArgsWithHints(0, "`da config relevance` takes no positional args; use --app-type / --task / --stage / --filter / --recompute."),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.stdout = cmd.OutOrStdout()
			opts.stderr = cmd.ErrOrStderr()
			opts.jsonOut = deps.jsonFlag() // honor the global persistent --json
			if opts.cwd == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return deps.ErrorWithHints("could not resolve current directory", err.Error())
				}
				opts.cwd = cwd
			}
			return runRelevance(opts, deps)
		},
	}
	cmd.Flags().StringVar(&opts.filter, "filter", filterAll, "Facet to render: units | topology | lenses | all")
	cmd.Flags().StringVar(&opts.appType, "app-type", "", "app_type to resolve the profile for (overridden by --task's own app_type)")
	cmd.Flags().StringVar(&opts.stage, "stage", "", "Restrict the units facet to one stage (e.g. orchestrate, verify, review)")
	cmd.Flags().StringVar(&opts.task, "task", "", "Resolve app_type from a task: <plan-id>/<task-id> (or just <task-id> when --app-type names the plan context)")
	// recompute is the explicit driver event (design §5): it reads the scored
	// iteration corpus and proposes relevance-class changes. It is a flag on this
	// command (not a subcommand) so the public surface matches the design
	// contract `da config relevance --recompute [--write]`. See
	// relevance_recompute.go.
	cmd.Flags().BoolVar(&opts.recompute, "recompute", false, "Recompute unit relevance from the scored iteration corpus (explicit-only driver event)")
	cmd.Flags().BoolVar(&opts.write, "write", false, "With --recompute: emit a PROPOSED execution_profile layer diff (never auto-applies)")
	return cmd
}

// runRelevance is the test-friendly entry point — it receives an
// already-prepared runRelevanceOptions so tests can stub cwd/stdout/stderr
// without cobra.
func runRelevance(opts *runRelevanceOptions, deps Deps) error {
	// --recompute switches to the explicit driver-event path before any
	// inspector-only setup (--filter normalisation) runs: recompute slices by
	// --app-type/--stage, not by --filter, so the two paths share flags but not
	// resolution. See relevance_recompute.go.
	if opts.recompute {
		return runRecompute(opts, deps)
	}
	// --write is meaningful only for --recompute; flag it loudly rather than
	// silently ignoring it on the inspector path.
	if opts.write {
		return deps.UsageError(
			"--write only applies with --recompute",
			"Add --recompute to emit a proposed execution_profile layer diff, or drop --write.",
		)
	}
	if err := normalizeFilter(opts, deps); err != nil {
		return err
	}

	snap, _, err := loadFlatSnapshot(opts.cwd)
	if err != nil {
		return deps.ErrorWithHints(err.Error(),
			"Run `da install --generate` to create .agentsrc.json from current state.",
		)
	}

	profile, err := resolveExecutionProfile(snap)
	if err != nil {
		return deps.ErrorWithHints(err.Error(),
			"The execution_profile layer in .agentsrc.json is not shaped as expected; see .agents/proposals/skill-relevance-filter.md §2.",
		)
	}

	appType, source, err := resolveAppType(opts, deps)
	if err != nil {
		return err
	}

	result := buildRelevanceResult(opts, profile, appType, source)
	return renderRelevance(opts, result)
}

// normalizeFilter lower-cases and validates --filter, defaulting an empty
// value to "all". An unknown facet is a usage error so a typo never silently
// renders the whole profile.
func normalizeFilter(opts *runRelevanceOptions, deps Deps) error {
	opts.filter = strings.ToLower(strings.TrimSpace(opts.filter))
	if opts.filter == "" {
		opts.filter = filterAll
	}
	switch opts.filter {
	case filterUnits, filterTopology, filterLenses, filterAll:
		return nil
	default:
		return deps.UsageError(
			fmt.Sprintf("unknown --filter %q", opts.filter),
			"Choose one of: "+strings.Join(validFilters, ", "),
		)
	}
}

// resolveExecutionProfile extracts the effective execution_profile sub-object
// from the merged snapshot and decodes it into the typed cfg.ExecutionProfile.
// Decoding goes through a JSON round-trip so the struct's json tags (the
// canonical layer shape) drive the mapping. A missing layer yields a non-nil
// empty profile so every caller resolves to safe defaults without nil checks.
func resolveExecutionProfile(snap *snapshot) (*cfg.ExecutionProfile, error) {
	raw, ok := snap.effective["execution_profile"]
	if !ok || raw == nil {
		return &cfg.ExecutionProfile{}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("re-encoding execution_profile layer: %w", err)
	}
	var profile cfg.ExecutionProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("decoding execution_profile layer: %w", err)
	}
	return &profile, nil
}

// resolveAppType applies the design's selector precedence (§3): the named
// task's own app_type wins, else that plan's default_app_type, else the
// --app-type flag. It returns the resolved app_type plus a source label for the
// result envelope. An empty resolution ("none") is valid — defaults render.
func resolveAppType(opts *runRelevanceOptions, deps Deps) (string, string, error) {
	if opts.task != "" {
		taskAppType, planDefault, err := lookupTaskAppType(opts.cwd, opts.task)
		if err != nil {
			return "", "", deps.ErrorWithHints(err.Error(),
				"Pass --task as <plan-id>/<task-id>, or use --app-type directly.",
			)
		}
		if taskAppType != "" {
			return taskAppType, "task", nil
		}
		if planDefault != "" {
			return planDefault, "plan-default", nil
		}
		// The task resolved but declared no app_type and its plan has no
		// default — fall through to the flag rather than failing.
	}
	if opts.appType != "" {
		return opts.appType, "flag", nil
	}
	return "", "none", nil
}

// lookupTaskAppType reads PLAN.yaml + TASKS.yaml for the given --task selector
// and returns (task.app_type, plan.default_app_type). The selector is
// <plan-id>/<task-id>; a bare <task-id> is rejected because the plan cannot be
// located without it. Only the two fields needed for selection are decoded, so
// this stays independent of the commands/workflow package (no import cycle).
func lookupTaskAppType(projectPath, selector string) (string, string, error) {
	plan, task, err := splitTaskSelector(selector)
	if err != nil {
		return "", "", err
	}
	plansDir := filepath.Join(projectPath, ".agents", "workflow", "plans", plan)

	var planDoc struct {
		DefaultAppType string `yaml:"default_app_type"`
	}
	if err := readYAMLFile(filepath.Join(plansDir, "PLAN.yaml"), &planDoc); err != nil {
		return "", "", fmt.Errorf("reading plan %q: %w", plan, err)
	}

	var tasksDoc struct {
		Tasks []struct {
			ID      string `yaml:"id"`
			AppType string `yaml:"app_type"`
		} `yaml:"tasks"`
	}
	if err := readYAMLFile(filepath.Join(plansDir, "TASKS.yaml"), &tasksDoc); err != nil {
		return "", "", fmt.Errorf("reading tasks for plan %q: %w", plan, err)
	}

	for _, t := range tasksDoc.Tasks {
		if t.ID == task {
			return t.AppType, planDoc.DefaultAppType, nil
		}
	}
	return "", "", fmt.Errorf("task %q not found in plan %q", task, plan)
}

// splitTaskSelector parses a <plan-id>/<task-id> selector. Both halves are
// required; a selector without a slash cannot name a plan to load.
func splitTaskSelector(selector string) (plan, task string, err error) {
	selector = strings.TrimSpace(selector)
	idx := strings.Index(selector, "/")
	if idx <= 0 || idx == len(selector)-1 {
		return "", "", fmt.Errorf("--task %q must be <plan-id>/<task-id>", selector)
	}
	return selector[:idx], selector[idx+1:], nil
}

// readYAMLFile reads path and decodes it into out. A missing file or parse
// error is returned to the caller (the selector path is fatal — there is
// nothing to resolve the app_type from).
func readYAMLFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}

// buildRelevanceResult assembles the result envelope, attaching only the facets
// the chosen --filter requested. Resolution against a missing app_type entry
// renders defaults/empty facets and sets Matched=false.
func buildRelevanceResult(opts *runRelevanceOptions, profile *cfg.ExecutionProfile, appType, source string) relevanceResult {
	result := relevanceResult{
		AppType:       appType,
		AppTypeSource: source,
		Filter:        opts.filter,
		Matched:       appTypeMatched(profile, appType),
	}

	prof := appTypeProfile(profile, appType)

	if opts.filter == filterUnits || opts.filter == filterAll {
		result.Units = buildUnitsFacet(profile, prof, appType, opts.stage)
		result.Stage = opts.stage
	}
	if opts.filter == filterTopology || opts.filter == filterAll {
		result.Topology = buildTopologyFacet(prof)
	}
	if opts.filter == filterLenses || opts.filter == filterAll {
		result.Lenses = buildLensesFacet(prof)
	}
	return result
}

// appTypeMatched reports whether the resolved app_type has an explicit entry in
// the effective profile. A non-empty app_type that is not in ByAppType is not
// an error — it just has no overrides.
func appTypeMatched(profile *cfg.ExecutionProfile, appType string) bool {
	if profile == nil || appType == "" || profile.ByAppType == nil {
		return false
	}
	_, ok := profile.ByAppType[appType]
	return ok
}

// appTypeProfile returns the AppTypeProfile for appType, or a zero profile when
// the app_type has no entry (so facet builders always have a value to read).
func appTypeProfile(profile *cfg.ExecutionProfile, appType string) cfg.AppTypeProfile {
	if profile == nil || appType == "" || profile.ByAppType == nil {
		return cfg.AppTypeProfile{}
	}
	return profile.ByAppType[appType]
}

// buildUnitsFacet renders facet 1. When stage is non-empty only that stage is
// included; otherwise every stage declared for the app_type is included. The
// DefaultClass is surfaced so the operator can see what unlisted units resolve
// to. For every included stage it also resolves the working set through
// ExecutionProfile.SuppressNoise, so the noise filter is shown as the live
// kept/suppressed view it produces rather than a static class list.
func buildUnitsFacet(profile *cfg.ExecutionProfile, prof cfg.AppTypeProfile, appType, stage string) *unitsFacet {
	byStage := map[string]cfg.RelevanceClasses{}
	if prof.Relevance != nil {
		if stage != "" {
			if classes, ok := prof.Relevance[stage]; ok {
				byStage[stage] = classes
			}
		} else {
			for s, classes := range prof.Relevance {
				byStage[s] = classes
			}
		}
	}
	working := map[string]workingSetView{}
	for s, classes := range byStage {
		working[s] = resolveWorkingSet(profile, appType, s, classes)
	}
	return &unitsFacet{
		DefaultClass: profile.EffectiveDefaultClass(),
		ByStage:      byStage,
		WorkingSet:   working,
	}
}

// resolveWorkingSet runs the live noise filter for one stage: it feeds the
// stage's listed candidates through ExecutionProfile.SuppressNoise and projects
// the resulting reversible WorkingSet into the kept/suppressed view. This is the
// production caller for the suppression API — the resolved working set the
// design (§2) promises, computed rather than merely described.
func resolveWorkingSet(profile *cfg.ExecutionProfile, appType, stage string, classes cfg.RelevanceClasses) workingSetView {
	ws := profile.SuppressNoise(appType, stage, stageCandidates(classes))
	return workingSetView{Kept: ws.Kept(), Suppressed: ws.Suppressed()}
}

// stageCandidates returns every unit a stage lists across its three classes, in
// a stable order (core, then situational, then noise) with duplicates removed,
// so a unit (mis)listed in two classes is offered to the filter once. This is
// the candidate set the noise filter classifies.
func stageCandidates(classes cfg.RelevanceClasses) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{classes.Core, classes.Situational, classes.Noise} {
		for _, u := range list {
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// buildTopologyFacet renders facet 2 from the resolved profile's topology.
func buildTopologyFacet(prof cfg.AppTypeProfile) *topologyFacet {
	t := prof.Topology
	return &topologyFacet{
		Executors:            t.Executors,
		VerifiersPerExecutor: t.VerifiersPerExecutor,
		Reviewers:            t.Reviewers,
		VerifierSequence:     t.VerifierSequence,
	}
}

// buildLensesFacet renders facet 3 from the resolved profile's lenses.
func buildLensesFacet(prof cfg.AppTypeProfile) *lensesFacet {
	l := prof.Lenses
	return &lensesFacet{
		LensSet:         l.LensSet,
		LensConcurrency: l.LensConcurrency,
	}
}

// renderRelevance emits the result as JSON (stable envelope) or as the
// human-readable facet view.
func renderRelevance(opts *runRelevanceOptions, result relevanceResult) error {
	if opts.jsonOut {
		return writeJSON(opts.stdout, result)
	}
	return printRelevanceHuman(opts.stdout, result)
}

// printRelevanceHuman renders the resolution context header followed by only
// the requested facet(s).
func printRelevanceHuman(w io.Writer, result relevanceResult) error {
	fmt.Fprintf(w, "Execution profile (filter: %s)\n", result.Filter)
	fmt.Fprintf(w, "  app_type : %s\n", relevanceAppTypeLabel(result.AppType))
	fmt.Fprintf(w, "  selector : %s\n", result.AppTypeSource)
	fmt.Fprintf(w, "  matched  : %t\n", result.Matched)
	fmt.Fprintln(w)

	if result.Units != nil {
		printUnitsHuman(w, result.Units, result.Stage)
	}
	if result.Topology != nil {
		printTopologyHuman(w, result.Topology)
	}
	if result.Lenses != nil {
		printLensesHuman(w, result.Lenses)
	}
	return nil
}

// relevanceAppTypeLabel renders an empty app_type as "(none)" so the header
// never shows a hanging blank.
func relevanceAppTypeLabel(appType string) string {
	if appType == "" {
		return "(none)"
	}
	return appType
}

func printUnitsHuman(w io.Writer, units *unitsFacet, stage string) {
	if stage != "" {
		fmt.Fprintf(w, "units (stage: %s, default_class: %s)\n", stage, units.DefaultClass)
	} else {
		fmt.Fprintf(w, "units (default_class: %s)\n", units.DefaultClass)
	}
	if len(units.ByStage) == 0 {
		fmt.Fprintln(w, "  (no relevance classes declared for this app_type)")
		fmt.Fprintln(w)
		return
	}
	for _, s := range sortedStringKeys(units.ByStage) {
		classes := units.ByStage[s]
		fmt.Fprintf(w, "  [%s]\n", s)
		fmt.Fprintf(w, "    core        : %s\n", joinUnits(classes.Core))
		fmt.Fprintf(w, "    situational : %s\n", joinUnits(classes.Situational))
		fmt.Fprintf(w, "    noise       : %s\n", joinUnits(classes.Noise))
		if ws, ok := units.WorkingSet[s]; ok {
			fmt.Fprintf(w, "    working_set : %s\n", joinUnits(ws.Kept))
			fmt.Fprintf(w, "    suppressed  : %s\n", joinUnits(ws.Suppressed))
		}
	}
	fmt.Fprintln(w)
}

func printTopologyHuman(w io.Writer, t *topologyFacet) {
	fmt.Fprintln(w, "topology")
	fmt.Fprintf(w, "  executors              : %d\n", t.Executors)
	fmt.Fprintf(w, "  verifiers_per_executor : %d\n", t.VerifiersPerExecutor)
	fmt.Fprintf(w, "  reviewers              : %s\n", emptyAsDash(t.Reviewers))
	fmt.Fprintf(w, "  verifier_sequence      : %s\n", joinUnits(t.VerifierSequence))
	fmt.Fprintln(w)
}

func printLensesHuman(w io.Writer, l *lensesFacet) {
	fmt.Fprintln(w, "lenses")
	fmt.Fprintf(w, "  lens_set         : %s\n", joinUnits(l.LensSet))
	fmt.Fprintf(w, "  lens_concurrency : %s\n", emptyAsDash(l.LensConcurrency))
	fmt.Fprintln(w)
}

// joinUnits renders a unit list as a comma-joined string, or "-" when empty, so
// every facet line is visually balanced.
func joinUnits(list []string) string {
	if len(list) == 0 {
		return "-"
	}
	return strings.Join(list, ", ")
}

// sortedStringKeys returns the keys of a RelevanceClasses-valued map in
// deterministic order so the human render and any diff stay stable.
func sortedStringKeys(m map[string]cfg.RelevanceClasses) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
