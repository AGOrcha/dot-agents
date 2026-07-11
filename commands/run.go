package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	nodes, err := parseNodes(effectiveLines(string(data)))
	if err != nil {
		return err
	}
	step := 0
	return execNodes(nodes, dispatch, &step)
}

// maxBlockNesting bounds how deeply `for … in … end` and `if … end` blocks may
// nest. Mechanical loops let a recipe fan a fixed body over a
// statically-discovered set (every file in a folder); a shallow, data-driven
// conditional lets a recipe skip a body when a file/flag is absent. Both stay
// mechanical — the iteration set and the condition are filesystem/env STATE
// resolved before dispatch, never a reaction to a command's outcome (that stays
// skill territory, spec D3). A strict 1–2 level cap keeps a recipe readable and
// its cost obviously bounded; deeper nesting wants a skill or the Workflow engine.
const maxBlockNesting = 2

// recipeNode is one parsed unit of a recipe: a single command line (line set),
// a `for … in … end` loop (loop set), or an `if … end` conditional (cond set).
type recipeNode struct {
	line string
	loop *loopNode
	cond *condNode
}

// loopNode is a parsed `for <var> in <pattern>` … `end` block. pattern is kept
// un-expanded; it is env-expanded and glob-resolved at execution time so the
// iteration set reflects the filesystem when the recipe runs.
type loopNode struct {
	varName string
	pattern string
	body    []recipeNode
	srcLine string // original header line, for error messages
}

// condNode is a parsed `if [not] <pred> <arg>` … `end` block. The predicate is
// data-driven and evaluated before dispatch: `exists <glob>` (≥1 path matches)
// or `set <NAME>` (env var non-empty). `not` negates. There is deliberately no
// predicate over a command's exit status — outcome branching stays skill
// territory (spec D3).
type condNode struct {
	pred    string // "exists" | "set"
	arg     string // un-expanded glob (exists) or env var name (set)
	negate  bool
	body    []recipeNode
	srcLine string
}

// parseNodes turns the flat effective lines into a node tree, recognizing
// `for … in … / end` loops and `if … / end` conditionals. It enforces the
// nesting cap and balanced open/`end` pairing; any structural error aborts
// before dispatch.
func parseNodes(lines []string) ([]recipeNode, error) {
	nodes, next, err := parseBlock(lines, 0, 0, "")
	if err != nil {
		return nil, err
	}
	if next != len(lines) {
		return nil, fmt.Errorf("recipe: 'end' without a matching 'for' or 'if'")
	}
	return nodes, nil
}

// parseBlock parses lines[start:] at the given nesting depth. opener is the
// header line of the enclosing block ("" at depth 0), used for a precise
// unterminated-block error. It returns the nodes, the index just past this
// block's terminating `end` (or len(lines) at depth 0), and any structural error.
func parseBlock(lines []string, start, depth int, opener string) ([]recipeNode, int, error) {
	var nodes []recipeNode
	i := start
	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "end" {
			if depth == 0 {
				return nil, 0, fmt.Errorf("recipe: 'end' without a matching 'for' or 'if'")
			}
			return nodes, i + 1, nil
		}
		if v, pat, ok := parseForHeader(line); ok {
			if depth+1 > maxBlockNesting {
				return nil, 0, fmt.Errorf("recipe %q: block nesting exceeds the depth cap of %d", line, maxBlockNesting)
			}
			body, next, err := parseBlock(lines, i+1, depth+1, line)
			if err != nil {
				return nil, 0, err
			}
			nodes = append(nodes, recipeNode{loop: &loopNode{varName: v, pattern: pat, body: body, srcLine: line}})
			i = next
			continue
		}
		if c, ok := parseIfHeader(line); ok {
			if depth+1 > maxBlockNesting {
				return nil, 0, fmt.Errorf("recipe %q: block nesting exceeds the depth cap of %d", line, maxBlockNesting)
			}
			body, next, err := parseBlock(lines, i+1, depth+1, line)
			if err != nil {
				return nil, 0, err
			}
			c.body = body
			nodes = append(nodes, recipeNode{cond: c})
			i = next
			continue
		}
		nodes = append(nodes, recipeNode{line: line})
		i++
	}
	if depth > 0 {
		return nil, 0, fmt.Errorf("recipe %q: unterminated block (missing 'end')", opener)
	}
	return nodes, i, nil
}

