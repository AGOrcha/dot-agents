package worktree

// Task/plan-first app_type resolution for `da worktree create`.
//
// This is a deliberately tiny, standalone reader of the two canonical workflow
// files a create invocation may reference — TASKS.yaml and PLAN.yaml — using
// minimal local structs (only the id / app_type / default_app_type fields it
// needs). It does NOT import commands/workflow's CanonicalTaskFile/CanonicalPlan:
// the create command needs one field from each file, not the heavy coupling to
// (and construction cost of) the workflow package. This mirrors the standalone
// minimal-YAML precedent in internal/adapters/builtin/sdd-register/ingest.go.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// planTaskFile is the minimal shape read from a canonical TASKS.yaml
// (.agents/workflow/plans/<plan>/TASKS.yaml): only each task's id and app_type.
type planTaskFile struct {
	Tasks []struct {
		ID      string `yaml:"id"`
		AppType string `yaml:"app_type"`
	} `yaml:"tasks"`
}

// planFile is the minimal shape read from a canonical PLAN.yaml
// (.agents/workflow/plans/<plan>/PLAN.yaml): only the plan-wide default_app_type.
type planFile struct {
	DefaultAppType string `yaml:"default_app_type"`
}

// planDir builds the canonical plan directory path under the repo root.
func planDir(repoRoot, plan string) string {
	return filepath.Join(repoRoot, ".agents", "workflow", "plans", plan)
}

// taskAppType reads <repoRoot>/.agents/workflow/plans/<plan>/TASKS.yaml and
// returns the app_type of the task whose id == taskID. A missing file, missing
// task, or parse error returns "" so the caller falls through to the next
// precedence tier — resolving app_type is best-effort, never a hard failure.
func taskAppType(repoRoot, plan, taskID string) string {
	data, err := os.ReadFile(filepath.Join(planDir(repoRoot, plan), "TASKS.yaml"))
	if err != nil {
		return ""
	}
	var tf planTaskFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return ""
	}
	for _, t := range tf.Tasks {
		if t.ID == taskID {
			return t.AppType
		}
	}
	return ""
}

// planDefaultAppType reads <repoRoot>/.agents/workflow/plans/<plan>/PLAN.yaml and
// returns its default_app_type. A missing file or parse error returns "" (fall
// through to the empty tier).
func planDefaultAppType(repoRoot, plan string) string {
	data, err := os.ReadFile(filepath.Join(planDir(repoRoot, plan), "PLAN.yaml"))
	if err != nil {
		return ""
	}
	var pf planFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return ""
	}
	return pf.DefaultAppType
}

// resolveEffectiveAppType resolves the app_type to load + record for a create,
// applying this precedence (highest first) BEFORE the execution_profile lookup:
//
//  1. appTypeFlag (--app-type) when non-empty — an explicit override.
//  2. --task's app_type in the plan's TASKS.yaml (requires --plan to locate it).
//  3. --plan's default_app_type in PLAN.yaml (when --task is unset or its
//     app_type is empty).
//  4. "" — nothing resolved.
//
// It is deliberately graceful: a missing plan/task file, missing task, or parse
// error falls through to the next tier rather than failing. The one loud case is
// --task given without --plan: the plan dir cannot be located, so it warns and
// falls through to tiers 3/4.
func resolveEffectiveAppType(warn io.Writer, repoRoot, appTypeFlag, plan, task string) string {
	if appTypeFlag != "" {
		return appTypeFlag
	}
	if task != "" {
		if plan == "" {
			fmt.Fprintf(warn, "warning: --task %q needs --plan to locate its TASKS.yaml — cannot resolve app_type from the task\n", task)
		} else if at := taskAppType(repoRoot, plan, task); at != "" {
			return at
		}
	}
	if plan != "" {
		if at := planDefaultAppType(repoRoot, plan); at != "" {
			return at
		}
	}
	return ""
}
