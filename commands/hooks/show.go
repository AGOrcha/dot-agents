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

func runHooksShow(deps Deps, scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findHookSpec(deps, agentsHome, scope, name)
	if err != nil {
		return err
	}
	ui.Header("Hook " + spec.Name + " (" + scope + ")")
	fmt.Fprintf(os.Stdout, "  %skind:%s %s\n", ui.Dim, ui.Reset, hookKindLabel(spec.SourceKind))
	fmt.Fprintf(os.Stdout, "  %smanifest:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(spec.SourcePath))
	if spec.SourceKind == platform.HookSourceCanonicalBundle {
		fmt.Fprintf(os.Stdout, "  %sbundle dir:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(filepath.Dir(spec.SourcePath)))
	}
	if spec.Description != "" {
		fmt.Fprintf(os.Stdout, "  %sdescription:%s %s\n", ui.Dim, ui.Reset, spec.Description)
	}
	if spec.When != "" {
		fmt.Fprintf(os.Stdout, "  %swhen:%s %s\n", ui.Dim, ui.Reset, spec.When)
	}
	if len(spec.MatchTools) > 0 {
		fmt.Fprintf(os.Stdout, "  %smatch.tools:%s %s\n", ui.Dim, ui.Reset, strings.Join(spec.MatchTools, ", "))
	}
	if spec.MatchExpression != "" {
		fmt.Fprintf(os.Stdout, "  %smatch.expression:%s %s\n", ui.Dim, ui.Reset, spec.MatchExpression)
	}
	cmd := strings.TrimSpace(platform.ResolveHookCommand(*spec))
	if cmd != "" {
		fmt.Fprintf(os.Stdout, "  %scommand:%s %s\n", ui.Dim, ui.Reset, cmd)
	}
	if spec.TimeoutMS > 0 {
		fmt.Fprintf(os.Stdout, "  %stimeout_ms:%s %d\n", ui.Dim, ui.Reset, spec.TimeoutMS)
	}
	if len(spec.EnabledOn) > 0 {
		fmt.Fprintf(os.Stdout, "  %senabled_on:%s %s\n", ui.Dim, ui.Reset, strings.Join(spec.EnabledOn, ", "))
	}
	if len(spec.RequiredOn) > 0 {
		fmt.Fprintf(os.Stdout, "  %srequired_on:%s %s\n", ui.Dim, ui.Reset, strings.Join(spec.RequiredOn, ", "))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}
