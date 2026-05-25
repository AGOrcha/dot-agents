package lifecycle

import "github.com/spf13/cobra"

// GlobalFlags mirrors the subset of commands.Flags consumed by lifecycle
// subcommands. Kept as a parallel type to commands.GlobalFlags so the
// lifecycle subpackage has no import on the parent commands/ package,
// matching the agents.GlobalFlags / skills.GlobalFlags pattern.
//
// Extended in t03 with DryRun/Force/Verbose because the install command
// reads all four flags from a package-level Flags var (preserving the
// existing commands.Flags package-var seam per t01 SHAPE.md decision —
// "PRESERVE current package-var seams during the moves").
type GlobalFlags struct {
	DryRun  bool
	Force   bool
	Verbose bool
	Yes     bool
}

// Flags is the package-level mirror of commands.Flags consumed by lifecycle
// subcommands. The transitional shim in commands/install.go syncs this from
// commands.Flags before invoking any lifecycle entry point. Once t13 deletes
// the shim and root.go wires lifecycle.NewInstallCmd directly, the shim's
// RunE wrapper takes over the sync responsibility.
var Flags GlobalFlags

// Version/Commit/Describe mirror the build-info vars defined in
// commands/refresh.go. The shim in commands/install.go assigns these at
// init time so finalizeInstall's WriteRefreshToAgentsRC call sees the same
// values the root command does.
var (
	Version  = "dev"
	Commit   = ""
	Describe = ""
)

// ErrorWithHintsFn is a package-var seam onto commands.ErrorWithHints.
// Lifecycle cannot import commands (cycle); the t03 shim in
// commands/install.go assigns this to commands.ErrorWithHints at init
// time. A default value (plain fmt.Errorf-style formatting) keeps tests
// that exercise lifecycle entry points directly green even when the shim
// is absent.
var ErrorWithHintsFn = defaultErrorWithHints

func defaultErrorWithHints(message string, hints ...string) error {
	msg := message
	for _, hint := range hints {
		msg += "\n  hint: " + hint
	}
	return errString(msg)
}

type errString string

func (e errString) Error() string { return string(e) }

// Deps carries UX helpers from the commands package without an import
// cycle. Mirrors agents.Deps and skills.Deps so the three extracted
// subpackages share the same shape. Only fields actually consumed by
// lifecycle subcommands are present.
//
// Per SHAPE.md OD-7, there is intentionally no NewDeps() factory: the
// composition root in commands/root.go (and the transitional shims in
// commands/<verb>.go during t03–t12) construct the struct inline, the
// same way commands.NewAgentsCmd and commands.NewSkillsCmd do today.
type Deps struct {
	Flags                 GlobalFlags
	ErrorWithHints        func(message string, hints ...string) error
	UsageError            func(message string, hints ...string) error
	MaximumNArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
	RangeArgsWithHints    func(min, max int, hints ...string) cobra.PositionalArgs
	ExactArgsWithHints    func(n int, hints ...string) cobra.PositionalArgs
	NoArgsWithHints       func(hints ...string) cobra.PositionalArgs

	// ExampleBlock formats a multi-line cobra Example block. Mirrors the
	// commands.ExampleBlock helper. Lifecycle subcommands use it for the
	// cobra Example field at construction time.
	ExampleBlock func(lines ...string) string

	// RunRefresh is the back-edge into commands.runRefresh used by
	// NewRefreshCmd's RunE. The actual run body, package-var seams
	// (stdRefreshConfigLoader, stdImportDeps, stdAddDeps), and the helper
	// fan-out (mapResourceRelToDest, restoreFromResources) remain in
	// commands/ until t04 (add) and t06 (import) merge and the
	// cross-cluster constants / interfaces (addDeps, importDeps,
	// importScope*, rel*Dir) can be re-homed into lifecycle. See
	// .agents/active/fold-back/t07-refresh-body-deferred.md.
	RunRefresh func(projectFilter string, importAlso bool) error
}
