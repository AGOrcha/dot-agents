// Package scheduler is the in-process job scheduler that hosts the
// background tasks of the `da service` runtime. It is deliberately small
// (interval + fsnotify triggers, panic-recovering task dispatch, last-run
// bookkeeping, drain-on-stop) rather than a durable queue — see
// .agents/workflow/specs/r3-background-worker-service/design.md (D2) for the
// rationale and rejected alternatives.
//
// The package owns goroutine lifecycle only. It deliberately knows nothing
// about scoring, watermarks, HTTP, or the event bus: tasks are plain funcs and
// the bus plugs in at the call site (the runtime composes them). This keeps the
// scheduler reusable and trivially testable.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// RunFunc is the unit of work a Task performs on each trigger fire. The context
// is derived from the scheduler's run context and is additionally bounded by
// the task's Timeout when one is set. A returned error is recorded as the
// task's last-error and increments its consecutive-failure count; a nil error
// resets that count.
type RunFunc func(ctx context.Context) error

// Task binds a name to a trigger and a unit of work.
type Task struct {
	// Name uniquely identifies the task within a scheduler. Required.
	Name string
	// Trigger drives when RunFn executes. Required.
	Trigger Trigger
	// RunFn is the work performed on each fire. Required.
	RunFn RunFunc
	// Timeout bounds a single RunFn execution. Zero means no timeout.
	Timeout time.Duration
}

