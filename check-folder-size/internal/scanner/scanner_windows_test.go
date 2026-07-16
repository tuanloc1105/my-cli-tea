//go:build windows

package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestScanDirectoryIncludesWindowsHiddenAttribute(t *testing.T) {
	root := t.TempDir()
	name := "hidden-without-dot.txt"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(pathPtr, attributes|windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Skipf("setting hidden attribute unavailable: %v", err)
	}

	result, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeAllocated})
	if err != nil {
		t.Fatal(err)
	}
	if item := findItem(t, result.Items, name); item.Type != ItemTypeFile {
		t.Fatalf("hidden item = %#v", item)
	}
}
