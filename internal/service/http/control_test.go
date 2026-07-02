//go:build unix

package http

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	nethttp "net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/service/events"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
)

// Test names in this file are kept short: t.TempDir feeds the socket path,
// and Unix-domain socket paths are limited to ~104 bytes on macOS.

// sockPath returns a per-test socket path inside a fresh temp dir.
func sockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ctl.sock")
}

// startCtl runs c.Serve on a fresh context, waits for the socket to answer,
// and registers cleanup that cancels and waits for Serve to return.
func startCtl(t *testing.T, c *Control) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Serve(ctx) }()
	waitForSocket(t, c.SocketPath())
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("control Serve returned %v, want nil", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("control Serve did not return after cancel")
		}
	})
}

// waitForSocket polls until the control socket accepts a connection.
func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial(unixNetwork, path)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("control socket %q never came up", path)
}

// TestCtlStatus proves the status op is answered over a *socket* listener
// (the §2A verifier-chain note: integration against UDS, not TCP), that the
// JSON shape is scheduler.State(), and that the socket file is owner-only.
func TestCtlStatus(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	want := []scheduler.TaskState{
		{Name: "ingest", LastRunAt: &now, Runs: 3},
		{Name: "rescore", LastError: "boom", ConsecutiveFailures: 2, Runs: 1},
	}
	c := NewControl(sockPath(t), fakeState{states: want}, func() {})
	startCtl(t, c)

	info, err := os.Stat(c.SocketPath())
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket mode = %v, want 0600 (owner-only, §2A stop gate fence 1)", perm)
	}

	// A ctx deadline shorter than controlIOTimeout also exercises the
	// caller-deadline arm of exchangeDeadline.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := NewControlClient(c.SocketPath()).Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Status = %+v, want %+v", got, want)
	}
}

// TestCtlStop proves an owner peer may stop the service: the client hears OK,
// the stop callback fires, and Serve unwinds cleanly once the callback
// cancels the run context (the wiring the service runtime will use).
func TestCtlStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	c := NewControl(sockPath(t), fakeState{}, func() {
		close(stopped)
		cancel() // the runtime wiring: stop cancels the service ctx
	})
	done := make(chan error, 1)
	go func() { done <- c.Serve(ctx) }()
	waitForSocket(t, c.SocketPath())

	if err := NewControlClient(c.SocketPath()).Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop callback never fired")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v after stop, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not unwind after an authorized stop")
	}
}

// TestCtlStopDenied is the peer-credential gate: a peer whose uid does not
// match the owner is refused, and the stop callback never fires.
func TestCtlStopDenied(t *testing.T) {
	stopped := make(chan struct{}, 1)
	c := NewControl(sockPath(t), fakeState{}, func() { stopped <- struct{}{} })
	c.ownerUID = -1 // no real peer can match
	startCtl(t, c)

	err := NewControlClient(c.SocketPath()).Stop(context.Background())
	if err == nil || !strings.Contains(err.Error(), "peer not authorized") {
		t.Fatalf("Stop = %v, want peer-not-authorized refusal", err)
	}
	select {
	case <-stopped:
		t.Fatal("stop callback fired for an unauthorized peer")
	default:
	}
}

// TestCtlStopCredErr refuses stop when the peer credential cannot be read at
// all — the gate fails closed.
func TestCtlStopCredErr(t *testing.T) {
	c := NewControl(sockPath(t), fakeState{}, func() { t.Error("stop fired") })
	c.peerUID = func(net.Conn) (uint32, error) { return 0, errors.New("no cred") }
	startCtl(t, c)

	err := NewControlClient(c.SocketPath()).Stop(context.Background())
	if err == nil || !strings.Contains(err.Error(), "peer credential unavailable") {
		t.Fatalf("Stop = %v, want credential-unavailable refusal", err)
	}
}

