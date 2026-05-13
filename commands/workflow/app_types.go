package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

type workflowAppTypesView struct {
	Project  string                 `json:"project"`
	Path     string                 `json:"path"`
	Source   string                 `json:"source"`
	AppTypes []workflowAppTypeEntry `json:"app_types"`
}

type workflowAppTypeEntry struct {
	Name                 string   `json:"name"`
	VerifierSequence     []string `json:"verifier_sequence"`
	Recommended          bool     `json:"recommended,omitempty"`
	AliasOf              string   `json:"alias_of,omitempty"`
	RecommendationReason string   `json:"recommendation_reason,omitempty"`
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
		if strings.TrimSpace(format) != "" {
			return fmt.Errorf("--format cannot be combined with --json")
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
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
		fmt.Fprintf(os.Stdout, "  Add app_type_verifier_map entries to: %s\n", config.DisplayPath(view.Source))
		return nil
	}

	ui.Header("Workflow App Types")
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Bold, view.Project, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, view.Path, ui.Reset)
	fmt.Fprintln(os.Stdout)

	for _, entry := range view.AppTypes {
		suffix := ""
		switch {
		case entry.AliasOf != "":
			suffix = "  alias of " + entry.AliasOf
		case entry.Recommended:
			suffix = "  recommended"
		}
		fmt.Fprintf(os.Stdout, "  %-24s -> [%s]%s\n", entry.Name, strings.Join(entry.VerifierSequence, ", "), suffix)
	}

	if verbose {
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

	if recommended, ok := singleRecommendedAppType(view.AppTypes); ok {
		fmt.Fprintln(os.Stdout)
		ui.Section("Authoring Examples")
		fmt.Fprintf(os.Stdout, "  --app-type %s\n", recommended.Name)
		fmt.Fprintf(os.Stdout, "  app_type: %s\n", recommended.Name)
		fmt.Fprintf(os.Stdout, "  default_app_type: %s\n", recommended.Name)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func collectWorkflowAppTypes(project workflowProjectRef) (workflowAppTypesView, error) {
	view := workflowAppTypesView{
		Project: project.Name,
		Path:    project.Path,
		Source:  config.DisplayPath(filepathAgentsRC(project.Path)),
	}
	d, err := loadAgentsrcFanoutDispatch(project.Path)
	if err != nil {
		return view, err
	}
	if d == nil || len(d.AppTypeVerifierMap) == 0 {
		return view, nil
	}

	entries := make([]workflowAppTypeEntry, 0, len(d.AppTypeVerifierMap))
	for name, seq := range d.AppTypeVerifierMap {
		entries = append(entries, workflowAppTypeEntry{
			Name:             name,
			VerifierSequence: append([]string(nil), seq...),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	markRecommendedAppTypes(entries, project.Name)
	view.AppTypes = entries
	return view, nil
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
		if len(indexes) < 2 {
			continue
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
			continue
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
	return projectPath + string(os.PathSeparator) + config.AgentsRCFile
}