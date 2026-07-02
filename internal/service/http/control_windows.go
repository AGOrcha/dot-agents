//go:build windows

package http

import (
	"context"
	"errors"
	"net"
)

// DEFERRED WORK — tracked in plan r3-background-worker-service (the
// http-server and cobra-surface task notes carry the requirement): implement
// the Windows named-pipe control plane (`\\.\pipe\...`) behind this same
// listenControl/dialControl/defaultPeerUID seam. Spec §2A's cross-platform
// note names go-winio (or a named-pipe equivalent) behind one small dialer
// abstraction, and requires the named-pipe path to be EXERCISED ON THE
// WINDOWS BOX before it ships — "not assumed to mirror the UDS path"
// (§2A cross-platform note; the task note repeats the requirement). Two
// things block doing that here: go-winio is only an indirect dependency
// today, and promoting it is a go.mod change outside this task's write_scope
// (internal/service/http/ only); and no Windows-box verification has run.
// Until that lands, the control plane fails fast and clearly on Windows
// instead of shipping a half-broken pipe implementation. The stop
// authorization equivalent on Windows is the named-pipe client token
// (per §2A), to be wired when the pipe listener lands.
//
// The HTTP/SSE edge (Server) is fully functional on Windows; only the local
// control plane (da service status/stop over the socket) is gated.

// errControlUnsupportedWindows is returned by every control-plane entry point
// on Windows until the named-pipe transport lands.
var errControlUnsupportedWindows = errors.New(
	"service/http: named-pipe control plane not yet implemented on windows (spec §2A cross-platform note)")

// listenControl fails fast on Windows: the named-pipe listener is not yet
// implemented (see the deferred-work note above).
func listenControl(string) (net.Listener, error) {
	return nil, errControlUnsupportedWindows
}

// dialControl fails fast on Windows: the named-pipe dialer is not yet
// implemented (see the deferred-work note above).
func dialControl(context.Context, string) (net.Conn, error) {
	return nil, errControlUnsupportedWindows
}

// defaultPeerUID fails fast on Windows: peer identity will be the named-pipe
// client token once the pipe transport lands (spec §2A cross-platform note).
func defaultPeerUID(net.Conn) (uint32, error) {
	return 0, errControlUnsupportedWindows
}
