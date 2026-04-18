package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"go.yaml.in/yaml/v3"
)

type WorkflowPreferences struct {
	Verification WorkflowVerificationPrefs `json:"verification" yaml:"verification"`
	Planning     WorkflowPlanningPrefs     `json:"planning" yaml:"planning"`
	Review       WorkflowReviewPrefs       `json:"review" yaml:"review"`
	Execution    WorkflowExecutionPrefs    `json:"execution" yaml:"execution"`
}

type WorkflowVerificationPrefs struct {
	TestCommand                    *string `json:"test_command,omitempty" yaml:"test_command,omitempty"`
	LintCommand                    *string `json:"lint_command,omitempty" yaml:"lint_command,omitempty"`
	RequireRegressionBeforeHandoff *bool   `json:"require_regression_before_handoff,omitempty" yaml:"require_regression_before_handoff,omitempty"`
}

type WorkflowPlanningPrefs struct {
	PlanDirectory         *string `json:"plan_directory,omitempty" yaml:"plan_directory,omitempty"`
	RequirePlanBeforeCode *bool   `json:"require_plan_before_code,omitempty" yaml:"require_plan_before_code,omitempty"`
}

type WorkflowReviewPrefs struct {
	ReviewOrder          *string `json:"review_order,omitempty" yaml:"review_order,omitempty"`
	RequireFindingsFirst *bool   `json:"require_findings_first,omitempty" yaml:"require_findings_first,omitempty"`
}

type WorkflowExecutionPrefs struct {
	PackageManager *string `json:"package_manager,omitempty" yaml:"package_manager,omitempty"`
	Formatter      *string `json:"formatter,omitempty" yaml:"formatter,omitempty"`
}

type WorkflowPreferencesFile struct {
	SchemaVersion       int `json:"schema_version" yaml:"schema_version"`
	WorkflowPreferences `yaml:",inline" json:",inline"`
}

type preferenceSource struct {
	Key    string
	Value  string
	Source string
}

func defaultWorkflowPreferences() WorkflowPreferences {
	trueVal := true
	testCmd := "go test ./..."
	lintCmd := "go vet ./..."
	planDir := ".agents/active"
	reviewOrder := "findings-first"
	pkgMgr := "go"
	formatter := "gofmt"
	return WorkflowPreferences{
		Verification: WorkflowVerificationPrefs{
			TestCommand:                    &testCmd,
			LintCommand:                    &lintCmd,
			RequireRegressionBeforeHandoff: &trueVal,
		},
		Planning: WorkflowPlanningPrefs{
			PlanDirectory:         &planDir,
			RequirePlanBeforeCode: &trueVal,
		},
		Review: WorkflowReviewPrefs{
			ReviewOrder:          &reviewOrder,
			RequireFindingsFirst: &trueVal,
		},
		Execution: WorkflowExecutionPrefs{
			PackageManager: &pkgMgr,
			Formatter:      &formatter,
		},
	}
}

func loadRepoPreferences(projectPath string) (*WorkflowPreferencesFile, error) {
	path := filepath.Join(projectPath, ".agents", "workflow", "preferences.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f WorkflowPreferencesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse repo preferences: %w", err)
	}
	return &f, nil
}

func loadLocalPreferences(project string) (*WorkflowPreferencesFile, error) {
	path := filepath.Join(config.ProjectContextDir(project), "preferences.local.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f WorkflowPreferencesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse local preferences: %w", err)
	}
	return &f, nil
}

func mergePreferences(defaults, repo, local WorkflowPreferences) WorkflowPreferences {
	out := defaults
	mergeVerificationPrefs(&out.Verification, repo.Verification)
	mergePlanningPrefs(&out.Planning, repo.Planning)
	mergeReviewPrefs(&out.Review, repo.Review)
	mergeExecutionPrefs(&out.Execution, repo.Execution)
	mergeVerificationPrefs(&out.Verification, local.Verification)
	mergePlanningPrefs(&out.Planning, local.Planning)
	mergeReviewPrefs(&out.Review, local.Review)
	mergeExecutionPrefs(&out.Execution, local.Execution)
	return out
}

func mergeVerificationPrefs(dst *WorkflowVerificationPrefs, src WorkflowVerificationPrefs) {
	if src.TestCommand != nil {
		dst.TestCommand = src.TestCommand
	}
	if src.LintCommand != nil {
		dst.LintCommand = src.LintCommand
	}
	if src.RequireRegressionBeforeHandoff != nil {
		dst.RequireRegressionBeforeHandoff = src.RequireRegressionBeforeHandoff
	}
}

