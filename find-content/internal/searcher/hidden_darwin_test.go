//go:build darwin

package searcher

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDarwinFilesystemHiddenFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "visible-name")
	if err := os.WriteFile(path, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Chflags(path, ufHidden); err != nil {
		t.Skipf("setting UF_HIDDEN is unavailable: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !isHidden(path, info) {
		t.Fatal("UF_HIDDEN file was not classified as hidden")
	}
}
