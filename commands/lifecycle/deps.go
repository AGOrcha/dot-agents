package lifecycle

import "github.com/spf13/cobra"

// GlobalFlags mirrors the subset of commands.Flags consumed by lifecycle
// subcommands. Kept as a parallel type to commands.GlobalFlags so the
// lifecycle subpackage has no import on the parent commands/ package,
// matching the agents.GlobalFlags / skills.GlobalFlags pattern.
type GlobalFlags struct {
	Yes bool
}

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
