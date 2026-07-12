package workflow

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// pipeline_emit.go is the CLI surface for the O1 Layer-2 emitter
// (full-loop-craft §1/§2/§6): `da workflow pipeline emit` renders the resolved
// profile IR (stage_profiles + execution_profile topology) into a target
// harness's materialized per-task pipeline. The swarm YAML under
// .agents/workflow/runtime/full-loop/ becomes a GENERATED build artifact instead
// of a hand-written source, so the running pipeline never drifts from
// stage_profiles (craft §7). Both the OMP swarm-YAML projector and the
// Claude-Code dynamic-workflow projector are implemented; each projector owns
// its own runtime output directory (see PipelineProjector.RuntimeRelDir).

// pipelineEmitArtifact is one emitted file in the machine-readable result.
type pipelineEmitArtifact struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

// pipelineEmitResult is the --json payload: what was (or would be) emitted, the
// config digest it was produced from, and the disk targets.
type pipelineEmitResult struct {
	Platform  string                 `json:"platform"`
	AppType   string                 `json:"app_type,omitempty"`
	Plan      string                 `json:"plan,omitempty"`
	Digest    string                 `json:"config_digest"`
	DryRun    bool                   `json:"dry_run"`
	Artifacts []pipelineEmitArtifact `json:"artifacts"`
}

func newWorkflowPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Emit the materialized per-task pipeline from the profile IR",
		Long: `Projects the resolved profile IR (stage_profiles + execution_profile topology)
into a target harness's materialized per-task pipeline. The emitted swarm YAML is
a generated build artifact — regenerate it from config rather than hand-editing.`,
	}
	cmd.AddCommand(newWorkflowPipelineEmitCmd())
	return cmd
}

func newWorkflowPipelineEmitCmd() *cobra.Command {
	var platformID, appType, plan string
	cmd := &cobra.Command{
		Use:   "emit",
		Short: "Emit the swarm/pipeline artifacts for a target platform",
		Long: `Renders the resolved profile IR into a target harness's per-task pipeline.

With no --app-type, emits the app_type-agnostic maximal skeleton (7 verify + 4
routine review lens slots + the cross-family gate) that the runtime profile_resolve
stage narrows per task — this regenerates the checked-in full-loop swarm YAMLs.
With --app-type X, emits the pipeline specialized to that app_type's
verifier_sequence and lens_set (still capped at 7 verifiers / 4 routine lenses).

The output is deterministic: re-emitting the same profile IR yields byte-identical
YAML, and a config digest is stamped in the header comment (no wall-clock).`,
		Example: deps.ExampleBlock(
			"  da workflow pipeline emit --platform omp",
			"  da workflow pipeline emit --platform omp --app-type go-cli",
			"  da -n workflow pipeline emit --platform omp",
			"  da --json workflow pipeline emit --platform omp --app-type daemon",
			"  da workflow pipeline emit --platform claude-code --app-type go-cli",
		),
		Args: deps.NoArgsWithHints("Run workflow pipeline emit from inside the project repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPipelineEmit(cmd, platformID, appType, plan)
		},
	}
	cmd.Flags().StringVar(&platformID, "platform", "", "target harness platform: "+strings.Join(platform.SupportedPipelinePlatforms(), ", ")+" (required)")
	cmd.Flags().StringVar(&appType, "app-type", "", "specialize the pipeline for this app_type (default: app_type-agnostic skeleton)")
	cmd.Flags().StringVar(&plan, "plan", "", "canonical plan this pipeline serves (validated when set; the emitted pipeline is plan-agnostic)")
	cmd.Flags().Bool("dry-run", false, "preview the emission without writing any file")
	return cmd
}

// pipelineEmitDryRun OR-merges the local --dry-run flag with the global -n/--dry-run
// so `da -n workflow pipeline emit` previews like every other mutating command.
func pipelineEmitDryRun(cmd *cobra.Command) bool {
	local, _ := cmd.Flags().GetBool("dry-run")
	return local || safeDryRun()
}

func runWorkflowPipelineEmit(cmd *cobra.Command, platformID, appType, plan string) error {
	platformID = strings.TrimSpace(platformID)
	if platformID == "" {
		return deps.UsageError("--platform is required", "Supported platforms: "+strings.Join(platform.SupportedPipelinePlatforms(), ", "))
	}
	projector, err := platform.PipelineProjectorFor(platformID)
	if err != nil {
		return err
	}

	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	// A set --plan is validated against canonical state so a typo fails loudly;
	// the emitted pipeline itself is plan-agnostic.
	plan = strings.TrimSpace(plan)
	if plan != "" {
		if _, err := loadCanonicalPlan(project.Path, plan); err != nil {
			return err
		}
	}

	snap, err := appTypeSnapshot(project.Path)
	if err != nil {
		if isMissingManifestErr(err) {
			return deps.ErrorWithHints(
				"no .agentsrc.json in this repo; cannot resolve stage_profiles / execution_profile",
				"Run `da workflow pipeline emit` from inside a configured project repository.",
			)
		}
		return err
	}

	spec, err := platform.BuildPipelineSpec(project.Path, appType, snap.Effective.StageProfiles, snap.Effective.ExecutionProfile)
	if err != nil {
		return err
	}
	artifacts, err := projector.Emit(spec)
	if err != nil {
		return err
	}

	outDir := filepath.Join(project.Path, projector.RuntimeRelDir())
	result := pipelineEmitResult{
		Platform: projector.Platform(),
		AppType:  spec.AppType,
		Plan:     plan,
		Digest:   spec.Digest(),
		DryRun:   pipelineEmitDryRun(cmd),
	}
	for _, art := range artifacts {
		target := filepath.Join(outDir, art.Name)
		result.Artifacts = append(result.Artifacts, pipelineEmitArtifact{
			Name:  art.Name,
			Path:  config.DisplayPath(target),
			Bytes: len(art.Content),
		})
	}

	if result.DryRun {
		return renderPipelineEmit(cmd, result, true)
	}

	if err := fsops.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create runtime dir %s: %w", outDir, err)
	}
	for _, art := range artifacts {
		target := filepath.Join(outDir, art.Name)
		if err := fsops.WriteFile(target, []byte(art.Content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}
	return renderPipelineEmit(cmd, result, false)
}

func renderPipelineEmit(cmd *cobra.Command, result pipelineEmitResult, dryRun bool) error {
	out := cmd.OutOrStdout()
	if deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	ui.Header("Pipeline emit")
	fmt.Fprintf(out, "  platform : %s\n", result.Platform)
	appType := result.AppType
	if appType == "" {
		appType = "(skeleton)"
	}
	fmt.Fprintf(out, "  app_type : %s\n", appType)
	fmt.Fprintf(out, "  digest   : %s\n", result.Digest)
	for _, art := range result.Artifacts {
		verb := "wrote"
		if dryRun {
			verb = "[dry-run] would write"
		}
		fmt.Fprintf(out, "  %s %s (%d bytes)\n", verb, art.Path, art.Bytes)
	}
	return nil
}
