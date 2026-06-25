package ui

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"find-everything/internal/types"
)

func TestSaveResultsToFileExplicitOutputPath(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "results.txt")
	files := []types.FileResult{{Path: "b.txt", Size: 2048}, {Path: "a.txt", Size: 1024}}
	dirs := []string{"dir-b", "dir-a"}

	filename, err := SaveResultsToFile(files, dirs, "*.txt", "/tmp/base", true, false, outputPath)
	if err != nil {
		t.Fatalf("SaveResultsToFile returned error: %v", err)
	}
	if filename != outputPath {
		t.Fatalf("filename = %q, want %q", filename, outputPath)
	}

	contentBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	content := string(contentBytes)

	for _, want := range []string{
		"Enhanced File and Directory Finder Results",
		"Base Path: /tmp/base",
		"Search Pattern: *.txt",
		"Files found: 2",
		"Directories found: 2",
		"Total results: 4",
		"  a.txt (1.0 KB)",
		"  dir-a",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("output file does not contain %q\ncontent:\n%s", want, content)
		}
	}
}

func TestSaveResultsToFileReturnsErrorForInvalidPath(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "missing", "results.txt")

	filename, err := SaveResultsToFile(nil, nil, "*", "/tmp/base", false, false, outputPath)
	if err == nil {
		t.Fatal("SaveResultsToFile returned nil error for invalid path")
	}
	if filename != "" {
		t.Fatalf("filename = %q, want empty string", filename)
	}
}

func TestPromptLargeResultsAction(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "short save", input: "s\n", want: LargeResultsActionSave},
		{name: "word save", input: "save\n", want: LargeResultsActionSave},
		{name: "short display", input: "d\n", want: LargeResultsActionDisplay},
		{name: "word display", input: "display\n", want: LargeResultsActionDisplay},
		{name: "empty defaults save", input: "\n", want: LargeResultsActionSave},
		{name: "invalid defaults save after attempts", input: "xyz", want: LargeResultsActionSave},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer

			got := promptLargeResultsAction(strings.NewReader(tt.input), &output)
			if got != tt.want {
				t.Fatalf("promptLargeResultsAction() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintResultsLargeDisplayActionPrintsDetails(t *testing.T) {
	files := makeFileResults(101)

	output := captureStdout(t, func() error {
		return PrintResults(files, nil, ResultsOutputOptions{
			NoSort:             true,
			LargeResultsAction: LargeResultsActionDisplay,
		})
	})

	if !strings.Contains(output, "Total results: 101 (exceeds 100)") {
		t.Fatalf("output does not contain total results summary:\n%s", output)
	}
	if !strings.Contains(output, "Matching Files:") {
		t.Fatalf("output does not contain matching files section:\n%s", output)
	}
	if !strings.Contains(output, "file-100.txt") {
		t.Fatalf("output does not contain displayed result:\n%s", output)
	}
	if strings.Contains(output, "Results saved to:") {
		t.Fatalf("display action unexpectedly saved results:\n%s", output)
	}
}

func TestPrintResultsLargeSaveUsesExplicitOutputPath(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "results.txt")

	output := captureStdout(t, func() error {
		return PrintResults(makeFileResults(101), nil, ResultsOutputOptions{
			NoSort:             true,
			LargeResultsAction: LargeResultsActionSave,
			OutputPath:         outputPath,
		})
	})

	if !strings.Contains(output, "Results saved to: "+outputPath) {
		t.Fatalf("output does not contain explicit saved path:\n%s", output)
	}
	contentBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !strings.Contains(string(contentBytes), "Total results: 101") {
		t.Fatalf("saved output does not contain total results:\n%s", string(contentBytes))
	}
}

func TestPrintResultsLargeAskNonInteractiveFallsBackToSave(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "results.txt")
	var promptOutput bytes.Buffer

	captureStdout(t, func() error {
		return PrintResults(makeFileResults(101), nil, ResultsOutputOptions{
			NoSort:             true,
			LargeResultsAction: LargeResultsActionAsk,
			OutputPath:         outputPath,
			PromptReader:       strings.NewReader("display\n"),
			PromptWriter:       &promptOutput,
		})
	})

	if !strings.Contains(promptOutput.String(), "Non-interactive terminal detected") {
		t.Fatalf("prompt output does not contain non-interactive fallback message:\n%s", promptOutput.String())
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected fallback save file: %v", err)
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	os.Stdout = writer
	callErr := fn()
	closeErr := writer.Close()
	os.Stdout = oldStdout

	output, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read stdout pipe: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("close stdout pipe: %v", closeErr)
	}
	if callErr != nil {
		t.Fatalf("function returned error: %v", callErr)
	}

	return string(output)
}

func makeFileResults(count int) []types.FileResult {
	files := make([]types.FileResult, count)
	for i := range files {
		files[i] = types.FileResult{
			Path: filepath.Join("/tmp/base", "file-"+strconv.Itoa(i)+".txt"),
			Size: int64(i),
		}
	}
	return files
}
