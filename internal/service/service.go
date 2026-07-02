// Package service composes the `da service` runtime out of the pieces the
// sibling packages ship (spec §2A shape, OQ5 option B): the event bus behind
// the D4.1 EventBus interface seam, the in-process scheduler hosting the two
// v1 background tasks (iter-log ingester + rubric-bump rescorer), and BOTH
// transport listeners — the Unix-domain-socket control plane (`da service
// status`/`stop`, peer-credential gated per OQ4/§2A) and the HTTP/SSE edge
// (loopback-only by default per OQ3).
//
// The package owns composition and lifecycle only: construction order is
// bus → scheduler (tasks registered) → listeners, and shutdown unwinds in
// reverse — listeners down, scheduler drained (bounded), bus closed. It
// deliberately contains no cobra surface and never calls os.Exit; errors are
// returned clean so the CLI layer can render them.
package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/service/events"
	servicehttp "github.com/AGOrcha/dot-agents/internal/service/http"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
	"github.com/AGOrcha/dot-agents/internal/service/tasks"
)

// shutdownTimeout bounds the scheduler drain once both listeners are down:
// in-flight task runs get this long to finish before their context is
// cancelled. Together with the listeners' own bounded shutdowns it keeps the
// whole Run teardown inside the spec's exit-cleanly-within-5s criterion.
const shutdownTimeout = 5 * time.Second

// listenerCount is the number of transport listeners the runtime serves: the
// UDS control plane and the HTTP/SSE edge (spec §2A surface→transport map).
const listenerCount = 2

// ErrUnknownTask is returned by Run when Config.EnabledTasks names a task
// the runtime does not know how to build.
var ErrUnknownTask = errors.New("service: unknown task name in EnabledTasks")

// listener is the slice of the transport types Run composes: both
// servicehttp.Control and servicehttp.Server serve until their context is
// cancelled and then shut down within their own bounded timeouts.
type listener interface {
	Serve(ctx context.Context) error
}

// Run composes and runs the full service runtime until ctx is cancelled or
// an authorized stop request arrives on the control socket (OQ4: stop rides
// the peer-credential-gated control plane, wired here to the runtime
// cancel). It wires the event bus into the scheduler tasks, starts the
// scheduler, serves both listeners, and on shutdown unwinds everything in
// reverse construction order: listeners first, then the scheduler drain
// (bounded by shutdownTimeout), then the bus. A nil return means a clean
// ctx- or stop-driven shutdown with every goroutine accounted for.
func Run(ctx context.Context, cfg Config) error {
	cfg, err := cfg.withDefaults()
	if err != nil {
		return err
	}
	bus := cfg.Bus
	if bus == nil {
		// The builtin in-process backend (D4.3) is the default and, in v1,
		// the only constructed backend (OQ6 interface-only ruling).
		bus = events.NewInProcBus()
	}
	// The bus closes last, after the listeners and the scheduler tasks that
	// publish on it have fully stopped (reverse construction order).
	defer func() { _ = bus.Close() }()

	sched := scheduler.New()
	if err := registerTasks(sched, cfg, bus); err != nil {
		return err
	}
	if err := ensureRuntimeDirs(cfg); err != nil {
		return err
	}

	// runCtx is the runtime's own lifetime: cancelled by the parent ctx, by
	// a control-plane stop request, or by a listener failure at startup.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ctl := servicehttp.NewControl(cfg.ControlSocket, sched, cancel)
	edge := servicehttp.New(cfg.HTTPAddr, sched, bus)

	if err := sched.Start(runCtx); err != nil {
		return err
	}
	err = serveListeners(runCtx, cancel, ctl, edge)
	// Listeners are down; drain the scheduler before the deferred bus close
	// so no task publishes into a closed bus.
	return stopScheduler(sched, shutdownTimeout, err)
}

// stopScheduler drains sched within timeout, folding a drain failure into
// the listener outcome: a listener error (the more causal failure) wins, a
// drain failure surfaces only against an otherwise-clean shutdown.
func stopScheduler(sched *scheduler.Scheduler, timeout time.Duration, err error) error {
	if stopErr := sched.Stop(timeout); stopErr != nil && err == nil {
		return fmt.Errorf("service: scheduler drain: %w", stopErr)
	}
	return err
}

// registerTasks builds every enabled task against the shared bus and
// registers it with the scheduler. A duplicate name in EnabledTasks is
// rejected by the scheduler's own duplicate check.
func registerTasks(sched *scheduler.Scheduler, cfg Config, bus events.EventBus) error {
	for _, name := range cfg.EnabledTasks {
		task, err := buildTask(name, cfg, bus)
		if err != nil {
			return err
		}
		if err := sched.Register(task); err != nil {
			return err
		}
	}
	return nil
}

// buildTask constructs one builtin background task by scheduler task name.
// Every task receives the EventBus interface (D4.1 seam), never a concrete
// backend.
func buildTask(name string, cfg Config, bus events.EventBus) (scheduler.Task, error) {
	switch name {
	case tasks.IterLogIngesterName:
		return tasks.NewIterLogIngester(tasks.IterLogIngesterConfig{
			IterLogDir: cfg.IterLogDir,
			RepoDir:    cfg.RepoDir,
			Bus:        bus,
		})
	case tasks.RescoreName:
		return tasks.NewRescore(tasks.RescoreConfig{
			IterLogDir: cfg.IterLogDir,
			RepoDir:    cfg.RepoDir,
			Bus:        bus,
			Interval:   cfg.RescoreInterval,
		})
	default:
		return scheduler.Task{}, fmt.Errorf("%w: %q", ErrUnknownTask, name)
	}
}

// ensureRuntimeDirs creates the directories the transports and triggers
// require before startup: the control socket's parent directory (the UDS
// bind fails on a missing parent) and the iteration-log directory (the
// ingester's fsnotify trigger can only watch an existing path — a fresh
// repo may not have one yet).
func ensureRuntimeDirs(cfg Config) error {
	if err := fsops.MkdirAll(filepath.Dir(cfg.ControlSocket), 0o755); err != nil {
		return fmt.Errorf("service: create control socket dir: %w", err)
	}
	if err := fsops.MkdirAll(cfg.IterLogDir, 0o755); err != nil {
		return fmt.Errorf("service: create iteration-log dir: %w", err)
	}
	return nil
}

// serveListeners serves the control plane and the HTTP/SSE edge until ctx is
// cancelled, cancelling the sibling (via cancel) as soon as either returns
// an error — a listener that cannot bind must not leave the other half
// serving. It returns only after BOTH Serve calls have returned, so no
// listener goroutine ever outlives Run, and reports the first error (nil on
// a clean ctx-driven shutdown of both).
func serveListeners(ctx context.Context, cancel context.CancelFunc, ctl, edge listener) error {
	errc := make(chan error, listenerCount)
	go func() { errc <- ctl.Serve(ctx) }()
	go func() { errc <- edge.Serve(ctx) }()

	var first error
	for i := 0; i < listenerCount; i++ {
		if err := <-errc; err != nil {
			if first == nil {
				first = err
			}
			cancel()
		}
	}
	return first
}
