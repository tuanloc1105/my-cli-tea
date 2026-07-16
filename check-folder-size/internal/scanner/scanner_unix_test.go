//go:build darwin || linux

package scanner

import (
	"net"
	"path/filepath"
	"syscall"
	"testing"
)

func TestScanDirectoryClassifiesSpecialEntries(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("FIFO unavailable: %v", err)
	}
	socket := filepath.Join(root, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("Unix socket unavailable: %v", err)
	}
	defer listener.Close()

	result, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeAllocated})
	if err != nil {
		t.Fatal(err)
	}
	if item := findItem(t, result.Items, "fifo"); item.Type != ItemTypeOther {
		t.Fatalf("FIFO = %#v, want other", item)
	}
	if item := findItem(t, result.Items, "socket"); item.Type != ItemTypeOther {
		t.Fatalf("socket = %#v, want other", item)
	}
}
