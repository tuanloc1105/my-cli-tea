//go:build darwin || linux

package searcher

import (
	"errors"
	"os"
	"sort"
	"syscall"
)

func openRegularFile(path string) (fileHandle, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errUnsafeFileType
		}
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), path), nil
}

func readDirectoryNoFollow(path string) ([]os.DirEntry, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			return nil, errUnsafeFileType
		}
		return nil, err
	}
	directory := os.NewFile(uintptr(descriptor), path)
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	return entries, nil
}
