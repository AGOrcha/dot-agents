package service

import (
	"context"
	"errors"
	"net"
	nethttp "net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/service/events"
	servicehttp "github.com/AGOrcha/dot-agents/internal/service/http"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
	"github.com/AGOrcha/dot-agents/internal/service/tasks"
)

// Poll/assert windows. runReturnTimeout mirrors the task contract: Run must
// return within 5s of cancellation.
const (
	pollInterval     = 10 * time.Millisecond
	startupTimeout   = 5 * time.Second
	eventTimeout     = 2 * time.Second
	runReturnTimeout = 5 * time.Second
)

// skipNoControlPlane skips tests that need the local control plane where it
// is not implemented (the Windows named-pipe transport is deferred work).
func skipNoControlPlane(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("named-pipe control plane not implemented on windows")
	}
}

// sock returns a short per-test socket path (UDS paths are limited to ~104
// bytes on macOS, so the default under a deep temp RepoDir is avoided).
func sock(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "s.sock")
}

// gitRepo returns a temp directory initialized as a git repository, which
// the real scoring pipeline (git-topology signals) requires of RepoDir.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

// freeAddr reserves an ephemeral loopback port and releases it, returning
// the address for Run's HTTP edge to bind. The tiny release-to-rebind window
// is accepted: tests run serially against loopback.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// startRun launches Run in a goroutine. Cleanup cancels the context; tests
// that assert on Run's return value drain done themselves via waitRun.
func startRun(t *testing.T, cfg Config) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()
	t.Cleanup(cancel)
	return cancel, done
}

// waitRun asserts Run returns within the bounded shutdown window and hands
// back its error.
func waitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(runReturnTimeout):
		t.Fatalf("Run did not return within %v", runReturnTimeout)
		return nil
	}
}

// waitControl polls the control socket until a status round-trip succeeds,
// returning the reported task states.
func waitControl(t *testing.T, socket string) []scheduler.TaskState {
	t.Helper()
	client := servicehttp.NewControlClient(socket)
	deadline := time.Now().Add(startupTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		states, err := client.Status(ctx)
		cancel()
		if err == nil {
			return states
		}
		lastErr = err
		time.Sleep(pollInterval)
	}
	t.Fatalf("control socket never answered status: %v", lastErr)
	return nil
}

// waitHealthz polls the HTTP edge until /healthz answers 200.
func waitHealthz(t *testing.T, addr string) {
	t.Helper()
	url := "http://" + addr + "/healthz"
	deadline := time.Now().Add(startupTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := nethttp.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == nethttp.StatusOK {
				return
			}
		}
		lastErr = err
		time.Sleep(pollInterval)
	}
	t.Fatalf("HTTP edge never served /healthz: %v", lastErr)
}

