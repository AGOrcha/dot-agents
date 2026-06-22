package config

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

// TestRunContextBind_GetwdError covers bind's working-directory resolution
// failure branch: when cwd is unset and getwd returns an error, bind surfaces a
// hinted error rather than proceeding with an empty cwd.
func TestRunContextBind_GetwdError(t *testing.T) {
	orig := getwd
	t.Cleanup(func() { getwd = orig })
	getwd = func() (string, error) { return "", errors.New("no cwd") }

	rc := &runContext{}
	err := rc.bind(&cobra.Command{}, testDeps())
	if err == nil {
		t.Fatal("expected bind to error when getwd fails")
	}
}

// TestRunContextBind_ResolvesCwdAndStreams covers bind's success path: it fills
// stdout/stderr/jsonOut from the command + Deps and resolves cwd via getwd when
// not preset.
func TestRunContextBind_ResolvesCwdAndStreams(t *testing.T) {
	orig := getwd
	t.Cleanup(func() { getwd = orig })
	getwd = func() (string, error) { return "/tmp/resolved", nil }

	rc := &runContext{}
	if err := rc.bind(&cobra.Command{}, testDeps()); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if rc.cwd != "/tmp/resolved" {
		t.Errorf("cwd = %q, want /tmp/resolved", rc.cwd)
	}
	if rc.stdout == nil || rc.stderr == nil {
		t.Error("stdout/stderr not bound from the command streams")
	}
}

// TestRunContextBind_PresetCwdSkipsGetwd confirms a preset cwd (as tests inject)
// is honored without calling getwd.
func TestRunContextBind_PresetCwdSkipsGetwd(t *testing.T) {
	orig := getwd
	t.Cleanup(func() { getwd = orig })
	getwd = func() (string, error) { return "", errors.New("must not be called") }

	rc := &runContext{cwd: "/preset"}
	if err := rc.bind(&cobra.Command{}, testDeps()); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if rc.cwd != "/preset" {
		t.Errorf("cwd = %q, want /preset (getwd should be skipped)", rc.cwd)
	}
}
