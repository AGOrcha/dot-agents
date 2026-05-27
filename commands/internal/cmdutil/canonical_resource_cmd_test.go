package cmdutil

import (
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
)

// acceptAll is a permissive PositionalArgs validator used to assert
// pre-bound Args propagate from spec → assembled cobra.Command.
func acceptAll(_ *cobra.Command, _ []string) error { return nil }

// rejectAll always errors; used to assert spec ListArgs/ShowArgs/
// RemoveArgs flow through unchanged (not replaced by cobra defaults).
func rejectAll(_ *cobra.Command, _ []string) error { return errors.New("nope") }

// runRecorder captures closure invocations so tests can assert the
// spec's runners are the ones the cobra RunE actually invokes (not
// silently dropped or swapped).
type runRecorder struct {
	listScopes  []string
	showCalls   [][2]string
	removeCalls [][2]string
}

func (r *runRecorder) list(scope string) error {
	r.listScopes = append(r.listScopes, scope)
	return nil
}

func (r *runRecorder) show(scope, name string) error {
	r.showCalls = append(r.showCalls, [2]string{scope, name})
	return fmt.Errorf("show:%s/%s", scope, name)
}

func (r *runRecorder) remove(scope, name string) error {
	r.removeCalls = append(r.removeCalls, [2]string{scope, name})
	return nil
}

// sampleSpec constructs a fully-populated CanonicalFileSpec the tests
// reuse so changes to the spec shape light up every assertion. Only
// the CLI-surface fields are populated — the data-layer callbacks
// (List/Resolve/EnsureScope) are exercised by the in-package
// RunCanonical* tests in canonfile_test.go.
func sampleSpec(r *runRecorder) CanonicalFileSpec {
	return CanonicalFileSpec{
		Use:     "demo",
		Short:   "demo short",
		Long:    "demo long body referencing ~/.agents/demo/",
		Example: "  da demo list\n  da demo show global x",
		ListSub: SubCmdStrings{
			Use:     "list [scope]",
			Short:   "list demo files",
			Example: "  da demo list",
		},
		ListArgs: acceptAll,
		ListRun:  r.list,
		ShowSub: SubCmdStrings{
			Use:   "show <scope> <name>",
			Short: "show one demo file",
		},
		ShowArgs: acceptAll,
		ShowRun:  r.show,
		RemoveSub: SubCmdStrings{
			Use:   "remove <scope> <name>",
			Short: "remove one demo file",
			Long:  "removes from canonical storage only",
		},
		RemoveArgs: rejectAll,
		RemoveRun:  r.remove,
	}
}

