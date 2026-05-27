package canonical

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

// acceptAll is a permissive PositionalArgs validator used to assert
// pre-bound Args propagate from spec → assembled cobra.Command.
func acceptAll(_ *cobra.Command, _ []string) error { return nil }

// rejectAll always errors; used to assert spec.Args is wired by reference,
// not silently replaced with cobra defaults.
func rejectAll(_ *cobra.Command, _ []string) error { return errors.New("nope") }

// sampleSpec constructs a fully-populated ResourceCmdSpec the tests
// reuse so changes to the spec shape light up every assertion.
func sampleSpec() ResourceCmdSpec {
	return ResourceCmdSpec{
		Use:     "demo",
		Short:   "demo short",
		Long:    "demo long body referencing ~/.agents/demo/",
		Example: "  da demo list\n  da demo show global x",
		List: SubCmdSpec{
			Use:     "list [scope]",
			Short:   "list demo files",
			Example: "  da demo list",
			Args:    acceptAll,
			RunE:    func(_ *cobra.Command, _ []string) error { return nil },
		},
		Show: SubCmdSpec{
			Use:   "show <scope> <name>",
			Short: "show one demo file",
			Args:  acceptAll,
			RunE:  func(_ *cobra.Command, _ []string) error { return errors.New("show ran") },
		},
		Remove: SubCmdSpec{
			Use:   "remove <scope> <name>",
			Short: "remove one demo file",
			Long:  "removes from canonical storage only",
			Args:  rejectAll,
			RunE:  func(_ *cobra.Command, _ []string) error { return nil },
		},
	}
}

// TestNewCanonicalResourceCmd_ParentMetadata asserts the parent cobra
// command carries Use/Short/Long/Example verbatim from spec — these are
// the strings duplication-detection previously flagged.
func TestNewCanonicalResourceCmd_ParentMetadata(t *testing.T) {
	spec := sampleSpec()
	cmd := NewCanonicalResourceCmd(spec)

	if cmd.Use != spec.Use {
		t.Errorf("Use = %q, want %q", cmd.Use, spec.Use)
	}
	if cmd.Short != spec.Short {
		t.Errorf("Short = %q, want %q", cmd.Short, spec.Short)
	}
	if cmd.Long != spec.Long {
		t.Errorf("Long = %q, want %q", cmd.Long, spec.Long)
	}
	if cmd.Example != spec.Example {
		t.Errorf("Example = %q, want %q", cmd.Example, spec.Example)
	}
}

