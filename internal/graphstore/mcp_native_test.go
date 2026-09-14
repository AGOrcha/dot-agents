package graphstore

import (
	"path/filepath"
	"testing"
)

// The kg-native backend must never share a database file with the retained
// bridge: a rollback that read a graph the other backend wrote would mix two
// different node-identity spaces.
func TestNativeGraphDBPathIsRepoLocal(t *testing.T) {
	root := t.TempDir()
	got := NativeGraphDBPath(root)
	want := filepath.Join(root, ".dot-agents", "code-graph.db")
	if got != want {
		t.Errorf("NativeGraphDBPath = %q, want %q", got, want)
	}
	if got == CRGDBPath(root) {
		t.Error("the native backend and the bridge resolve to the same database file")
	}
}

// A provider that implements NativeToolSet is wired as the in-process tool
// implementation; one that does not leaves every tool on the bridge. That
// assertion is what makes the backend choice a provider swap rather than a
// server rewrite.
func TestNewMCPServerWithProviderWiresNativeToolSet(t *testing.T) {
	t.Run("plain provider has no native tool set", func(t *testing.T) {
		// A CodeGraphProvider that is not also a NativeToolSet must leave
		// every tool on the bridge rather than being wired in as one.
		provider := &CRGBridge{RepoRoot: t.TempDir()}
		srv := NewMCPServerWithProvider(t.TempDir(), provider, nil)
		if srv.native != nil {
			t.Error("a provider that is not a NativeToolSet was wired as one")
		}
		if srv.provider == nil {
			t.Error("provider not retained")
		}
	})

	t.Run("provider error is retained for capability reporting", func(t *testing.T) {
		srv := NewMCPServerWithProvider(t.TempDir(), nil, errBackendProbe)
		if srv.providerErr == nil {
			t.Error("provider error not retained")
		}
	})
}
