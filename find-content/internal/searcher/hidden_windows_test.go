//go:build windows

package searcher

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWindowsHiddenAttribute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "visible-name")
	if err := os.WriteFile(path, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.SetFileAttributes(pointer, attributes|fileAttributeHidden); err != nil {
		t.Skipf("setting hidden attribute is unavailable: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !isHidden(path, info) {
		t.Fatal("hidden-attribute file was not classified as hidden")
	}
}