// parseForHeader recognizes `for <var> in <pattern>`. The pattern is the
// remainder after " in " (verbatim, un-expanded). ok=false for a malformed
// line, which then falls through to normal dispatch and fails loudly.
func parseForHeader(line string) (varName, pattern string, ok bool) {
	trimmed := strings.TrimSpace(line)
	fields := strings.Fields(trimmed)
	if len(fields) < 4 || fields[0] != "for" || fields[2] != "in" {
		return "", "", false
	}
	idx := strings.Index(trimmed, " in ")
	if idx < 0 {
		return "", "", false
	}
	varName = fields[1]
	pattern = strings.TrimSpace(trimmed[idx+len(" in "):])
	if varName == "" || pattern == "" {
		return "", "", false
	}
	return varName, pattern, true
}

// parseIfHeader recognizes `if [not] exists <glob>` or `if [not] set <NAME>`.
// Returns ok=false for anything else so a malformed `if` line dispatches (and
// fails) rather than silently mis-parsing.
func parseIfHeader(line string) (*condNode, bool) {
	trimmed := strings.TrimSpace(line)
	fields := strings.Fields(trimmed)
	if len(fields) < 3 || fields[0] != "if" {
		return nil, false
	}
	c := &condNode{srcLine: line}
	rest := fields[1:]
	if rest[0] == "not" {
		c.negate = true
		rest = rest[1:]
	}
	if len(rest) < 2 || (rest[0] != "exists" && rest[0] != "set") {
		return nil, false
	}
	c.pred = rest[0]
	idx := strings.Index(trimmed, " "+c.pred+" ")
	if idx < 0 {
		return nil, false
	}
	c.arg = strings.TrimSpace(trimmed[idx+len(c.pred)+2:])
	if c.arg == "" {
		return nil, false
	}
	return c, true
}

// execNodes dispatches a node tree in order, threading a monotonic step counter
// so error messages (and flat-recipe step numbering) stay stable: every
// dispatched command increments step, including each loop-body iteration.
func execNodes(nodes []recipeNode, dispatch recipeDispatcher, step *int) error {
	for _, n := range nodes {
		switch {
		case n.loop != nil:
			if err := execLoop(n.loop, dispatch, step); err != nil {
				return err
			}
		case n.cond != nil:
			if err := execCond(n.cond, dispatch, step); err != nil {
				return err
			}
		default:
			*step++
			if err := dispatchStep(*step, n.line, dispatch); err != nil {
				return err
			}
		}
	}
	return nil
}

// execLoop resolves the loop's pattern (env-expand, then glob) into a sorted,
// static iteration set and runs the body once per match with the loop variable
// bound in the environment. An empty match set runs the body zero times — a
// folder with no matching files is a clean no-op, not an error. The prior value
// of the loop variable is restored afterward. Determinism (sorted matches, no
// branching on command outcomes) keeps the loop mechanical per spec D3.
func execLoop(l *loopNode, dispatch recipeDispatcher, step *int) error {
	matches, err := expandGlob(expandEnv(l.pattern))
	if err != nil {
		return fmt.Errorf("recipe %q: invalid loop pattern: %w", l.srcLine, err)
	}
	sort.Strings(matches)
	prev, had := os.LookupEnv(l.varName)
	defer func() {
		if had {
			_ = os.Setenv(l.varName, prev)
		} else {
			_ = os.Unsetenv(l.varName)
		}
	}()
	for _, m := range matches {
		if err := os.Setenv(l.varName, m); err != nil {
			return fmt.Errorf("recipe %q: binding loop variable %s: %w", l.srcLine, l.varName, err)
		}
		if err := execNodes(l.body, dispatch, step); err != nil {
			return err
		}
	}
	return nil
}