// rawExchange writes raw bytes on a fresh control connection and decodes the
// single JSON response.
func rawExchange(t *testing.T, path, payload string) controlResponse {
	t.Helper()
	conn, err := net.Dial(unixNetwork, path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	var resp controlResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// TestCtlBadRequests covers the protocol error answers: an unknown op and a
// payload that is not JSON both get a structured refusal, not a hangup.
func TestCtlBadRequests(t *testing.T) {
	c := NewControl(sockPath(t), fakeState{}, func() {})
	startCtl(t, c)

	cases := []struct {
		name, payload, wantErr string
	}{
		{"unknown op", `{"op":"bogus"}`, `unknown control op "bogus"`},
		{"malformed", "not-json\n", "malformed control request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := rawExchange(t, c.SocketPath(), tc.payload)
			if resp.OK || !strings.Contains(resp.Error, tc.wantErr) {
				t.Fatalf("resp = %+v, want error containing %q", resp, tc.wantErr)
			}
		})
	}
}

// TestCtlUnavailable is the §D6 routes-when-present signal: with no service
// listening, the client reports ErrControlUnavailable so the CLI can fall
// back to cold-file operation.
func TestCtlUnavailable(t *testing.T) {
	_, err := NewControlClient(sockPath(t)).Status(context.Background())
	if !errors.Is(err, ErrControlUnavailable) {
		t.Fatalf("Status with no service = %v, want ErrControlUnavailable", err)
	}
}

// TestCtlHangupResp covers the client's response-decode error path: a server
// that accepts and closes without answering is a protocol error, not
// ErrControlUnavailable.
func TestCtlHangupResp(t *testing.T) {
	path := sockPath(t)
	ln, err := net.Listen(unixNetwork, path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	_, err = NewControlClient(path).Status(context.Background())
	if err == nil || errors.Is(err, ErrControlUnavailable) || !strings.Contains(err.Error(), "response") {
		t.Fatalf("Status against hanging-up server = %v, want response decode error", err)
	}
}

// makeStaleSocket leaves a dead socket file at path — exactly what a crashed
// service leaves behind (a listener closed without unlinking).
func makeStaleSocket(t *testing.T, path string) {
	t.Helper()
	ln, err := net.Listen(unixNetwork, path)
	if err != nil {
		t.Fatalf("prep listen: %v", err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
}

// alreadyServedMsg is the refusal takeOverStaleSocket returns when the
// under-lock probe finds a live service on the path.
const alreadyServedMsg = "already served by a running service"

// stageStaleBindFailure creates a stale socket at path and returns the bare
// bind failure a starter observes on it — the precondition of every takeover.
func stageStaleBindFailure(t *testing.T, path string) error {
	t.Helper()
	makeStaleSocket(t, path)
	_, err := net.Listen(unixNetwork, path)
	if err == nil {
		t.Fatal("bare listen on a stale socket file succeeded; cannot stage the race")
	}
	return err
}

// TestCtlStaleSocket proves crash recovery: a leftover socket file with no
// listener behind it is detected by the under-lock dial probe, removed via
// fsops, and the bind retried successfully.
func TestCtlStaleSocket(t *testing.T) {
	path := sockPath(t)
	makeStaleSocket(t, path)

	c := NewControl(path, fakeState{}, func() {})
	startCtl(t, c)
	if _, err := NewControlClient(path).Status(context.Background()); err != nil {
		t.Fatalf("Status after stale-socket recovery: %v", err)
	}
}

// TestCtlTakeoverRace replays the probe→remove→rebind TOCTOU deterministically:
// starter A fails its bare bind on a stale socket file (its "the socket is
// dead" observation), then starter B completes the full locked takeover and
// is live; when A proceeds into its own takeover it must re-probe under the
// lock, see B alive, and back off — B's live socket is never deleted.
func TestCtlTakeoverRace(t *testing.T) {
	path := sockPath(t)
	// A, step 1: bare bind fails on the stale file. A now believes the
	// socket needs recovering.
	listenErr := stageStaleBindFailure(t, path)

	// B: full takeover completes first (lock → probe dead → remove → bind).
	lnB, err := listenControl(path)
	if err != nil {
		t.Fatalf("B listenControl: %v", err)
	}
	defer lnB.Close()

	// A, step 2: its takeover re-probes under the lock, sees B live, and
	// must refuse rather than remove B's socket.
	_, err = takeOverStaleSocket(path, listenErr)
	if err == nil || !strings.Contains(err.Error(), alreadyServedMsg) {
		t.Fatalf("A takeOverStaleSocket = %v, want already-served refusal", err)
	}

	// B's live socket survived A's takeover attempt: on disk and connectable.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("B's live socket was deleted by A's takeover: %v", err)
	}
	conn, err := net.Dial(unixNetwork, path)
	if err != nil {
		t.Fatalf("B's socket no longer accepts after A's takeover attempt: %v", err)
	}
	conn.Close()
}

// TestCtlTakeoverBlocks proves the serialization itself: while another
// starter holds the takeover lock, takeOverStaleSocket parks instead of
// touching the path; once the holder has bound a live socket and released,
// the parked starter re-probes, sees it live, and backs off.
func TestCtlTakeoverBlocks(t *testing.T) {
	path := sockPath(t)
	listenErr := stageStaleBindFailure(t, path)

	// B holds the takeover lock.
	release, err := agentslock.AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire takeover lock: %v", err)
	}

	aErr := make(chan error, 1)
	go func() {
		ln, err := takeOverStaleSocket(path, listenErr)
		if ln != nil {
			ln.Close()
		}
		aErr <- err
	}()

	// A must be parked on the lock — the stale file untouched meanwhile.
	select {
	case err := <-aErr:
		t.Fatalf("takeover returned (%v) while the lock was held", err)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("A touched the socket file while parked on the lock: %v", err)
	}

	// B, still under the lock, completes its takeover: remove stale, bind
	// live. Then release.
	if err := fsops.Remove(path); err != nil {
		t.Fatalf("B remove stale: %v", err)
	}
	lnB, err := net.Listen(unixNetwork, path)
	if err != nil {
		t.Fatalf("B bind: %v", err)
	}
	defer lnB.Close()
	if err := release(); err != nil {
		t.Fatalf("release takeover lock: %v", err)
	}

	// A unblocks, re-probes under the lock, sees B live, and refuses.
	select {
	case err := <-aErr:
		if err == nil || !strings.Contains(err.Error(), alreadyServedMsg) {
			t.Fatalf("A takeover = %v, want already-served refusal", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("A never returned after the lock was released")
	}
	if conn, err := net.Dial(unixNetwork, path); err != nil {
		t.Fatalf("B's socket no longer accepts after A's takeover attempt: %v", err)
	} else {
		conn.Close()
	}
}

// TestCtlTakeoverLockTimeout covers the lock-acquisition failure arm: a
// holder that never releases makes the takeover fail with the lock error
// after the acquire budget, leaving the path alone.
func TestCtlTakeoverLockTimeout(t *testing.T) {
	path := sockPath(t)
	listenErr := stageStaleBindFailure(t, path)

	release, err := agentslock.AcquireFileLock(path)
	if err != nil {
		t.Fatalf("acquire takeover lock: %v", err)
	}
	defer func() { _ = release() }()

	if _, err := takeOverStaleSocket(path, listenErr); err == nil ||
		!strings.Contains(err.Error(), "takeover lock") {
		t.Fatalf("takeover under a never-released lock = %v, want takeover-lock failure", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("socket file touched despite failed lock acquisition: %v", err)
	}
}

// TestCtlSecondBind proves one-service-per-socket: a second control plane on
// the same path refuses to steal a live listener.
func TestCtlSecondBind(t *testing.T) {
	c := NewControl(sockPath(t), fakeState{}, func() {})
	startCtl(t, c)

	second := NewControl(c.SocketPath(), fakeState{}, func() {})
	err := second.Serve(context.Background())
	if err == nil || !strings.Contains(err.Error(), alreadyServedMsg) {
		t.Fatalf("second Serve = %v, want already-served error", err)
	}
}

// TestCtlBadDir covers the unrecoverable bind failure: the socket's parent
// directory does not exist, so listen, the stale probe, and the fsops removal
// all fail, and Serve reports it.
func TestCtlBadDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "ctl.sock")
	err := NewControl(path, fakeState{}, func() {}).Serve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stale-file removal failed") {
		t.Fatalf("Serve = %v, want bind failure with removal detail", err)
	}
}

// TestCtlChmodFail covers secureControlSocket's failure arm directly: if the
// socket file cannot be restricted to owner-only, the listener is closed and
// the bind fails rather than serving with lax permissions.
func TestCtlChmodFail(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen(unixNetwork, filepath.Join(dir, "real.sock"))
	if err != nil {
		t.Fatalf("prep listen: %v", err)
	}
	got, err := secureControlSocket(ln, filepath.Join(dir, "no-such.sock"))
	if err == nil || got != nil {
		t.Fatalf("secureControlSocket = (%v, %v), want chmod error", got, err)
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("listener still accepting after failed securing; want closed")
	}
}

// TestBothListenersShutdown is the transport-layer graceful-shutdown
// contract: the HTTP/SSE edge and the UDS control plane serve under one run
// context, and cancelling it unwinds both within the deadline — the control
// socket file is unlinked on the way out.
func TestBothListenersShutdown(t *testing.T) {
	srv := New("127.0.0.1:0", fakeState{}, events.NewInProcBus())
	ctl := NewControl(sockPath(t), fakeState{}, func() {})

	ctx, cancel := context.WithCancel(context.Background())
	edgeDone := make(chan error, 1)
	ctlDone := make(chan error, 1)
	go func() { edgeDone <- srv.Serve(ctx) }()
	go func() { ctlDone <- ctl.Serve(ctx) }()

	// Both listeners answer while running.
	addr := waitForAddr(t, srv)
	resp, err := nethttp.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz on live edge: %v", err)
	}
	resp.Body.Close()
	waitForSocket(t, ctl.SocketPath())
	if _, err := NewControlClient(ctl.SocketPath()).Status(ctx); err != nil {
		t.Fatalf("Status on live control plane: %v", err)
	}

	cancel()
	for name, done := range map[string]chan error{"edge": edgeDone, "control": ctlDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s Serve returned %v, want nil", name, err)
			}
		case <-time.After(defaultShutdownTimeout + 2*time.Second):
			t.Fatalf("%s Serve did not return within the shutdown deadline", name)
		}
	}
	if _, err := os.Stat(ctl.SocketPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("control socket still on disk after shutdown: %v", err)
	}
}

