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
	Flags GlobalFlags
	// RunRefreshCurrentProject re-projects ~/.agents into the CURRENT project
	// only. `da sync pull` updates the shared home; the follow-up refresh
	// applies that to the repo the user ran it in. It deliberately cannot
	// express a machine-wide sweep — that is `da refresh --all`, an explicit
	// invocation the user makes themselves.
	RunRefreshCurrentProject func() error
}
