//go:build unix

package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
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
// at path. A leftover socket file from a crashed service is recovered by the
// serialized takeover protocol below. The socket file is then restricted to
// 0600 so only the owning user may connect at all — the first fence of the
// §2A "filesystem-permission + peer-uid" stop gate.
//
// Stale-takeover protocol (closes the probe→remove→rebind TOCTOU): a bare
// listen is attempted FIRST — on a fresh path it succeeds and no recovery is
// involved. Only when the bind fails on an existing socket file does takeover
// start, and every takeover step — dial probe, stale-file removal, re-bind —
// runs while HOLDING the repo's interprocess advisory lock
// (agentslock.AcquireFileLock on the socket path). Two concurrent starters
// therefore serialize: the loser either blocks on the lock until the winner
// has bound and then sees it answer the under-lock probe, or fails its own
// bare listen against the winner's fresh socket and takes the same
// under-lock path. Without the lock, starter A could probe a dead socket,
// starter B could meanwhile remove it and bind a LIVE one, and A would then
// remove B's live socket and bind over it — two servers each believing they
// own the control plane.
func listenControl(path string) (net.Listener, error) {
	ln, err := net.Listen(unixNetwork, path)
	if err != nil {
		// Takeover is only sound when the bind provably failed because a
		// socket file already occupies the path (EADDRINUSE). Any other bind
		// error (permissions, missing parent, path oddities) propagates
		// untouched — entering takeover there could remove an unrelated file
		// that happens to live at the path.
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
		if ln, err = takeOverStaleSocket(path, err); err != nil {
			return nil, err
		}
	}
	return secureControlSocket(ln, path)
}

// takeOverStaleSocket handles a failed bind on an existing socket file, with
// every step under the takeover lock: if a live service answers the
// under-lock dial probe the bind failure is real (one service per socket);
// otherwise the stale file is removed (through fsops, per the cross-platform
// fs-helpers rule) and the bind retried before the lock is released.
func takeOverStaleSocket(path string, listenErr error) (net.Listener, error) {
	release, lockErr := agentslock.AcquireFileLock(path)
	if lockErr != nil {
		return nil, fmt.Errorf("service/http: control socket %q: %w (takeover lock: %v)", path, listenErr, lockErr)
	}
	defer func() { _ = release() }()

	if conn, dialErr := net.DialTimeout(unixNetwork, path, staleDialTimeout); dialErr == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("service/http: control socket %q already served by a running service: %w", path, listenErr)
	}
	// Only a socket file is ever removed: EADDRINUSE proved a bind conflict,
	// but the occupant could still have been swapped for a regular file or
	// directory between the bind and this point — refuse rather than delete
	// something that is not ours.
	if fi, statErr := os.Lstat(path); statErr != nil || fi.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("service/http: control path %q is not a socket; refusing takeover: %w", path, listenErr)
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
