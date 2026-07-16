package ui

import (
	"bytes"
	"check-folder-size/internal/scanner"
	"strings"
	"testing"
)

func TestPrintResultsShowsTypeAndFullNames(t *testing.T) {
	longFileName := "this-is-a-very-long-file-name-that-should-keep-its-important-suffix.txt"
	longDirName := "this-is-a-very-long-directory-name-that-should-not-be-truncated-at-the-end"

	var output bytes.Buffer
	PrintResults(&output, []scanner.ItemInfo{
		{Name: longFileName, Size: 5, Type: "file"},
		{Name: longDirName, Size: 0, Type: "directory"},
	}, "/tmp/example", "name", false)

	for _, want := range []string{"Type", "file", "directory", longFileName, longDirName} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output does not contain %q:\n%s", want, output.String())
		}
	}

	if strings.Contains(output.String(), "...") {
		t.Fatalf("output contains truncation marker:\n%s", output.String())
	}
}
