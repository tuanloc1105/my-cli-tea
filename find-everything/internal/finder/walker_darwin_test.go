//go:build darwin

package finder

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinHiddenFlagIsIncluded(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "native-hidden.log")
	writeTestFile(t, path, 1)

	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	originalFlags := int(stat.Flags)
	if err := unix.Chflags(path, originalFlags|unix.UF_HIDDEN); err != nil {
		t.Fatalf("Chflags(UF_HIDDEN): %v", err)
	}
	t.Cleanup(func() {
		if err := unix.Chflags(path, originalFlags); err != nil {
			t.Errorf("restore flags: %v", err)
		}
	})

	if err := unix.Stat(path, &stat); err != nil {
		t.Fatalf("Stat(%q) after Chflags: %v", path, err)
	}
	if stat.Flags&uint32(unix.UF_HIDDEN) == 0 {
		t.Fatal("UF_HIDDEN was not set")
	}

	results := runTestFinder(t, base, "*.log", testFinderOptions())
	assertPathNames(t, results.Files, "native-hidden.log")
}
