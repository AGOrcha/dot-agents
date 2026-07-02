//go:build unix

package http

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// unixNetwork is the net.Listen/Dial network of the POSIX control transport
// (spec §2A cross-platform note: Unix-domain socket on POSIX, named pipe on
// Windows, behind this one small listener/dialer seam).
const unixNetwork = "unix"

// staleDialTimeout bounds the probe that distinguishes a live service from a
// stale socket file left behind by a crashed one.
const staleDialTimeout = 250 * time.Millisecond

// listenControl binds the local control-plane listener: a Unix-domain socket
// at path. A leftover socket file from a crashed service is recovered by
// dial-probing it — if nothing answers, the stale file is removed (through
// fsops, per the cross-platform fs-helpers rule) and the bind retried. The
// socket file is then restricted to 0600 so only the owning user may connect
// at all — the first fence of the §2A "filesystem-permission + peer-uid"
// stop gate.
func listenControl(path string) (net.Listener, error) {
	ln, err := net.Listen(unixNetwork, path)
	if err != nil {
		if ln, err = relistenStaleSocket(path, err); err != nil {
			return nil, err
		}
	}
	return secureControlSocket(ln, path)
}

// relistenStaleSocket handles a failed bind on an existing socket file: if a
// live service answers a dial probe the bind failure is real (one service per
// socket), otherwise the stale file is removed and the bind retried.
func relistenStaleSocket(path string, listenErr error) (net.Listener, error) {
	if conn, dialErr := net.DialTimeout(unixNetwork, path, staleDialTimeout); dialErr == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("service/http: control socket %q already served by a running service: %w", path, listenErr)
	}
	if rmErr := fsops.Remove(path); rmErr != nil {
		return nil, fmt.Errorf("service/http: control socket %q: %w (stale-file removal failed: %v)", path, listenErr, rmErr)
	}
	return net.Listen(unixNetwork, path)
}

// secureControlSocket tightens the bound socket file to owner-only access,
// closing the listener if that cannot be guaranteed — an open control socket
// with lax permissions would silently widen who can talk to the service.
func secureControlSocket(ln net.Listener, path string) (net.Listener, error) {
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("service/http: restrict control socket %q: %w", path, err)
	}
	return ln, nil
}

// dialControl connects to the control socket at path, honouring ctx for the
// dial itself (exchange deadlines are set by the caller on the returned conn).
func dialControl(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, unixNetwork, path)
}