// TaskState is an immutable snapshot of one task's health, surfaced to the
// status CLI / HTTP endpoint via Scheduler.State.
type TaskState struct {
	Name                string     `json:"name"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	Runs                int        `json:"runs"`
	// Dropped counts trigger fires skipped because a previous run of this task
	// was still in flight (OQ2: drop-on-overrun, single goroutine per task).
	Dropped int `json:"dropped"`
	// Running reports whether a RunFn is currently executing.
	Running bool `json:"running"`
}

// taskRuntime is the scheduler's mutable per-task record. All fields are
// guarded by Scheduler.mu.
type taskRuntime struct {
	task                Task
	lastRunAt           *time.Time
	nextRunAt           *time.Time
	lastError           string
	consecutiveFailures int
	runs                int
	dropped             int
	running             bool
}

// Errors returned by Register / Start.
var (
	ErrNoName         = errors.New("scheduler: task name is required")
	ErrNoTrigger      = errors.New("scheduler: task trigger is required")
	ErrNoRunFn        = errors.New("scheduler: task RunFn is required")
	ErrDuplicateName  = errors.New("scheduler: duplicate task name")
	ErrAlreadyStarted = errors.New("scheduler: already started")
	ErrNotStarted     = errors.New("scheduler: not started")
)

// Scheduler hosts a set of tasks, each driven by its own trigger. It is safe
// for concurrent use: Register before Start, State at any time, Stop once.
type Scheduler struct {
	mu      sync.Mutex
	tasks   map[string]*taskRuntime
	order   []string // registration order, for stable State output
	started bool
	stopped bool

	// now is a clock seam for deterministic tests.
	now func() time.Time

	// trigCancel stops the trigger loops; cancelling it halts new dispatches.
	// runCancel cancels every in-flight RunFn's context; Stop calls it only after
	// the drain timeout elapses (notes: "drain ... then cancel their context").
	// The contexts themselves are not stored as fields (S8242): trigCtx is passed
	// to each loop goroutine and runCtx is threaded through loop -> dispatch.
	trigCancel context.CancelFunc
	runCancel  context.CancelFunc
	// wg tracks trigger loops; inFlight tracks executing RunFns so Stop can
	// drain them separately from the trigger goroutines.
	wg       sync.WaitGroup
	inFlight sync.WaitGroup
}

// New constructs an empty scheduler.
func New() *Scheduler {
	return &Scheduler{
		tasks: make(map[string]*taskRuntime),
		now:   time.Now,
	}
}

// Register adds a task. It must be called before Start. Returns an error for
// missing required fields or a duplicate name.
func (s *Scheduler) Register(t Task) error {
	if t.Name == "" {
		return ErrNoName
	}
	if t.Trigger == nil {
		return ErrNoTrigger
	}
	if t.RunFn == nil {
		return ErrNoRunFn
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return ErrAlreadyStarted
	}
	if _, dup := s.tasks[t.Name]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateName, t.Name)
	}
	s.tasks[t.Name] = &taskRuntime{task: t}
	s.order = append(s.order, t.Name)
	return nil
}

// Start launches every registered task's trigger loop. The provided context
// governs the whole scheduler: cancelling it stops all triggers and (via Stop's
// drain semantics) the in-flight runs. Start returns once all triggers are
// live, or an error if a trigger fails to start (in which case any triggers
// already started are torn down).
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}
	s.started = true
	// Trigger loops and RunFns derive from the same parent ctx, but are
	// cancellable independently so Stop can halt new dispatches while letting
	// in-flight RunFns drain before their own context is cancelled.
	trigCtx, trigCancel := context.WithCancel(ctx)
	runCtx, runCancel := context.WithCancel(ctx)
	s.trigCancel = trigCancel
	s.runCancel = runCancel
	// Snapshot tasks under lock for trigger startup outside the lock.
	rts := make([]*taskRuntime, 0, len(s.order))
	for _, name := range s.order {
		rts = append(rts, s.tasks[name])
	}
	s.mu.Unlock()

	for _, rt := range rts {
		ch, err := rt.task.Trigger.Start(trigCtx)
		if err != nil {
			trigCancel()
			runCancel()
			s.wg.Wait()
			s.mu.Lock()
			s.started = false
			s.trigCancel = nil
			s.runCancel = nil
			s.mu.Unlock()
			return fmt.Errorf("scheduler: start trigger for %q: %w", rt.task.Name, err)
		}
		if it, ok := rt.task.Trigger.(*IntervalTrigger); ok {
			next := s.now().Add(it.Every)
			s.mu.Lock()
			rt.nextRunAt = &next
			s.mu.Unlock()
		}
		s.wg.Add(1)
		go s.loop(trigCtx, runCtx, rt, ch)
	}
	return nil
}

// loop reads ticks for a single task and dispatches RunFn. trigCtx stops the
// loop on shutdown; runCtx is passed down to each RunFn so it can outlive the
// trigger loop during Stop's drain window.
func (s *Scheduler) loop(trigCtx, runCtx context.Context, rt *taskRuntime, ch <-chan time.Time) {
	defer s.wg.Done()
	for {
		select {
		case <-trigCtx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			s.dispatch(runCtx, rt)
		}
	}
}

// dispatch handles one tick for a task, honouring drop-on-overrun (OQ2). If a
// previous run is still in flight, the tick is dropped and counted rather than
// queued. Otherwise the run is launched in its own goroutine so the task's loop
// stays responsive and can observe — and drop — ticks that arrive during the
// run. A single task therefore never runs two RunFns concurrently, but the loop
// never blocks on a long run either.
func (s *Scheduler) dispatch(runCtx context.Context, rt *taskRuntime) {
	s.mu.Lock()
	if rt.running {
		rt.dropped++
		s.mu.Unlock()
		return
	}
	rt.running = true
	s.mu.Unlock()

	s.inFlight.Add(1)
	go func() {
		defer s.inFlight.Done()
		s.run(runCtx, rt)
	}()
}

// run executes a single RunFn and records the outcome.
func (s *Scheduler) run(ctx context.Context, rt *taskRuntime) {
	runCtx := ctx
	var cancel context.CancelFunc
	if rt.task.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, rt.task.Timeout)
		defer cancel()
	}

	start := s.now()
	err := s.invoke(runCtx, rt.task.RunFn)
	end := s.now()

	s.mu.Lock()
	rt.running = false
	rt.runs++
	last := start
	rt.lastRunAt = &last
	if it, ok := rt.task.Trigger.(*IntervalTrigger); ok {
		next := end.Add(it.Every)
		rt.nextRunAt = &next
	}
	if err != nil {
		rt.lastError = err.Error()
		rt.consecutiveFailures++
	} else {
		rt.lastError = ""
		rt.consecutiveFailures = 0
	}
	s.mu.Unlock()
}

// invoke runs fn with panic recovery. A panic is converted to an error so it is
// recorded as last-error and never propagated past the task boundary (req 7).
func (s *Scheduler) invoke(ctx context.Context, fn RunFunc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("scheduler: task panicked: %v", r)
		}
	}()
	return fn(ctx)
}

// Stop performs a graceful shutdown: it stops accepting new ticks (cancelling
// trigger loops), then waits up to timeout for in-flight RunFns to finish. If
// the timeout elapses first, the run context is cancelled so RunFns honouring
// ctx can abort, and Stop returns context.DeadlineExceeded. A nil error means a
// clean drain. Stop is idempotent-safe to call once; a second call returns
// ErrNotStarted.
func (s *Scheduler) Stop(timeout time.Duration) error {
	s.mu.Lock()
	if !s.started || s.stopped {
		s.mu.Unlock()
		return ErrNotStarted
	}
	s.stopped = true
	trigCancel := s.trigCancel
	runCancel := s.runCancel
	s.mu.Unlock()

	// Stop trigger loops first so no new dispatches start. In-flight RunFns keep
	// running under runCtx; we drain them before cancelling that context.
	trigCancel()
	s.wg.Wait()

	done := make(chan struct{})
	go func() {
		s.inFlight.Wait()
		close(done)
	}()

	if timeout <= 0 {
		<-done
		runCancel()
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		runCancel()
		return nil
	case <-timer.C:
		// Drain timeout breached: cancel the run context so RunFns that honour it
		// unwind, then wait for them to actually return (no goroutine leak) and
		// report the deadline.
		runCancel()
		<-done
		return context.DeadlineExceeded
	}
}

// State returns a snapshot of every task's health, in registration order.
func (s *Scheduler) State() []TaskState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TaskState, 0, len(s.order))
	for _, name := range s.order {
		rt := s.tasks[name]
		ts := TaskState{
			Name:                rt.task.Name,
			LastError:           rt.lastError,
			ConsecutiveFailures: rt.consecutiveFailures,
			Runs:                rt.runs,
			Dropped:             rt.dropped,
			Running:             rt.running,
		}
		if rt.lastRunAt != nil {
			t := *rt.lastRunAt
			ts.LastRunAt = &t
		}
		if rt.nextRunAt != nil {
			t := *rt.nextRunAt
			ts.NextRunAt = &t
		}
		out = append(out, ts)
	}
	return out
}
