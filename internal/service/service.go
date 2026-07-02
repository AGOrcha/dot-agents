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
	"strings"
	"sync/atomic"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/service/events"
	servicehttp "github.com/AGOrcha/dot-agents/internal/service/http"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
	"github.com/AGOrcha/dot-agents/internal/service/tasks"
)

// shutdownTimeout is the default single budget covering the ENTIRE teardown —
// both listeners draining plus the scheduler stop, run concurrently — once
// shutdown begins. It is one global bound, not a per-stage timeout: the spec's
// exit-cleanly-within-5s criterion is a wall-clock bound on cancel→return, so
// stacked per-stage waits (each listener's own ~5s drain, then another 5s for
// the scheduler) would violate it. When the budget expires Run abandons any
// straggler (a misbehaving in-flight request, a ctx-ignoring task) and returns
// — SIGINT responsiveness wins over goroutine hygiene at exit; the process is
// terminating regardless. Overridable via Config.ShutdownTimeout (tests use a
// short budget to assert the bound under load).
const shutdownTimeout = 5 * time.Second

// listenerCount is the number of transport listeners the runtime serves: the
// UDS control plane and the HTTP/SSE edge (spec §2A surface→transport map).
const listenerCount = 2

// ErrUnknownTask is returned by Run when Config.EnabledTasks names a task
// the runtime does not know how to build.
var ErrUnknownTask = errors.New("service: unknown task name in EnabledTasks")

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

	// Both listeners serve concurrently until runCtx is cancelled (parent ctx,
	// control-plane stop) or one returns an error at startup (bind failure) —
	// in which case its sibling is cancelled.
	lerrc := make(chan error, listenerCount)
	go func() { lerrc <- ctl.Serve(runCtx) }()
	go func() { lerrc <- edge.Serve(runCtx) }()

	var listenerErr error
	remaining := listenerCount
	select {
	case <-runCtx.Done():
	case listenerErr = <-lerrc:
		remaining--
		cancel()
	}

	// Shutdown has begun: bound the entire remaining teardown by one budget.
	return shutdownWithin(cfg.ShutdownTimeout, sched, lerrc, remaining, listenerErr)
}

// shutdownWithin bounds the whole teardown — the still-serving listeners
// draining plus the scheduler stop, run CONCURRENTLY — by a single budget.
// Returns nil on a clean in-budget shutdown; the causal listener error if one
// bound; a wrapped scheduler-drain error against an otherwise-clean stop; or a
// budget-exceeded error naming the undrained stage(s) when a straggler is
// abandoned.
func shutdownWithin(budget time.Duration, sched *scheduler.Scheduler, lerrc <-chan error, remainingListeners int, listenerErr error) error {
	var listenersDrained, schedDrained atomic.Bool
	if remainingListeners == 0 {
		listenersDrained.Store(true)
	}

	// Drain the scheduler concurrently with the remaining listeners — the
	// scheduler's own Stop is bounded by the same budget so a ctx-ignoring
	// task cannot outlast it.
	schedErrc := make(chan error, 1)
	go func() { schedErrc <- sched.Stop(budget) }()

	done := make(chan struct{})
	var (
		firstListenerErr = listenerErr
		schedErr         error
	)
	go func() {
		for remainingListeners > 0 {
			if e := <-lerrc; e != nil && firstListenerErr == nil {
				firstListenerErr = e
			}
			remainingListeners--
		}
		listenersDrained.Store(true)
		schedErr = <-schedErrc
		schedDrained.Store(true)
		close(done)
	}()

	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-done:
		if firstListenerErr != nil {
			return firstListenerErr
		}
		if schedErr != nil {
			return fmt.Errorf("service: scheduler drain: %w", schedErr)
		}
		return nil
	case <-timer.C:
		// Budget spent: abandon whatever is still draining and return so a
		// misbehaving in-flight request or ctx-ignoring task can never hold
		// SIGINT hostage. Read only the atomic stage flags here — the
		// collector goroutine owns firstListenerErr/schedErr and may still be
		// running.
		var undrained []string
		if !listenersDrained.Load() {
			undrained = append(undrained, "listeners")
		}
		if !schedDrained.Load() {
			undrained = append(undrained, "scheduler")
		}
		return fmt.Errorf("service: shutdown exceeded %s budget; abandoned: %s", budget, strings.Join(undrained, ", "))
	}
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
