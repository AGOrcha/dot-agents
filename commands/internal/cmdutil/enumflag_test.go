package cmdutil

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestEnumSpecUsageString(t *testing.T) {
	cases := []struct {
		name string
		spec EnumSpec
		want string
	}{
		{
			name: "lists values inline",
			spec: EnumSpec{Name: "status", Usage: "New task status", Values: []string{"pending", "completed"}},
			want: "New task status (one of: pending|completed)",
		},
		{
			name: "marks required after the listing",
			spec: EnumSpec{Name: "kind", Usage: "Kind of run", Values: []string{"test", "review"}, Required: true},
			want: "Kind of run (one of: test|review) (required)",
		},
		{
			name: "points at the live source when config-derived",
			spec: EnumSpec{Name: "app-type", Usage: "App type", DynamicFrom: "da workflow app-types"},
			want: "App type (values come from: da workflow app-types)",
		},
		{
			// Cobra reads the first backticked word as the flag's value-type
			// name, so a backtick in an author's Note would silently rewrite
			// "--kind string" into "--kind review".
			name: "strips backticks so cobra keeps the string type name",
			spec: EnumSpec{Name: "kind", Usage: "Kind", Values: []string{"test"}, Note: "`review` is special"},
			want: "Kind (one of: test); review is special",
		},
		{
			name: "appends the note after the listing",
			spec: EnumSpec{Name: "scope", Usage: "Coverage", Values: []string{"file", "repo"}, Note: "defaults to repo"},
			want: "Coverage (one of: file|repo); defaults to repo",
		},
		{
			name: "omits the listing when neither values nor source are declared",
			spec: EnumSpec{Name: "profile", Usage: "Profile label"},
			want: "Profile label",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertEqual(t, "usage", tc.spec.UsageString(), tc.want)
		})
	}
}

func TestEnumSpecValidate(t *testing.T) {
	spec := EnumSpec{Name: "kind", Values: []string{"test", "lint", "review"}}
	cases := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "member passes", value: "lint"},
		{name: "surrounding space is tolerated", value: "  review  "},
		{name: "empty defers to the required check", value: ""},
		{name: "non-member fails", value: "bogus", wantError: true},
		{name: "wrong case fails", value: "Test", wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := spec.Validate(tc.value)
			assertErrorPresence(t, err, tc.wantError)
		})
	}
}

func TestEnumSpecValidateNamesEveryValue(t *testing.T) {
	spec := EnumSpec{Name: "status", Values: []string{"pass", "fail", "partial", "unknown"}}
	err := spec.Validate("done")
	if err == nil {
		t.Fatal("want error for out-of-set value")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--status must be one of") {
		t.Fatalf("error should name the flag and the constraint, got %q", msg)
	}
	for _, v := range spec.Values {
		if !strings.Contains(msg, v) {
			t.Errorf("error omits allowed value %q: %s", v, msg)
		}
	}
	if !strings.Contains(msg, `"done"`) {
		t.Errorf("error should echo the rejected value, got %q", msg)
	}
}

func TestEnumSpecValidateSuggestsNearMiss(t *testing.T) {
	// An agent composing a flag from prose reaches for the hyphenated or
	// title-cased form; the hint should land it on the real value rather than
	// making it re-read the whole vocabulary.
	spec := EnumSpec{Name: "status", Values: []string{"in_progress", "completed"}}
	err := spec.Validate("in-progress")
	if err == nil {
		t.Fatal("want error")
	}
	assertContains(t, err.Error()+" "+hintsOf(t, spec, "in-progress"), "--status in_progress")
}

func TestEnumSpecContainsAcceptsAnythingWhenDynamic(t *testing.T) {
	spec := EnumSpec{Name: "app-type", DynamicFrom: "da workflow app-types"}
	if !spec.Contains("whatever-the-repo-declares") {
		t.Fatal("a config-derived enum must not reject values the binary cannot know")
	}
	if err := spec.Validate("whatever-the-repo-declares"); err != nil {
		t.Fatalf("Validate should be a no-op for a dynamic enum, got %v", err)
	}
}

func TestRegisterEnumWiresHelpAndValidation(t *testing.T) {
	var got string
	cmd := &cobra.Command{Use: "record", RunE: func(*cobra.Command, []string) error { return nil }}
	RegisterEnum(cmd, &got, EnumSpec{
		Name:   "kind",
		Usage:  "Class of run",
		Values: []string{"test", "review"},
	})

	flag := cmd.Flags().Lookup("kind")
	if flag == nil {
		t.Fatal("flag was not registered")
	}
	assertContains(t, flag.Usage, "(one of: test|review)")
	assertEqual(t, "annotation", strings.Join(EnumValues(cmd, "kind"), "|"), "test|review")

	if err := runCommand(cmd, "--kind", "review"); err != nil {
		t.Fatalf("in-set value rejected: %v", err)
	}
	assertEqual(t, "bound target", got, "review")

	err := runCommand(cmd, "--kind", "bogus")
	if err == nil || !strings.Contains(err.Error(), "--kind must be one of test|review") {
		t.Fatalf("out-of-set value should fail with the vocabulary named, got %v", err)
	}
}

func TestRegisterEnumFlagValidatesUnboundFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "closeout", RunE: func(*cobra.Command, []string) error { return nil }}
	RegisterEnumFlag(cmd, EnumSpec{Name: "decision", Values: []string{"accept", "reject"}})

	if err := runCommand(cmd, "--decision", "accept"); err != nil {
		t.Fatalf("in-set value rejected: %v", err)
	}
	err := runCommand(cmd, "--decision", "maybe")
	if err == nil || !strings.Contains(err.Error(), "--decision must be one of accept|reject") {
		t.Fatalf("want vocabulary-naming error, got %v", err)
	}
}

