package platform

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// noopPlatform is the P6 "proof of abstraction" stub. It implements the full
// Platform interface plus every diagnostics sister interface, doing nothing
// observable beyond honoring the contract shapes. Its existence proves the
// central claim of the platform-driven-diagnostics plan: adding a platform is
// a single internal/platform/<name>.go change that satisfies the core
// Platform contract AND opts into doctor/status reporting purely by
// implementing the sister interfaces — no edits to doctor.go or status.go
// required for the new platform to be driven cleanly through the abstraction.
//
// noopPlatform is intentionally NOT registered in All(): the proof is that the
// uniform contract assertions used for real platforms apply unchanged to a
// brand-new implementor, and that the doctor/status sister-interface dispatch
// type-asserts onto it successfully.
type noopPlatform struct{}

// --- core Platform interface ---

func (noopPlatform) ID() string          { return "noop" }
func (noopPlatform) DisplayName() string { return "Noop Platform" }
func (noopPlatform) IsInstalled() bool   { return false }
func (noopPlatform) Version() string     { return "" }

func (noopPlatform) CreateLinks(string, string) error { return nil }
func (noopPlatform) RemoveLinks(string, string) error { return nil }

func (noopPlatform) HasDeprecatedFormat(string) bool { return false }
func (noopPlatform) DeprecatedDetails(string) string { return "" }

func (noopPlatform) SharedTargetIntents(string) ([]ResourceIntent, error) {
	// A pure stub owns no shared targets, so it returns an empty intent set.
	// The contract test treats nil/empty as valid (it only validates the
	// shape of any intents that ARE returned).
	return nil, nil
}

// --- diagnostics sister interfaces ---

func (noopPlatform) BrokenLinks(string, string, string) []BrokenLink { return nil }
func (noopPlatform) CountLinks(string, string, string) (int, int)    { return 0, 0 }
func (noopPlatform) Badge(string, string, string) PlatformBadge {
	return PlatformBadge{Name: "Noop Platform"}
}
func (noopPlatform) UserBrokenLinks(string) []BrokenLink { return nil }
func (noopPlatform) UserBadge(string) PlatformBadge      { return PlatformBadge{Name: "Noop Platform"} }
func (noopPlatform) OrphanCanonicals(string, string, string, string) []OrphanCanonical {
	return nil
}
func (noopPlatform) PrintAudit(io.Writer, string, string, string) {}

// Compile-time proof that the single noopPlatform value satisfies the core
// Platform interface and every diagnostics sister interface simultaneously.
// If any interface method signature drifts, this block fails to build.
var (
	_ Platform                = noopPlatform{}
	_ BrokenLinkReporter      = noopPlatform{}
	_ LinkCounter             = noopPlatform{}
	_ StatusBadger            = noopPlatform{}
	_ UserConfigReporter      = noopPlatform{}
	_ OrphanCanonicalReporter = noopPlatform{}
	_ AuditPrinter            = noopPlatform{}
)

// TestNoopPlatform_HonorsPlatformContract proves a brand-new Platform
// implementor passes the exact same identity / deprecation / intents /
// RemoveLinks contract that every real platform in All() is held to — without
// the contract helpers knowing anything about noopPlatform. This is the
// "single-file change" proof: the new platform is contract-clean on arrival.
func TestNoopPlatform_HonorsPlatformContract(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))

	seenIDs := map[string]bool{}
	seenNames := map[string]bool{}
	assertPlatformContract(t, noopPlatform{}, tmp, seenIDs, seenNames)
}

// TestNoopPlatform_DrivesDoctorDispatch mirrors the type-assert dispatch
// doctor performs over a platform: BrokenLinks, CountLinks, OrphanCanonicals.
// It proves the stub is reachable through the doctor-side sister interfaces
// when consumed as a bare Platform value (exactly how doctor iterates All()).
func TestNoopPlatform_DrivesDoctorDispatch(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")

	var p Platform = noopPlatform{}

	r, ok := p.(BrokenLinkReporter)
	if !ok {
		t.Fatal("noopPlatform must satisfy BrokenLinkReporter via Platform value")
	}
	if got := r.BrokenLinks("proj", repo, agentsHome); got != nil {
		t.Errorf("BrokenLinks = %+v, want nil", got)
	}

	c, ok := p.(LinkCounter)
	if !ok {
		t.Fatal("noopPlatform must satisfy LinkCounter via Platform value")
	}
	if okN, brokenN := c.CountLinks("proj", repo, agentsHome); okN != 0 || brokenN != 0 {
		t.Errorf("CountLinks = (%d,%d), want (0,0)", okN, brokenN)
	}

	o, ok := p.(OrphanCanonicalReporter)
	if !ok {
		t.Fatal("noopPlatform must satisfy OrphanCanonicalReporter via Platform value")
	}
	if got := o.OrphanCanonicals("proj", repo, agentsHome, "skills"); got != nil {
		t.Errorf("OrphanCanonicals = %+v, want nil", got)
	}
}

// TestNoopPlatform_DrivesStatusDispatch mirrors the type-assert dispatch
// status performs: Badge (project) and UserBadge (user-config). It proves the
// stub surfaces in status output automatically once it implements the badger
// interfaces, with no edits to status.go.
func TestNoopPlatform_DrivesStatusDispatch(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	home := filepath.Join(tmp, "home")
	agentsHome := filepath.Join(tmp, ".agents")

	var p Platform = noopPlatform{}

	b, ok := p.(StatusBadger)
	if !ok {
		t.Fatal("noopPlatform must satisfy StatusBadger via Platform value")
	}
	badge := b.Badge("proj", repo, agentsHome)
	if badge.Name != "Noop Platform" {
		t.Errorf("Badge().Name = %q, want %q", badge.Name, "Noop Platform")
	}
	if badge.Present || badge.Broken {
		t.Errorf("stub badge should report no managed state, got %+v", badge)
	}

	uc, ok := p.(UserConfigReporter)
	if !ok {
		t.Fatal("noopPlatform must satisfy UserConfigReporter via Platform value")
	}
	if ub := uc.UserBadge(home); ub.Name != "Noop Platform" {
		t.Errorf("UserBadge().Name = %q, want %q", ub.Name, "Noop Platform")
	}
	if links := uc.UserBrokenLinks(home); links != nil {
		t.Errorf("UserBrokenLinks = %+v, want nil", links)
	}
}

// TestNoopPlatform_AuditPrinterWritesNothing proves the audit sister
// interface is reachable and bounded: the stub writes nothing, so the audit
// loop in status remains a no-op for a platform with no managed state.
func TestNoopPlatform_AuditPrinterWritesNothing(t *testing.T) {
	var p Platform = noopPlatform{}
	ap, ok := p.(AuditPrinter)
	if !ok {
		t.Fatal("noopPlatform must satisfy AuditPrinter via Platform value")
	}

	tmp := t.TempDir()
	buf := &countingWriter{}
	ap.PrintAudit(buf, "proj", filepath.Join(tmp, "repo"), filepath.Join(tmp, ".agents"))
	if buf.n != 0 {
		t.Errorf("stub PrintAudit wrote %d bytes, want 0", buf.n)
	}
}

// countingWriter records how many bytes were written to it.
type countingWriter struct{ n int }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += len(p)
	return len(p), nil
}
