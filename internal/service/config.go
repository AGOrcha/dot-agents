package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AGOrcha/dot-agents/internal/service/events"
	"github.com/AGOrcha/dot-agents/internal/service/tasks"
)

// DefaultHTTPAddr is the default bind address of the HTTP/SSE edge:
// loopback-only per spec OQ3. Binding beyond loopback is a deliberate
// external opt-in made by passing an explicit non-loopback HTTPAddr.
const DefaultHTTPAddr = "127.0.0.1:7878"

// ControlSocketName is the filename of the default per-repo control socket,
// placed under <RepoDir>/.agents/active/ alongside the other live service
// artifacts (the iteration log, the service-state watermarks).
const ControlSocketName = "service.sock"

// Config configures the composed `da service` runtime (Run). The zero value
// is usable: every field resolves to a sensible per-repo default that
// matches the rest of the CLI's conventions.
type Config struct {
	// ControlSocket is the local control-plane endpoint (spec §2A "local
	// control plane" row): the Unix-domain socket path (named-pipe name on
	// Windows, once the pipe transport lands) that answers `da service
	// status`/`stop`. Empty defaults to
	// <RepoDir>/.agents/active/service.sock.
	ControlSocket string
	// HTTPAddr is the HTTP/SSE edge bind address. Empty defaults to
	// DefaultHTTPAddr (loopback-only, OQ3); an explicit non-loopback
	// address is the deliberate external opt-in.
	HTTPAddr string
	// IterLogDir is the iteration-log directory the ingester task watches.
	// Empty defaults to <RepoDir>/.agents/active/iteration-log.
	IterLogDir string
	// RepoDir is the repository root the tasks resolve scoring and
	// watermark state against. Empty defaults to the current working
	// directory.
	RepoDir string
	// RescoreInterval is the rescore task's rubric-version check cadence.
	// Zero applies the tasks package default.
	RescoreInterval time.Duration
	// ShutdownTimeout is the single wall-clock budget covering the whole
	// teardown (both listeners + scheduler drain, concurrently) once shutdown
	// begins. Zero applies shutdownTimeout (5s, the spec exit-within-5s bound).
	// A straggler still draining when the budget expires is abandoned.
	ShutdownTimeout time.Duration
	// EnabledTasks selects which background tasks Run registers, by
	// scheduler task name (tasks.IterLogIngesterName, tasks.RescoreName).
	// Empty enables every builtin task; an unknown name is rejected.
	EnabledTasks []string
	// Bus is the event transport backend behind the spec D4.1 EventBus
	// interface seam. Nil constructs the in-process builtin (D4.3) — the
	// only backend that ships in v1 per the OQ6 interface-only ruling.
	// Injecting a bus is how a future config-selected adapter (D4.4) — or
	// a test subscriber — plugs in; either way Run owns the bus lifecycle
	// and closes it on shutdown.
	Bus events.EventBus
}

// withDefaults returns a copy of c with every unset field resolved to its
// documented default. It fails only when RepoDir must be defaulted and the
// working directory cannot be resolved.
func (c Config) withDefaults() (Config, error) {
	if c.RepoDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("service: resolve repo dir: %w", err)
		}
		c.RepoDir = wd
	}
	activeDir := filepath.Join(c.RepoDir, ".agents", "active")
	if c.IterLogDir == "" {
		c.IterLogDir = filepath.Join(activeDir, "iteration-log")
	}
	if c.ControlSocket == "" {
		c.ControlSocket = filepath.Join(activeDir, ControlSocketName)
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = DefaultHTTPAddr
	}
	if len(c.EnabledTasks) == 0 {
		c.EnabledTasks = []string{tasks.IterLogIngesterName, tasks.RescoreName}
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = shutdownTimeout
	}
	return c, nil
}