// TestListenControlNonAddrInUseErrorPropagates pins the takeover trigger
// classification: a bind failure that is NOT EADDRINUSE (here: missing parent
// directory) must propagate untouched — takeover would otherwise run its
// remove path against an arbitrary listen error.
func TestListenControlNonAddrInUseErrorPropagates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-parent", "ctl.sock")
	if _, err := listenControl(path); err == nil {
		t.Fatal("listenControl on a missing parent should fail")
	} else if strings.Contains(err.Error(), "refusing takeover") ||
		strings.Contains(err.Error(), "already served") {
		t.Fatalf("non-EADDRINUSE bind error entered takeover: %v", err)
	}
}

// TestCtlTakeoverRefusesNonSocket pins the occupant check: when the path
// holds a REGULAR FILE (not a socket), takeover must refuse rather than
// delete it — even though a dial probe fails just like a stale socket.
func TestCtlTakeoverRefusesNonSocket(t *testing.T) {
	path := sockPath(t)
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := takeOverStaleSocket(path, syscall.EADDRINUSE); err == nil {
		t.Fatal("takeover over a regular file must refuse")
	} else if !strings.Contains(err.Error(), "refusing takeover") {
		t.Fatalf("expected refusing-takeover error, got: %v", err)
	}
	// The imposter file must be untouched.
	if b, err := os.ReadFile(path); err != nil || string(b) != "not a socket" {
		t.Fatalf("non-socket occupant was modified/removed: %v %q", err, b)
	}
}
