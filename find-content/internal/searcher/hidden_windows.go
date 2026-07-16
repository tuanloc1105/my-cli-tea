//go:build windows

package searcher

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const fileAttributeHidden = 0x2

func isHidden(path string, _ os.FileInfo) bool {
	if strings.HasPrefix(filepath.Base(path), ".") {
		return true
	}
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attributes, err := syscall.GetFileAttributes(pointer)
	return err == nil && attributes&fileAttributeHidden != 0
}
