//go:build windows

package finder

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsHiddenAttributeIsIncluded(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "native-hidden.log")
	writeTestFile(t, path, 1)

	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(%q): %v", path, err)
	}
	originalAttributes, err := windows.GetFileAttributes(pathUTF16)
	if err != nil {
		t.Fatalf("GetFileAttributes(%q): %v", path, err)
	}
	hiddenAttributes := originalAttributes | windows.FILE_ATTRIBUTE_HIDDEN
	hiddenAttributes &^= windows.FILE_ATTRIBUTE_NORMAL
	if err := windows.SetFileAttributes(pathUTF16, hiddenAttributes); err != nil {
		t.Fatalf("SetFileAttributes(HIDDEN): %v", err)
	}
	t.Cleanup(func() {
		if err := windows.SetFileAttributes(pathUTF16, originalAttributes); err != nil {
			t.Errorf("restore attributes: %v", err)
		}
	})

	attributes, err := windows.GetFileAttributes(pathUTF16)
	if err != nil {
		t.Fatalf("GetFileAttributes(%q) after SetFileAttributes: %v", path, err)
	}
	if attributes&windows.FILE_ATTRIBUTE_HIDDEN == 0 {
		t.Fatal("FILE_ATTRIBUTE_HIDDEN was not set")
	}

	results := runTestFinder(t, base, "*.log", testFinderOptions())
	assertPathNames(t, results.Files, "native-hidden.log")
}