// TestRunLifecycle is the task's integration test: Run composes the bus,
// scheduler, and both listeners; a dropped iter-1.yaml is scored end-to-end
// (real scoring pipeline) with the sidecar on disk and iteration.scored
// published within 2s; cancelling the context shuts everything down cleanly
// within 5s — both listeners provably gone.
func TestRunLifecycle(t *testing.T) {
	skipNoControlPlane(t)
	repo := gitRepo(t)
	bus := events.NewInProcBus()
	sub, unsubscribe, err := bus.Subscribe(events.TopicIterationScored)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()

	cfg := Config{
		RepoDir:       repo,
		ControlSocket: sock(t),
		HTTPAddr:      freeAddr(t),
		// Keep the interval task quiet so the test exercises only the
		// fsnotify-driven ingest path deterministically.
		RescoreInterval: time.Hour,
		Bus:             bus,
	}
	cancel, done := startRun(t, cfg)

	states := waitControl(t, cfg.ControlSocket)
	var names []string
	for _, s := range states {
		names = append(names, s.Name)
	}
	wantNames := []string{tasks.IterLogIngesterName, tasks.RescoreName}
	if strings.Join(names, ",") != strings.Join(wantNames, ",") {
		t.Errorf("registered tasks = %v, want %v", names, wantNames)
	}
	waitHealthz(t, cfg.HTTPAddr)

	iterDir := filepath.Join(repo, ".agents", "active", "iteration-log")
	entry := "schema_version: 1\niteration: 1\n"
	if err := os.WriteFile(filepath.Join(iterDir, "iter-1.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatalf("write iter-1.yaml: %v", err)
	}

	select {
	case evt := <-sub:
		payload, ok := evt.Payload.(events.IterationScored)
		if !ok {
			t.Fatalf("payload type = %T, want events.IterationScored", evt.Payload)
		}
		if payload.Iteration != 1 {
			t.Errorf("scored iteration = %d, want 1", payload.Iteration)
		}
		if _, err := os.Stat(payload.SidecarPath); err != nil {
			t.Errorf("score sidecar not on disk: %v", err)
		}
	case <-time.After(eventTimeout):
		t.Fatalf("no iteration.scored event within %v", eventTimeout)
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Errorf("Run = %v, want nil on clean shutdown", err)
	}
	if _, err := os.Stat(cfg.ControlSocket); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("control socket still present after shutdown (stat err %v)", err)
	}
	if _, err := nethttp.Get("http://" + cfg.HTTPAddr + "/healthz"); err == nil {
		t.Error("HTTP edge still serving after shutdown")
	}
}

// TestRunStopViaControl proves the OQ4 stop path: an authorized stop request
// on the peer-credential-gated control socket shuts the whole runtime down
// cleanly, without touching the parent context.
func TestRunStopViaControl(t *testing.T) {
	skipNoControlPlane(t)
	cfg := Config{
		RepoDir:         t.TempDir(),
		ControlSocket:   sock(t),
		HTTPAddr:        freeAddr(t),
		RescoreInterval: time.Hour,
	}
	_, done := startRun(t, cfg)
	waitControl(t, cfg.ControlSocket)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := servicehttp.NewControlClient(cfg.ControlSocket).Stop(ctx); err != nil {
		t.Fatalf("control stop: %v", err)
	}
	if err := waitRun(t, done); err != nil {
		t.Errorf("Run = %v, want nil after control-plane stop", err)
	}
}

// TestRunUnknownTask rejects an EnabledTasks name the runtime cannot build.
func TestRunUnknownTask(t *testing.T) {
	cfg := Config{RepoDir: t.TempDir(), EnabledTasks: []string{"no-such-task"}}
	err := Run(context.Background(), cfg)
	if !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("Run = %v, want ErrUnknownTask", err)
	}
}

// TestRunDuplicateTask surfaces the scheduler's duplicate-name rejection for
// a repeated EnabledTasks entry.
func TestRunDuplicateTask(t *testing.T) {
	cfg := Config{
		RepoDir:      t.TempDir(),
		EnabledTasks: []string{tasks.RescoreName, tasks.RescoreName},
	}
	err := Run(context.Background(), cfg)
	if !errors.Is(err, scheduler.ErrDuplicateName) {
		t.Fatalf("Run = %v, want scheduler.ErrDuplicateName", err)
	}
}

// TestRunControlDirBlocked surfaces a control-socket parent directory that
// cannot be created (a regular file occupies the path).
func TestRunControlDirBlocked(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blk")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	cfg := Config{RepoDir: dir, ControlSocket: filepath.Join(blocker, "s.sock")}
	err := Run(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "control socket dir") {
		t.Fatalf("Run = %v, want control-socket-dir creation error", err)
	}
}

// TestRunIterLogDirBlocked surfaces an iteration-log directory that cannot
// be created (a regular file occupies the path).
func TestRunIterLogDirBlocked(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "iterlog")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	cfg := Config{RepoDir: dir, ControlSocket: sock(t), IterLogDir: blocker}
	err := Run(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "iteration-log dir") {
		t.Fatalf("Run = %v, want iteration-log-dir creation error", err)
	}
}

