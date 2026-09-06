package cmdutil

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Enum flags: one declaration drives help, completion, and validation.
//
// A "closed-set" flag is one whose value must come from a finite vocabulary
// (`--status pass|fail|partial|unknown`). Historically each such flag in this
// CLI declared its vocabulary twice — once as prose inside the usage string and
// once as a literal list inside the runner's validation — which let the two
// drift apart. Drift is invisible to humans and actively harmful to AI agents,
// which read `--help` as the source of truth and cannot guess a value that the
// help text omits.
//
// EnumSpec collapses those into a single declaration. Register the flag with
// RegisterEnum and the same []string produces:
//
//  1. the "(one of: a|b|c)" listing appended to the `--help` usage string,
//  2. the shell-completion candidates,
//  3. the PreRunE validation error, which names the allowed values.
//
// See docs/CLI_HELP_CONVENTIONS.md for the authoring rules.

// NewUsageError builds the error returned when a flag value falls outside its
// declared set. Package commands overrides it so enum failures render through
// the shared CLIError path (docs/ERROR_MESSAGE_CONTRACT.md); the default keeps
// this package free of an import cycle back into commands.
var NewUsageError = func(message string, hints ...string) error {
	return fmt.Errorf("%s", message)
}

// enumAnnotation marks a registered flag as closed-set. The value is the
// canonical-order vocabulary joined by "|", so tooling (and the drift test in
// enumflag_test.go) can recover the declared set from a live command tree
// without re-parsing the usage prose.
const enumAnnotation = "da_enum_values"

// enumDynamicAnnotation records the command that prints the live vocabulary for
// a config-derived enum (one whose set is not compiled in).
const enumDynamicAnnotation = "da_enum_dynamic_source"

// EnumSpec declares a flag whose value must come from a closed set.
//
// Values holds the compiled-in vocabulary in canonical (documented) order —
// not sorted, because the documented order usually encodes meaning, such as a
// state-machine progression. Leave Values empty and set DynamicFrom when the
// vocabulary is config-derived and can only be enumerated at runtime; help then
// points the reader at the command that prints the live set instead of listing
// values the binary cannot know.
type EnumSpec struct {
	// Name is the flag name without leading dashes.
	Name string
	// Usage describes the flag WITHOUT the value listing; RegisterEnum appends
	// the listing so the two can never disagree.
	Usage string
	// Values is the closed vocabulary in canonical order.
	Values []string
	// Default is the flag's default value. It must be a member of Values (or
	// empty), which RegisterEnum enforces at construction time.
	Default string
	// Required appends "(required)" to the usage string. It does NOT enforce
	// presence — commands keep using MarkFlagRequired or their own runner-level
	// required checks, which produce better-targeted messages.
	Required bool
	// DynamicFrom names the command that prints the live vocabulary when the
	// set is config-derived, for example "da workflow app-types".
	DynamicFrom string
	// Note is an extra clause appended after the value listing, for semantics
	// that the vocabulary alone does not convey.
	Note string
}

// UsageString renders the help text for the flag: the description, the value
// listing (or the dynamic-source pointer), the required marker, and any note.
//
// The result carries no backticks. Cobra reads the first backtick-quoted word
// in a usage string as the flag's value-type name, so a backtick anywhere in
// here would replace the "string" in `--kind string` with whatever it wrapped —
// a silent, wrong rendering. Stripping them centrally means no author of an
// EnumSpec has to know that rule.
func (s EnumSpec) UsageString() string {
	parts := []string{strings.TrimSpace(s.Usage)}
	switch {
	case len(s.Values) > 0:
		parts = append(parts, "(one of: "+s.ValueList()+")")
	case s.DynamicFrom != "":
		parts = append(parts, "(values come from: "+s.DynamicFrom+")")
	}
	if s.Required {
		parts = append(parts, "(required)")
	}
	out := strings.Join(nonEmpty(parts), " ")
	if note := strings.TrimSpace(s.Note); note != "" {
		out += "; " + note
	}
	return strings.ReplaceAll(out, "`", "")
}

// WithNote returns a copy of the spec carrying a different trailing note, for
// the case where one vocabulary is shared by several commands that need to say
// something different about it. The vocabulary itself stays single-sourced.
func (s EnumSpec) WithNote(note string) EnumSpec {
	s.Note = note
	return s
}

// WithUsage returns a copy of the spec carrying a different description, again
// without forking the vocabulary.
func (s EnumSpec) WithUsage(usage string) EnumSpec {
	s.Usage = usage
	return s
}

// ValueList renders the vocabulary as a pipe-joined list, the form used in both
// help text and validation errors.
func (s EnumSpec) ValueList() string {
	return strings.Join(s.Values, "|")
}

// Contains reports whether value (after trimming surrounding space) is a member
// of the declared vocabulary. A spec with no compiled-in Values accepts
// anything, because only the runtime source can adjudicate membership.
func (s EnumSpec) Contains(value string) bool {
	if len(s.Values) == 0 {
		return true
	}
	trimmed := strings.TrimSpace(value)
	for _, v := range s.Values {
		if v == trimmed {
			return true
		}
	}
	return false
}

// Validate returns a hinted usage error when value is outside the declared set.
//
// An empty value is always accepted: absence is a different failure from a
// wrong value, and commands report it through MarkFlagRequired or their own
// runner checks, which can say more about why the flag is needed.
func (s EnumSpec) Validate(value string) error {
	if strings.TrimSpace(value) == "" || s.Contains(value) {
		return nil
	}
	return NewUsageError(
		fmt.Sprintf("--%s must be one of %s (got %q)", s.Name, s.ValueList(), value),
		s.suggestionHint(value),
	)
}

