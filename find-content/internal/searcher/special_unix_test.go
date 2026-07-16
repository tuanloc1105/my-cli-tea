//go:build !windows

package searcher

import (
	"net"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSearchSkipsFIFOAndSocketWithoutOpening(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "pipe.txt")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	socketPath := filepath.Join(root, "socket.txt")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	summary, events, err := collectSearch(defaultOptions(root, "needle"), nil)
	if err != nil || summary.Matches != 0 || summary.PartialErrors != 0 || len(events) != 0 {
		t.Fatalf("special-file summary/events/error = %+v/%+v/%v", summary, events, err)
	}
}
