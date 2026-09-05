package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

// appTypeSnapshot produces the effective-config Snapshot that app-type detection
// reads. It is a package var so tests can stub the snapshot directly; production
// consumes the read-only, lock-backed resolution path (LayeredResolver.
// ResolveLocked) so `workflow app-types` reads the SAME units-lock-backed
// effective config as `da config explain` — offline, no fetch, no lock mutation
// (config-distribution-model §7A units model). For a flat project (no `extends`)
// ResolveLocked degrades to the FLAT layer set, so a project with no lockfile
// still resolves exactly as before.
var appTypeSnapshot func(projectPath string) (*config.Snapshot, error) = func(projectPath string) (*config.Snapshot, error) {
	return config.NewLayeredResolver().ResolveLocked(projectPath)
}

type workflowAppTypesView struct {
	Project  string                 `json:"project"`
	Path     string                 `json:"path"`
	Source   string                 `json:"source"`
	AppTypes []workflowAppTypeEntry `json:"app_types"`
	// Incomplete lists layers skipped during offline resolution whose absence may
	// have shrunk the effective execution_profile (and thus the app_type list).
	// Empty when fully resolved.
	Incomplete []string `json:"incomplete,omitempty"`
}

type workflowAppTypeEntry struct {
	Name             string   `json:"name"`
	VerifierSequence []string `json:"verifier_sequence"`
	// Model is the app_type's resolved model route (execution_profile facet 5),
	// omitted when the profile pins none. It is the read-only projection an
	// orchestrator queries to route a worker's model tier mechanically instead of
	// reading an advisory lesson — see
	// .agents/proposals/model-facet-apptypeprofile.md §D4.
	Model                string `json:"model,omitempty"`
	Recommended          bool   `json:"recommended,omitempty"`
	AliasOf              string `json:"alias_of,omitempty"`
	RecommendationReason string `json:"recommendation_reason,omitempty"`
}

func runWorkflowAppTypes(format string, verbose bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	view, err := collectWorkflowAppTypes(project)
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		return renderWorkflowAppTypesJSON(view, format)
	}
	// Warn (to stderr) when offline resolution skipped a layer, since that may
	// have shrunk the list. stderr keeps --format/JSON consumers uncorrupted.
	renderWorkflowAppTypesIncomplete(view)
	if strings.TrimSpace(format) != "" {
		snippet, err := renderWorkflowAppTypeFormat(view, format)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, snippet)
		return nil
	}
	if len(view.AppTypes) == 0 {
		fmt.Fprintln(os.Stdout, "No app_types found for this repo.")
		fmt.Fprintf(os.Stdout, "  Add execution_profile.by_app_type entries (topology.verifier_sequence) to: %s\n", config.DisplayPath(view.Source))
		return nil
	}

	renderWorkflowAppTypesHeader(view)
	renderWorkflowAppTypeList(view.AppTypes)
	if verbose {
		renderWorkflowAppTypeDetails(view)
	}
	renderWorkflowAppTypeAuthoring(view.AppTypes)
	fmt.Fprintln(os.Stdout)
	return nil
}

func renderWorkflowAppTypesJSON(view workflowAppTypesView, format string) error {
	if strings.TrimSpace(format) != "" {
		return fmt.Errorf("--format cannot be combined with --json")
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(view)
}

// renderWorkflowAppTypesIncomplete prints (to stderr) a note for each layer that
// offline resolution skipped, so a silently-shrunk execution_profile is never
// passed off as the complete list. No-op when resolution was complete.
func renderWorkflowAppTypesIncomplete(view workflowAppTypesView) {
	for _, note := range view.Incomplete {
		fmt.Fprintf(os.Stderr, "note: app-types may be incomplete — layer not resolved offline: %s (run `da install` / `da config sync`)\n", note)
	}
}

func renderWorkflowAppTypesHeader(view workflowAppTypesView) {
	ui.Header("Workflow App Types")
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Bold, view.Project, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, view.Path, ui.Reset)
	fmt.Fprintln(os.Stdout)
}