// suggestionHint offers the closest declared value when the input looks like a
// typo, and otherwise repeats the vocabulary as the recovery step.
func (s EnumSpec) suggestionHint(value string) string {
	if near := s.nearest(value); near != "" {
		return fmt.Sprintf("Did you mean `--%s %s`?", s.Name, near)
	}
	return fmt.Sprintf("Pass --%s with one of: %s.", s.Name, s.ValueList())
}

// nearest returns the declared value that differs from input only by case,
// surrounding space, or a hyphen/underscore swap — the three near-misses an
// agent composing a flag from prose actually makes. Anything further away
// returns "" so the caller falls back to listing the vocabulary.
func (s EnumSpec) nearest(input string) string {
	norm := func(v string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(v)), "-", "_")
	}
	target := norm(input)
	for _, v := range s.Values {
		if norm(v) == target {
			return v
		}
	}
	return ""
}

// RegisterEnum declares a closed-set string flag on cmd, binding it to target.
//
// It registers the flag with the generated usage string, wires shell completion
// to the same vocabulary, and chains a PreRunE that validates the parsed value.
// Because all three read the one Values slice, help can never advertise a value
// that validation rejects, nor omit one it accepts.
//
// RegisterEnum panics when Default is not a member of Values: that is a
// programming error in the command definition, catchable by any test that
// builds the command tree.
func RegisterEnum(cmd *cobra.Command, target *string, spec EnumSpec) {
	assertEnumDefault(spec)
	cmd.Flags().StringVar(target, spec.Name, spec.Default, spec.UsageString())
	finishEnumRegistration(cmd, spec)
}

// RegisterEnumFlag is RegisterEnum for commands that read their flags back out
// of the command (cmd.Flags().GetString) instead of binding them to a variable.
// The vocabulary, help, completion, and validation behave identically.
func RegisterEnumFlag(cmd *cobra.Command, spec EnumSpec) {
	assertEnumDefault(spec)
	cmd.Flags().String(spec.Name, spec.Default, spec.UsageString())
	finishEnumRegistration(cmd, spec)
}

// assertEnumDefault rejects a default outside the declared set. That is a bug
// in the command definition, not a user error, so it fails loudly at the point
// any test builds the command tree.
func assertEnumDefault(spec EnumSpec) {
	if spec.Default != "" && !spec.Contains(spec.Default) {
		panic(fmt.Sprintf("cmdutil: --%s default %q is not in its declared value set %s", spec.Name, spec.Default, spec.ValueList()))
	}
}

// finishEnumRegistration applies the three consumers of the vocabulary that are
// identical for bound and unbound flags.
func finishEnumRegistration(cmd *cobra.Command, spec EnumSpec) {
	annotateEnum(cmd, spec)
	registerEnumCompletion(cmd, spec)
	chainEnumValidation(cmd, spec)
}

// annotateEnum stamps the declared vocabulary onto the pflag so the command
// tree stays self-describing for tooling and drift tests.
func annotateEnum(cmd *cobra.Command, spec EnumSpec) {
	flag := cmd.Flags().Lookup(spec.Name)
	if flag == nil {
		return
	}
	if flag.Annotations == nil {
		flag.Annotations = map[string][]string{}
	}
	if len(spec.Values) > 0 {
		flag.Annotations[enumAnnotation] = spec.Values
	}
	if spec.DynamicFrom != "" {
		flag.Annotations[enumDynamicAnnotation] = []string{spec.DynamicFrom}
	}
}

// registerEnumCompletion offers the vocabulary as shell-completion candidates.
// A config-derived enum has nothing to offer, so it is left alone rather than
// completing to an empty list.
func registerEnumCompletion(cmd *cobra.Command, spec EnumSpec) {
	if len(spec.Values) == 0 {
		return
	}
	values := append([]string(nil), spec.Values...)
	_ = cmd.RegisterFlagCompletionFunc(spec.Name, func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	})
}

// chainEnumValidation appends spec's check to cmd's PreRunE, preserving any
// PreRunE the command already had. Validating here rather than inside the
// runner means a command gets the check for free the moment it declares the
// flag, and keeps the error identical across every command using the helper.
//
// The value is read back off the flag rather than through the bound variable so
// bound and unbound registrations share one code path.
func chainEnumValidation(cmd *cobra.Command, spec EnumSpec) {
	prev := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		value, err := c.Flags().GetString(spec.Name)
		if err != nil {
			return err
		}
		if err := spec.Validate(value); err != nil {
			return err
		}
		if prev != nil {
			return prev(c, args)
		}
		return nil
	}
}

// EnumValues recovers the vocabulary declared for a flag on cmd, or nil when
// the flag is absent or was not registered as an enum.
func EnumValues(cmd *cobra.Command, flagName string) []string {
	flag := cmd.Flags().Lookup(flagName)
	if flag == nil || flag.Annotations == nil {
		return nil
	}
	return flag.Annotations[enumAnnotation]
}

// EnumDynamicSource returns the command a config-derived flag points at for its
// live vocabulary, or "" when the flag is absent or has a compiled-in set.
func EnumDynamicSource(cmd *cobra.Command, flagName string) string {
	flag := cmd.Flags().Lookup(flagName)
	if flag == nil || flag.Annotations == nil {
		return ""
	}
	src := flag.Annotations[enumDynamicAnnotation]
	if len(src) == 0 {
		return ""
	}
	return src[0]
}

// SortedValues returns a copy of values in lexical order. Use it for
// vocabularies whose members carry no inherent ordering (query intents, note
// types) so the help listing stays stable as members are added.
func SortedValues(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// nonEmpty drops blank entries so UsageString never emits doubled spaces when a
// spec omits its description.
func nonEmpty(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}
