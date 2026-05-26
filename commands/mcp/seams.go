package mcp

import (
	"fmt"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/platform"
)

// canonicalSpec wires the MCP-flavoured implementations of the
// cmdutil.CanonicalFileSpec callbacks: List/Resolve forward to the
// platform helpers and EnsureScope reuses platform.EnsureUnderMCPScopeTree.
//
// deps is threaded through so the Resolve callback can wrap
// platform.ResolveCanonicalMCPFile errors via findMCPSpec, which prefers
// deps.ErrorWithHints / deps.UsageError when provided (matching the parent
// commands package's user-facing error shape).
func canonicalSpec(deps Deps) cmdutil.CanonicalFileSpec {
	return cmdutil.CanonicalFileSpec{
		Kind:        "MCP",
		DirSegment:  "mcp",
		SingularRem: "MCP file",
		EmptyHint: func(scope string) string {
			return "No MCP config files (.json/.yaml/.yml/.toml) under ~/.agents/mcp/" + scope + "/"
		},
		MissingDirHint: func(scope string) string {
			return "No ~/.agents/mcp/" + scope + "/ directory yet (no canonical MCP files for this scope)."
		},
		List: func(agentsHome, scope string) ([]cmdutil.CanonicalFileEntry, error) {
			specs, err := platform.ListCanonicalMCPFiles(agentsHome, scope)
			if err != nil {
				return nil, err
			}
			out := make([]cmdutil.CanonicalFileEntry, len(specs))
			for i, sp := range specs {
				out[i] = cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}
			}
			return out, nil
		},
		Resolve: func(agentsHome, scope, name string) (cmdutil.CanonicalFileEntry, error) {
			sp, err := findMCPSpec(deps, agentsHome, scope, name)
			if err != nil {
				return cmdutil.CanonicalFileEntry{}, err
			}
			return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
		},
		EnsureScope: platform.EnsureUnderMCPScopeTree,
	}
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
