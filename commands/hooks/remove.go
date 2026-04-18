package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

func runHooksRemove(deps Deps, scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findHookSpec(deps, agentsHome, scope, name)
	if err != nil {
		return err
	}
	target, err := hookRemovalTarget(spec)
	if err != nil {
		return err
	}
	if err := ensureUnderHooksScopeTree(agentsHome, scope, target); err != nil {
		return err
	}

	ui.Header("dot-agents hooks remove")
	fmt.Fprintf(os.Stdout, "Remove %s hook %q\n", ui.BoldText(scope), name)
	fmt.Fprintf(os.Stdout, "  %s\n", config.DisplayPath(target))

	if deps.Flags.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}
	if !deps.Flags.Yes && !deps.Flags.Force {
		if !ui.Confirm("Remove this hook from ~/.agents/hooks/?", false) {
			ui.Info("Cancelled.")
			return nil
		}
	}

	if spec.SourceKind == platform.HookSourceCanonicalBundle {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("removing bundle: %w", err)
		}
	} else {
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("removing file: %w", err)
		}
	}
	ui.Success(fmt.Sprintf("Removed hook %q from scope %s.", name, scope))
	return nil
}

func hookRemovalTarget(spec *platform.HookSpec) (string, error) {
	switch spec.SourceKind {
	case platform.HookSourceCanonicalBundle:
		return filepath.Dir(spec.SourcePath), nil
	case platform.HookSourceLegacyFile:
		return spec.SourcePath, nil
	default:
		return "", fmt.Errorf("unsupported hook source kind %q", spec.SourceKind)
	}
}

func ensureUnderHooksScopeTree(agentsHome, scope, target string) error {
	root := filepath.Join(agentsHome, "hooks", scope)
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove path outside %s", cleanRoot)
	}
	return nil
}
