package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ----- effectiveLines tests -----

func TestEffectiveLines_DropsShebang(t *testing.T) {
	got := effectiveLines("#!/usr/bin/env -S da run\nworkflow orient\n")
	want := []string{"workflow orient"}
	assertStringSlice(t, got, want)
}

func TestEffectiveLines_DropsCommentLines(t *testing.T) {
	got := effectiveLines("# this is a comment\nstatus\n# another\nrefresh\n")
	want := []string{"status", "refresh"}
	assertStringSlice(t, got, want)
}

func TestEffectiveLines_DropsBlankLines(t *testing.T) {
	got := effectiveLines("add .\n\n  \nstatus\n")
	want := []string{"add .", "status"}
	assertStringSlice(t, got, want)
}

func TestEffectiveLines_AllDropped(t *testing.T) {
	got := effectiveLines("#!/usr/bin/env da\n# comment\n\n   \n")
	if len(got) != 0 {
		t.Errorf("expected no effective lines, got %v", got)
	}
}

func TestEffectiveLines_InlinePoundNotComment(t *testing.T) {
	// A "#" in the middle of a line is NOT a comment; only lines whose first
	// non-whitespace character is "#" are dropped.
	got := effectiveLines(`skills new my-skill --project my#proj` + "\n")
	want := []string{`skills new my-skill --project my#proj`}
	assertStringSlice(t, got, want)
}

func TestEffectiveLines_Empty(t *testing.T) {
	got := effectiveLines("")
	if len(got) != 0 {
		t.Errorf("expected empty slice for empty content, got %v", got)
	}
}

// TestEffectiveLines_StripsTrailingCR proves CRLF (Windows) recipes yield
// clean lines with no stray "\r" — the shebang/comment/blank filters and the
// returned lines all operate on the CR-stripped text (R4).
func TestEffectiveLines_StripsTrailingCR(t *testing.T) {
	got := effectiveLines("#!/usr/bin/env da\r\nstatus\r\nrefresh\r\n")
	want := []string{"status", "refresh"}
	assertStringSlice(t, got, want)
	for _, l := range got {
		if strings.Contains(l, "\r") {
			t.Errorf("line %q still contains a carriage return", l)
		}
	}
}

// ----- tokenize tests -----

func TestTokenize_SimpleLine(t *testing.T) {
	got := mustTokenize(t, "workflow orient")
	want := []string{"workflow", "orient"}
	assertStringSlice(t, got, want)
}

func TestTokenize_DoubleQuotedArg(t *testing.T) {
	got := mustTokenize(t, `skills new "my skill" --verbose`)
	want := []string{"skills", "new", "my skill", "--verbose"}
	assertStringSlice(t, got, want)
}

func TestTokenize_SingleQuotedArg(t *testing.T) {
	got := mustTokenize(t, "add 'path with spaces'")
	want := []string{"add", "path with spaces"}
	assertStringSlice(t, got, want)
}

func TestTokenize_MultipleSpaces(t *testing.T) {
	got := mustTokenize(t, "sync   status")
	want := []string{"sync", "status"}
	assertStringSlice(t, got, want)
}

func TestTokenize_TabDelimiter(t *testing.T) {
	got := mustTokenize(t, "add\t.")
	want := []string{"add", "."}
	assertStringSlice(t, got, want)
}

func TestTokenize_EmptyString(t *testing.T) {
	got := mustTokenize(t, "")
	if len(got) != 0 {
		t.Errorf("expected no tokens for empty string, got %v", got)
	}
}

func TestTokenize_OnlyWhitespace(t *testing.T) {
	got := mustTokenize(t, "   \t  ")
	if len(got) != 0 {
		t.Errorf("expected no tokens for whitespace-only string, got %v", got)
	}
}

func TestTokenize_QuotesAdjacentToWords(t *testing.T) {
	// Quoted span directly adjacent to unquoted text forms a single token.
	got := mustTokenize(t, `--project="my project"`)
	want := []string{"--project=my project"}
	assertStringSlice(t, got, want)
}

func TestTokenize_NestedQuoteCharInsideSingle(t *testing.T) {
	// Double-quote character is literal inside single quotes.
	got := mustTokenize(t, `say 'he said "hello"'`)
	want := []string{"say", `he said "hello"`}
	assertStringSlice(t, got, want)
}

func TestTokenize_NestedSingleInsideDouble(t *testing.T) {
	// Single-quote character is literal inside double quotes.
	got := mustTokenize(t, `say "it's here"`)
	want := []string{"say", "it's here"}
	assertStringSlice(t, got, want)
}

