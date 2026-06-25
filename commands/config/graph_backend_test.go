package config

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// ---------- resolveGraphBackend (registry ref resolver, t1) ----------

func TestResolveGraphBackend_NoneAdapter(t *testing.T) {
	cases := []string{
		"dotagents-builtin:graph/none@^1.0",
		"dotagents-builtin:graph/none@1.0.0",
		"dotagents-builtin:graph/none",
		"none",
	}
	for _, ref := range cases {
		t.Run(ref, func(t *testing.T) {
			adapter, err := resolveGraphBackend(ref)
			if err != nil {
				t.Fatalf("resolveGraphBackend(%q): %v", ref, err)
			}
			if adapter.Name() != "none" {
				t.Fatalf("adapter name = %q, want none", adapter.Name())
			}
		})
	}
}

func TestResolveGraphBackend_UnknownAdapter(t *testing.T) {
	if _, err := resolveGraphBackend("dotagents-builtin:graph/does-not-exist@^1.0"); err == nil {
		t.Fatal("expected error resolving an unregistered adapter")
	}
}

func TestResolveGraphBackend_VersionMismatch(t *testing.T) {
	// The none adapter ships at 1.0.0; a ^2.0 constraint cannot be satisfied.
	if _, err := resolveGraphBackend("dotagents-builtin:graph/none@^2.0"); err == nil {
		t.Fatal("expected version-constraint error")
	}
}

func TestResolveGraphBackend_BadRef(t *testing.T) {
	if _, err := resolveGraphBackend(""); err == nil {
		t.Fatal("expected error parsing an empty ref")
	}
}

// TestResolveGraphBackend_RegistrationFailure exercises the registration-failure
// branch of builtinGraphRegistry via the seam, a path production never hits.
func TestResolveGraphBackend_RegistrationFailure(t *testing.T) {
	orig := registerBuiltinGraphBackends
	t.Cleanup(func() { registerBuiltinGraphBackends = orig })
	registerBuiltinGraphBackends = func(*registry.Registry) error {
		return errors.New("boom")
	}
	if _, err := resolveGraphBackend("none"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected registration failure to propagate, got %v", err)
	}
}

// ---------- buildGraphFacet ----------

func TestBuildGraphFacet_Resolves(t *testing.T) {
	prof := cfg.AppTypeProfile{GraphBackend: "dotagents-builtin:graph/none@^1.0"}
	g := buildGraphFacet(prof)
	if !g.Resolved {
		t.Fatalf("expected resolved, got %+v", g)
	}
	if g.Adapter != "none" || g.Version != "1.0.0" {
		t.Fatalf("adapter/version = %q/%q, want none/1.0.0", g.Adapter, g.Version)
	}
	if g.Ref != "dotagents-builtin:graph/none@^1.0" {
		t.Fatalf("ref = %q", g.Ref)
	}
	if g.Error != "" {
		t.Fatalf("unexpected error %q", g.Error)
	}
}

func TestBuildGraphFacet_InheritDefault(t *testing.T) {
	// An empty graph_backend is the inherit-the-default case: resolved, no
	// adapter named, no error.
	g := buildGraphFacet(cfg.AppTypeProfile{})
	if !g.Resolved {
		t.Fatalf("empty ref should resolve to the default, got %+v", g)
	}
	if g.Ref != "" || g.Adapter != "" || g.Error != "" {
		t.Fatalf("expected empty fields for inherit-default, got %+v", g)
	}
}

func TestBuildGraphFacet_Unresolved(t *testing.T) {
	prof := cfg.AppTypeProfile{GraphBackend: "dotagents-builtin:graph/ghost@^1.0"}
	g := buildGraphFacet(prof)
	if g.Resolved {
		t.Fatalf("ghost adapter should not resolve, got %+v", g)
	}
	if g.Ref != "dotagents-builtin:graph/ghost@^1.0" {
		t.Fatalf("ref = %q", g.Ref)
	}
	if g.Error == "" {
		t.Fatal("expected an error reason for an unresolved ref")
	}
	if g.Adapter != "" || g.Version != "" {
		t.Fatalf("expected no adapter/version on failure, got %+v", g)
	}
}

// ---------- printGraphHuman ----------

func TestPrintGraphHuman(t *testing.T) {
	var buf bytes.Buffer
	printGraphHuman(&buf, &graphFacet{
		Ref:      "dotagents-builtin:graph/none@^1.0",
		Resolved: true,
		Adapter:  "none",
		Version:  "1.0.0",
	})
	out := buf.String()
	for _, want := range []string{"graph", "graph_backend", "none@1.0.0", "resolved      : true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("graph human output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintGraphHuman_InheritDefault(t *testing.T) {
	var buf bytes.Buffer
	printGraphHuman(&buf, &graphFacet{Resolved: true})
	if !strings.Contains(buf.String(), "(inherit default)") {
		t.Fatalf("expected inherit-default label, got:\n%s", buf.String())
	}
}

func TestPrintGraphHuman_Error(t *testing.T) {
	var buf bytes.Buffer
	printGraphHuman(&buf, &graphFacet{
		Ref:      "dotagents-builtin:graph/ghost@^1.0",
		Resolved: false,
		Error:    "registry: no adapter named \"ghost\" registered",
	})
	out := buf.String()
	if !strings.Contains(out, "error") || !strings.Contains(out, "ghost") {
		t.Fatalf("expected error line, got:\n%s", out)
	}
}
