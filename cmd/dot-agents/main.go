package main

import (
	"os"

	"github.com/NikashPrakash/dot-agents/commands"
)

func main() {
	root := commands.NewRootCommand()
	if err := root.Execute(); err != nil {
		commands.RenderCommandError(os.Stderr, root, os.Args[1:], err)
		os.Exit(1)
	}
}