// TestNewCanonicalResourceCmd_SubcommandsAttached asserts list/show/remove
// are all present as children of the parent — mirrors the metadata test
// each leaf package keeps as its anchor.
func TestNewCanonicalResourceCmd_SubcommandsAttached(t *testing.T) {
	cmd := NewCanonicalResourceCmd(sampleSpec())
	want := map[string]bool{"list": false, "show": false, "remove": false}
	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

// TestNewCanonicalResourceCmd_SubcommandMetadata asserts every per-leaf
// string (Use/Short/Long/Example) round-trips from SubCmdSpec into the
// assembled cobra.Command.
func TestNewCanonicalResourceCmd_SubcommandMetadata(t *testing.T) {
	spec := sampleSpec()
	cmd := NewCanonicalResourceCmd(spec)
	byName := map[string]*cobra.Command{}
	for _, sub := range cmd.Commands() {
		byName[sub.Name()] = sub
	}

	cases := []struct {
		name string
		sub  SubCmdSpec
	}{
		{"list", spec.List},
		{"show", spec.Show},
		{"remove", spec.Remove},
	}
	for _, tc := range cases {
		got := byName[tc.name]
		if got == nil {
			t.Fatalf("subcommand %q missing", tc.name)
		}
		if got.Use != tc.sub.Use {
			t.Errorf("%s Use = %q, want %q", tc.name, got.Use, tc.sub.Use)
		}
		if got.Short != tc.sub.Short {
			t.Errorf("%s Short = %q, want %q", tc.name, got.Short, tc.sub.Short)
		}
		if got.Long != tc.sub.Long {
			t.Errorf("%s Long = %q, want %q", tc.name, got.Long, tc.sub.Long)
		}
		if got.Example != tc.sub.Example {
			t.Errorf("%s Example = %q, want %q", tc.name, got.Example, tc.sub.Example)
		}
	}
}

// TestNewCanonicalResourceCmd_ArgsPropagate asserts the spec.Args
// PositionalArgs values are the ones the cobra.Command runs — including
// a rejecting validator on remove, proving the helper does not swap in
// cobra defaults.
func TestNewCanonicalResourceCmd_ArgsPropagate(t *testing.T) {
	cmd := NewCanonicalResourceCmd(sampleSpec())
	var list, show, remove *cobra.Command
	for _, sub := range cmd.Commands() {
		switch sub.Name() {
		case "list":
			list = sub
		case "show":
			show = sub
		case "remove":
			remove = sub
		}
	}

	if list.Args == nil || show.Args == nil || remove.Args == nil {
		t.Fatalf("expected non-nil Args on every subcommand")
	}
	if err := list.Args(list, []string{"anything"}); err != nil {
		t.Errorf("list Args(acceptAll) should pass: %v", err)
	}
	if err := show.Args(show, []string{"a", "b", "c"}); err != nil {
		t.Errorf("show Args(acceptAll) should pass: %v", err)
	}
	if err := remove.Args(remove, []string{"scope", "name"}); err == nil {
		t.Error("remove Args(rejectAll) should reject")
	}
}

// TestNewCanonicalResourceCmd_RunEPropagates asserts RunE closures are
// preserved by invoking show — its closure returns a sentinel error.
func TestNewCanonicalResourceCmd_RunEPropagates(t *testing.T) {
	cmd := NewCanonicalResourceCmd(sampleSpec())
	for _, sub := range cmd.Commands() {
		if sub.RunE == nil {
			t.Errorf("subcommand %q: RunE nil", sub.Name())
		}
		if sub.Name() == "show" {
			err := sub.RunE(sub, []string{"a", "b"})
			if err == nil || err.Error() != "show ran" {
				t.Errorf("show RunE should propagate sentinel, got %v", err)
			}
		}
	}
}

// TestNewCanonicalResourceCmd_EmptyOptionalFields asserts Long and
// Example default to empty strings (cobra-native behavior) when the
// spec leaves them unset — important so leaves with no Long don't get
// surprise non-empty cobra defaults.
func TestNewCanonicalResourceCmd_EmptyOptionalFields(t *testing.T) {
	cmd := NewCanonicalResourceCmd(ResourceCmdSpec{
		Use:    "minimal",
		Short:  "min",
		List:   SubCmdSpec{Use: "list", Short: "l", Args: acceptAll, RunE: func(*cobra.Command, []string) error { return nil }},
		Show:   SubCmdSpec{Use: "show", Short: "s", Args: acceptAll, RunE: func(*cobra.Command, []string) error { return nil }},
		Remove: SubCmdSpec{Use: "remove", Short: "r", Args: acceptAll, RunE: func(*cobra.Command, []string) error { return nil }},
	})
	if cmd.Long != "" {
		t.Errorf("Long should be empty, got %q", cmd.Long)
	}
	if cmd.Example != "" {
		t.Errorf("Example should be empty, got %q", cmd.Example)
	}
	for _, sub := range cmd.Commands() {
		if sub.Long != "" {
			t.Errorf("subcommand %q Long should be empty, got %q", sub.Name(), sub.Long)
		}
		if sub.Example != "" {
			t.Errorf("subcommand %q Example should be empty, got %q", sub.Name(), sub.Example)
		}
	}
}

// TestNewSubCmd_DirectPassthrough exercises NewSubCmd directly so
// coverage hits each field assignment line without relying on
// NewCanonicalResourceCmd as the only entry point. Leaf packages also
// call NewSubCmd from their per-verb constructors.
func TestNewSubCmd_DirectPassthrough(t *testing.T) {
	in := SubCmdSpec{
		Use:     "u",
		Short:   "s",
		Long:    "l",
		Example: "e",
		Args:    rejectAll,
		RunE:    func(*cobra.Command, []string) error { return errors.New("ran") },
	}
	got := NewSubCmd(in)
	if got.Use != "u" || got.Short != "s" || got.Long != "l" || got.Example != "e" {
		t.Errorf("string fields not propagated: %+v", got)
	}
	if got.Args == nil || got.RunE == nil {
		t.Fatalf("function fields not propagated")
	}
	if err := got.Args(got, nil); err == nil {
		t.Error("Args(rejectAll) should error")
	}
	if err := got.RunE(got, nil); err == nil || err.Error() != "ran" {
		t.Errorf("RunE sentinel lost: %v", err)
	}
}