func mergePlanningPrefs(dst *WorkflowPlanningPrefs, src WorkflowPlanningPrefs) {
	if src.PlanDirectory != nil {
		dst.PlanDirectory = src.PlanDirectory
	}
	if src.RequirePlanBeforeCode != nil {
		dst.RequirePlanBeforeCode = src.RequirePlanBeforeCode
	}
}

func mergeReviewPrefs(dst *WorkflowReviewPrefs, src WorkflowReviewPrefs) {
	if src.ReviewOrder != nil {
		dst.ReviewOrder = src.ReviewOrder
	}
	if src.RequireFindingsFirst != nil {
		dst.RequireFindingsFirst = src.RequireFindingsFirst
	}
}

func mergeExecutionPrefs(dst *WorkflowExecutionPrefs, src WorkflowExecutionPrefs) {
	if src.PackageManager != nil {
		dst.PackageManager = src.PackageManager
	}
	if src.Formatter != nil {
		dst.Formatter = src.Formatter
	}
}

func resolvePreferences(projectPath, project string) (WorkflowPreferences, error) {
	defaults := defaultWorkflowPreferences()
	var repo WorkflowPreferences
	repoFile, err := loadRepoPreferences(projectPath)
	if err != nil {
		return defaults, err
	}
	if repoFile != nil {
		repo = repoFile.WorkflowPreferences
	}
	var local WorkflowPreferences
	localFile, err := loadLocalPreferences(project)
	if err != nil {
		return defaults, err
	}
	if localFile != nil {
		local = localFile.WorkflowPreferences
	}
	return mergePreferences(defaults, repo, local), nil
}

var knownPreferenceKeys = map[string]struct{}{
	"verification.test_command":                      {},
	"verification.lint_command":                      {},
	"verification.require_regression_before_handoff": {},
	"planning.plan_directory":                        {},
	"planning.require_plan_before_code":              {},
	"review.review_order":                            {},
	"review.require_findings_first":                  {},
	"execution.package_manager":                      {},
	"execution.formatter":                            {},
}

func isValidPreferenceKey(key string) bool {
	_, ok := knownPreferenceKeys[key]
	return ok
}

