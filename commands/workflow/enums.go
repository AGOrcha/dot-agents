package workflow

import (
	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// Closed-set flag vocabularies for the workflow surface.
//
// Every enum flag under `da workflow` is declared exactly once here and
// registered with cmdutil.RegisterEnum, so the `--help` listing, the shell
// completions, and the validation error can never disagree. Runners that need
// the same vocabulary call spec.Contains / spec.Validate rather than re-listing
// the members. See docs/CLI_HELP_CONVENTIONS.md.

// appTypesCommand is the command that prints the live app_type vocabulary. The
// set is config-derived (execution_profile.by_app_type in .agentsrc.json), so
// help points at this command instead of listing values the binary cannot know.
const appTypesCommand = "da workflow app-types"

// taskStatusEnum is the persisted task-status vocabulary from task_status.go,
// in state-machine order. `advance` additionally enforces the §3.1 transition
// graph, which constrains the value further than membership does.
var taskStatusEnum = cmdutil.EnumSpec{
	Name:     "status",
	Usage:    "New task status",
	Values:   validTaskStatusList,
	Required: true,
	Note:     "the §3.1 state machine also constrains which of these the task's current status may move to",
}

// planStatusEnum is the plan-level lifecycle vocabulary written to PLAN.yaml.
var planStatusEnum = cmdutil.EnumSpec{
	Name:   "status",
	Usage:  "New plan status",
	Values: []string{"draft", "active", "paused", "completed", "archived"},
}

// verificationStatusValues is the verdict vocabulary shared by checkpoint,
// merge-back, and `verify record` for non-review kinds.
var verificationStatusValues = []string{"pass", "fail", "partial", "unknown"}

// checkpointVerificationStatusEnum records a verdict on a checkpoint.
var checkpointVerificationStatusEnum = cmdutil.EnumSpec{
	Name:    "verification-status",
	Usage:   "Verification verdict recorded on the checkpoint",
	Values:  verificationStatusValues,
	Default: workflowDefaultVerificationState,
}

// mergeBackVerificationStatusEnum records the delegate's own verdict on the
// merge-back artifact the parent then gates on.
var mergeBackVerificationStatusEnum = cmdutil.EnumSpec{
	Name:    "verification-status",
	Usage:   "Verdict the delegate reached on its own slice",
	Values:  verificationStatusValues,
	Default: workflowDefaultVerificationState,
}

// checkpointRoleEnum selects which per-role block of an iteration-log entry
// `--log-to-iter` merges into.
var checkpointRoleEnum = cmdutil.EnumSpec{
	Name:   "role",
	Usage:  "With --log-to-iter: merge only this role's block",
	Values: []string{"impl", "verifier", "review"},
}

// verifyKindEnum names the class of verification being recorded. `review` is
// the odd member: it takes phase decisions instead of `--status`.
var verifyKindEnum = cmdutil.EnumSpec{
	Name:     "kind",
	Usage:    "Class of verification being recorded",
	Values:   []string{"test", "lint", "build", "format", "custom", "review"},
	Required: true,
	Note:     "review is the odd member: it takes --phase1-decision/--phase2-decision instead of --status",
}

// verifyStatusEnum is the verdict for every non-review kind.
var verifyStatusEnum = cmdutil.EnumSpec{
	Name:   "status",
	Usage:  "Verdict of the run",
	Values: verificationStatusValues,
	Note:   "required unless --kind review, where the verdict is derived from the phase decisions instead",
}

// verifyScopeEnum records how much of the tree the run covered.
var verifyScopeEnum = cmdutil.EnumSpec{
	Name:    "scope",
	Usage:   "How much of the tree the run covered",
	Values:  []string{"file", "package", "repo", "custom"},
	Default: "repo",
}

// reviewDecisionValues is the per-phase reviewer verdict vocabulary.
var reviewDecisionValues = []string{"accept", "reject", "escalate"}

var reviewPhase1DecisionEnum = cmdutil.EnumSpec{
	Name:   "phase1-decision",
	Usage:  "When --kind review: first phase verdict",
	Values: reviewDecisionValues,
}

var reviewPhase2DecisionEnum = cmdutil.EnumSpec{
	Name:   "phase2-decision",
	Usage:  "When --kind review: second phase verdict",
	Values: reviewDecisionValues,
}

var reviewOverallDecisionEnum = cmdutil.EnumSpec{
	Name:   "overall-decision",
	Usage:  "When --kind review: optional consolidated verdict",
	Values: reviewDecisionValues,
	Note:   "when set it must match the verdict derived from the two phase decisions",
}

// taskAppTypeEnum routes verifier dispatch for a task. Config-derived.
var taskAppTypeEnum = cmdutil.EnumSpec{
	Name:        "app-type",
	Usage:       "App type that selects this task's verifier sequence",
	DynamicFrom: appTypesCommand,
}

// contractModeEnum distinguishes orchestrator-owned work from delegated work.
var contractModeEnum = cmdutil.EnumSpec{
	Name:    "mode",
	Usage:   "Contract mode",
	Values:  []string{"direct", "delegated"},
	Default: "",
	Note:    "overrides --direct/--delegated when set",
}

// delegationDecisionEnum is the parent's integration verdict at closeout.
var delegationDecisionEnum = cmdutil.EnumSpec{
	Name:     "decision",
	Usage:    "Parent integration verdict for the delegated slice",
	Values:   []string{"accept", "reject"},
	Required: true,
}

// resolvePromptKindEnum names the stage_profiles stage a composed prompt is
// resolved from.
var resolvePromptKindEnum = cmdutil.EnumSpec{
	Name:     "kind",
	Usage:    "stage_profiles stage to resolve the prompt from",
	Values:   profileStages,
	Required: true,
}

// appTypesFormatEnum picks which authoring snippet `app-types` prints.
var appTypesFormatEnum = cmdutil.EnumSpec{
	Name:   "format",
	Usage:  "Print only the recommended authoring snippet in this form",
	Values: []string{"flag", "task", "plan", "doc"},
}

// graphQueryIntentEnum lists the workflow-memory intents this command answers
// directly; code-structure intents are forwarded to `da kg bridge query`.
var graphQueryIntentEnum = cmdutil.EnumSpec{
	Name:   "intent",
	Usage:  "Bridge intent to answer",
	Values: []string{"plan_context", "decision_lookup", "entity_context", "workflow_memory", "contradictions"},
	Note:   "code-structure intents are forwarded to da kg bridge query",
}

// pipelinePlatformEnum names the harness a pipeline is emitted for. The
// vocabulary is owned by internal/platform, not restated here.
var pipelinePlatformEnum = cmdutil.EnumSpec{
	Name:     "platform",
	Usage:    "Target harness platform",
	Values:   platform.SupportedPipelinePlatforms(),
	Required: true,
}

// journalActorEnum names the agent role that produced a journal event.
var journalActorEnum = cmdutil.EnumSpec{
	Name:    "actor",
	Usage:   "Agent role that produced the event",
	Values:  []string{string(journal.ActorMain), string(journal.ActorLoopWorker), string(journal.ActorOrchestrator)},
	Default: string(journal.ActorMain),
}

// journalEventTypeEnum classifies what the journalled command actually did.
var journalEventTypeEnum = cmdutil.EnumSpec{
	Name:    "event-type",
	Usage:   "What the journalled command did",
	Values:  []string{string(journal.EventDurableDelta), string(journal.EventInputOnly), string(journal.EventFailed)},
	Default: string(journal.EventDurableDelta),
}

// scoreRecomputeEnum picks how much of the scored corpus close-task recomputes.
// `recent-N` is a family (recent-5, recent-20), so the vocabulary is open at
// that member and the note carries the shape instead of the listing.
var scoreRecomputeEnum = cmdutil.EnumSpec{
	Name:    "score-recompute",
	Usage:   "How much of the scored iteration corpus to recompute",
	Default: "current",
	Note:    "one of: current (the just-closed iteration only) | recent-N (the newest N, e.g. recent-5) | all",
}

// hookOutcomeSkillEnum names the skill whose sentinel an outcome anchors to.
var hookOutcomeSkillEnum = cmdutil.EnumSpec{
	Name:     "skill",
	Usage:    "Skill whose sentinel this outcome anchors to",
	Values:   []string{"iteration-close", "isp", "loop-worker", "orchestrator-session-start", "delegation-lifecycle"},
	Required: true,
}

// hookOutcomeLifecycleEnum names the platform lifecycle event that fired.
var hookOutcomeLifecycleEnum = cmdutil.EnumSpec{
	Name:     "lifecycle-point",
	Usage:    "Platform lifecycle event that fired the hook",
	Values:   []string{"pre_tool_use", "stop", "subagent_stop", "subagent_start", "pre_compact", "post_tool_use", "post_tool_use_failure"},
	Required: true,
}

// hookOutcomeInterventionEnum classifies when the gate intervened.
var hookOutcomeInterventionEnum = cmdutil.EnumSpec{
	Name:     "intervention-class",
	Usage:    "When the gate intervened relative to the action",
	Values:   []string{"prevent_before_action", "remediate_at_stop", "continuity_advice", "observe_tool_result"},
	Required: true,
}

// hookOutcomeResultEnum records how hard the gate pushed back.
var hookOutcomeResultEnum = cmdutil.EnumSpec{
	Name:     "result",
	Usage:    "How hard the gate pushed back",
	Values:   []string{"allow", "advise", "remediate"},
	Required: true,
}

// hookOutcomePlatformEnum names the agent platform the hook ran under.
var hookOutcomePlatformEnum = cmdutil.EnumSpec{
	Name:     "platform",
	Usage:    "Agent platform the hook ran under",
	Values:   []string{"claude", "codex", "copilot", "cursor"},
	Required: true,
}

// hookSentinelAgentTypeEnum names the caller role writing a sentinel.
var hookSentinelAgentTypeEnum = cmdutil.EnumSpec{
	Name:     "agent-type",
	Usage:    "Caller agent role",
	Values:   []string{"main", "loop-worker"},
	Required: true,
}

// hookSentinelDecisionEnum is the parent_closeout companion verdict.
var hookSentinelDecisionEnum = cmdutil.EnumSpec{
	Name:   "decision",
	Usage:  "Companion: parent_closeout verdict",
	Values: []string{"accept", "reject"},
}

// hookSentinelOperationEnum discriminates which companion gate applies.
var hookSentinelOperationEnum = cmdutil.EnumSpec{
	Name:   "operation",
	Usage:  "Companion-gate operation discriminator",
	Values: []string{"fanout_handoff", "existing_bundle_handoff", "parent_closeout"},
}