func renderWorkflowAppTypeList(entries []workflowAppTypeEntry) {
	for _, entry := range entries {
		suffix := ""
		switch {
		case entry.AliasOf != "":
			suffix = "  alias of " + entry.AliasOf
		case entry.Recommended:
			suffix = "  recommended"
		}
		if entry.Model != "" {
			suffix = "  model=" + entry.Model + suffix
		}
		fmt.Fprintf(os.Stdout, "  %-24s -> [%s]%s\n", entry.Name, strings.Join(entry.VerifierSequence, ", "), suffix)
	}
}

func renderWorkflowAppTypeDetails(view workflowAppTypesView) {
	fmt.Fprintln(os.Stdout)
	ui.Section("Details")
	fmt.Fprintf(os.Stdout, "  source: %s\n", view.Source)
	for _, entry := range view.AppTypes {
		if entry.RecommendationReason == "" && entry.AliasOf == "" {
			continue
		}
		detail := entry.RecommendationReason
		if detail == "" && entry.AliasOf != "" {
			detail = "alias of " + entry.AliasOf
		}
		fmt.Fprintf(os.Stdout, "  %s: %s\n", entry.Name, detail)
	}
}

func renderWorkflowAppTypeAuthoring(entries []workflowAppTypeEntry) {
	recommended, ok := singleRecommendedAppType(entries)
	if !ok {
		return
	}
	fmt.Fprintln(os.Stdout)
	ui.Section("Authoring Examples")
	fmt.Fprintf(os.Stdout, "  --app-type %s\n", recommended.Name)
	fmt.Fprintf(os.Stdout, "  app_type: %s\n", recommended.Name)
	fmt.Fprintf(os.Stdout, "  default_app_type: %s\n", recommended.Name)
}

