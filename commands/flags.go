package commands

// GlobalFlags holds flags shared across commands.
type GlobalFlags struct {
	DryRun  bool
	Force   bool
	Verbose bool
	Yes     bool
	JSON    bool
}

var Flags GlobalFlags
