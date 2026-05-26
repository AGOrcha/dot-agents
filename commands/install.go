package commands

// Transitional shim for the install command — t03 of root-command-decomposition.
//
// The implementation moved to commands/lifecycle/install.go in this task. This
// file remains as a thin wiring shim because:
//
//  1. seams_test.go (split deferred to t11) reaches the install pipeline
//     through unexported names in package commands: runInstall,
//     runInstallGenerate, registerInstallProject, findProjectByPath,
//     linkResourceFromSources, plus the installDeps interface and
//     stdInstallDeps zero-value type. The lower-cased forwarders below
//     keep those compile-time references valid until seams_test.go is
//     re-homed.
//
//  2. root.go still imports commands.NewInstallCmd; the switch to
//     lifecycle.NewInstallCmd directly happens in t13 when this file is
//     deleted entirely.
//
//  3. The lifecycle package mirrors the commands-package globals (Flags,
//     Version, Commit, Describe, ErrorWithHintsFn) as parallel package
//     vars per the t01 SHAPE.md decision to "PRESERVE current package-var
//     seams during the moves". This file is the single sync point between
//     the two packages — see syncLifecycleGlobals below.

import (
	"github.com/NikashPrakash/dot-agents/commands/lifecycle"
	"github.com/spf13/cobra"
)

// installDeps aliases lifecycle.InstallDeps so seams_test.go's fakeInstallDeps
// (which already implements the four-method interface) continues to satisfy
// the parameter type expected by the lower-cased forwarders below.
type installDeps = lifecycle.InstallDeps

// stdInstallDeps aliases lifecycle.StdInstallDeps so test sites that
// construct stdInstallDeps{} keep compiling.
type stdInstallDeps = lifecycle.StdInstallDeps

// syncLifecycleGlobals copies the current commands-package globals into
// the lifecycle package's parallel package vars. Called from every
// commands-side entry point that crosses into lifecycle so the moved
// helpers (which read lifecycle.Flags / lifecycle.Version /
// lifecycle.ErrorWithHintsFn directly) observe live state. Once t13
// removes the shim, RunE will sync once at the boundary and the
// forwarders disappear.
func syncLifecycleGlobals() {
	lifecycle.Flags = lifecycle.GlobalFlags{
		DryRun:  Flags.DryRun,
		Force:   Flags.Force,
		Verbose: Flags.Verbose,
		Yes:     Flags.Yes,
	}
	lifecycle.Version = Version
	lifecycle.Commit = Commit
	lifecycle.Describe = Describe
	lifecycle.ErrorWithHintsFn = ErrorWithHints
}

// lifecycleDeps builds the Deps struct passed to lifecycle.NewInstallCmd.
// Mirrors agentsDeps() / skillsDeps() in the same directory.
func lifecycleDeps() lifecycle.Deps {
	return lifecycle.Deps{
		Flags: lifecycle.GlobalFlags{
			DryRun:  Flags.DryRun,
			Force:   Flags.Force,
			Verbose: Flags.Verbose,
			Yes:     Flags.Yes,
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		RangeArgsWithHints:    RangeArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
		NoArgsWithHints:       NoArgsWithHints,
		ExampleBlock:          ExampleBlock,
	}
}

// NewInstallCmd wires the install command tree. Thin shim preserved for
// t03; root.go switches to lifecycle.NewInstallCmd in t13.
func NewInstallCmd() *cobra.Command {
	cmd := lifecycle.NewInstallCmd(lifecycleDeps())
	// Wrap RunE so we sync commands-package globals into the lifecycle
	// package vars right before the moved implementation reads them. The
	// inner RunE is the one assigned by lifecycle.NewInstallCmd.
	innerRunE := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		syncLifecycleGlobals()
		return innerRunE(c, args)
	}
	return cmd
}

// ─── seams_test.go-facing forwarders ─────────────────────────────────────────
//
// These lower-case wrappers exist so the deferred seams_test.go split (t11)
// keeps compiling. Each forwarder syncs the lifecycle globals first so the
// moved helpers see live commands-package state, then delegates.

func runInstall(strict bool, deps installDeps) error {
	syncLifecycleGlobals()
	return lifecycle.RunInstall(strict, deps)
}

func runInstallGenerate(deps installDeps) error {
	syncLifecycleGlobals()
	return lifecycle.RunInstallGenerate(deps)
}

func registerInstallProject(projectName, projectPath string, deps installDeps) error {
	syncLifecycleGlobals()
	return lifecycle.RegisterInstallProject(projectName, projectPath, deps)
}

func findProjectByPath(projectPath string, deps installDeps) string {
	syncLifecycleGlobals()
	return lifecycle.FindProjectByPath(projectPath, deps)
}

func linkResourceFromSources(resourceType, name, project string, sources []string, deps installDeps) error {
	syncLifecycleGlobals()
	return lifecycle.LinkResourceFromSources(resourceType, name, project, sources, deps)
}

func cloneGitSource(gitBin, url, ref, cacheDir string, deps installDeps) (string, error) {
	syncLifecycleGlobals()
	return lifecycle.CloneGitSource(gitBin, url, ref, cacheDir, deps)
}

func shouldUseCachedGitSource(cacheDir, url string) bool {
	syncLifecycleGlobals()
	return lifecycle.ShouldUseCachedGitSource(cacheDir, url)
}
