package workflow

const (
	workflowDefaultNextAction        = "Review active plan"
	workflowDefaultVerificationState = "unknown"

	defaultDelegateProfile        = "loop-worker"
	defaultDelegationFeedbackGoal = "Complete the delegated task within write scope with verification and merge-back."
)

type workflowProjectRef struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
}

type workflowGitSummary struct {
	Branch         string   `json:"branch" yaml:"branch"`
	SHA            string   `json:"sha" yaml:"sha"`
	DirtyFileCount int      `json:"dirty_file_count" yaml:"dirty_file_count"`
	RecentCommits  []string `json:"recent_commits,omitempty" yaml:"-"`
}

type workflowPlanSummary struct {
	Path         string   `json:"path"`
	Title        string   `json:"title"`
	PendingItems []string `json:"pending_items"`
}

type workflowHandoffSummary struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

type workflowProposalSummary struct {
	PendingCount int `json:"pending_count"`
}

type workflowCheckpoint struct {
	SchemaVersion int                `json:"schema_version" yaml:"schema_version"`
	Timestamp     string             `json:"timestamp" yaml:"timestamp"`
	Project       workflowProjectRef `json:"project" yaml:"project"`
	Git           workflowGitSummary `json:"git" yaml:"git"`
	Files         struct {
		Modified []string `json:"modified" yaml:"modified"`
	} `json:"files" yaml:"files"`
	Message      string `json:"message" yaml:"message"`
	Verification struct {
		Status  string `json:"status" yaml:"status"`
		Summary string `json:"summary" yaml:"summary"`
	} `json:"verification" yaml:"verification"`
	NextAction string   `json:"next_action" yaml:"next_action"`
	Blockers   []string `json:"blockers" yaml:"blockers"`
}

type workflowOrientState struct {
	Project           workflowProjectRef             `json:"project"`
	Git               workflowGitSummary             `json:"git"`
	ActivePlans       []workflowPlanSummary          `json:"active_plans"`
	CanonicalPlans    []workflowCanonicalPlanSummary `json:"canonical_plans"`
	Checkpoint        *workflowCheckpoint            `json:"checkpoint"`
	Handoffs          []workflowHandoffSummary       `json:"handoffs"`
	Lessons           []string                       `json:"lessons"`
	Proposals         workflowProposalSummary        `json:"proposals"`
	NextAction        string                         `json:"next_action"`
	NextActionSource  string                         `json:"next_action_source"`
	Warnings          []string                       `json:"warnings"`
	Health            *WorkflowHealthSnapshot        `json:"health,omitempty"`
	Preferences       *WorkflowPreferences           `json:"preferences,omitempty"`
	ActiveDelegations workflowDelegationSummary      `json:"active_delegations"`
	PendingMergeBacks int                            `json:"pending_merge_backs"`
	LocalDrift        *RepoDriftReport               `json:"local_drift,omitempty"`
}

// workflowDelegationSummary is a compact view of active delegation state for orient/status.
type workflowDelegationSummary struct {
	ActiveCount    int `json:"active_count"`
	PendingIntents int `json:"pending_intents"`
}

// CanonicalPlan is the PLAN.yaml schema for .agents/workflow/plans/<id>/PLAN.yaml
type CanonicalPlan struct {
	SchemaVersion        int    `json:"schema_version" yaml:"schema_version"`
	ID                   string `json:"id" yaml:"id"`
	Title                string `json:"title" yaml:"title"`
	Status               string `json:"status" yaml:"status"`
	Summary              string `json:"summary" yaml:"summary"`
	CreatedAt            string `json:"created_at" yaml:"created_at"`
	UpdatedAt            string `json:"updated_at" yaml:"updated_at"`
	Owner                string `json:"owner" yaml:"owner"`
	SuccessCriteria      string `json:"success_criteria" yaml:"success_criteria"`
	VerificationStrategy string `json:"verification_strategy" yaml:"verification_strategy"`
	CurrentFocusTask     string `json:"current_focus_task" yaml:"current_focus_task"`
	DefaultAppType       string `json:"default_app_type,omitempty" yaml:"default_app_type,omitempty"`
}

