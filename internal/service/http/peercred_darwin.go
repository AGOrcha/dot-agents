//go:build darwin

package http

import "golang.org/x/sys/unix"

// getsockoptPeerUID reads the peer uid of the connected Unix-domain socket fd
// via LOCAL_PEERCRED — the macOS arm of the §2A peer-credential stop gate.
func getsockoptPeerUID(fd int) (uint32, error) {
	cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}