func collectWorkflowAppTypes(project workflowProjectRef) (workflowAppTypesView, error) {
	view := workflowAppTypesView{
		Project: project.Name,
		Path:    project.Path,
		Source:  config.DisplayPath(filepathAgentsRC(project.Path)),
	}

	profiles, incomplete, err := resolveEffectiveAppTypeProfiles(project.Path)
	if err != nil {
		return view, err
	}
	view.Incomplete = incomplete
	if len(profiles) == 0 {
		return view, nil
	}

	entries := make([]workflowAppTypeEntry, 0, len(profiles))
	for name, prof := range profiles {
		entries = append(entries, workflowAppTypeEntry{
			Name:             name,
			VerifierSequence: append([]string(nil), prof.Topology.VerifierSequence...),
			Model:            prof.ModelRef(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	markRecommendedAppTypes(entries, project.Name)
	view.AppTypes = entries
	return view, nil
}

// resolveEffectiveAppTypeMap is the verifier-sequence narrowing of
// resolveEffectiveAppTypeProfiles: app_type -> its ordered verifier sequence,
// which is all app_type validation needs.
func resolveEffectiveAppTypeMap(projectPath string) (map[string][]string, []string, error) {
	profiles, notes, err := resolveEffectiveAppTypeProfiles(projectPath)
	if err != nil {
		return nil, nil, err
	}
	return verifierSequencesOf(profiles), notes, nil
}

// resolveEffectiveAppTypeProfiles reads the effective per-app_type profiles from
// the units-lock-backed config Snapshot so app-type detection sees the same merged
// effective config every other surface does (config-distribution-model §7A units
// model), rather than re-reading only the repo-local .agentsrc.json. Resolution is
// read-only and offline: it reconstructs the imported layers from the units lock at
// their locked digests without ever triggering a fetch (the same seam `da config
// explain` parses through). The profiles live in typed
// execution_profile.by_app_type (the deprecated app_type_verifier_map is folded
// into it on load), read directly off snap.Effective.
//
// It returns whole profiles — every facet (topology, lenses, graph_backend, model)
// — rather than only the verifier sequence, so a renderer needing more than one
// facet resolves the snapshot once and both projections agree on the app_type set
// by construction.
//
// A missing repo-local manifest is not an error here: it yields an empty map, so
// `workflow app-types` prints the same "No app_types found" notice it did before
// the snapshot refactor instead of failing.
//
// The second return value carries human-readable notes for any layer that was
// SKIPPED during offline resolution (an optional `extends` entry whose lock/cache
// is missing, or a protected-field drop). Such a skip can shrink the effective
// execution_profile, so the notes let the caller warn the user rather than
// silently print an incomplete list (PR #207 adversarial-lens fix).
func resolveEffectiveAppTypeProfiles(projectPath string) (map[string]config.AppTypeProfile, []string, error) {
	snap, err := appTypeSnapshot(projectPath)
	if err != nil {
		if isMissingManifestErr(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return appTypeProfilesFromExecution(snap.Effective.ExecutionProfile), incompleteResolutionNotes(snap.Warnings), nil
}

// appTypeProfilesFromExecution selects the execution_profile entries app-type
// detection renders: the successor to the retired app_type_verifier_map (legacy
// entries are folded into execution_profile.topology.verifier_sequence on load).
// Only app_types whose topology declares a non-empty verifier_sequence are
// included. Returns nil when nothing qualifies.
func appTypeProfilesFromExecution(ep *config.ExecutionProfile) map[string]config.AppTypeProfile {
	if ep == nil || len(ep.ByAppType) == 0 {
		return nil
	}
	out := make(map[string]config.AppTypeProfile, len(ep.ByAppType))
	for appType, prof := range ep.ByAppType {
		if len(prof.Topology.VerifierSequence) == 0 {
			continue
		}
		out[appType] = prof
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// appTypeVerifierSequencesFromExecution projects the execution_profile topology
// into the app_type -> verifier sequence map app-type validation reads.
func appTypeVerifierSequencesFromExecution(ep *config.ExecutionProfile) map[string][]string {
	return verifierSequencesOf(appTypeProfilesFromExecution(ep))
}

// verifierSequencesOf narrows a selected profile set to the app_type -> verifier
// sequence map, copying each sequence so callers cannot mutate resolved config.
func verifierSequencesOf(profiles map[string]config.AppTypeProfile) map[string][]string {
	if len(profiles) == 0 {
		return nil
	}
	out := make(map[string][]string, len(profiles))
	for appType, prof := range profiles {
		out[appType] = append([]string(nil), prof.Topology.VerifierSequence...)
	}
	return out
}

// incompleteResolutionNotes turns the snapshot's resolution warnings into
// user-facing notes for the warnings that can SHRINK the effective config — a
// skipped optional layer or a dropped (protected-field) value. A cache_hit_offline
// warning means the layer WAS resolved (just from cache), so it never indicates an
// incomplete map and is excluded. Returns nil when nothing was skipped.
func incompleteResolutionNotes(warnings []config.ProvenanceWarning) []string {
	var notes []string
	for _, w := range warnings {
		if !strings.HasPrefix(w.Outcome, "optional_skipped") && w.Outcome != "dropped" {
			continue
		}
		notes = append(notes, fmt.Sprintf("%s (%s)", w.FieldPath, w.Outcome))
	}
	return notes
}

// isMissingManifestErr reports whether err is the resolver's "no repo-local
// manifest" condition. The FlatResolver surfaces an absent .agentsrc.json as a
// fatal error; app-type detection treats absence as "no app_types" instead, so
// the pre-refactor no-file behavior is preserved.
//
// Follow-up (internal/config): the substring match below is a fragile
// cross-package contract on the exact wording of FlatResolver.loadLayers' error
// (resolver.go ~147). It should become errors.Is against a shared typed sentinel
// (e.g. config.ErrNoManifest) once internal/config exports one — that is a config
// change outside this PR's write scope. TestIsMissingManifestErr pins the current
// string so a wording drift fails in CI until the sentinel lands.
func isMissingManifestErr(err error) bool {
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return true
	}
	return strings.Contains(err.Error(), "no "+config.AgentsRCFile+" found")
}

func markRecommendedAppTypes(entries []workflowAppTypeEntry, projectName string) {
	if len(entries) == 1 {
		entries[0].Recommended = true
		entries[0].RecommendationReason = "only available app_type"
		return
	}

	groups := make(map[string][]int)
	for i, entry := range entries {
		groups[sequenceKey(entry.VerifierSequence)] = append(groups[sequenceKey(entry.VerifierSequence)], i)
	}
	for _, indexes := range groups {
		markRecommendedAppTypeGroup(entries, indexes, projectName)
	}
}

// markRecommendedAppTypeGroup resolves a single group of entries sharing the
// same verifier sequence: when exactly one non-repo-named alias exists it is
// marked recommended and the rest become aliases of it.
func markRecommendedAppTypeGroup(entries []workflowAppTypeEntry, indexes []int, projectName string) {
	if len(indexes) < 2 {
		return
	}
	nonProject := -1
	for _, idx := range indexes {
		if entries[idx].Name != projectName {
			if nonProject != -1 {
				nonProject = -2
				break
			}
			nonProject = idx
		}
	}
	if nonProject < 0 {
		return
	}
	entries[nonProject].Recommended = true
	entries[nonProject].RecommendationReason = "non-repo alias preferred for authoring"
	for _, idx := range indexes {
		if idx == nonProject {
			continue
		}
		entries[idx].AliasOf = entries[nonProject].Name
	}
}

func singleRecommendedAppType(entries []workflowAppTypeEntry) (workflowAppTypeEntry, bool) {
	var recommended *workflowAppTypeEntry
	for i := range entries {
		if !entries[i].Recommended {
			continue
		}
		if recommended != nil {
			return workflowAppTypeEntry{}, false
		}
		recommended = &entries[i]
	}
	if recommended == nil {
		return workflowAppTypeEntry{}, false
	}
	return *recommended, true
}

func renderWorkflowAppTypeFormat(view workflowAppTypesView, format string) (string, error) {
	recommended, ok := singleRecommendedAppType(view.AppTypes)
	if !ok {
		return "", fmt.Errorf("--format requires exactly one recommended app_type; run `da workflow app-types` to inspect all available values")
	}
	switch strings.TrimSpace(format) {
	case "flag":
		return "--app-type " + recommended.Name, nil
	case "task":
		return "app_type: " + recommended.Name, nil
	case "plan":
		return "default_app_type: " + recommended.Name, nil
	case "doc":
		return fmt.Sprintf("Use app_type: %s in TASKS.yaml for this repo.", recommended.Name), nil
	default:
		return "", fmt.Errorf("unknown --format %q (want flag, task, plan, or doc)", format)
	}
}

func sequenceKey(seq []string) string {
	return strings.Join(seq, "\x00")
}

func filepathAgentsRC(projectPath string) string {
	return filepath.Join(projectPath, config.AgentsRCFile)
}

// validateTaskAppType checks a candidate app_type against the same effective
// execution_profile.by_app_type set `workflow app-types` renders, so `task add`
// and `task update` agree on what's valid without a second source of truth.
//
// appType == "" is always accepted silently (nothing to validate — the field is
// simply left unset).
//
// When resolution itself fails, or resolves but yields no app_type entries (no
// execution profile configured for this repo), the candidate is accepted as-is:
// a non-empty warning is returned for the caller to print, but err is nil. This
// keeps `task update --app-type` usable for repos/teams that haven't adopted
// execution profiles yet, per the same "absence isn't fatal" posture
// resolveEffectiveAppTypeMap already takes for `workflow app-types`.
//
// When resolution DOES produce a non-empty set and appType isn't in it, err
// spells out the valid values directly in its message (rather than as a
// deps.ErrorWithHints hint) so the full list survives regardless of how the
// caller renders the error — the same choice renderWorkflowAppTypeFormat's
// own validation error makes a few lines below.
func validateTaskAppType(projectPath, appType string) (warning string, err error) {
	if appType == "" {
		return "", nil
	}
	appTypeMap, _, resolveErr := resolveEffectiveAppTypeMap(projectPath)
	if resolveErr != nil {
		return fmt.Sprintf("could not resolve app_type set (%v); recording app_type %q without validation", resolveErr, appType), nil
	}
	if len(appTypeMap) == 0 {
		return fmt.Sprintf("no execution profile configured for this repo; recording app_type %q without validation", appType), nil
	}
	if _, ok := appTypeMap[appType]; ok {
		return "", nil
	}
	valid := make([]string, 0, len(appTypeMap))
	for name := range appTypeMap {
		valid = append(valid, name)
	}
	sort.Strings(valid)
	return "", fmt.Errorf("invalid app_type %q; valid values for this repo: %s", appType, strings.Join(valid, ", "))
}