// TestNewCanonicalResourceCmd_ParentMetadata asserts the parent cobra
// command carries Use/Short/Long/Example verbatim from spec — these are
// the strings duplication-detection previously flagged.
func TestNewCanonicalResourceCmd_ParentMetadata(t *testing.T) {
	spec := sampleSpec(&runRecorder{})
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
	cmd := NewCanonicalResourceCmd(sampleSpec(&runRecorder{}))
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
// string (Use/Short/Long/Example) round-trips from SubCmdStrings into
// the assembled cobra.Command.
func TestNewCanonicalResourceCmd_SubcommandMetadata(t *testing.T) {
	spec := sampleSpec(&runRecorder{})
	cmd := NewCanonicalResourceCmd(spec)
	byName := map[string]*cobra.Command{}
	for _, sub := range cmd.Commands() {
		byName[sub.Name()] = sub
	}

	cases := []struct {
		name string
		sub  SubCmdStrings
	}{
		{"list", spec.ListSub},
		{"show", spec.ShowSub},
		{"remove", spec.RemoveSub},
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

// TestNewCanonicalResourceCmd_ArgsPropagate asserts the spec's per-verb
// Args validators are the ones the cobra.Command runs — including a
// rejecting validator on remove, proving the helper does not swap in
// cobra defaults.
func TestNewCanonicalResourceCmd_ArgsPropagate(t *testing.T) {
	cmd := NewCanonicalResourceCmd(sampleSpec(&runRecorder{}))
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

// TestListRunE_DefaultsScopeToGlobal asserts the scope-defaulting
// behavior previously open-coded in each leaf is owned by cmdutil:
// zero args invokes ListRun("global"); a single arg passes it through
// verbatim.
func TestListRunE_DefaultsScopeToGlobal(t *testing.T) {
	r := &runRecorder{}
	cmd := NewCanonicalListCmd(sampleSpec(r))
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("list RunE(nil args) err: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"billing"}); err != nil {
		t.Errorf("list RunE(['billing']) err: %v", err)
	}
	want := []string{"global", "billing"}
	if len(r.listScopes) != len(want) {
		t.Fatalf("listScopes len=%d want %d", len(r.listScopes), len(want))
	}
	for i, s := range want {
		if r.listScopes[i] != s {
			t.Errorf("listScopes[%d]=%q want %q", i, r.listScopes[i], s)
		}
	}
}

// TestShowRunE_UnpacksTwoArgs asserts the 2-arg unpack pattern lives in
// cmdutil: RunE forwards args[0],args[1] to ShowRun and propagates the
// error verbatim.
func TestShowRunE_UnpacksTwoArgs(t *testing.T) {
	r := &runRecorder{}
	cmd := NewCanonicalShowCmd(sampleSpec(r))
	err := cmd.RunE(cmd, []string{"scope-a", "name-b"})
	if err == nil || err.Error() != "show:scope-a/name-b" {
		t.Errorf("show sentinel lost: %v", err)
	}
	if len(r.showCalls) != 1 || r.showCalls[0] != [2]string{"scope-a", "name-b"} {
		t.Errorf("showCalls = %v", r.showCalls)
	}
}

// TestRemoveRunE_UnpacksTwoArgs covers the symmetric 2-arg unpack on
// remove (no error return path so the test asserts arg-forwarding only).
func TestRemoveRunE_UnpacksTwoArgs(t *testing.T) {
	r := &runRecorder{}
	cmd := NewCanonicalRemoveCmd(sampleSpec(r))
	if err := cmd.RunE(cmd, []string{"s", "n"}); err != nil {
		t.Errorf("remove RunE err: %v", err)
	}
	if len(r.removeCalls) != 1 || r.removeCalls[0] != [2]string{"s", "n"} {
		t.Errorf("removeCalls = %v", r.removeCalls)
	}
}

// TestNewCanonicalResourceCmd_EmptyOptionalFields asserts Long and
// Example default to empty strings (cobra-native behavior) when the
// spec leaves them unset — important so leaves with no Long don't get
// surprise non-empty cobra defaults.
func TestNewCanonicalResourceCmd_EmptyOptionalFields(t *testing.T) {
	r := &runRecorder{}
	cmd := NewCanonicalResourceCmd(CanonicalFileSpec{
		Use:        "minimal",
		Short:      "min",
		ListSub:    SubCmdStrings{Use: "list", Short: "l"},
		ListArgs:   acceptAll,
		ListRun:    r.list,
		ShowSub:    SubCmdStrings{Use: "show", Short: "s"},
		ShowArgs:   acceptAll,
		ShowRun:    r.show,
		RemoveSub:  SubCmdStrings{Use: "remove", Short: "r"},
		RemoveArgs: acceptAll,
		RemoveRun:  r.remove,
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

// TestNewCanonicalListCmd_DirectStandalone asserts the exported per-verb
// constructor builds the leaf without requiring NewCanonicalResourceCmd
// — leaf packages use this for their NewListCmd shims.
func TestNewCanonicalListCmd_DirectStandalone(t *testing.T) {
	r := &runRecorder{}
	cmd := NewCanonicalListCmd(sampleSpec(r))
	if cmd.Use != "list [scope]" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.RunE == nil || cmd.Args == nil {
		t.Fatal("RunE/Args nil")
	}
}
