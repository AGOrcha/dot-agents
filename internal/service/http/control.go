package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
)

// Control-plane wire protocol (spec §2A, "local control plane" row): the
// client writes exactly one JSON request object on the connection, the server
// answers with exactly one JSON response object and closes. No HTTP, no TLS,
// no framing beyond JSON's own delimiting — the peer is same-host by
// construction (Unix-domain socket / named pipe), so the §2A selection rule
// resolves this surface to the local high-efficiency transport.
const (
	// ControlOpStatus asks the running service for its scheduler task-health
	// snapshot (the same projection the HTTP edge serves at /api/tasks).
	ControlOpStatus = "status"
	// ControlOpStop asks the running service to shut down. It is authorized
	// by the socket file permission plus a peer-credential check (spec OQ4 as
	// reshaped by §2A) — NOT by a network ACL.
	ControlOpStop = "stop"
)

// controlIOTimeout bounds a single control exchange on both ends so a stuck
// peer can never wedge the accept loop or a CLI invocation.
const controlIOTimeout = 5 * time.Second

// maxControlRequestBytes caps how much a client may send: requests are one
// tiny JSON object, so anything larger is malformed by definition.
const maxControlRequestBytes = 4096

var (
	// ErrControlUnavailable is returned by ControlClient when no service is
	// listening on the control socket — the §D6 "CLI-routes-when-present"
	// signal that callers use to fall back to direct cold-file operation.
	ErrControlUnavailable = errors.New("service/http: control socket unavailable (is da service running?)")
	// ErrStopNotAuthorized is returned when a stop request's peer credential
	// does not match the service owner (spec OQ4 / §2A: filesystem-permission
	// + peer-uid check replaces the old loopback network ACL).
	ErrStopNotAuthorized = errors.New("service/http: stop refused, peer not authorized")
)

// controlRequest is the single message a client sends per connection.
type controlRequest struct {
	Op string `json:"op"`
}

// controlResponse is the single message the server answers with.
type controlResponse struct {
	OK    bool                  `json:"ok"`
	Error string                `json:"error,omitempty"`
	Tasks []scheduler.TaskState `json:"tasks,omitempty"`
}

// Control is the local control-plane listener of the service transport layer
// (spec §2A "local control plane" row): a Unix-domain socket (named pipe on
// Windows — not yet implemented, see control_windows.go) that answers
// `da service status` / `da service stop` and the §D6 CLI-routes-when-present
// path. Stop is authorized by socket file permission plus a peer-credential
// check (SO_PEERCRED on Linux, LOCAL_PEERCRED on macOS), never by a network
// ACL. Construct with NewControl.
type Control struct {
	socketPath  string
	provider    StateProvider
	requestStop func()

	// peerUID resolves the connecting peer's uid; defaults to the platform
	// peer-credential reader and is overridable in tests to exercise the
	// authorization branches deterministically.
	peerUID func(net.Conn) (uint32, error)
	// ownerUID is the uid a stop peer must match; defaults to the effective
	// uid of this process.
	ownerUID int

	wg sync.WaitGroup
}

// NewControl builds the control-plane listener for socketPath, projecting
// provider.State() for status requests and invoking requestStop when an
// authorized peer asks the service to shut down. requestStop is expected to
// cancel the runtime context that Serve (and the HTTP edge) run under.
func NewControl(socketPath string, provider StateProvider, requestStop func()) *Control {
	return &Control{
		socketPath:  socketPath,
		provider:    provider,
		requestStop: requestStop,
		peerUID:     defaultPeerUID,
		ownerUID:    os.Geteuid(),
	}
}

// SocketPath returns the socket path the control plane binds on Serve.
func (c *Control) SocketPath() string { return c.socketPath }

// Serve binds the control socket and answers requests until ctx is cancelled,
// then closes the listener (which unlinks the socket file) and waits for
// in-flight exchanges — each bounded by controlIOTimeout — to finish. It
// returns nil on a clean ctx-driven shutdown.
func (c *Control) Serve(ctx context.Context) error {
	ln, err := listenControl(c.socketPath)
	if err != nil {
		return err
	}
	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
		case <-closed:
		}
		_ = ln.Close()
	}()

	defer c.wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("service/http: control accept: %w", err)
		}
		c.wg.Add(1)
		go c.handleConn(conn)
	}
}