func TestRegisterEnumPreservesExistingPreRunE(t *testing.T) {
	var target string
	ran := false
	cmd := &cobra.Command{
		Use:     "write",
		PreRunE: func(*cobra.Command, []string) error { ran = true; return nil },
		RunE:    func(*cobra.Command, []string) error { return nil },
	}
	RegisterEnum(cmd, &target, EnumSpec{Name: "result", Values: []string{"allow", "advise"}})

	if err := runCommand(cmd, "--result", "allow"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Fatal("the command's own PreRunE must still run")
	}

	ran = false
	if err := runCommand(cmd, "--result", "nope"); err == nil {
		t.Fatal("want validation error")
	}
	if ran {
		t.Fatal("the command's PreRunE must not run once validation has failed")
	}
}

func TestRegisterEnumOffersCompletionCandidates(t *testing.T) {
	var target string
	cmd := &cobra.Command{Use: "advance", RunE: func(*cobra.Command, []string) error { return nil }}
	RegisterEnum(cmd, &target, EnumSpec{Name: "status", Values: []string{"pending", "completed"}})

	completer, ok := cmd.GetFlagCompletionFunc("status")
	if !ok {
		t.Fatal("no completion function registered for --status")
	}
	comps, directive := completer(cmd, nil, "")
	assertEqual(t, "completion candidates", strings.Join(comps, "|"), "pending|completed")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("want NoFileComp directive, got %v", directive)
	}
}

func TestRegisterEnumSkipsCompletionForDynamicEnum(t *testing.T) {
	// A config-derived enum has no candidates to offer, and completing to an
	// empty list is worse than leaving the shell's default behaviour alone.
	var target string
	cmd := &cobra.Command{Use: "create", RunE: func(*cobra.Command, []string) error { return nil }}
	RegisterEnum(cmd, &target, EnumSpec{Name: "app-type", DynamicFrom: "da workflow app-types"})
	if _, ok := cmd.GetFlagCompletionFunc("app-type"); ok {
		t.Fatal("a dynamic enum must not register empty completions")
	}
}

func TestRegisterEnumRejectsDefaultOutsideSet(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a default outside the declared set is a command-definition bug and must panic")
		}
	}()
	var target string
	RegisterEnum(&cobra.Command{Use: "x"}, &target, EnumSpec{
		Name:    "scope",
		Values:  []string{"file", "repo"},
		Default: "everything",
	})
}

func TestWithNoteAndWithUsageDoNotForkTheVocabulary(t *testing.T) {
	base := EnumSpec{Name: "app-type", Usage: "App type", Values: []string{"go-cli"}}
	derived := base.WithNote("validated against execution_profile").WithUsage("Pipeline app type")
	assertEqual(t, "values", strings.Join(derived.Values, "|"), strings.Join(base.Values, "|"))
	assertContains(t, derived.UsageString(), "Pipeline app type")
	assertContains(t, derived.UsageString(), "validated against execution_profile")
	assertEqual(t, "base note untouched", base.Note, "")
}

func TestSortedValuesDoesNotMutateInput(t *testing.T) {
	in := []string{"c", "a", "b"}
	out := SortedValues(in)
	assertEqual(t, "sorted", strings.Join(out, "|"), "a|b|c")
	assertEqual(t, "input untouched", strings.Join(in, "|"), "c|a|b")
}

func TestEnumValuesReturnsNilForNonEnumFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().String("plain", "", "not an enum")
	if got := EnumValues(cmd, "plain"); got != nil {
		t.Fatalf("want nil for a plain flag, got %v", got)
	}
	if got := EnumValues(cmd, "absent"); got != nil {
		t.Fatalf("want nil for a missing flag, got %v", got)
	}
}

func TestEnumDynamicSource(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	var staticValue, dynamicValue string
	RegisterEnum(cmd, &staticValue, EnumSpec{
		Name:   "status",
		Values: []string{"pending", "completed"},
	})
	RegisterEnum(cmd, &dynamicValue, EnumSpec{
		Name:        "app-type",
		DynamicFrom: "da workflow app-types",
	})

	assertEqual(t, "missing flag source", EnumDynamicSource(cmd, "missing"), "")
	assertEqual(t, "static enum source", EnumDynamicSource(cmd, "status"), "")
	assertEqual(t, "dynamic enum source", EnumDynamicSource(cmd, "app-type"), "da workflow app-types")
}

func TestAnnotateEnumIgnoresMissingFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	annotateEnum(cmd, EnumSpec{Name: "missing", Values: []string{"one", "two"}})
	if got := EnumValues(cmd, "missing"); got != nil {
		t.Fatalf("missing flag unexpectedly has enum values %v", got)
	}
}

func TestChainEnumValidationReturnsMissingFlagError(t *testing.T) {
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	chainEnumValidation(cmd, EnumSpec{Name: "missing", Values: []string{"one", "two"}})

	err := runCommand(cmd)
	if err == nil {
		t.Fatal("want missing flag error, got nil")
	}
	assertContains(t, err.Error(), "flag accessed but not defined: missing")
}

// ── helpers ───────────────────────────────────────────────────────────────────

func runCommand(cmd *cobra.Command, args ...string) error {
	cmd.SetArgs(args)
	cmd.SetOut(discard{})
	cmd.SetErr(discard{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func hintsOf(t *testing.T, spec EnumSpec, value string) string {
	t.Helper()
	return spec.suggestionHint(value)
}

func assertEqual(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q to contain %q", haystack, needle)
	}
}

func assertErrorPresence(t *testing.T, err error, want bool) {
	t.Helper()
	if want && err == nil {
		t.Fatal("want error, got nil")
	}
	if !want && err != nil {
		t.Fatalf("want no error, got %v", err)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
