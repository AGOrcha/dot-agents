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
// subcommands' moved helper bodies (which still read the package var directly
// per the t01 SHAPE.md "PRESERVE current package-var seams during the moves"
// decision). Three populating paths converge here:
//
//  1. The transitional commands/install.go shim's syncLifecycleGlobals helper
//     (active until t13b deletes the shim) writes commands.Flags into this
//     package var before invoking any lifecycle entry point.
//  2. The NewInstallCmd / NewDoctorCmd / NewInitCmd constructors' RunE
//     wrappers (added by t13a) call applyDepsToGlobals(deps) before delegating
//     to the moved RunE body, so a t13b call site of the form
//     `lifecycle.NewInstallCmd(buildLifecycleDeps())` works end-to-end without
//     a separate sync step.
//  3. Tests that exercise lifecycle entry points directly write the var via
//     `Flags = lifecycle.GlobalFlags{...}` and restore it on cleanup.
var Flags GlobalFlags

// Version/Commit/Describe mirror the build-info vars defined in
// commands/refresh.go. Populated by the same two paths as Flags above:
// the transitional install.go shim writes them via syncLifecycleGlobals, and
// the NewInstallCmd / NewDoctorCmd / NewInitCmd RunE wrappers (t13a) write
// them from Deps via applyDepsToGlobals.
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
// is absent. The NewXxxCmd constructors' RunE wrappers also write this from
// Deps.ErrorWithHints when non-nil (t13a) so a t13b call site needs no extra
// sync step.
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

// Deps carries UX helpers and runtime state from the commands package into
// lifecycle without an import cycle. Mirrors agents.Deps and skills.Deps so
// the three extracted subpackages share the same shape. Only fields actually
// consumed by lifecycle subcommands are present.
//
// Per SHAPE.md OD-7, there is intentionally no NewDeps() factory: the
// composition root in commands/root.go (and the transitional shims in
// commands/<verb>.go during t03–t12) construct the struct inline, the
// same way commands.NewAgentsCmd and commands.NewSkillsCmd do today.
//
// ── Constructor contract after t13a ────────────────────────────────────────
//
// All four lifecycle command constructors accept Deps as the first (and only)
// argument:
//
//	NewInstallCmd(deps Deps) *cobra.Command
//	NewDoctorCmd(deps Deps)  *cobra.Command
//	NewInitCmd(deps Deps)    *cobra.Command
//	NewStatusCmd(deps Deps, jsonOutput func() bool) *cobra.Command
//
// NewStatusCmd retains the second jsonOutput argument (option (c) in the
// t13a fold-back observation) rather than absorbing it into Deps.JSONFlag.
// Rationale: the parent commands/status.go shim already passes the jsonFlag
// closure as a second positional arg so the globalflagcov static analyzer
// (which loads ./commands but not ./commands/lifecycle) can see the
// Flags.JSON read for handler coverage. Folding the closure into Deps would
// move the read site into lifecycle and break that coverage. T13b will
// either widen globalflagcov's load set (preferred) or keep the second-arg
// shape — the t13a contract leaves both doors open. T13b's worker can pass
// `lifecycle.NewStatusCmd(buildStatusDeps(), func() bool { return Flags.JSON })`
// without further wrapping.
//
// ── Constructor-side global sync (t13a) ───────────────────────────────────
//
// NewInstallCmd / NewDoctorCmd / NewInitCmd's RunE wrappers internally call
// applyDepsToGlobals(deps) before delegating to the moved RunE body. That
// absorbs the parent shim's syncLifecycleGlobals dance: t13b's worker just
// constructs Deps with live commands.Flags / Version / Commit / Describe /
// ErrorWithHints in FlagsFn / Version / Commit / Describe / ErrorWithHints
// and calls the constructor. The constructor handles the rest. FlagsFn is a
// closure (not a value) so it captures live state at each RunE invocation —
// critical because cobra parses flags AFTER constructor calls. NewStatusCmd
// does NOT call applyDepsToGlobals because its moved helpers do not read the
// commands-package globals (status uses Flags only for jsonOutput, which the
// caller-supplied closure carries directly).
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
	// NewRefreshCmd's RunE. importAlso/inexact carry the parsed --import and
	// --inexact flags through to the legacy body. The actual run body,
	// package-var seams (stdRefreshConfigLoader, stdImportDeps, stdAddDeps),
	// and the helper fan-out (mapResourceRelToDest, restoreFromResources)
	// remain in commands/ until t04 (add) and t06 (import) merge and the
	// cross-cluster constants / interfaces (addDeps, importDeps,
	// importScope*, rel*Dir) can be re-homed into lifecycle. See
	// .agents/active/fold-back/t07-refresh-body-deferred.md.
	RunRefresh func(projectFilter string, importAlso, inexact bool) error

	// FlagsFn is an optional closure over the caller's live flag state.
	// When non-nil it is invoked by NewInstallCmd / NewDoctorCmd /
	// NewInitCmd's RunE wrapper to populate the lifecycle.Flags package
	// var at RunE time (cobra has parsed flags by then; a Deps.Flags
	// value snapshot taken at constructor time would be stale). When
	// nil, Deps.Flags is copied as-is.
	//
	// T13b's worker passes a closure of the form
	//   func() GlobalFlags { return GlobalFlags{DryRun: commands.Flags.DryRun, ...} }
	// so the constructor reads live state on each invocation. Test code
	// that exercises the constructor with a deterministic snapshot leaves
	// FlagsFn nil and sets Deps.Flags directly.
	FlagsFn func() GlobalFlags

	// Version/Commit/Describe carry the build-info values the install
	// pipeline's finalizeInstall helper writes into .agentsrc.json. When
	// non-empty they are copied to the lifecycle.Version / .Commit /
	// .Describe package vars by NewInstallCmd / NewDoctorCmd / NewInitCmd's
	// RunE wrapper (the moved helpers read those vars directly). The
	// zero-value "" defaults to leaving the package var untouched, so the
	// build-info defaults set on the package vars themselves ("dev"/"" for
	// Version/Commit/Describe) survive when a caller omits them.
	Version  string
	Commit   string
	Describe string
}

// applyDepsToGlobals copies the runtime-sync fields of deps into the
// lifecycle package vars (Flags / Version / Commit / Describe /
// ErrorWithHintsFn) that the moved RunE bodies read directly. Called from
// NewInstallCmd / NewDoctorCmd / NewInitCmd's RunE wrapper so a t13b call
// site of the form `lifecycle.NewInstallCmd(buildLifecycleDeps())` works
// end-to-end without a separate syncLifecycleGlobals step.
//
// FlagsFn takes precedence over Deps.Flags so callers can pass a live
// closure (cobra parses flags AFTER constructor return, so a value
// snapshot taken at constructor time is stale). Version/Commit/Describe
// write only when non-empty — the zero value "" leaves the package var
// at its compile-time default ("dev"/"" per refresh.go). ErrorWithHints
// writes only when non-nil so a caller that omits the field keeps the
// in-package defaultErrorWithHints formatter.
//
// This is a no-op-safe call: invoking it with the zero-value Deps leaves
// every package var untouched.
func applyDepsToGlobals(deps Deps) {
	if deps.FlagsFn != nil {
		Flags = deps.FlagsFn()
	} else {
		Flags = deps.Flags
	}
	if deps.Version != "" {
		Version = deps.Version
	}
	if deps.Commit != "" {
		Commit = deps.Commit
	}
	if deps.Describe != "" {
		Describe = deps.Describe
	}
	if deps.ErrorWithHints != nil {
		ErrorWithHintsFn = deps.ErrorWithHints
	}
}
