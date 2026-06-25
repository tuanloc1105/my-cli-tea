package ui

import (
	"check-folder-size/internal/scanner"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPrintResultsShowsTypeAndFullNames(t *testing.T) {
	longFileName := "this-is-a-very-long-file-name-that-should-keep-its-important-suffix.txt"
	longDirName := "this-is-a-very-long-directory-name-that-should-not-be-truncated-at-the-end"

	output := captureStdout(t, func() {
		PrintResults([]scanner.ItemInfo{
			{Name: longFileName, Size: 5, Type: "file"},
			{Name: longDirName, Size: 0, Type: "directory"},
		}, "/tmp/example", "name", false)
	})

	for _, want := range []string{"Type", "file", "directory", longFileName, longDirName} {
		if !strings.Contains(output, want) {
			t.Fatalf("output does not contain %q:\n%s", want, output)
		}
	}

	if strings.Contains(output, "...") {
		t.Fatalf("output contains truncation marker:\n%s", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	defer func() {
		os.Stdout = oldStdout
	}()

	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(out)
}
