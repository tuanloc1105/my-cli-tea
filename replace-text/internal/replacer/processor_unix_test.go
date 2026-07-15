//go:build darwin || linux

package replacer

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadOnlyOutcomesPreserveAccessTime(t *testing.T) {
	tests := []struct {
		name      string
		content   []byte
		configure func(*Options)
	}{
		{name: "dry-run", content: []byte("old"), configure: func(options *Options) { options.DryRun = true }},
		{name: "no-match", content: []byte("text without search")},
		{name: "NUL skip", content: []byte{'o', 'l', 'd', 0}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "file.txt")
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			oldTime := time.Unix(946_684_800, 0)
			if err := os.Chtimes(path, oldTime, oldTime); err != nil {
				t.Fatal(err)
			}
			before := snapshotPathMetadata(t, path)
			options := testOptions(path)
			if test.configure != nil {
				test.configure(&options)
			}

			if _, err := Run(context.Background(), options, nil); err != nil {
				t.Fatalf("Run: %v", err)
			}
			after := snapshotPathMetadata(t, path)
			if !after.accessTime.Equal(before.accessTime) || !after.modificationTime.Equal(before.modificationTime) {
				t.Fatalf("timestamps changed: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestRunSkipsSymlinkHardlinkFIFOAndSocket(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	writeTestFile(t, target, "old", 0o600)

	tests := []struct {
		name   string
		path   string
		create func(string)
		reason SkipReason
	}{
		{
			name: "symlink",
			path: filepath.Join(dir, "link.txt"),
			create: func(path string) {
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			},
			reason: SkipSymlink,
		},
		{
			name: "hardlink",
			path: filepath.Join(dir, "hard.txt"),
			create: func(path string) {
				if err := os.Link(target, path); err != nil {
					t.Fatal(err)
				}
			},
			reason: SkipHardlink,
		},
		{
			name: "fifo",
			path: filepath.Join(dir, "pipe"),
			create: func(path string) {
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			reason: SkipNonRegular,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.create(test.path)
			summary, err := Run(context.Background(), testOptions(test.path), nil)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if summary.Skipped[test.reason] != 1 {
				t.Fatalf("summary = %+v", summary)
			}
		})
	}

	socketDir, err := os.MkdirTemp("/tmp", "replace-text-socket-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	socketPath := filepath.Join(socketDir, "socket")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	summary, err := Run(context.Background(), testOptions(socketPath), nil)
	if err != nil {
		t.Fatalf("Run socket: %v", err)
	}
	if summary.Skipped[SkipNonRegular] != 1 {
		t.Fatalf("socket summary = %+v", summary)
	}

	if got := readTestFile(t, target); got != "old" {
		t.Fatalf("target was modified through a link: %q", got)
	}
}