// TestTokenize_EmptyDoubleQuotedArg proves an empty double-quoted span emits
// an empty-string token rather than being dropped.
func TestTokenize_EmptyDoubleQuotedArg(t *testing.T) {
	got := mustTokenize(t, `cmd ""`)
	assertStringSlice(t, got, []string{"cmd", ""})
}

// TestTokenize_EmptySingleQuotedArg mirrors the double-quote case for single
// quotes.
func TestTokenize_EmptySingleQuotedArg(t *testing.T) {
	got := mustTokenize(t, "cmd ''")
	assertStringSlice(t, got, []string{"cmd", ""})
}

// TestTokenize_EmptyQuotedArgBetween proves an empty quoted arg between two
// real args keeps its position (does not collapse).
func TestTokenize_EmptyQuotedArgBetween(t *testing.T) {
	got := mustTokenize(t, `a "" b`)
	assertStringSlice(t, got, []string{"a", "", "b"})
}

// TestTokenize_UnterminatedDoubleQuote proves an unclosed double quote is a
// hard error, not a silently-accepted token.
func TestTokenize_UnterminatedDoubleQuote(t *testing.T) {
	if _, err := tokenize(`cmd "oops`); err == nil {
		t.Fatal("expected unterminated-quote error, got nil")
	}
}

// TestTokenize_UnterminatedSingleQuote mirrors the double-quote error case.
func TestTokenize_UnterminatedSingleQuote(t *testing.T) {
	_, err := tokenize("cmd 'oops")
	if err == nil {
		t.Fatal("expected unterminated-quote error, got nil")
	}
	if !strings.Contains(err.Error(), "unterminated quote") {
		t.Errorf("expected 'unterminated quote' message, got %v", err)
	}
}

// ----- runRecipe / dispatch tests -----

// recordingDispatcher records every dispatch call in order and returns a
// configurable error for a specific call index (-1 = never fail).
type recordingDispatcher struct {
	calls    [][]string
	failAt   int
	failWith error
}

func (r *recordingDispatcher) dispatch(args []string) error {
	r.calls = append(r.calls, args)
	if r.failAt >= 0 && len(r.calls)-1 == r.failAt {
		return r.failWith
	}
	return nil
}

