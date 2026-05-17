//go:build windows

package links

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createLink creates a managed link at linkPath pointing to target using a
// Windows-native mechanism that needs no SeCreateSymbolicLinkPrivilege
// (works without Developer Mode or admin): a directory junction for
// directory targets, a hard link for file targets. This is the
// pnpm-analogous model.
//
// There is deliberately no copy fallback. A copied file is not a managed
// reference — it would not resolve to the canonical inode, so the link
// contract (IsManagedLink / CollectBrokenUserLinks / doctor repair) would
// silently treat it as unmanaged. Failing loudly is correct here.
func createLink(target, linkPath string) error {
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("stat link target %s: %w", target, err)
	}
	if info.IsDir() {
		return createJunction(linkPath, target)
	}
	if err := os.Link(target, linkPath); err != nil {
		return fmt.Errorf("hardlink %s -> %s: %w", linkPath, target, err)
	}
	return nil
}

// CreateManagedLink exposes the production Windows link primitive (junction
// for directories, hard link for files) so test-support code (internal/
// linktest) can mirror production behavior without re-implementing the
// Win32 reparse-point machinery. This is the single source of truth;
// linktest must not drift from it.
func CreateManagedLink(target, link string) error {
	return createLink(target, link)
}

// CreateJunction exposes the production directory-junction primitive for
// the same test-support reuse reason as CreateManagedLink. Signature is
// (link, target) to match the junction-creation convention.
func CreateJunction(link, target string) error {
	return createJunction(link, target)
}

// createJunction creates an NTFS directory junction at linkPath pointing to
// target. A junction (IO_REPARSE_TAG_MOUNT_POINT) needs no special
// privilege, unlike a true symlink. Go's os.Readlink and os.Lstat resolve
// junctions as symlinks, so the existing readlink-based contract checks see
// a junction as a managed link.
//
// The junction is created with the Win32 reparse-point API directly (open
// the empty directory with FILE_FLAG_OPEN_REPARSE_POINT |
// FILE_FLAG_BACKUP_SEMANTICS, then DeviceIoControl FSCTL_SET_REPARSE_POINT
// with a MountPointReparseBuffer). No shell, no cmd.exe, no exec at all, so
// caller-controlled paths cannot be reinterpreted as commands.
func createJunction(linkPath, target string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve junction target %s: %w", target, err)
	}
	if err := setJunction(linkPath, absTarget); err != nil {
		return fmt.Errorf("create junction %s -> %s: %w", linkPath, absTarget, err)
	}
	return nil
}

// mountPointReparseBuffer mirrors the Win32 MOUNTPOINT_REPARSE_BUFFER. The
// path bytes are an open-ended array; we serialize the fixed header then the
// path data manually so no shell ever parses the (caller-controlled) path.
type mountPointReparseBuffer struct {
	ReparseTag           uint32
	ReparseDataLength    uint16
	Reserved             uint16
	SubstituteNameOffset uint16
	SubstituteNameLength uint16
	PrintNameOffset      uint16
	PrintNameLength      uint16
}

// setJunction makes linkPath an NTFS directory junction (mount point)
// pointing at absTarget using only syscalls. absTarget must be absolute.
func setJunction(linkPath, absTarget string) error {
	if err := os.Mkdir(linkPath, 0o755); err != nil {
		return fmt.Errorf("create junction directory: %w", err)
	}

	// The substitute name is the NT object path: \??\C:\...
	substitute := `\??\` + absTarget
	subUTF16, err := syscall.UTF16FromString(substitute)
	if err != nil {
		return fmt.Errorf("encode substitute name: %w", err)
	}
	printUTF16, err := syscall.UTF16FromString(absTarget)
	if err != nil {
		return fmt.Errorf("encode print name: %w", err)
	}
	// UTF16FromString appends a NUL terminator we must not count in lengths.
	subBytes := uint16((len(subUTF16) - 1) * 2)
	printBytes := uint16((len(printUTF16) - 1) * 2)

	hdr := mountPointReparseBuffer{
		ReparseTag:           windows.IO_REPARSE_TAG_MOUNT_POINT,
		SubstituteNameOffset: 0,
		SubstituteNameLength: subBytes,
		PrintNameOffset:      subBytes + 2,
		PrintNameLength:      printBytes,
	}
	// PathBuffer layout: substitute + NUL + print + NUL (UTF-16).
	pathBuf := make([]uint16, 0, len(subUTF16)+len(printUTF16))
	pathBuf = append(pathBuf, subUTF16...)
	pathBuf = append(pathBuf, printUTF16...)
	pathByteLen := len(pathBuf) * 2

	const headerSize = 8 // ReparseTag(4)+ReparseDataLength(2)+Reserved(2)
	hdr.ReparseDataLength = uint16(8 + pathByteLen)

	buf := make([]byte, headerSize+8+pathByteLen)
	*(*mountPointReparseBuffer)(unsafe.Pointer(&buf[0])) = hdr
	copy(
		(*[1 << 20]byte)(unsafe.Pointer(&buf[16]))[:pathByteLen:pathByteLen],
		(*[1 << 20]byte)(unsafe.Pointer(&pathBuf[0]))[:pathByteLen:pathByteLen],
	)

	pLink, err := syscall.UTF16PtrFromString(linkPath)
	if err != nil {
		return fmt.Errorf("encode link path: %w", err)
	}
	h, err := syscall.CreateFile(
		pLink,
		syscall.GENERIC_WRITE,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return fmt.Errorf("open junction directory: %w", err)
	}
	defer syscall.CloseHandle(h)

	var bytesReturned uint32
	if err := syscall.DeviceIoControl(
		h,
		windows.FSCTL_SET_REPARSE_POINT,
		&buf[0],
		uint32(len(buf)),
		nil,
		0,
		&bytesReturned,
		nil,
	); err != nil {
		return fmt.Errorf("set reparse point: %w", err)
	}
	return nil
}