// TestRunControlSocketBusy proves a second runtime against a live control
// socket fails fast — and tears the sibling HTTP edge down with it instead
// of leaving half a service running.
func TestRunControlSocketBusy(t *testing.T) {
	skipNoControlPlane(t)
	socket := sock(t)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("occupy socket: %v", err)
	}
	defer func() { _ = ln.Close() }()

	cfg := Config{RepoDir: t.TempDir(), ControlSocket: socket, HTTPAddr: freeAddr(t)}
	_, done := startRun(t, cfg)
	err = waitRun(t, done)
	if err == nil || !strings.Contains(err.Error(), "already served") {
		t.Fatalf("Run = %v, want already-served control socket error", err)
	}
	if _, getErr := nethttp.Get("http://" + cfg.HTTPAddr + "/healthz"); getErr == nil {
		t.Error("HTTP edge still serving after control listener failure")
	}
}

// TestRunHTTPAddrBusy proves an unbindable HTTP edge fails Run fast and
// tears the control plane down with it (the socket file is unlinked).
func TestRunHTTPAddrBusy(t *testing.T) {
	skipNoControlPlane(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	defer func() { _ = ln.Close() }()

	cfg := Config{RepoDir: t.TempDir(), ControlSocket: sock(t), HTTPAddr: ln.Addr().String()}
	_, done := startRun(t, cfg)
	err = waitRun(t, done)
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("Run = %v, want HTTP listen error", err)
	}
	if _, statErr := os.Stat(cfg.ControlSocket); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("control socket still present after edge failure (stat err %v)", statErr)
	}
}

// TestRunTriggerStartFailure surfaces a scheduler start failure: the
// ingester's fsnotify trigger cannot watch an unreadable iteration-log dir.
func TestRunTriggerStartFailure(t *testing.T) {
	skipNoControlPlane(t)
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not bind as root")
	}
	dir := t.TempDir()
	iterDir := filepath.Join(dir, "iterlog")
	if err := os.Mkdir(iterDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(iterDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(iterDir, 0o755) })

	cfg := Config{RepoDir: dir, ControlSocket: sock(t), IterLogDir: iterDir}
	err := Run(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "start trigger") {
		t.Fatalf("Run = %v, want scheduler trigger start error", err)
	}
}

// TestStopSchedulerDrainTimeout covers the drain-failure fold: a scheduler
// whose in-flight run outlives the drain window turns an otherwise-clean
// shutdown into a wrapped drain error, while a listener error keeps
// precedence over the drain outcome.
func TestStopSchedulerDrainTimeout(t *testing.T) {
	sched := scheduler.New()
	err := sched.Register(scheduler.Task{
		Name:    "slow",
		Trigger: scheduler.Interval(time.Millisecond),
		RunFn: func(ctx context.Context) error {
			// Outlive the drain window, but honour cancellation so Stop's
			// post-timeout context cancel can reap the run.
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := sched.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(startupTimeout)
	for !sched.State()[0].Running {
		if time.Now().After(deadline) {
			t.Fatal("slow task never started running")
		}
		time.Sleep(pollInterval)
	}

	got := stopScheduler(sched, time.Millisecond, nil)
	if !errors.Is(got, context.DeadlineExceeded) || !strings.Contains(got.Error(), "scheduler drain") {
		t.Fatalf("stopScheduler = %v, want wrapped scheduler-drain deadline error", got)
	}
}

// TestRunWindowsControlUnsupported pins the Windows fail-fast contract: until
// the named-pipe control plane lands, Run returns the deferred-transport
// error instead of serving half a runtime.
func TestRunWindowsControlUnsupported(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only contract")
	}
	cfg := Config{RepoDir: t.TempDir(), HTTPAddr: freeAddr(t)}
	_, done := startRun(t, cfg)
	err := waitRun(t, done)
	if err == nil || !strings.Contains(err.Error(), "named-pipe") {
		t.Fatalf("Run = %v, want named-pipe-unsupported error", err)
	}
}