func TestRunRecipe_DispatchesInOrder(t *testing.T) {
	f := writeTempRecipe(t, "workflow orient\nstatus\nrefresh\n")
	rec := &recordingDispatcher{failAt: -1}

	if err := runRecipe(f, rec.dispatch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantCalls := [][]string{
		{"workflow", "orient"},
		{"status"},
		{"refresh"},
	}
	assertCallSlice(t, rec.calls, wantCalls)
}

func TestRunRecipe_SkipsShebangAndComments(t *testing.T) {
	content := "#!/usr/bin/env -S da run\n# header comment\nworkflow orient\n# middle\nstatus\n"
	f := writeTempRecipe(t, content)
	rec := &recordingDispatcher{failAt: -1}

	if err := runRecipe(f, rec.dispatch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantCalls := [][]string{
		{"workflow", "orient"},
		{"status"},
	}
	assertCallSlice(t, rec.calls, wantCalls)
}

// TestRunRecipe_CRLFDispatchesCleanTokens proves a recipe authored with
// Windows CRLF line endings dispatches clean tokens with no trailing "\r"
// on any command token (R4, cross-platform).
func TestRunRecipe_CRLFDispatchesCleanTokens(t *testing.T) {
	f := writeTempRecipe(t, "workflow orient\r\nstatus\r\n")
	rec := &recordingDispatcher{failAt: -1}

	if err := runRecipe(f, rec.dispatch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantCalls := [][]string{
		{"workflow", "orient"},
		{"status"},
	}
	assertCallSlice(t, rec.calls, wantCalls)
	for _, call := range rec.calls {
		for _, tok := range call {
			if strings.Contains(tok, "\r") {
				t.Errorf("dispatched token %q contains a carriage return", tok)
			}
		}
	}
}

func TestRunRecipe_StopsOnFirstError(t *testing.T) {
	f := writeTempRecipe(t, "step-a\nstep-b\nstep-c\n")
	sentinel := errors.New("step-b failed")
	rec := &recordingDispatcher{failAt: 1, failWith: sentinel}

	err := runRecipe(f, rec.dispatch)
	// The sentinel must be unwrappable through the step-context wrapper.
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error to be unwrappable, got %v", err)
	}
	// Message must name the step index (step-b is the 2nd effective line).
	if !strings.Contains(err.Error(), "step 2 (") {
		t.Errorf("expected 'step 2 (' in message, got %v", err)
	}
	// Message must include the source line text.
	if !strings.Contains(err.Error(), "step-b") {
		t.Errorf("expected source line text in message, got %v", err)
	}
	// Message must include the underlying error text.
	if !strings.Contains(err.Error(), sentinel.Error()) {
		t.Errorf("expected underlying error text in message, got %v", err)
	}
	// step-c must NOT have been dispatched (fail-fast).
	if len(rec.calls) != 2 {
		t.Errorf("expected 2 dispatch calls, got %d: %v", len(rec.calls), rec.calls)
	}
}

// TestRunRecipe_FailFastCountsOnlyEffectiveLines proves that N in the
// "step N (...)" message counts only dispatched (effective) lines —
// comments and blank lines preceding the failing step do not increment N.
func TestRunRecipe_FailFastCountsOnlyEffectiveLines(t *testing.T) {
	// A comment and a blank line precede the two effective steps.
	// step-b is the 2nd effective line, so the message must say "step 2".
	f := writeTempRecipe(t, "# header comment\n\nstep-a\nstep-b\n")
	sentinel := errors.New("injected failure")
	rec := &recordingDispatcher{failAt: 1, failWith: sentinel}

	err := runRecipe(f, rec.dispatch)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error unwrappable, got %v", err)
	}
	if !strings.Contains(err.Error(), "step 2 (") {
		t.Errorf("expected 'step 2 (' in message (comments/blanks must not be counted), got %v", err)
	}
}

// TestRunRecipe_FailFastIncludesSourceLine proves the original source line
// text — including any quoted arguments — is embedded verbatim in the error
// message so operators can identify exactly which recipe line failed.
func TestRunRecipe_FailFastIncludesSourceLine(t *testing.T) {
	srcLine := `skills new "my cool skill" --project proj`
	f := writeTempRecipe(t, srcLine+"\n")
	sentinel := errors.New("dispatch error")
	rec := &recordingDispatcher{failAt: 0, failWith: sentinel}

	err := runRecipe(f, rec.dispatch)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The source line text (including quoted args) must appear in the message.
	if !strings.Contains(err.Error(), "my cool skill") {
		t.Errorf("source line text not found in error message: %v", err)
	}
	if !strings.Contains(err.Error(), "--project proj") {
		t.Errorf("source line args not found in error message: %v", err)
	}
}

// testStepErr is a concrete error type used to verify errors.As unwrapping
// through the step-context wrapper produced by dispatchStep.
type testStepErr struct{ msg string }

func (e *testStepErr) Error() string { return e.msg }

// TestRunRecipe_WrappedErrorPreservesIdentity proves errors.Is and errors.As
// both penetrate the step-context wrapper so exit-code-carrying errors and
// typed sentinel errors survive the fail-fast wrapping.
func TestRunRecipe_WrappedErrorPreservesIdentity(t *testing.T) {
	f := writeTempRecipe(t, "some-cmd\n")
	original := &testStepErr{"original dispatch error"}
	rec := &recordingDispatcher{failAt: 0, failWith: original}

	err := runRecipe(f, rec.dispatch)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, original) {
		t.Errorf("errors.Is failed to find original error through wrapper: %v", err)
	}
	var target *testStepErr
	if !errors.As(err, &target) {
		t.Errorf("errors.As failed to extract original error type through wrapper: %v", err)
	}
	if target.msg != original.msg {
		t.Errorf("errors.As target msg: got %q, want %q", target.msg, original.msg)
	}
}

func TestRunRecipe_EmptyFileNoDispatches(t *testing.T) {
	f := writeTempRecipe(t, "# just a comment\n\n")
	rec := &recordingDispatcher{failAt: -1}

	if err := runRecipe(f, rec.dispatch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected no dispatches for comment-only file, got %v", rec.calls)
	}
}

func TestRunRecipe_FileNotFound(t *testing.T) {
	err := runRecipe(filepath.Join(t.TempDir(), "missing.da"), func([]string) error { return nil })
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestRunRecipe_QuotedArgsPassedCorrectly(t *testing.T) {
	f := writeTempRecipe(t, `skills new "my cool skill" --project proj`+"\n")
	rec := &recordingDispatcher{failAt: -1}

	if err := runRecipe(f, rec.dispatch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rec.calls))
	}
	want := []string{"skills", "new", "my cool skill", "--project", "proj"}
	assertStringSlice(t, rec.calls[0], want)
}

