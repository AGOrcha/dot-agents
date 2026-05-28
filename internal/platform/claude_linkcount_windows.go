//go:build windows

package platform

import "syscall"

// hasMultipleHardLinks reports whether path has more than one directory entry
// referencing its file index (NumberOfLinks > 1). On Windows a managed file
// link is a hard link with no reparse point, so this is how a managed
// hard-linked file is distinguished from a standalone regular file when no
// canonical source path is available to compare against.
//
// Relocated from commands/internal/lifecycle/linkcount_windows.go (the
// lifecycle HasMultipleHardLinks func-var seam) per the P1 fold-back at
// .agents/active/fold-back/p1-hasmultiplehardlinks-move-deferred-to-p3.md.
// The lifecycle seam is now backed by this helper so backup.go and
// status.go cannot silently diverge.
func hasMultipleHardLinks(path string) bool {
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

// HasMultipleHardLinks is the exported entry point that the
// commands/internal/lifecycle HasMultipleHardLinks func-var seam now
// delegates to. See claude_linkcount_unix.go for rationale.
func HasMultipleHardLinks(path string) bool { return hasMultipleHardLinks(path) }
