//go:build windows

package linktest

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createManagedLink mirrors the production Windows link model: a directory
// junction for directory targets, a hard link for file targets. Neither
// needs SeCreateSymbolicLinkPrivilege.
func createManagedLink(target, link string) error {
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("stat link target %s: %w", target, err)
	}
	if info.IsDir() {
		return junction(link, target)
	}
	if err := os.Link(target, link); err != nil {
		return fmt.Errorf("hardlink %s -> %s: %w", link, target, err)
	}
	return nil
}

// createDanglingLink produces a broken reparse point: create a real
// directory, junction to it, then remove the directory. os.Readlink on the
// junction still returns missingTarget, but os.Stat of it now fails — the
// same shape the broken-link detectors expect. A hard link cannot dangle
// (its target must exist), so a junction is the only viable analogue.
func createDanglingLink(link, missingTarget string) error {
	if err := os.MkdirAll(missingTarget, 0o755); err != nil {
		return fmt.Errorf("seed dangling target %s: %w", missingTarget, err)
	}
	if err := junction(link, missingTarget); err != nil {
		return err
	}
	if err := os.RemoveAll(missingTarget); err != nil {
		return fmt.Errorf("remove dangling target %s: %w", missingTarget, err)
	}
	return nil
}

// junction creates an NTFS directory junction at link pointing to target.
// It uses the Win32 reparse-point API directly (open the empty directory
// with FILE_FLAG_OPEN_REPARSE_POINT | FILE_FLAG_BACKUP_SEMANTICS, then
// DeviceIoControl FSCTL_SET_REPARSE_POINT with a MountPointReparseBuffer).
// No shell, no cmd.exe, no exec at all, so caller-controlled paths cannot be
// reinterpreted as commands.
func junction(link, target string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve junction target %s: %w", target, err)
	}
	if err := setJunction(link, absTarget); err != nil {
		return fmt.Errorf("create junction %s -> %s: %w", link, absTarget, err)
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

// setJunction makes link an NTFS directory junction (mount point) pointing
// at absTarget using only syscalls. absTarget must be absolute.
func setJunction(link, absTarget string) error {
	if err := os.Mkdir(link, 0o755); err != nil {
		return fmt.Errorf("create junction directory: %w", err)
	}

	substitute := `\??\` + absTarget
	subUTF16, err := syscall.UTF16FromString(substitute)
	if err != nil {
		return fmt.Errorf("encode substitute name: %w", err)
	}
	printUTF16, err := syscall.UTF16FromString(absTarget)
	if err != nil {
		return fmt.Errorf("encode print name: %w", err)
	}
	subBytes := uint16((len(subUTF16) - 1) * 2)
	printBytes := uint16((len(printUTF16) - 1) * 2)

	hdr := mountPointReparseBuffer{
		ReparseTag:           windows.IO_REPARSE_TAG_MOUNT_POINT,
		SubstituteNameOffset: 0,
		SubstituteNameLength: subBytes,
		PrintNameOffset:      subBytes + 2,
		PrintNameLength:      printBytes,
	}
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

	pLink, err := syscall.UTF16PtrFromString(link)
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
