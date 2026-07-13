//go:build linux || darwin

package http

import (
	"fmt"
	"net"
)

// peerCredErrFmt wraps every peer-credential failure with one consistent
// prefix so authorizeStop renders a uniform refusal reason.
const peerCredErrFmt = "service/http: peer credential: %w"

// defaultPeerUID resolves the connecting peer's uid from a Unix-domain socket
// connection using the platform getsockopt (SO_PEERCRED on Linux,
// LOCAL_PEERCRED on macOS — see the per-OS getsockoptPeerUID). This is what
// makes the §2A stop gate a kernel-attested identity check rather than a
// network ACL: the kernel, not the client, reports who is on the other end.
func defaultPeerUID(conn net.Conn) (uint32, error) {
	return peerUIDFromConn(conn, getsockoptPeerUID)
}

// peerUIDFromConn extracts the peer uid of conn via getUID, which receives
// the raw socket descriptor. Split from defaultPeerUID so tests can exercise
// the failure branches with an injected getUID.
func peerUIDFromConn(conn net.Conn, getUID func(fd int) (uint32, error)) (uint32, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("service/http: peer credential requires a unix socket, got %T", conn)
	}
	raw, err := uc.SyscallConn()
	var uid uint32
	if err == nil {
		var credErr error
		if ctlErr := raw.Control(func(fd uintptr) { uid, credErr = getUID(int(fd)) }); ctlErr != nil {
			err = ctlErr
		} else {
			err = credErr
		}
	}
	if err != nil {
		return 0, fmt.Errorf(peerCredErrFmt, err)
	}
	return uid, nil
}