// handleConn performs one request/response exchange. The stop callback fires
// only after the response has been written, so the requesting CLI always
// hears the acknowledgement before the service begins tearing itself down.
func (c *Control) handleConn(conn net.Conn) {
	defer c.wg.Done()
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(controlIOTimeout))

	var req controlRequest
	if err := json.NewDecoder(io.LimitReader(conn, maxControlRequestBytes)).Decode(&req); err != nil {
		writeControlResponse(conn, controlResponse{Error: "malformed control request: " + err.Error()})
		return
	}
	resp, stopAfter := c.dispatch(conn, req)
	writeControlResponse(conn, resp)
	if stopAfter {
		c.requestStop()
	}
}

// dispatch routes one decoded request, returning the response and whether the
// stop callback should fire after the response is written.
func (c *Control) dispatch(conn net.Conn, req controlRequest) (controlResponse, bool) {
	switch req.Op {
	case ControlOpStatus:
		return controlResponse{OK: true, Tasks: c.provider.State()}, false
	case ControlOpStop:
		if err := c.authorizeStop(conn); err != nil {
			return controlResponse{Error: err.Error()}, false
		}
		return controlResponse{OK: true}, true
	default:
		return controlResponse{Error: fmt.Sprintf("unknown control op %q", req.Op)}, false
	}
}

// authorizeStop enforces the §2A stop gate: the connecting peer's credential
// (uid) must match the service owner's effective uid. The socket file's 0600
// mode is the first fence; this check is the second, and the one that holds
// even where the platform is lax about socket-file permission enforcement.
func (c *Control) authorizeStop(conn net.Conn) error {
	uid, err := c.peerUID(conn)
	if err != nil {
		return fmt.Errorf("%w (peer credential unavailable: %v)", ErrStopNotAuthorized, err)
	}
	if int(uid) != c.ownerUID {
		return fmt.Errorf("%w (peer uid %d, owner uid %d)", ErrStopNotAuthorized, uid, c.ownerUID)
	}
	return nil
}

// writeControlResponse encodes resp on conn; a write failure is deliberately
// dropped — the peer is gone and the exchange is already bounded by deadline.
func writeControlResponse(conn net.Conn, resp controlResponse) {
	_ = json.NewEncoder(conn).Encode(resp)
}

// ControlClient is the CLI-side half of the control plane: `da service
// status`/`stop` and any §D6 routes-when-present command dial the socket
// through it. A dial failure surfaces as ErrControlUnavailable so callers can
// distinguish "service not running — fall back to cold files" from a protocol
// error.
type ControlClient struct {
	socketPath string
}

// NewControlClient builds a client for the control socket at socketPath.
func NewControlClient(socketPath string) *ControlClient {
	return &ControlClient{socketPath: socketPath}
}

// Status fetches the running service's scheduler task-health snapshot.
func (c *ControlClient) Status(ctx context.Context) ([]scheduler.TaskState, error) {
	resp, err := c.roundTrip(ctx, ControlOpStatus)
	if err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// Stop asks the running service to shut down. The server enforces the
// peer-credential gate; an unauthorized caller receives the server's
// ErrStopNotAuthorized text inside the returned error.
func (c *ControlClient) Stop(ctx context.Context) error {
	_, err := c.roundTrip(ctx, ControlOpStop)
	return err
}

// roundTrip performs the one-request/one-response control exchange for op.
func (c *ControlClient) roundTrip(ctx context.Context, op string) (*controlResponse, error) {
	conn, err := dialControl(ctx, c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrControlUnavailable, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(exchangeDeadline(ctx))

	if err := json.NewEncoder(conn).Encode(controlRequest{Op: op}); err != nil {
		return nil, fmt.Errorf("service/http: control %s request: %w", op, err)
	}
	var resp controlResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("service/http: control %s response: %w", op, err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("service/http: control %s: %s", op, resp.Error)
	}
	return &resp, nil
}

// exchangeDeadline bounds a client exchange by the sooner of the default
// control I/O timeout and the caller's context deadline.
func exchangeDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(controlIOTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return deadline
}
