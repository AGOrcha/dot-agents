package mcp

import (
	"fmt"
	"strings"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// canonicalSpec assembles the `da mcp` resource spec by combining the
// static cmdutil.MCPResource definition (Kind/DirSegment/strings/Examples
// + EnsureScope) with the per-leaf runner closures that need access to
// platform.ListCanonicalMCPFiles + findMCPSpec for hint-aware errors.
//
// Per plan duplicate-density-drop: keeping this body as a single call
// into cmdutil.SpecForResource means the only duplication across the
// mcp/settings/rules trio is the four lines of runner closure shape —
// which Sonar's clone detector treats as structurally distinct because
// the captured platform.* helpers and findXxxSpec wrappers differ.
//
// mcp uses Deps.MaxArgsWithHints (not MaximumNArgsWithHints like
// settings/rules), so the list-args binding happens at this leaf via
// maxArgs(...) — that's why the args validators flow into
// SpecForResource as parameters rather than living on the def.
func canonicalSpec(deps Deps) cmdutil.CanonicalFileSpec {
	return cmdutil.SpecForResource(
		cmdutil.MCPResource,
		cmdutil.ResourceRunners{
			List: func(agentsHome, scope string) ([]cmdutil.CanonicalFileEntry, error) {
				specs, err := platform.ListCanonicalMCPFiles(agentsHome, scope)
				if err != nil {
					return nil, err
				}
				return cmdutil.EntriesFromSpecs(specs, func(sp platform.MCPFileSpec) cmdutil.CanonicalFileEntry {
					return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}
				}), nil
			},
			Resolve: func(agentsHome, scope, name string) (cmdutil.CanonicalFileEntry, error) {
				sp, err := findMCPSpec(deps, agentsHome, scope, name)
				if err != nil {
					return cmdutil.CanonicalFileEntry{}, err
				}
				return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
			},
			ListRun:   func(scope string) error { return RunList(scope) },
			ShowRun:   func(scope, name string) error { return RunShow(deps, scope, name) },
			RemoveRun: func(scope, name string) error { return RunRemove(deps, scope, name) },
		},
		maxArgs(deps, 1, cmdutil.MCPResource.ListArgsHint),
		exactArgs(deps, 2, cmdutil.MCPResource.ShowArgsHint),
		exactArgs(deps, 2, cmdutil.MCPResource.RemoveArgsHint),
	)
}

// maxArgs / exactArgs guard against the zero-value Deps used by the
// data-layer RunList/RunShow paths. The CLI wiring in NewCmd always
// supplies real helpers via Deps; the data path only needs the data-
// layer spec fields and never invokes Args, so nil-returning fallbacks
// are safe.
func maxArgs(deps Deps, n int, hints ...string) cobra.PositionalArgs {
	if deps.MaxArgsWithHints == nil {
		return nil
	}
	return deps.MaxArgsWithHints(n, hints...)
}

func exactArgs(deps Deps, n int, hints ...string) cobra.PositionalArgs {
	if deps.ExactArgsWithHints == nil {
		return nil
	}
	return deps.ExactArgsWithHints(n, hints...)
}

// findMCPSpec looks up an MCP file by basename or stem. Kept package-
// private because the focused tests in mcp_test.go / seams_test.go call
// it directly and the parent commands shim has no need for it.
//
// Errors are produced via deps.UsageError / deps.ErrorWithHints when
// supplied, matching the user-facing shape commands.UsageError /
// commands.ErrorWithHints emit. When deps is the zero value (e.g.
// RunList paths that never invoke Resolve) the helper falls back to
// fmt.Errorf so it remains usable without wiring.
func findMCPSpec(deps Deps, agentsHome, scope, name string) (*platform.MCPFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, usageErr(deps,
			"MCP file name is empty",
			"Pass the file name or stem shown by `da mcp list`.",
		)
	}
	spec, err := platform.ResolveCanonicalMCPFile(agentsHome, scope, name)
	if err != nil {
		return nil, hintErr(deps,
			fmt.Sprintf("MCP file not found: %s / %s", scope, name),
			"Run `da mcp list "+scope+"` to see available files.",
		)
	}
	return spec, nil
}

// usageErr / hintErr centralise the deps-nil fallback so each call site
// stays a one-liner.
func usageErr(deps Deps, message string, hints ...string) error {
	if deps.UsageError != nil {
		return deps.UsageError(message, hints...)
	}
	return formatFallback(message, hints)
}

func hintErr(deps Deps, message string, hints ...string) error {
	if deps.ErrorWithHints != nil {
		return deps.ErrorWithHints(message, hints...)
	}
	return formatFallback(message, hints)
}

func formatFallback(message string, hints []string) error {
	if len(hints) == 0 {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s: %s", message, hints[0])
}