// CanonicalTaskFile is the TASKS.yaml schema for .agents/workflow/plans/<id>/TASKS.yaml
type CanonicalTaskFile struct {
	SchemaVersion int             `json:"schema_version" yaml:"schema_version"`
	PlanID        string          `json:"plan_id" yaml:"plan_id"`
	Tasks         []CanonicalTask `json:"tasks" yaml:"tasks"`
}

// CanonicalTask is one entry in TASKS.yaml
type CanonicalTask struct {
	ID                   string   `json:"id" yaml:"id"`
	Title                string   `json:"title" yaml:"title"`
	Status               string   `json:"status" yaml:"status"`
	DependsOn            []string `json:"depends_on" yaml:"depends_on"`
	Blocks               []string `json:"blocks" yaml:"blocks"`
	Owner                string   `json:"owner" yaml:"owner"`
	WriteScope           []string `json:"write_scope" yaml:"write_scope"`
	VerificationRequired bool     `json:"verification_required" yaml:"verification_required"`
	Notes                string   `json:"notes" yaml:"notes"`
	AppType              string   `json:"app_type,omitempty" yaml:"app_type,omitempty"`
}

// CanonicalSliceFile is the SLICES.yaml schema for .agents/workflow/plans/<id>/SLICES.yaml
type CanonicalSliceFile struct {
	SchemaVersion int              `json:"schema_version" yaml:"schema_version"`
	PlanID        string           `json:"plan_id" yaml:"plan_id"`
	Slices        []CanonicalSlice `json:"slices" yaml:"slices"`
}

// CanonicalSlice is one bounded sub-slice under a canonical task.
type CanonicalSlice struct {
	ID                string   `json:"id" yaml:"id"`
	ParentTaskID      string   `json:"parent_task_id" yaml:"parent_task_id"`
	Title             string   `json:"title" yaml:"title"`
	Summary           string   `json:"summary" yaml:"summary"`
	Status            string   `json:"status" yaml:"status"`
	DependsOn         []string `json:"depends_on" yaml:"depends_on"`
	WriteScope        []string `json:"write_scope" yaml:"write_scope"`
	VerificationFocus string   `json:"verification_focus" yaml:"verification_focus"`
	Owner             string   `json:"owner" yaml:"owner"`
}

// foldBackArtifact is stored at .agents/active/fold-back/{id}.yaml (Phase 6 fold-back reconciliation).
type foldBackArtifact struct {
	SchemaVersion  int    `json:"schema_version" yaml:"schema_version"`
	ID             string `json:"id" yaml:"id"`
	PlanID         string `json:"plan_id" yaml:"plan_id"`
	TaskID         string `json:"task_id" yaml:"task_id"`
	Observation    string `json:"observation" yaml:"observation"`
	Classification string `json:"classification" yaml:"classification"`
	RoutedTo       string `json:"routed_to" yaml:"routed_to"`
	CreatedAt      string `json:"created_at" yaml:"created_at"`
}

// workflowDelegationCloseoutRecord is stored next to archived delegation + merge-back artifacts (Phase 7).
type workflowDelegationCloseoutRecord struct {
	SchemaVersion int    `json:"schema_version" yaml:"schema_version"`
	PlanID        string `json:"plan_id" yaml:"plan_id"`
	TaskID        string `json:"task_id" yaml:"task_id"`
	DelegationID  string `json:"delegation_id" yaml:"delegation_id"`
	Decision      string `json:"decision" yaml:"decision"`
	Note          string `json:"note,omitempty" yaml:"note,omitempty"`
	ClosedAt      string `json:"closed_at" yaml:"closed_at"`
}

