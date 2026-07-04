package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// maxRecipeDepth bounds how deeply `da run` may recurse (a recipe that
// invokes `da run` on itself, or a cycle of recipes). Past this depth
// runRecipe fails instead of recursing without limit.
const maxRecipeDepth = 32

// recipeDepthEnv carries the current recursion depth across nested in-process
// (and shebang-exec) `da run` invocations. An env var (rather than a package
// counter) survives the process boundary when a chmod-+x `.da` file re-execs
// `da run`, so the guard holds for both in-process and exec recursion.
const recipeDepthEnv = "DA_RECIPE_DEPTH"

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

// enterRecipe increments the recursion-depth guard, returning a restore
// closure (to defer) and an error when the depth limit is exceeded.
func enterRecipe() (func(), error) {
	depth, _ := strconv.Atoi(os.Getenv(recipeDepthEnv))
	if depth >= maxRecipeDepth {
		return nil, fmt.Errorf(
			"da run: recipe recursion limit (%d) exceeded — a recipe is invoking `da run` on itself or a cycle exists",
			maxRecipeDepth)
	}
	prev, had := os.LookupEnv(recipeDepthEnv)
	os.Setenv(recipeDepthEnv, strconv.Itoa(depth+1))
	return func() {
		if had {
			os.Setenv(recipeDepthEnv, prev)
		} else {
			os.Unsetenv(recipeDepthEnv)
		}
	}, nil
}

// runRecipe reads the recipe file, extracts effective lines, and dispatches
// them in order via dispatchStep. The first failure aborts the run (fail-fast,
// D4/R3); subsequent steps are never dispatched. The returned error wraps the
// original underlying error with %w so errors.Is/errors.As still work.
func runRecipe(path string, dispatch recipeDispatcher) error {
	restore, err := enterRecipe()
	if err != nil {
		return err
	}
	defer restore()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for i, line := range effectiveLines(string(data)) {
		if err := dispatchStep(i+1, line, dispatch); err != nil {
			return err
		}
	}
	return nil
}

// dispatchStep tokenizes one effective recipe line and dispatches it.
// n is the 1-based index of this step among the effective (dispatched) lines.
// Any error — tokenization or dispatch — is returned as a wrapped error that
// names the step index and the original source line, preserving the underlying
// error via %w for errors.Is/errors.As and exit-code propagation (D4/R3).
func dispatchStep(n int, line string, dispatch recipeDispatcher) error {
	tokens, err := tokenize(line)
	if err == nil {
		err = dispatch(tokens)
	}
	if err != nil {
		return fmt.Errorf("step %d (%q) failed: %w", n, line, err)
	}
	return nil
}

// effectiveLines returns the lines from the recipe content that should be
// dispatched, dropping the shebang (first line starting with "#!"), comment
// lines (any line whose trimmed content starts with "#"), and blank lines
// (D2/R2). A trailing "\r" is stripped first so CRLF (Windows) recipes
// dispatch clean tokens rather than a stray "\r" on the last token (R4).
func effectiveLines(content string) []string {
	raw := strings.Split(content, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// The shebang (line 0) and every comment line start with "#".
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// flushToken appends the current token to tokens when a token was started
// (including an empty token produced by an empty quoted span), and resets the
// builder. The caller clears its "started" flag after calling.
func flushToken(tokens []string, cur *strings.Builder, started bool) []string {
	if started {
		tokens = append(tokens, cur.String())
		cur.Reset()
	}
	return tokens
}

// tokenize splits a recipe line into tokens using a minimal shell-like
// quoted-field splitter. It handles single and double quotes but does NOT
// perform glob expansion, variable substitution, pipe/redirect processing,
// or any other shell evaluation (D5).
//
// Rules:
//   - whitespace (space/tab) outside quotes delimits tokens
//   - content inside single or double quotes is literal (including spaces)
//   - the quote characters themselves are not part of the token
//   - an empty quoted span (a pair of quotes) still emits an empty-string token
//   - an unterminated quote is an error, not a silently-accepted token
func tokenize(s string) ([]string, error) {
	var tokens []string
	var cur strings.Builder
	started := false
	inSingle := false
	inDouble := false

	for _, ch := range s {
		switch {
		case inSingle:
			if inSingle = ch != '\''; inSingle {
				cur.WriteRune(ch)
			}
		case inDouble:
			if inDouble = ch != '"'; inDouble {
				cur.WriteRune(ch)
			}
		case ch == '\'':
			inSingle, started = true, true
		case ch == '"':
			inDouble, started = true, true
		case ch == ' ' || ch == '\t':
			tokens = flushToken(tokens, &cur, started)
			started = false
		default:
			cur.WriteRune(ch)
			started = true
		}
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quote in recipe line: %q", s)
	}
	return flushToken(tokens, &cur, started), nil
}
