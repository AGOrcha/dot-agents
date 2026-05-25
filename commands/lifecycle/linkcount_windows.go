//go:build windows

package lifecycle

import "syscall"

// HasMultipleHardLinks reports whether path has more than one directory entry
// referencing its file index (NumberOfLinks > 1). On Windows a managed file
// link is a hard link with no reparse point, so this is how a managed
// hard-linked file is distinguished from a standalone regular file when no
// canonical source path is available to compare against.
//
// Exported during the t08→t09 window per SHAPE.md OD-2 so doctor.go (still in
// root before t09) can keep importing the helper via lifecycle.HasMultipleHardLinks.
// Once t09 lands doctor.go in this same package, the cross-package consumer
// disappears and this becomes the only call site — at which point t09 can
// lowercase the name back to package-private.
func HasMultipleHardLinks(path string) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(
		p,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
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