// execCond evaluates the conditional's data-driven predicate and runs the body
// only when it holds. The predicate reads filesystem/env STATE (never a command
// outcome), so a recipe stays mechanical (spec D3).
func execCond(c *condNode, dispatch recipeDispatcher, step *int) error {
	ok, err := evalCond(c)
	if err != nil {
		return fmt.Errorf("recipe %q: %w", c.srcLine, err)
	}
	if c.negate {
		ok = !ok
	}
	if !ok {
		return nil
	}
	return execNodes(c.body, dispatch, step)
}

// evalCond resolves a data predicate: `exists <glob>` is true iff the
// env-expanded pattern matches ≥1 path; `set <NAME>` is true iff the env var is
// non-empty.
func evalCond(c *condNode) (bool, error) {
	switch c.pred {
	case "exists":
		matches, err := expandGlob(expandEnv(c.arg))
		if err != nil {
			return false, fmt.Errorf("invalid 'exists' pattern: %w", err)
		}
		return len(matches) > 0, nil
	case "set":
		return strings.TrimSpace(os.Getenv(c.arg)) != "", nil
	default:
		return false, fmt.Errorf("unknown predicate %q", c.pred)
	}
}

// expandGlob resolves a loop/predicate pattern to matching paths. It extends
// filepath.Glob with a single `**` segment meaning "this directory and any
// descendant": `base/**/<filepat>` matches <filepat> against the basename of
// every file under base at any depth (base's own children included). Patterns
// without `**` use filepath.Glob unchanged. Filesystem errors during the walk are
// skipped (mirroring Glob's ignore-errors contract) and a missing base yields an
// empty set; the caller sorts results for determinism. Only the first `**` is
// honored and the tail is a filename pattern (no embedded '/').
func expandGlob(pattern string) ([]string, error) {
	if !strings.Contains(pattern, "**") {
		return filepath.Glob(pattern)
	}
	i := strings.Index(pattern, "**")
	base := strings.TrimRight(pattern[:i], "/")
	if base == "" {
		base = "."
	}
	tail := strings.TrimLeft(pattern[i+2:], "/")
	if tail == "" {
		tail = "*"
	}
	if _, err := filepath.Match(tail, ""); err != nil {
		return nil, err // ErrBadPattern — mirror filepath.Glob's bad-pattern contract
	}
	var out []string
	_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable subtrees, like Glob ignores fs errors
		}
		if d.IsDir() {
			return nil
		}
		if ok, _ := filepath.Match(tail, filepath.Base(p)); ok {
			out = append(out, p)
		}
		return nil
	})
	return out, nil
}

// expandEnv applies $VAR and ${VAR} substitution to a recipe line using
// os.Expand semantics. Undefined variables expand to the empty string (the
// os.Getenv default for unknown keys). Substitution runs on the whole raw line
// BEFORE tokenization — this is intentional: a recipe is not a shell (D5), so
// single quotes do NOT suppress expansion (quote-blind expansion).
//
// v1: env-var expansion only. Positional arguments ($1, $2, $@) are not
// supported; os.Getenv("1") always returns "" so $1 silently expands to empty.
func expandEnv(line string) string {
	return os.Expand(line, os.Getenv)
}

// dispatchStep expands environment variables in line, tokenizes the result,
// and dispatches the tokens.
// n is the 1-based index of this step among the effective (dispatched) lines.
// Any error — tokenization or dispatch — is returned as a wrapped error that
// names the step index and the ORIGINAL (un-expanded) source line. Keeping the
// original line in the error is load-bearing: expanded values may contain
// secrets (e.g. $TOKEN), so echoing them into error messages would leak
// credentials. It also shows what the recipe author wrote, not the runtime
// value. The underlying error is preserved via %w for errors.Is/errors.As and
// exit-code propagation (D4/R3).
func dispatchStep(n int, line string, dispatch recipeDispatcher) error {
	tokens, err := tokenize(expandEnv(line)) // expand BEFORE tokenize; original line kept for error
	if err == nil {
		err = dispatch(tokens)
	}
	if err != nil {
		return fmt.Errorf("step %d (%q) failed: %w", n, line, err) // ORIGINAL line, not expanded
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
