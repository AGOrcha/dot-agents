// Package globalflagcov computes which persistent global CLI flags (commands.Flags)
// are referenced transitively from each cobra command's RunE handler.
//
//go:generate go run ../../cmd/globalflag-coverage -markdown -o ../../docs/generated/GLOBAL_FLAG_COVERAGE.md
package globalflagcov

import (
	"reflect"
	"runtime"
	"sort"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands"
	"github.com/spf13/cobra"
)

// FlagSet records whether each global persistent flag is read on some path from the handler.
type FlagSet struct {
	JSON    bool
	DryRun  bool
	Yes     bool
	Force   bool
	Verbose bool
}

// Row is one CLI command path and observed global-flag coverage.
type Row struct {
	// Path is space-separated from the second segment (e.g. "workflow status"), excluding "dot-agents".
	Path string
	// Handler is the runtime symbol for the RunE func (or closure).
	Handler string
	// Flags is transitive coverage through same-package calls.
	Flags FlagSet
	// Notes is non-empty when analysis was partial (e.g. unresolved closure).
	Notes string
}

// Report generates coverage rows for every command with a RunE (or Run) on the default root tree.
func Report(moduleRoot string) ([]Row, error) {
	st, err := loadStatic(moduleRoot)
	if err != nil {
		return nil, err
	}

	root := commands.NewRootCommand()
	var runs []runRecord
	walkRunHandlers(root, &runs)

	rows := make([]Row, 0, len(runs))
	for _, rr := range runs {
		row := Row{
			Path:    strings.Join(rr.path, " "),
			Handler: rr.handlerName,
		}
		if rr.handlerName == "" {
			row.Notes = "no handler symbol"
			rows = append(rows, row)
			continue
		}
		fs, note := st.flagsForRuntimeHandler(rr.handlerName, rr.pc)
		row.Flags = fs
		row.Notes = note
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	return rows, nil
}

func trimDotAgents(parts []string) []string {
	if len(parts) == 0 {
		return parts
	}
	if parts[0] == "dot-agents" {
		return parts[1:]
	}
	return parts
}

type runRecord struct {
	path        []string
	handlerName string
	pc          uintptr
}

func walkRunHandlers(cmd *cobra.Command, out *[]runRecord) {
	if cmd.RunE != nil {
		parts := strings.Fields(cmd.CommandPath())
		*out = append(*out, runRecord{
			path:        trimDotAgents(parts),
			handlerName: runtimeFuncName(cmd.RunE),
			pc:          reflect.ValueOf(cmd.RunE).Pointer(),
		})
	} else if cmd.Run != nil {
		parts := strings.Fields(cmd.CommandPath())
		*out = append(*out, runRecord{
			path:        trimDotAgents(parts),
			handlerName: runtimeFuncName(cmd.Run),
			pc:          reflect.ValueOf(cmd.Run).Pointer(),
		})
	}

	for _, c := range cmd.Commands() {
		walkRunHandlers(c, out)
	}
}

func runtimeFuncName(fn any) string {
	v := reflect.ValueOf(fn)
	if !v.IsValid() {
		return ""
	}
	pc := v.Pointer()
	if pc == 0 {
		return ""
	}
	f := runtime.FuncForPC(pc)
	if f == nil {
		return ""
	}
	raw := f.Name()
	// .../commands.runInit or .../commands.NewKGCmd.func12
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		raw = raw[i+1:]
	}
	// Strip package qualifier from linker symbol (commands.runInit -> runInit; commands.NewKGCmd.func1 -> NewKGCmd.func1).
	if strings.HasPrefix(raw, "commands.") {
		raw = strings.TrimPrefix(raw, "commands.")
	}
	return raw
}
