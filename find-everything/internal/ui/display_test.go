package ui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"find-everything/internal/types"
)

func TestSaveResultsToFileExplicitOutputPath(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "results.txt")
	if err := os.WriteFile(outputPath, []byte("previous content"), 0o640); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}
	originalInfo, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat existing destination: %v", err)
	}
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
	if strings.Contains(content, "previous content") {
		t.Fatalf("output file still contains previous content:\n%s", content)
	}
	updatedInfo, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat replaced destination: %v", err)
	}
	if updatedInfo.Mode().Perm() != originalInfo.Mode().Perm() {
		t.Fatalf("destination permissions changed from %v to %v", originalInfo.Mode().Perm(), updatedInfo.Mode().Perm())
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

			got, err := promptLargeResultsAction(strings.NewReader(tt.input), &output)
			if err != nil || got != tt.want {
				t.Fatalf("promptLargeResultsAction() = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}
}

func TestPromptLargeResultsActionReturnsReadError(t *testing.T) {
	sentinel := errors.New("read failed")
	_, err := promptLargeResultsAction(errorReader{err: sentinel}, io.Discard)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestPrintResultsLargeDisplayActionPrintsDetails(t *testing.T) {
	files := makeFileResults(101)
	var output bytes.Buffer

	err := PrintSearchResults(types.SearchResults{Files: files}, ResultsOutputOptions{
		NoSort:             true,
		LargeResultsAction: LargeResultsActionDisplay,
		Stdout:             &output,
	})
	if err != nil {
		t.Fatalf("PrintResults returned error: %v", err)
	}

	if !strings.Contains(output.String(), "Total results: 101 (exceeds 100)") {
		t.Fatalf("output does not contain total results summary:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Matching Files:") {
		t.Fatalf("output does not contain matching files section:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "file-100.txt") {
		t.Fatalf("output does not contain displayed result:\n%s", output.String())
	}
	if strings.Contains(output.String(), "Results saved to:") {
		t.Fatalf("display action unexpectedly saved results:\n%s", output.String())
	}
}

func TestPrintResultsLargeSaveUsesExplicitOutputPath(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "results.txt")
	var output bytes.Buffer

	err := PrintSearchResults(types.SearchResults{Files: makeFileResults(101)}, ResultsOutputOptions{
		NoSort:             true,
		LargeResultsAction: LargeResultsActionSave,
		OutputPath:         outputPath,
		Stdout:             &output,
	})
	if err != nil {
		t.Fatalf("PrintResults returned error: %v", err)
	}

	if !strings.Contains(output.String(), "Results saved to: "+outputPath) {
		t.Fatalf("output does not contain explicit saved path:\n%s", output.String())
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
	var output bytes.Buffer

	err := PrintSearchResults(types.SearchResults{Files: makeFileResults(101)}, ResultsOutputOptions{
		NoSort:             true,
		LargeResultsAction: LargeResultsActionAsk,
		OutputPath:         outputPath,
		PromptReader:       strings.NewReader("display\n"),
		PromptWriter:       &promptOutput,
		Stdout:             &output,
	})
	if err != nil {
		t.Fatalf("PrintResults returned error: %v", err)
	}

	if !strings.Contains(promptOutput.String(), "Non-interactive terminal detected") {
		t.Fatalf("prompt output does not contain non-interactive fallback message:\n%s", promptOutput.String())
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected fallback save file: %v", err)
	}
}

func TestRendererTTYPolicy(t *testing.T) {
	results := types.SearchResults{
		Files: []types.FileResult{{Path: "file.txt", Size: 1024}},
	}

	t.Run("non-TTY is plain and progress is suppressed", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		renderer := NewRenderer(&stdout, &stderr, false, false)

		if err := renderer.RenderHeader(".", "*.txt"); err != nil {
			t.Fatalf("RenderHeader returned error: %v", err)
		}
		beforeProgress := stdout.Len()
		if err := renderer.RenderProgress(types.ProgressSnapshot{
			ProcessedDirectories: 2,
			FoundFiles:           1,
			Elapsed:              time.Second,
		}); err != nil {
			t.Fatalf("RenderProgress returned error: %v", err)
		}
		if stdout.Len() != beforeProgress {
			t.Fatalf("non-TTY progress wrote output: %q", stdout.String()[beforeProgress:])
		}
		if err := renderer.RenderResults(results, ResultsOutputOptions{}); err != nil {
			t.Fatalf("RenderResults returned error: %v", err)
		}

		for streamName, output := range map[string]string{
			"stdout": stdout.String(),
			"stderr": stderr.String(),
		} {
			if strings.Contains(output, "\x1b[") {
				t.Fatalf("%s contains ANSI: %q", streamName, output)
			}
			if strings.Contains(output, "\r") {
				t.Fatalf("%s contains carriage return: %q", streamName, output)
			}
		}
	})

	t.Run("TTY enables color and progress", func(t *testing.T) {
		var stdout bytes.Buffer
		renderer := NewRenderer(&stdout, io.Discard, true, true)

		if err := renderer.RenderHeader(".", "*.txt"); err != nil {
			t.Fatalf("RenderHeader returned error: %v", err)
		}
		if err := renderer.RenderProgress(types.ProgressSnapshot{
			ProcessedDirectories: 2,
			FoundFiles:           1,
			Elapsed:              time.Second,
		}); err != nil {
			t.Fatalf("RenderProgress returned error: %v", err)
		}

		if !strings.Contains(stdout.String(), "\x1b[") {
			t.Fatalf("TTY output does not contain ANSI: %q", stdout.String())
		}
		if !strings.Contains(stdout.String(), "\r") {
			t.Fatalf("TTY output does not contain progress carriage return: %q", stdout.String())
		}
	})
}

func TestRendererSeparatesStreamsAndEscapesControls(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer := NewRenderer(&stdout, &stderr, false, false)
	results := types.SearchResults{
		Files:       []types.FileResult{{Path: "file\nname\t\x1b[31m.txt"}},
		Directories: []string{"dir\rname"},
		Report: types.SearchReport{
			Incomplete:          true,
			TraversalErrorCount: 1,
			TraversalErrors: []types.PathIssue{{
				Path:      "bad\npath",
				Operation: "read\tentry",
				Err:       errors.New("denied\x1b[2J"),
			}},
			SkippedSymlinkCount: 1,
			SkippedSymlinks: []types.PathIssue{{
				Path:      "link\npath",
				Operation: "skip symlink",
				Err:       errors.New("directory target"),
			}},
		},
	}

	if err := renderer.RenderHeader("base\npath", "*.txt\t\x1b"); err != nil {
		t.Fatalf("RenderHeader returned error: %v", err)
	}
	if err := renderer.RenderResults(results, ResultsOutputOptions{}); err != nil {
		t.Fatalf("RenderResults returned error: %v", err)
	}

	stdoutText := stdout.String()
	stderrText := stderr.String()
	for _, want := range []string{
		`"base\npath"`,
		`"*.txt\t\x1b"`,
		`"file\nname\t\x1b[31m.txt"`,
		`"dir\rname"`,
		"Status: incomplete",
	} {
		if !strings.Contains(stdoutText, want) {
			t.Fatalf("stdout does not contain %q:\n%s", want, stdoutText)
		}
	}
	for _, unwanted := range []string{"Warning:", "Notice:", "traversal error"} {
		if strings.Contains(stdoutText, unwanted) {
			t.Fatalf("stdout contains stderr notice %q:\n%s", unwanted, stdoutText)
		}
	}

	for _, want := range []string{
		"Warning: search incomplete",
		`"read\tentry" "bad\npath": "denied\x1b[2J"`,
		"Notice: skipped 1",
		`skip symlink "link\npath": directory target`,
	} {
		if !strings.Contains(stderrText, want) {
			t.Fatalf("stderr does not contain %q:\n%s", want, stderrText)
		}
	}
	for _, unwanted := range []string{"Matching Files:", "Files found:"} {
		if strings.Contains(stderrText, unwanted) {
			t.Fatalf("stderr contains result output %q:\n%s", unwanted, stderrText)
		}
	}
	if strings.Contains(stdoutText+stderrText, "\x1b[") {
		t.Fatalf("non-TTY output contains raw ANSI:\nstdout=%q\nstderr=%q", stdoutText, stderrText)
	}
}

func TestRendererReportsSearchStates(t *testing.T) {
	tests := []struct {
		name    string
		results types.SearchResults
		want    string
	}{
		{name: "complete", results: types.SearchResults{Files: []types.FileResult{{Path: "file"}}}, want: "Status: complete"},
		{name: "no matches", results: types.SearchResults{}, want: "Status: complete (no matches)"},
		{name: "limit", results: types.SearchResults{Report: types.SearchReport{LimitReached: true}}, want: "Status: limit reached"},
		{name: "incomplete", results: types.SearchResults{Report: types.SearchReport{Incomplete: true}}, want: "Status: incomplete"},
		{name: "limit and incomplete", results: types.SearchResults{Report: types.SearchReport{LimitReached: true, Incomplete: true}}, want: "Status: incomplete (result limit reached)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			renderer := NewRenderer(&stdout, io.Discard, false, false)
			if err := renderer.RenderResults(tt.results, ResultsOutputOptions{}); err != nil {
				t.Fatalf("RenderResults returned error: %v", err)
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Fatalf("output does not contain %q:\n%s", tt.want, stdout.String())
			}
		})
	}
}

func TestPrintSearchResultsLargeAskUsesInjectedInteractiveIO(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := PrintSearchResults(types.SearchResults{Files: makeFileResults(101)}, ResultsOutputOptions{
		NoSort:             true,
		LargeResultsAction: LargeResultsActionAsk,
		PromptReader:       strings.NewReader("d\n"),
		PromptWriter:       &stderr,
		PromptTTY:          true,
		Stdout:             &stdout,
		Stderr:             &stderr,
	})
	if err != nil {
		t.Fatalf("PrintSearchResults returned error: %v", err)
	}
	if !strings.Contains(stderr.String(), "Choose output:") {
		t.Fatalf("prompt was not written to injected stderr:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "file-100.txt") {
		t.Fatalf("display choice did not render details:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "\r") {
		t.Fatalf("injected prompt output contains carriage return:\nstdout=%q\nstderr=%q", stdout.String(), stderr.String())
	}
}

func TestGeneratedOutputNamesUseRandomSuffix(t *testing.T) {
	dependencies := defaultSaveDependencies()
	dependencies.now = func() time.Time {
		return time.Date(2026, time.July, 16, 10, 11, 12, 0, time.UTC)
	}
	dependencies.random = bytes.NewReader([]byte{
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23,
		24, 25, 26, 27, 28, 29, 30, 31,
	})
	dependencies.lstat = func(string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}

	first, err := resolveOutputPath("", dependencies)
	if err != nil {
		t.Fatalf("resolveOutputPath first call: %v", err)
	}
	second, err := resolveOutputPath("", dependencies)
	if err != nil {
		t.Fatalf("resolveOutputPath second call: %v", err)
	}
	if first == second {
		t.Fatalf("generated names collided: %q", first)
	}
	for _, filename := range []string{first, second} {
		if !strings.HasPrefix(filename, "search_results_20260716_101112_") || !strings.HasSuffix(filename, ".txt") {
			t.Fatalf("generated filename has unexpected format: %q", filename)
		}
	}
}

func TestSaveResultsAutosaveCreatesDistinctFiles(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	first, err := SaveResultsToFile(nil, nil, "*", ".", false, true, "")
	if err != nil {
		t.Fatalf("first autosave: %v", err)
	}
	second, err := SaveResultsToFile(nil, nil, "*", ".", false, true, "")
	if err != nil {
		t.Fatalf("second autosave: %v", err)
	}
	if first == second {
		t.Fatalf("autosave filenames collided: %q", first)
	}
	for _, filename := range []string{first, second} {
		info, statErr := os.Stat(filename)
		if statErr != nil {
			t.Fatalf("stat autosave %q: %v", filename, statErr)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("autosave %q is not regular: %v", filename, info.Mode())
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read autosave directory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("autosave left unexpected files: %v", entries)
	}
}

func TestSaveFailuresPreserveExistingDestination(t *testing.T) {
	sentinel := errors.New("injected failure")
	tests := []struct {
		name   string
		mutate func(*saveDependencies)
	}{
		{
			name: "chmod",
			mutate: func(dependencies *saveDependencies) {
				dependencies.createTemp = func(directory, pattern string) (outputFile, error) {
					file, err := os.CreateTemp(directory, pattern)
					if err != nil {
						return nil, err
					}
					return &chmodErrorFile{File: file, err: sentinel}, nil
				}
			},
		},
		{
			name: "write",
			mutate: func(dependencies *saveDependencies) {
				dependencies.createTemp = func(directory, pattern string) (outputFile, error) {
					file, err := os.CreateTemp(directory, pattern)
					if err != nil {
						return nil, err
					}
					return &writeErrorFile{File: file, err: sentinel}, nil
				}
			},
		},
		{
			name: "close",
			mutate: func(dependencies *saveDependencies) {
				dependencies.createTemp = func(directory, pattern string) (outputFile, error) {
					file, err := os.CreateTemp(directory, pattern)
					if err != nil {
						return nil, err
					}
					return &closeErrorFile{File: file, err: sentinel}, nil
				}
			},
		},
		{
			name: "rename",
			mutate: func(dependencies *saveDependencies) {
				dependencies.rename = func(string, string) error { return sentinel }
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			destination := filepath.Join(directory, "results.txt")
			const original = "original destination"
			if err := os.WriteFile(destination, []byte(original), 0o600); err != nil {
				t.Fatalf("write destination: %v", err)
			}

			dependencies := defaultSaveDependencies()
			tt.mutate(&dependencies)
			filename, err := saveSearchResultsToFile(
				types.SearchResults{Files: []types.FileResult{{Path: "file.txt"}}},
				"*.txt",
				".",
				false,
				true,
				destination,
				dependencies,
			)
			if err == nil {
				t.Fatal("saveSearchResultsToFile returned nil error")
			}
			if filename != "" {
				t.Fatalf("filename = %q, want empty", filename)
			}

			content, readErr := os.ReadFile(destination)
			if readErr != nil {
				t.Fatalf("read destination: %v", readErr)
			}
			if string(content) != original {
				t.Fatalf("destination changed after failure: %q", content)
			}
			entries, readDirErr := os.ReadDir(directory)
			if readDirErr != nil {
				t.Fatalf("read temp directory: %v", readDirErr)
			}
			if len(entries) != 1 || entries[0].Name() != "results.txt" {
				t.Fatalf("temporary output was not cleaned up: %v", entries)
			}
		})
	}
}

func TestSaveResultsRejectsInvalidDestination(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		destination := t.TempDir()
		_, err := SaveResultsToFile(nil, nil, "*", ".", false, true, destination)
		if err == nil || !strings.Contains(err.Error(), "is a directory") {
			t.Fatalf("error = %v, want directory rejection", err)
		}
	})

	t.Run("non-regular symlink", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "target.txt")
		if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
			t.Fatalf("write target: %v", err)
		}
		destination := filepath.Join(directory, "results.txt")
		if err := os.Symlink(target, destination); err != nil {
			t.Skipf("create symlink: %v", err)
		}

		_, err := SaveResultsToFile(nil, nil, "*", ".", false, true, destination)
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("error = %v, want non-regular rejection", err)
		}
		content, readErr := os.ReadFile(target)
		if readErr != nil {
			t.Fatalf("read target: %v", readErr)
		}
		if string(content) != "target" {
			t.Fatalf("symlink target changed: %q", content)
		}
	})
}

func TestSavedOutputEscapesControlCharacters(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "results.txt")
	_, err := SaveResultsToFile(
		[]types.FileResult{{Path: "file\nname\t\x1b.txt"}},
		[]string{"dir\rname"},
		"*.txt\n\x1b",
		"base\tpath",
		false,
		true,
		outputPath,
	)
	if err != nil {
		t.Fatalf("SaveResultsToFile returned error: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		`Base Path: "base\tpath"`,
		`Search Pattern: "*.txt\n\x1b"`,
		`"file\nname\t\x1b.txt"`,
		`"dir\rname"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved output does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\x1b[") || strings.Contains(text, "\r") {
		t.Fatalf("saved output contains terminal controls: %q", text)
	}
}

type writeErrorFile struct {
	*os.File
	err error
}

type chmodErrorFile struct {
	*os.File
	err error
}

func (f *chmodErrorFile) Chmod(os.FileMode) error {
	return f.err
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func (f *writeErrorFile) Write([]byte) (int, error) {
	return 0, f.err
}

type closeErrorFile struct {
	*os.File
	err error
}

func (f *closeErrorFile) Close() error {
	if err := f.File.Close(); err != nil {
		return err
	}
	return f.err
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
