//go:build windows

package links

import (
	"os"
	"syscall"
)

// AreHardlinked checks whether two paths share the same inode (file index on Windows).
func AreHardlinked(a, b string) (bool, error) {
	infoA, err := os.Lstat(a)
	if err != nil {
		return false, err
	}
	infoB, err := os.Lstat(b)
	if err != nil {
		return false, err
	}

	// On Windows, FileIndex in Win32FileAttributeData is the equivalent of inode.
	// We use Sys() which returns *syscall.Win32FileAttributeData on Windows,
	// but for hard link detection we need to use the file ID via GetFileInformationByHandle.
	// As a pragmatic fallback, compare nlink count and size — not perfect but
	// avoids requiring unsafe/windows-only APIs in the main build.
	_ = infoA
	_ = infoB

	// Use syscall.GetFileInformationByHandle for accurate inode-equivalent comparison.
	aHandle, err := openFileForStat(a)
	if err != nil {
		return false, err
	}
	defer syscall.CloseHandle(aHandle)

	bHandle, err := openFileForStat(b)
	if err != nil {
		return false, err
	}
	defer syscall.CloseHandle(bHandle)

	var aInfo, bInfo syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(aHandle, &aInfo); err != nil {
		return false, err
	}
	if err := syscall.GetFileInformationByHandle(bHandle, &bInfo); err != nil {
		return false, err
	}

	return aInfo.FileIndexHigh == bInfo.FileIndexHigh &&
		aInfo.FileIndexLow == bInfo.FileIndexLow &&
		aInfo.VolumeSerialNumber == bInfo.VolumeSerialNumber, nil
}

// hasMultipleHardLinks reports whether the file at path has more than one
// hard link (NumberOfLinks > 1). A Windows managed file link is a hard link
// with no reparse point, so the canonical source and every managed link to
// it share a link count >= 2; a standalone file dot-agents wrote itself has
// a link count of 1. This is how a managed file link is distinguished from
// our own rendered output when there is no target to resolve.
func hasMultipleHardLinks(path string) bool {
	h, err := openFileForStat(path)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return false
	}
	return info.NumberOfLinks > 1
}

func openFileForStat(path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	return syscall.CreateFile(
		p,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS, // needed for directories
		0,
	)
}
