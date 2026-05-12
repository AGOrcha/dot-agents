package kg

// GlobalFlags mirrors the subset of commands.Flags used by kg subcommands.
type GlobalFlags struct {
	JSON   bool
	DryRun bool
}

// Deps carries UX helpers from commands without an import cycle.
type Deps struct {
	Flags        GlobalFlags
	ExampleBlock func(lines ...string) string
}
