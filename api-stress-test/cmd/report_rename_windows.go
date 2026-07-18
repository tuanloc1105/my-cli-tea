//go:build windows

package cmd

import (
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceReportFile(oldPath, newPath string) error {
	oldPathPtr, err := syscall.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPathPtr, err := syscall.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}

	result, _, callErr := moveFileExW.Call(
		uintptr(unsafe.Pointer(oldPathPtr)),
		uintptr(unsafe.Pointer(newPathPtr)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if result != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}
