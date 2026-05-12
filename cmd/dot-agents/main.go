package main

import (
	"os"

	"github.com/NikashPrakash/dot-agents/commands"
)

func main() {
	root := commands.NewRootCommand()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