// TestRunRecipe_UnterminatedQuoteAborts proves a malformed line (unterminated
// quote) surfaces the tokenizer error and aborts the run — earlier valid
// lines dispatch, the malformed line does not, and later lines never run.
func TestRunRecipe_UnterminatedQuoteAborts(t *testing.T) {
	f := writeTempRecipe(t, "status\ncmd \"oops\nrefresh\n")
	rec := &recordingDispatcher{failAt: -1}

	err := runRecipe(f, rec.dispatch)
	if err == nil {
		t.Fatal("expected error for unterminated-quote line, got nil")
	}
	if !strings.Contains(err.Error(), "unterminated quote") {
		t.Errorf("expected 'unterminated quote' message, got %v", err)
	}
	// Only the first valid line dispatched before the malformed line aborted.
	if len(rec.calls) != 1 || rec.calls[0][0] != "status" {
		t.Errorf("expected only [status] dispatched before abort, got %v", rec.calls)
	}
}

// TestRunRecipe_RecursionGuardStops proves a recipe that invokes `da run` on
// itself fails with a recursion/cycle error via the REAL production dispatcher
// (defaultDispatcher) instead of recursing without limit. This also exercises
// real in-process dispatch of the `run` subcommand end-to-end.
func TestRunRecipe_RecursionGuardStops(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "self.da")
	// The recipe's only step re-runs itself (path double-quoted so any spaces
	// in the temp dir survive tokenization).
	if err := os.WriteFile(self, []byte(`run "`+self+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runRecipe(self, defaultDispatcher)
	if err == nil {
		t.Fatal("expected recursion-limit error, got nil (guard did not fire)")
	}
	if !strings.Contains(err.Error(), "recursion limit") {
		t.Errorf("expected 'recursion limit' error, got %v", err)
	}
	// The guard must fully unwind: the depth env var is left unset afterward.
	if _, present := os.LookupEnv(recipeDepthEnv); present {
		t.Errorf("recursion-depth env var %q leaked after run", recipeDepthEnv)
	}
}

// ----- expandEnv / env-substitution tests -----

// TestExpandEnv covers $VAR, ${VAR}, quote-blind expansion, undefined-var
// empty-string semantics, and multi-var lines (D6/R6, OQ1).
func TestExpandEnv(t *testing.T) {
	const keyA = "DA_TEST_EXPKEY_A"
	const keyB = "DA_TEST_EXPKEY_B"
	const keyUndef = "DA_TEST_EXPKEY_UNDEF"

	tests := []struct {
		name  string
		setup func(t *testing.T)
		input string
		want  string
	}{
		{
			name:  "bare $VAR expands",
			setup: func(t *testing.T) { t.Setenv(keyA, "hello") },
			input: "cmd $" + keyA,
			want:  "cmd hello",
		},
		{
			name:  "braced ${VAR} expands",
			setup: func(t *testing.T) { t.Setenv(keyA, "world") },
			input: "cmd ${" + keyA + "}",
			want:  "cmd world",
		},
		{
			// Quote-blind: $VAR inside double quotes still expands. A recipe is
			// not a shell (D5); double quotes do not suppress expansion.
			name:  "expands inside double quotes (quote-blind)",
			setup: func(t *testing.T) { t.Setenv(keyA, "dval") },
			input: `cmd "prefix-$` + keyA + `"`,
			want:  `cmd "prefix-dval"`,
		},
		{
			// Quote-blind: $VAR inside single quotes also expands. A recipe is
			// not a shell (D5); single quotes do NOT suppress expansion.
			name:  "expands inside single quotes (quote-blind, not a shell)",
			setup: func(t *testing.T) { t.Setenv(keyA, "sval") },
			input: "cmd '$" + keyA + "'",
			want:  "cmd 'sval'",
		},
		{
			// Undefined variable: os.Getenv returns "" for unknown keys.
			name:  "undefined var expands to empty string",
			setup: func(t *testing.T) { os.Unsetenv(keyUndef) },
			input: "cmd $" + keyUndef + " end",
			want:  "cmd  end",
		},
		{
			name: "multiple vars on one line all expand",
			setup: func(t *testing.T) {
				t.Setenv(keyA, "foo")
				t.Setenv(keyB, "bar")
			},
			input: "cmd $" + keyA + " $" + keyB,
			want:  "cmd foo bar",
		},
		{
			// Lines with no $ are returned unchanged (no-op path).
			name:  "line without vars is unchanged",
			setup: func(t *testing.T) {},
			input: "workflow orient",
			want:  "workflow orient",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			if got := expandEnv(tc.input); got != tc.want {
				t.Errorf("expandEnv(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestRunRecipe_EnvVarExpandsBeforeDispatch proves the dispatcher receives the
// expanded token value, not the raw $VAR text. Substitution happens before
// tokenization per D6/R6.
func TestRunRecipe_EnvVarExpandsBeforeDispatch(t *testing.T) {
	const key = "DA_TEST_RECIPE_ARG"
	t.Setenv(key, "expanded-arg")

	f := writeTempRecipe(t, "cmd $"+key+"\n")
	rec := &recordingDispatcher{failAt: -1}
	if err := runRecipe(f, rec.dispatch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", len(rec.calls))
	}
	assertStringSlice(t, rec.calls[0], []string{"cmd", "expanded-arg"})
}

// TestDispatchStep_EnvVarNoLeak proves the fail-fast error message contains the
// original $VAR reference (not the expanded secret value). This is
// security-load-bearing: expanded values may contain credentials.
func TestDispatchStep_EnvVarNoLeak(t *testing.T) {
	const secretVar = "DA_TEST_SECRET_NOSHOW"
	const secretVal = "sentinel-secret-abc987"
	t.Setenv(secretVar, secretVal)

	srcLine := "da-cmd --token $" + secretVar
	f := writeTempRecipe(t, srcLine+"\n")
	sentinel := errors.New("injected failure")
	rec := &recordingDispatcher{failAt: 0, failWith: sentinel}

	err := runRecipe(f, rec.dispatch)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The expanded secret value MUST NOT appear in the error message.
	if strings.Contains(err.Error(), secretVal) {
		t.Errorf("error message leaks expanded env value %q: %v", secretVal, err)
	}
	// The original source line (with the literal $VAR text) MUST appear.
	if !strings.Contains(err.Error(), "$"+secretVar) {
		t.Errorf("error message must contain original $VAR reference, got: %v", err)
	}
}

// ----- newRunCmd RunE coverage -----

func TestNewRunCmd_RunE_InvokesRecipeViaInjectedDispatcher(t *testing.T) {
	f := writeTempRecipe(t, "workflow orient\nstatus\n")
	rec := &recordingDispatcher{failAt: -1}
	cmd := newRunCmd(rec.dispatch)
	cmd.SetArgs([]string{f})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantCalls := [][]string{{"workflow", "orient"}, {"status"}}
	assertCallSlice(t, rec.calls, wantCalls)
}

// ----- defaultDispatcher (real production dispatch) coverage -----

// TestDefaultDispatcher_RealSubcommandSucceeds drives a real, side-effect-free
// da subcommand (`explain links`) through the production dispatcher so the
// in-process seam is exercised against a genuine RunE (not just the fake sink
// or the --help fast path).
func TestDefaultDispatcher_RealSubcommandSucceeds(t *testing.T) {
	if err := defaultDispatcher([]string{"explain", "links"}); err != nil {
		t.Fatalf("real dispatch of `explain links` returned error: %v", err)
	}
}

func TestDefaultDispatcher_HelpExitsClean(t *testing.T) {
	// Exercise the production dispatcher seam: --help is handled by cobra
	// before any RunE logic, so no side-effects occur beyond stdout output.
	if err := defaultDispatcher([]string{"--help"}); err != nil {
		t.Fatalf("defaultDispatcher with --help returned error: %v", err)
	}
}

// ----- NewRunCmd wiring tests -----

func TestNewRunCmd_IsRegistered(t *testing.T) {
	root := NewRootCommand()
	for _, sub := range root.Commands() {
		if sub.Name() == "run" {
			return
		}
	}
	t.Fatal("'run' command not found in root command tree")
}

func TestNewRunCmd_RequiresOneArg(t *testing.T) {
	cmd := NewRunCmd()
	// Zero args should produce an error.
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error when no file argument is provided")
	}
}

// ----- helpers -----

func mustTokenize(t *testing.T, s string) []string {
	t.Helper()
	got, err := tokenize(s)
	if err != nil {
		t.Fatalf("tokenize(%q) unexpected error: %v", s, err)
	}
	return got
}

func writeTempRecipe(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "recipe.da")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func assertCallSlice(t *testing.T, got, want [][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("call count mismatch: got %d %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range got {
		assertStringSlice(t, got[i], want[i])
	}
}
