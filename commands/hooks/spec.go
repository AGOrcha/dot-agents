package hooks

import (
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/platform"
)

func hookKindLabel(kind platform.HookSourceKind) string {
	switch kind {
	case platform.HookSourceCanonicalBundle:
		return "canonical bundle"
	case platform.HookSourceLegacyFile:
		return "legacy file"
	default:
		return string(kind)
	}
}

func findHookSpec(deps Deps, agentsHome, scope, name string) (*platform.HookSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, deps.UsageError("hook name is empty", "Pass the logical name shown by `dot-agents hooks list`.")
	}
	specs, err := platform.ListHookSpecs(agentsHome, scope)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, deps.ErrorWithHints(
				fmt.Sprintf("no hooks directory for scope %q", scope),
				"Create ~/.agents/hooks/"+scope+"/ or run `dot-agents import` to populate hooks.",
			)
		}
		return nil, err
	}
	for i := range specs {
		if specs[i].Name == name {
			return &specs[i], nil
		}
	}
	return nil, deps.ErrorWithHints(
		fmt.Sprintf("hook not found: %s / %s", scope, name),
		"Run `dot-agents hooks list "+scope+"` to see available names.",
	)
}
