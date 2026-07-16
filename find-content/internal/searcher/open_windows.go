//go:build windows

package searcher

import (
	"os"
	"sort"
	"syscall"
)

const (
	fileShareDelete          = 0x00000004
	fileFlagOpenReparsePoint = 0x00200000
	fileFlagBackupSemantics  = 0x02000000
)

func openRegularFile(path string) (fileHandle, error) {
	return openWindowsPath(path, 0)
}

func readDirectoryNoFollow(path string) ([]os.DirEntry, error) {
	directory, err := openWindowsPath(path, fileFlagBackupSemantics)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errUnsafeFileType
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	return entries, nil
}

func openWindowsPath(path string, extraFlags uint32) (*os.File, error) {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(
		pointer,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|fileShareDelete,
		nil,
		syscall.OPEN_EXISTING,
		fileFlagOpenReparsePoint|extraFlags,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
