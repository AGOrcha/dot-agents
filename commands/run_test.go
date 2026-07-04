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
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	// step-c must NOT have been dispatched
	if len(rec.calls) != 2 {
		t.Errorf("expected 2 dispatch calls, got %d: %v", len(rec.calls), rec.calls)
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
