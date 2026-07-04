package commands

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// recipeDispatcher is the function type used to dispatch a single tokenized
// recipe line (the args that follow "da"). Injectable for testing.
type recipeDispatcher func(args []string) error

// defaultDispatcher invokes the da root command in-process with a fresh
// command tree so each recipe step runs with clean flag state (D5/R4).
func defaultDispatcher(args []string) error {
	root := NewRootCommand()
	root.SetArgs(args)
	return root.Execute()
}

// NewRunCmd builds the public `da run <file>` command using the production
// dispatcher. It is registered in the root command tree in root.go.
func NewRunCmd() *cobra.Command {
	return newRunCmd(defaultDispatcher)
}

// newRunCmd builds the `da run <file>` command with an injectable dispatcher
// so unit tests can verify ordered dispatch without side-effects.
func newRunCmd(dispatch recipeDispatcher) *cobra.Command {
	return &cobra.Command{
		Use:   "run <file>",
		Short: "Execute a da recipe file",
		Long: `Execute a da recipe file — a line-oriented sequence of da commands.

Each non-blank, non-comment line is tokenized (with shell-like single/double
quote handling) and dispatched in order as a da command. A leading shebang
line (#!/...) is ignored. No shell is invoked; recipes are cross-platform.`,
		Example: ExampleBlock(
			"  da run path/to/recipe.da",
			"  ./recipe.da   # if the file is chmod +x with #!/usr/bin/env -S da run",
		),
		Args: ExactArgsWithHints(1, "Provide the path to a .da recipe file."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecipe(args[0], dispatch)
		},
	}
}

// runRecipe reads the recipe file, extracts effective lines, tokenizes each,
// and dispatches them in order. For p1 the loop stops on the first error
// (full fail-fast messaging is added in p2).
func runRecipe(path string, dispatch recipeDispatcher) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := effectiveLines(string(data))
	for _, line := range lines {
		tokens := tokenize(line)
		if len(tokens) == 0 {
			continue
		}
		if err := dispatch(tokens); err != nil {
			return err
		}
	}
	return nil
}

// effectiveLines returns the lines from the recipe content that should be
// dispatched, dropping the shebang (first line starting with "#!"), comment
// lines (any line whose trimmed content starts with "#"), and blank lines
// (D2/R2).
func effectiveLines(content string) []string {
	raw := strings.Split(content, "\n")
	out := make([]string, 0, len(raw))
	for i, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Shebang on the first raw line and all comment lines start with "#".
		if strings.HasPrefix(trimmed, "#") {
			_ = i // shebang is line 0; both shebang and comments are caught here
			continue
		}
		out = append(out, line)
	}
	return out
}

// tokenize splits a recipe line into tokens using a minimal shell-like
// quoted-field splitter. It handles single and double quotes but does NOT
// perform glob expansion, variable substitution, pipe/redirect processing,
// or any other shell evaluation (D5).
//
// Rules:
//   - whitespace (space/tab) outside quotes delimits tokens
//   - content inside single quotes is literal (including spaces)
//   - content inside double quotes is literal (including spaces)
//   - the quote characters themselves are not included in the token
func tokenize(s string) []string {
	var tokens []string
	var cur strings.Builder
	inSingle := false
	inDouble := false

	for _, ch := range s {
		switch {
		case inSingle:
			if ch == '\'' {
				inSingle = false
			} else {
				cur.WriteRune(ch)
			}
		case inDouble:
			if ch == '"' {
				inDouble = false
			} else {
				cur.WriteRune(ch)
			}
		case ch == '\'':
			inSingle = true
		case ch == '"':
			inDouble = true
		case ch == ' ' || ch == '\t':
			tokens = flushToken(tokens, &cur)
		default:
			cur.WriteRune(ch)
		}
	}
	return flushToken(tokens, &cur)
}

// flushToken appends the accumulated token (if any) to tokens and resets cur.
func flushToken(tokens []string, cur *strings.Builder) []string {
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
		cur.Reset()
	}
	return tokens
}
