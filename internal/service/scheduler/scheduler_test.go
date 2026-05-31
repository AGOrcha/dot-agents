package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// manualTrigger lets a test fire ticks on demand, decoupling scheduler tests
// from wall-clock timing. fire() blocks until the scheduler's loop receives.
type manualTrigger struct {
	ch        chan time.Time
	startErr  error
	startedFn func()
}

func newManualTrigger() *manualTrigger { return &manualTrigger{ch: make(chan time.Time)} }

func (m *manualTrigger) Start(ctx context.Context) (<-chan time.Time, error) {
	if m.startErr != nil {
		return nil, m.startErr
	}
	if m.startedFn != nil {
		m.startedFn()
	}
	out := make(chan time.Time)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case t, ok := <-m.ch:
				if !ok {
					return
				}
				select {
				case out <- t:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (m *manualTrigger) fire() { m.ch <- time.Now() }

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name string
		task Task
		want error
	}{
		{"no name", Task{Trigger: newManualTrigger(), RunFn: func(context.Context) error { return nil }}, ErrNoName},
		{"no trigger", Task{Name: "a", RunFn: func(context.Context) error { return nil }}, ErrNoTrigger},
		{"no runfn", Task{Name: "a", Trigger: newManualTrigger()}, ErrNoRunFn},
		{"ok", Task{Name: "a", Trigger: newManualTrigger(), RunFn: func(context.Context) error { return nil }}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			if err := s.Register(tt.task); !errors.Is(err, tt.want) {
				t.Fatalf("Register() err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRegisterDuplicate(t *testing.T) {
	s := New()
	task := Task{Name: "dup", Trigger: newManualTrigger(), RunFn: func(context.Context) error { return nil }}
	if err := s.Register(task); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := s.Register(task); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("second Register err = %v, want ErrDuplicateName", err)
	}
}

func TestRegisterAfterStart(t *testing.T) {
	s := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(time.Second)
	err := s.Register(Task{Name: "late", Trigger: newManualTrigger(), RunFn: func(context.Context) error { return nil }})
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("Register after start err = %v, want ErrAlreadyStarted", err)
	}
}

func TestStartTwice(t *testing.T) {
	s := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(time.Second)
	if err := s.Start(ctx); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start err = %v, want ErrAlreadyStarted", err)
	}
}

func TestStartTriggerError(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	mt.startErr = errors.New("boom")
	if err := s.Register(Task{Name: "bad", Trigger: mt, RunFn: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx := context.Background()
	if err := s.Start(ctx); err == nil {
		t.Fatal("Start should fail when a trigger fails to start")
	}
	// After a failed start the scheduler must be re-startable.
	mt.startErr = nil
	if err := s.Start(ctx); err != nil {
		t.Fatalf("restart after failed start: %v", err)
	}
	s.Stop(time.Second)
}

func TestTaskFiresAndRecordsSuccess(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	var runs int32
	done := make(chan struct{}, 4)
	if err := s.Register(Task{Name: "t", Trigger: mt, RunFn: func(context.Context) error {
		atomic.AddInt32(&runs, 1)
		done <- struct{}{}
		return nil
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mt.fire()
	<-done
	if err := s.Stop(time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st := s.State()
	if len(st) != 1 {
		t.Fatalf("State len = %d", len(st))
	}
	if st[0].Runs != 1 || st[0].LastRunAt == nil || st[0].LastError != "" || st[0].ConsecutiveFailures != 0 {
		t.Fatalf("unexpected state: %+v", st[0])
	}
}

func TestTaskErrorIncrementsFailures(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	ran := make(chan struct{}, 4)
	if err := s.Register(Task{Name: "t", Trigger: mt, RunFn: func(context.Context) error {
		ran <- struct{}{}
		return errors.New("kaboom")
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mt.fire()
	<-ran
	// Second fire must observe the first run completed; wait for state to settle.
	waitFor(t, func() bool { return s.State()[0].Runs == 1 })
	mt.fire()
	<-ran
	waitFor(t, func() bool { return s.State()[0].Runs == 2 })
	s.Stop(time.Second)
	st := s.State()[0]
	if st.ConsecutiveFailures != 2 || st.LastError != "kaboom" {
		t.Fatalf("expected 2 consecutive failures with last err, got %+v", st)
	}
}

func TestSuccessResetsFailureCount(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	var fail atomic.Bool
	fail.Store(true)
	ran := make(chan struct{}, 4)
	if err := s.Register(Task{Name: "t", Trigger: mt, RunFn: func(context.Context) error {
		defer func() { ran <- struct{}{} }()
		if fail.Load() {
			return errors.New("e")
		}
		return nil
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mt.fire()
	<-ran
	waitFor(t, func() bool { return s.State()[0].ConsecutiveFailures == 1 })
	fail.Store(false)
	mt.fire()
	<-ran
	waitFor(t, func() bool { return s.State()[0].ConsecutiveFailures == 0 })
	s.Stop(time.Second)
	if st := s.State()[0]; st.LastError != "" || st.ConsecutiveFailures != 0 {
		t.Fatalf("success did not reset failure state: %+v", st)
	}
}

func TestPanicRecovered(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	ran := make(chan struct{}, 1)
	if err := s.Register(Task{Name: "boom", Trigger: mt, RunFn: func(context.Context) error {
		defer func() { ran <- struct{}{} }()
		panic("explode")
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mt.fire()
	<-ran
	waitFor(t, func() bool { return s.State()[0].Runs == 1 })
	// Scheduler must still be alive: a second fire must execute.
	mt.fire()
	<-ran
	waitFor(t, func() bool { return s.State()[0].Runs == 2 })
	s.Stop(time.Second)
	st := s.State()[0]
	if st.ConsecutiveFailures != 2 || st.LastError == "" {
		t.Fatalf("panic not recorded as error: %+v", st)
	}
}

func TestDropOnOverrun(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	enter := make(chan struct{})
	release := make(chan struct{})
	var entered atomic.Bool
	if err := s.Register(Task{Name: "slow", Trigger: mt, RunFn: func(context.Context) error {
		if !entered.Swap(true) {
			close(enter)
		}
		<-release
		return nil
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mt.fire() // starts the long run
	<-enter   // run is now in flight and blocked
	// These fires must be dropped because the first run is still running.
	mt.fire()
	mt.fire()
	waitFor(t, func() bool { return s.State()[0].Dropped == 2 })
	close(release)
	s.Stop(time.Second)
	st := s.State()[0]
	if st.Runs != 1 {
		t.Fatalf("expected 1 run (others dropped), got %d", st.Runs)
	}
	if st.Dropped != 2 {
		t.Fatalf("expected 2 dropped, got %d", st.Dropped)
	}
}

func TestStopDrainsInFlight(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	started := make(chan struct{})
	finished := atomic.Bool{}
	if err := s.Register(Task{Name: "t", Trigger: mt, RunFn: func(context.Context) error {
		close(started)
		time.Sleep(50 * time.Millisecond)
		finished.Store(true)
		return nil
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mt.fire()
	<-started
	if err := s.Stop(time.Second); err != nil {
		t.Fatalf("Stop should drain cleanly: %v", err)
	}
	if !finished.Load() {
		t.Fatal("Stop returned before in-flight run finished")
	}
}

func TestStopTimeoutCancelsContext(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	started := make(chan struct{})
	ctxCancelled := atomic.Bool{}
	if err := s.Register(Task{Name: "t", Trigger: mt, RunFn: func(rc context.Context) error {
		close(started)
		<-rc.Done() // only returns when the scheduler cancels us
		ctxCancelled.Store(true)
		return rc.Err()
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mt.fire()
	<-started
	err := s.Stop(20 * time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop err = %v, want DeadlineExceeded", err)
	}
	if !ctxCancelled.Load() {
		t.Fatal("run context was not cancelled on Stop timeout")
	}
}

func TestStopZeroTimeoutWaitsForDrain(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	started := make(chan struct{})
	done := atomic.Bool{}
	if err := s.Register(Task{Name: "t", Trigger: mt, RunFn: func(context.Context) error {
		close(started)
		time.Sleep(30 * time.Millisecond)
		done.Store(true)
		return nil
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mt.fire()
	<-started
	if err := s.Stop(0); err != nil {
		t.Fatalf("Stop(0): %v", err)
	}
	if !done.Load() {
		t.Fatal("Stop(0) did not wait for drain")
	}
}

func TestStopBeforeStart(t *testing.T) {
	s := New()
	if err := s.Stop(time.Second); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Stop before Start err = %v, want ErrNotStarted", err)
	}
}

func TestStopTwice(t *testing.T) {
	s := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(time.Second); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := s.Stop(time.Second); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("second Stop err = %v, want ErrNotStarted", err)
	}
}

func TestTimeoutBoundsRunFn(t *testing.T) {
	s := New()
	mt := newManualTrigger()
	ran := make(chan error, 1)
	if err := s.Register(Task{Name: "t", Trigger: mt, Timeout: 10 * time.Millisecond, RunFn: func(rc context.Context) error {
		<-rc.Done()
		ran <- rc.Err()
		return rc.Err()
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mt.fire()
	select {
	case err := <-ran:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("RunFn ctx err = %v, want DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout did not cancel RunFn")
	}
	s.Stop(time.Second)
}

func TestStateOrderAndNextRunForInterval(t *testing.T) {
	s := New()
	if err := s.Register(Task{Name: "first", Trigger: newManualTrigger(), RunFn: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("Register first: %v", err)
	}
	if err := s.Register(Task{Name: "second", Trigger: Interval(time.Hour), RunFn: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("Register second: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(time.Second)
	st := s.State()
	if len(st) != 2 || st[0].Name != "first" || st[1].Name != "second" {
		t.Fatalf("state order wrong: %+v", st)
	}
	if st[1].NextRunAt == nil {
		t.Fatal("interval task should have NextRunAt set after Start")
	}
	if st[0].NextRunAt != nil {
		t.Fatal("non-interval task should not have NextRunAt before first run")
	}
}

func TestConcurrentStateDuringRuns(t *testing.T) {
	s := New()
	triggers := make([]*manualTrigger, 4)
	for i := range triggers {
		triggers[i] = newManualTrigger()
		mt := triggers[i]
		name := string(rune('a' + i))
		if err := s.Register(Task{Name: name, Trigger: mt, RunFn: func(context.Context) error {
			return nil
		}}); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.State()
			}
		}
	}()
	for n := 0; n < 50; n++ {
		triggers[n%len(triggers)].fire()
	}
	close(stop)
	wg.Wait()
	s.Stop(time.Second)
}

// waitFor polls cond up to a generous deadline, failing the test on timeout.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}
