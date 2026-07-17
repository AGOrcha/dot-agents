package worktree

import (
	"testing"
)

// malformedYAML is a document go.yaml.in/yaml/v3 rejects: a tab where the
// parser demands space indentation. Both planTaskFile and planFile unmarshal
// fail on it, exercising the parse-error fall-through legs.
const malformedYAML = "tasks:\n\t- id: broken\n"

// TestTaskAppTypeMissingFile: a plan dir with no TASKS.yaml resolves to "" so
// the caller falls through to the next precedence tier (the os.ReadFile error
// leg).
func TestTaskAppTypeMissingFile(t *testing.T) {
	if got := taskAppType(t.TempDir(), "plan-absent", "task-go"); got != "" {
		t.Fatalf("taskAppType(missing TASKS.yaml)=%q, want empty", got)
	}
}

// TestTaskAppTypeMalformedYAML: a TASKS.yaml that fails to parse resolves to ""
// (the yaml.Unmarshal error leg) rather than surfacing a hard failure.
func TestTaskAppTypeMalformedYAML(t *testing.T) {
	repo := t.TempDir()
	writeTasksYAML(t, repo, "plan-bad", malformedYAML)
	if got := taskAppType(repo, "plan-bad", "task-go"); got != "" {
		t.Fatalf("taskAppType(malformed TASKS.yaml)=%q, want empty", got)
	}
}

// TestTaskAppTypeTaskNotFound: a well-formed TASKS.yaml that has no task with
// the requested id resolves to "" (the loop-exhausted leg), proving resolution
// is by id and a miss is graceful.
func TestTaskAppTypeTaskNotFound(t *testing.T) {
	repo := t.TempDir()
	writeTasksYAML(t, repo, "plan-wt", tasksGoCliYAML)
	if got := taskAppType(repo, "plan-wt", "task-absent"); got != "" {
		t.Fatalf("taskAppType(unknown id)=%q, want empty", got)
	}
	// Sanity: a present id still resolves, so the miss above is a real miss.
	if got := taskAppType(repo, "plan-wt", "task-go"); got != "go-cli" {
		t.Fatalf("taskAppType(task-go)=%q, want go-cli", got)
	}
}

// TestPlanDefaultAppTypeMissingFile: a plan dir with no PLAN.yaml resolves to ""
// (the os.ReadFile error leg).
func TestPlanDefaultAppTypeMissingFile(t *testing.T) {
	if got := planDefaultAppType(t.TempDir(), "plan-absent"); got != "" {
		t.Fatalf("planDefaultAppType(missing PLAN.yaml)=%q, want empty", got)
	}
}

// TestPlanDefaultAppTypeMalformedYAML: a PLAN.yaml that fails to parse resolves
// to "" (the yaml.Unmarshal error leg).
func TestPlanDefaultAppTypeMalformedYAML(t *testing.T) {
	repo := t.TempDir()
	writePlanYAML(t, repo, "plan-bad", malformedYAML)
	if got := planDefaultAppType(repo, "plan-bad"); got != "" {
		t.Fatalf("planDefaultAppType(malformed PLAN.yaml)=%q, want empty", got)
	}
}