func setLocalPreference(project, key, value string) error {
	path := filepath.Join(config.ProjectContextDir(project), "preferences.local.yaml")
	var f WorkflowPreferencesFile
	data, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("parse local preferences: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f.SchemaVersion = 1
	if err := applyPreferenceKey(&f.WorkflowPreferences, key, value); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	out, err := yaml.Marshal(&f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func applyPreferenceKey(p *WorkflowPreferences, key, value string) error {
	switch key {
	case "verification.test_command":
		p.Verification.TestCommand = &value
	case "verification.lint_command":
		p.Verification.LintCommand = &value
	case "verification.require_regression_before_handoff":
		b := value == "true"
		p.Verification.RequireRegressionBeforeHandoff = &b
	case "planning.plan_directory":
		p.Planning.PlanDirectory = &value
	case "planning.require_plan_before_code":
		b := value == "true"
		p.Planning.RequirePlanBeforeCode = &b
	case "review.review_order":
		p.Review.ReviewOrder = &value
	case "review.require_findings_first":
		b := value == "true"
		p.Review.RequireFindingsFirst = &b
	case "execution.package_manager":
		p.Execution.PackageManager = &value
	case "execution.formatter":
		p.Execution.Formatter = &value
	default:
		return deps.ErrorWithHints(
			fmt.Sprintf("unknown preference key %q", key),
			"Run `dot-agents workflow prefs` to list valid preference keys.",
		)
	}
	return nil
}

func resolvePreferencesWithSources(projectPath, project string) ([]preferenceSource, error) {
	defaults := defaultWorkflowPreferences()
	var repo WorkflowPreferences
	repoFile, err := loadRepoPreferences(projectPath)
	if err != nil {
		return nil, err
	}
	if repoFile != nil {
		repo = repoFile.WorkflowPreferences
	}
	var local WorkflowPreferences
	localFile, err := loadLocalPreferences(project)
	if err != nil {
		return nil, err
	}
	if localFile != nil {
		local = localFile.WorkflowPreferences
	}
	resolved := mergePreferences(defaults, repo, local)

	strSrc := func(_, r, l *string) string {
		if l != nil {
			return "local"
		}
		if r != nil {
			return "repo"
		}
		return "default"
	}
	boolSrc := func(_, r, l *bool) string {
		if l != nil {
			return "local"
		}
		if r != nil {
			return "repo"
		}
		return "default"
	}

	return []preferenceSource{
		{"verification.test_command", strPtrVal(resolved.Verification.TestCommand), strSrc(defaults.Verification.TestCommand, repo.Verification.TestCommand, local.Verification.TestCommand)},
		{"verification.lint_command", strPtrVal(resolved.Verification.LintCommand), strSrc(defaults.Verification.LintCommand, repo.Verification.LintCommand, local.Verification.LintCommand)},
		{"verification.require_regression_before_handoff", boolPtrStr(resolved.Verification.RequireRegressionBeforeHandoff), boolSrc(defaults.Verification.RequireRegressionBeforeHandoff, repo.Verification.RequireRegressionBeforeHandoff, local.Verification.RequireRegressionBeforeHandoff)},
		{"planning.plan_directory", strPtrVal(resolved.Planning.PlanDirectory), strSrc(defaults.Planning.PlanDirectory, repo.Planning.PlanDirectory, local.Planning.PlanDirectory)},
		{"planning.require_plan_before_code", boolPtrStr(resolved.Planning.RequirePlanBeforeCode), boolSrc(defaults.Planning.RequirePlanBeforeCode, repo.Planning.RequirePlanBeforeCode, local.Planning.RequirePlanBeforeCode)},
		{"review.review_order", strPtrVal(resolved.Review.ReviewOrder), strSrc(defaults.Review.ReviewOrder, repo.Review.ReviewOrder, local.Review.ReviewOrder)},
		{"review.require_findings_first", boolPtrStr(resolved.Review.RequireFindingsFirst), boolSrc(defaults.Review.RequireFindingsFirst, repo.Review.RequireFindingsFirst, local.Review.RequireFindingsFirst)},
		{"execution.package_manager", strPtrVal(resolved.Execution.PackageManager), strSrc(defaults.Execution.PackageManager, repo.Execution.PackageManager, local.Execution.PackageManager)},
		{"execution.formatter", strPtrVal(resolved.Execution.Formatter), strSrc(defaults.Execution.Formatter, repo.Execution.Formatter, local.Execution.Formatter)},
	}, nil
}

func strPtrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func boolPtrStr(p *bool) string {
	if p == nil {
		return ""
	}
	if *p {
		return "true"
	}
	return "false"
}

func runWorkflowPrefs() error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		prefs, err := resolvePreferences(project.Path, project.Name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(prefs)
	}
	sources, err := resolvePreferencesWithSources(project.Path, project.Name)
	if err != nil {
		return err
	}
	ui.Header("Workflow Preferences")
	currentCategory := ""
	for _, s := range sources {
		parts := strings.SplitN(s.Key, ".", 2)
		if len(parts) == 2 && parts[0] != currentCategory {
			currentCategory = parts[0]
			fmt.Fprintf(os.Stdout, "\n[%s]\n", currentCategory)
		}
		fmt.Fprintf(os.Stdout, "  %-48s %s  (%s)\n", s.Key, s.Value, s.Source)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowPrefsSetLocal(key, value string) error {
	if !isValidPreferenceKey(key) {
		return deps.ErrorWithHints(
			fmt.Sprintf("unknown preference key %q", key),
			"Run `dot-agents workflow prefs` to see valid preference keys.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if err := setLocalPreference(project.Name, key, value); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Set %s = %s  (local)", key, value))
	return nil
}

func runWorkflowPrefsSetShared(key, value string) error {
	if !isValidPreferenceKey(key) {
		return deps.ErrorWithHints(
			fmt.Sprintf("unknown preference key %q", key),
			"Run `dot-agents workflow prefs` to see valid preference keys.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	sources, err := resolvePreferencesWithSources(project.Path, project.Name)
	if err != nil {
		return err
	}
	currentVal := ""
	for _, s := range sources {
		if s.Key == key {
			currentVal = s.Value
			break
		}
	}
	id := fmt.Sprintf("pref-%s-%s", strings.ReplaceAll(key, ".", "-"), time.Now().UTC().Format("20060102T150405Z"))
	targetPath := filepath.Join(".agents", "workflow", "preferences.yaml")
	proposal := &config.Proposal{
		SchemaVersion: 1,
		ID:            id,
		Status:        "pending",
		Type:          "setting",
		Action:        "modify",
		Target:        targetPath,
		Rationale:     fmt.Sprintf("Set shared workflow preference %s to %q (was %q)", key, value, currentVal),
		Content:       fmt.Sprintf("%s: %s\n", key, value),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		CreatedBy:     "workflow prefs set-shared",
	}
	if err := config.SaveProposal(proposal, config.ProposalPath(id)); err != nil {
		return fmt.Errorf("save proposal: %w", err)
	}
	ui.Info(fmt.Sprintf("Proposal %s created for shared preference change.", id))
	ui.Info("Run 'dot-agents review' to approve and apply.")
	return nil
}
