package ui

import (
	"bytes"
	"check-folder-size/internal/scanner"
	"strings"
	"testing"
)

func TestPrintResultsPreservesOrderAndShowsMetricTypeAndFullNames(t *testing.T) {
	longFileName := "this-is-a-very-long-file-name-that-should-keep-its-important-suffix.txt"
	longDirName := "this-is-a-very-long-directory-name-that-should-not-be-truncated-at-the-end"

	var output bytes.Buffer
	PrintResults(&output, []scanner.ItemInfo{
		{Name: longFileName, Size: 5, Type: scanner.ItemTypeFile},
		{Name: longDirName, Size: 0, Type: scanner.ItemTypeDirectory},
	}, "/tmp/example", scanner.SizeModeAllocated)

	for _, want := range []string{"Total Allocated Size", "Type", "file", "directory", longFileName, longDirName} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output does not contain %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "...") {
		t.Fatalf("output contains truncation marker:\n%s", output.String())
	}
	assertOutputOrder(t, output.String(), longFileName, longDirName)
}

func TestPrintResultsLabelsLogicalMetric(t *testing.T) {
	var output bytes.Buffer
	PrintResults(&output, []scanner.ItemInfo{
		{Name: "item", Size: 1024, Type: scanner.ItemTypeFile},
	}, "/tmp/example", scanner.SizeModeLogical)
	if !strings.Contains(output.String(), "Total Logical Size") || strings.Contains(output.String(), "Total Allocated Size") {
		t.Fatalf("logical metric label is missing or ambiguous:\n%s", output.String())
	}
}

func TestPrintResultsEmpty(t *testing.T) {
	var output bytes.Buffer
	PrintResults(&output, nil, "/tmp/example", scanner.SizeModeAllocated)
	if output.String() != "No accessible folders or files found (allocated size).\n" {
		t.Fatalf("output = %q", output.String())
	}
}

func assertOutputOrder(t *testing.T, output string, names ...string) {
	t.Helper()
	previous := -1
	for _, name := range names {
		index := strings.Index(output, name)
		if index < 0 || index <= previous {
			t.Fatalf("output does not preserve order %v:\n%s", names, output)
		}
		previous = index
	}
}
