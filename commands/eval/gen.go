package eval

import (
	"context"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/spf13/cobra"
)

// genOptions are the parsed flags of `da eval gen`.
type genOptions struct {
	language   string
	difficulty string
	template   string
	out        string
}

// newGenCmd builds `da eval gen`. The RunE handler is injected by the root (see
// package doc); this constructor owns only the command shape + flag definitions.
func newGenCmd(runE handlerFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate an eval TaskSpec from the knowledge graph",
		Example: "  da eval gen --language go\n" +
			"  da eval gen --language go --difficulty medium\n" +
			"  da eval gen --language typescript --out task.yaml",
		Args: cobra.NoArgs,
		RunE: runE,
	}
	cmd.Flags().String(languageFlagName, "", languageFlagHelp)
	cmd.Flags().String(difficultyFlagName, "", "Constrain the difficulty band: easy, medium, or hard (default: generator's choice)")
	cmd.Flags().String(templateFlagName, "", "Task template id (default: impl-pure-fn)")
	cmd.Flags().String(outFlagName, "", "Write the TaskSpec YAML to this file instead of stdout")
	return cmd
}

// RunGen is the `da eval gen` entry point the root wires as the subcommand's
// RunE. It reads the gen flags off cmd and synthesises + writes a TaskSpec.
func RunGen(cmd *cobra.Command) error {
	return runGen(cmd.Context(), cmd.OutOrStdout(), genOptionsFrom(cmd))
}

// genOptionsFrom reads the gen subcommand's flags off cmd.
func genOptionsFrom(cmd *cobra.Command) genOptions {
	return genOptions{
		language:   flagString(cmd, languageFlagName),
		difficulty: flagString(cmd, difficultyFlagName),
		template:   flagString(cmd, templateFlagName),
		out:        flagString(cmd, outFlagName),
	}
}

// runGen builds the generator registry over the warm code graph, synthesises
// one TaskSpec for the requested language, and writes it. It validates the
// language up front so a typo fails before the (comparatively expensive) graph
// open.
func runGen(ctx context.Context, out io.Writer, opts genOptions) error {
	if err := validateLanguage(evalcore.Language(opts.language)); err != nil {
		return err
	}
	reg, closeFn, err := kgRegistry()
	if err != nil {
		return err
	}
	defer closeReader(closeFn)
	return generateAndWrite(ctx, out, reg, opts)
}

// generateAndWrite resolves the language's generator, produces one TaskSpec, and
// emits it as YAML to opts.out (or out when opts.out is empty). Splitting it
// from runGen keeps the registry-lifecycle and the generate/emit concerns
// separately testable (the latter with a hand-built registry).
func generateAndWrite(ctx context.Context, out io.Writer, reg *evalcore.Registry, opts genOptions) error {
	lang := evalcore.Language(opts.language)
	gen, ok := reg.Lookup(lang)
	if !ok {
		return fmt.Errorf("eval gen: no generator registered for language %q", opts.language)
	}
	spec, err := gen.Generate(ctx, evalcore.GenerateOptions{
		Difficulty: evalcore.Difficulty(opts.difficulty),
		TemplateID: opts.template,
	})
	if err != nil {
		return fmt.Errorf("eval gen: %w", err)
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("eval gen: marshal task spec: %w", err)
	}
	return emitSpec(out, data, spec.TaskID, opts.out)
}

// emitSpec writes the marshalled spec to stdout or, when outPath is set, to that
// file atomically (temp-then-rename via internal/fsops) with a confirmation line.
func emitSpec(out io.Writer, data []byte, taskID, outPath string) error {
	if outPath == "" {
		_, err := out.Write(data)
		return err
	}
	if err := fsops.WriteFileAtomic(outPath, data); err != nil {
		return fmt.Errorf("eval gen: write %s: %w", outPath, err)
	}
	if _, err := fmt.Fprintf(out, "Wrote TaskSpec %s to %s\n", taskID, outPath); err != nil {
		return fmt.Errorf("eval gen: confirm write: %w", err)
	}
	return nil
}