// delegationBundleYAML matches schemas/workflow-delegation-bundle.schema.json (Phase 8).
type delegationBundleYAML struct {
	SchemaVersion int    `yaml:"schema_version"`
	DelegationID  string `yaml:"delegation_id"`
	PlanID        string `yaml:"plan_id"`
	TaskID        string `yaml:"task_id"`
	SliceID       string `yaml:"slice_id,omitempty"`
	Owner         string `yaml:"owner"`
	Worker        struct {
		Profile             string   `yaml:"profile"`
		ProfileVersion      *int     `yaml:"profile_version,omitempty"`
		ProjectOverlayFiles []string `yaml:"project_overlay_files,omitempty"`
	} `yaml:"worker"`
	Selection *struct {
		SelectedBy string `yaml:"selected_by"`
		SelectedAt string `yaml:"selected_at"`
		Reason     string `yaml:"reason,omitempty"`
	} `yaml:"selection,omitempty"`
	Scope struct {
		WriteScope  []string `yaml:"write_scope"`
		Constraints []string `yaml:"constraints,omitempty"`
	} `yaml:"scope"`
	Prompt struct {
		Inline      []string `yaml:"inline,omitempty"`
		PromptFiles []string `yaml:"prompt_files,omitempty"`
	} `yaml:"prompt"`
	Context struct {
		RequiredFiles []string `yaml:"required_files,omitempty"`
		OptionalFiles []string `yaml:"optional_files,omitempty"`
	} `yaml:"context"`
	Verification struct {
		FeedbackGoal               string   `yaml:"feedback_goal"`
		ScenarioTags               []string `yaml:"scenario_tags,omitempty"`
		RegressionArtifacts        []string `yaml:"regression_artifacts,omitempty"`
		HigherLayerValidationQueue string   `yaml:"higher_layer_validation_queue,omitempty"`
		FocusedCommands            []string `yaml:"focused_commands,omitempty"`
		RegressionCommands         []string `yaml:"regression_commands,omitempty"`
		AppType                    string   `yaml:"app_type,omitempty"`
		VerifierSequence           []string `yaml:"verifier_sequence,omitempty"`
		EvidencePolicy             *struct {
			RequireNegativeCoverage *bool `yaml:"require_negative_coverage,omitempty"`
			ClassificationRequired  *bool `yaml:"classification_required,omitempty"`
			SandboxMutations        *bool `yaml:"sandbox_mutations,omitempty"`
			PrimaryChainMax         *int  `yaml:"primary_chain_max,omitempty"`
		} `yaml:"evidence_policy,omitempty"`
	} `yaml:"verification"`
	Closeout struct {
		WorkerMust []string `yaml:"worker_must,omitempty"`
		ParentMust []string `yaml:"parent_must,omitempty"`
	} `yaml:"closeout"`
}

type foldBackProposalFrontmatter struct {
	Title       string `yaml:"title"`
	Observation string `yaml:"observation"`
	PlanID      string `yaml:"plan_id"`
	TaskID      string `yaml:"task_id,omitempty"`
	CreatedAt   string `yaml:"created_at"`
}

// workflowCanonicalPlanSummary is a compact view used in orient/status output
type workflowCanonicalPlanSummary struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Status           string `json:"status"`
	CurrentFocusTask string `json:"current_focus_task"`
	PendingCount     int    `json:"pending_count"`
	BlockedCount     int    `json:"blocked_count"`
	CompletedCount   int    `json:"completed_count"`
}

// VerificationRecord is one line in verification-log.jsonl
type VerificationRecord struct {
	SchemaVersion int      `json:"schema_version"`
	Timestamp     string   `json:"timestamp"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	Command       string   `json:"command"`
	Scope         string   `json:"scope"`
	Summary       string   `json:"summary"`
	Artifacts     []string `json:"artifacts"`
	RecordedBy    string   `json:"recorded_by"`
}

// WorkflowHealthSnapshot is the health.json schema
type WorkflowHealthSnapshot struct {
	SchemaVersion int    `json:"schema_version"`
	Timestamp     string `json:"timestamp"`
	Git           struct {
		InsideRepo     bool   `json:"inside_repo"`
		Branch         string `json:"branch"`
		DirtyFileCount int    `json:"dirty_file_count"`
	} `json:"git"`
	Workflow struct {
		HasActivePlan      bool `json:"has_active_plan"`
		HasCheckpoint      bool `json:"has_checkpoint"`
		PendingProposals   int  `json:"pending_proposals"`
		CanonicalPlanCount int  `json:"canonical_plan_count"`
	} `json:"workflow"`
	Tooling struct {
		MCP       string `json:"mcp"`
		Auth      string `json:"auth"`
		Formatter string `json:"formatter"`
	} `json:"tooling"`
	Status   string   `json:"status"`
	Warnings []string `json:"warnings"`
}
