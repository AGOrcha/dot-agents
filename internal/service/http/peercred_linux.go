//go:build linux

package http

import "golang.org/x/sys/unix"

// getsockoptPeerUID reads the peer uid of the connected Unix-domain socket fd
// via SO_PEERCRED — the Linux arm of the §2A peer-credential stop gate.
func getsockoptPeerUID(fd int) (uint32, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}
