package sync

// GlobalFlags mirrors the subset of commands.GlobalFlags used by sync subcommands.
type GlobalFlags struct {
	DryRun bool
	Yes    bool
	Force  bool
}

// Deps carries cross-package behavior the sync subtree cannot import from commands
// without creating an import cycle.
type Deps struct {
	Flags      GlobalFlags
	RunRefresh func(projectFilter string) error
}
