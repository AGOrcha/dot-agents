package cmdutil

import (
	"strings"

	"github.com/spf13/cobra"
)

// ResourceRunners carries the per-leaf closures that cannot live alongside
// the static CanonicalResourceDef strings: List and Resolve close over the
// leaf's platform.ListCanonicalXxxFiles helper and the leaf's findXxxSpec
// error wrapper (which threads the leaf's Deps for hint-aware errors).
//
// The three verb runners (ListRun/ShowRun/RemoveRun) are also leaf-bound
// because each forwards into the leaf's own RunList/RunShow/RunRemove —
// some of those take deps directly (rules.RunList) while others bind
// deps via closure (mcp.RunList, settings.RunList).
type ResourceRunners struct {
	// List enumerates entries for a scope. Returning an os.IsNotExist
	// error indicates the scope directory is missing — RunCanonicalList
	// turns that into the def's MissingDirHint informational message.
	List func(agentsHome, scope string) ([]CanonicalFileEntry, error)
	// Resolve looks up one entry by basename or stem. Errors are
	// propagated verbatim, so leaves should wrap with the parent
	// commands.ErrorWithHints / commands.UsageError shape via their
	// deps before returning.
	Resolve func(agentsHome, scope, name string) (CanonicalFileEntry, error)

	// ListRun / ShowRun / RemoveRun are the leaf's per-verb runners.
	// The cobra RunE wrappers in canonical_cmd.go unpack args and call
	// these directly.
	ListRun   func(scope string) error
	ShowRun   func(scope, name string) error
	RemoveRun func(scope, name string) error
}

// SpecForResource assembles a CanonicalFileSpec from the static def +
// the leaf's runner closures + pre-built positional args validators.
// This is the SINGLE place the field-by-field assembly happens; each
// leaf's canonicalSpec(deps) becomes a one-liner forwarding into here.
//
// The args validators come pre-bound from the leaf because mcp uses
// Deps.MaxArgsWithHints while settings/rules use Deps.MaximumNArgsWithHints
// — both produce cobra.PositionalArgs, but the bindings live on different
// Deps fields so they cannot be moved into the def or this factory.
func SpecForResource(def CanonicalResourceDef, runners ResourceRunners, listArgs, showArgs, removeArgs cobra.PositionalArgs) CanonicalFileSpec {
	return CanonicalFileSpec{
		Kind:           def.Kind,
		DirSegment:     def.DirSegment,
		SingularRem:    def.SingularRem,
		EmptyHint:      def.EmptyHint,
		MissingDirHint: def.MissingDirHint,
		List:           runners.List,
		Resolve:        runners.Resolve,
		EnsureScope:    def.EnsureScope,

		Use:     def.Use,
		Short:   def.Short,
		Long:    def.Long,
		Example: strings.Join(def.Examples, "\n"),

		ListSub: SubCmdStrings{
			Use:     "list [scope]",
			Short:   def.ListShort,
			Example: strings.Join(def.ListExamples, "\n"),
		},
		ListArgs: listArgs,
		ListRun:  runners.ListRun,

		ShowSub: SubCmdStrings{
			Use:   "show <scope> <name>",
			Short: def.ShowShort,
		},
		ShowArgs: showArgs,
		ShowRun:  runners.ShowRun,

		RemoveSub: SubCmdStrings{
			Use:   "remove <scope> <name>",
			Short: def.RemoveShort,
			Long:  def.RemoveLong,
		},
		RemoveArgs: removeArgs,
		RemoveRun:  runners.RemoveRun,
	}
}

// EntriesFromSpecs converts a slice of typed platform.{MCP,Settings,Rule}FileSpec
// values into the CanonicalFileEntry projection the data-layer helpers
// operate on. The three leaves used to inline this 5-line loop inside
// their List closures; lifting it here removes the last bit of structural
// duplication. Callers pass a per-spec projector so this stays generic
// across the three distinct FileSpec types.
func EntriesFromSpecs[T any](specs []T, project func(T) CanonicalFileEntry) []CanonicalFileEntry {
	out := make([]CanonicalFileEntry, len(specs))
	for i, sp := range specs {
		out[i] = project(sp)
	}
	return out
}
