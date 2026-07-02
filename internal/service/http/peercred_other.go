//go:build unix && !linux && !darwin

package http

import (
	"errors"
	"net"
)

// errPeerCredUnsupported reports that this POSIX platform has no wired
// peer-credential reader yet; authorizeStop therefore refuses every stop
// request, which fails safe — the §2A gate ("filesystem-permission +
// peer-uid") never silently degrades to filesystem permission alone.
var errPeerCredUnsupported = errors.New(
	"service/http: peer-credential check not implemented on this platform; stop refused")

// defaultPeerUID fails on POSIX platforms other than Linux/macOS until a
// per-platform getsockopt (e.g. LOCAL_PEERCRED on FreeBSD) is wired.
func defaultPeerUID(net.Conn) (uint32, error) {
	return 0, errPeerCredUnsupported
}
