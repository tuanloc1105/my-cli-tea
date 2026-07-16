package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"find-everything/internal/finder"
	"find-everything/internal/types"
	"find-everything/internal/ui"
)

func TestCommandForwardsFlagsAndOutputOptions(t *testing.T) {
	var receivedContext context.Context
	var receivedBasePath, receivedPattern string
	var receivedFinderOptions finder.FinderOptions
	var receivedOutputOptions ui.ResultsOutputOptions
	clearCalls := 0
	run := func(ctx context.Context, basePath, pattern string, options finder.FinderOptions) ([]types.FileResult, []string, error) {
		receivedContext = ctx
		receivedBasePath = basePath
		receivedPattern = pattern
		receivedFinderOptions = options
		return []types.FileResult{{Path: "result.txt", Size: 1024}}, []string{"result-dir"}, nil
	}
	printResults := func(files []types.FileResult, dirs []string, options ui.ResultsOutputOptions) error {
		if len(files) != 1 || len(dirs) != 1 {
			t.Fatalf("results = %v/%v, want one file and one directory", files, dirs)
		}
		receivedOutputOptions = options
		return nil
	}

	ctx := context.WithValue(context.Background(), testContextKey{}, "value")
	code, stdout, stderr := runCommand(t, ctx, []string{
		"--case-sensitive",
		"--max-workers", "3",
		"--exclude-dirs", "node_modules,.git",
		"--exclude-patterns", "tmp$,cache$",
		"--file-types", ".go,.txt",
		"--min-size", "1KB",
		"--max-size", "2MB",
		"--max-results", "8",
		"--no-progress",
		"--show-details",
		"--no-sort",
		"--display-all",
		"/tmp/base", "*.go",
	}, run, printResults, func() { clearCalls++ })

	if code != 0 || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q, want 0/empty", code, stderr)
	}
	if receivedContext.Value(testContextKey{}) != "value" {
		t.Fatal("runner did not receive command context")
	}
	if receivedBasePath != "/tmp/base" || receivedPattern != "*.go" {
		t.Fatalf("base/pattern = %q/%q", receivedBasePath, receivedPattern)
	}
	if !receivedFinderOptions.CaseSensitive || receivedFinderOptions.MaxWorkers != 3 || receivedFinderOptions.ShowProgress || !receivedFinderOptions.NoSort {
		t.Fatalf("finder booleans/workers = %+v", receivedFinderOptions)
	}
	if receivedFinderOptions.MinSize != 1024 || receivedFinderOptions.MaxSize != 2*1024*1024 || receivedFinderOptions.MaxResults != 8 {
		t.Fatalf("finder sizes/results = %+v", receivedFinderOptions)
	}
	if got := strings.Join(receivedFinderOptions.ExcludeDirs, ","); got != "node_modules,.git" {
		t.Fatalf("exclude dirs = %q", got)
	}
	if got := strings.Join(receivedFinderOptions.ExcludePatterns, ","); got != "tmp$,cache$" {
		t.Fatalf("exclude patterns = %q", got)
	}
	if got := strings.Join(receivedFinderOptions.FileTypes, ","); got != ".go,.txt" {
		t.Fatalf("file types = %q", got)
	}
	if receivedFinderOptions.Writer == nil {
		t.Fatal("finder writer was not supplied")
	}
	if !receivedOutputOptions.ShowDetails || !receivedOutputOptions.NoSort || receivedOutputOptions.LargeResultsAction != ui.LargeResultsActionDisplay {
		t.Fatalf("output options = %+v", receivedOutputOptions)
	}
	if receivedOutputOptions.Writer == nil {
		t.Fatalf("output writer = %+v", receivedOutputOptions)
	}
	if receivedOutputOptions.PromptWriter != nil {
		t.Fatalf("prompt writer should use the output-writer fallback: %+v", receivedOutputOptions)
	}
	if clearCalls != 1 {
		t.Fatalf("clear calls = %d, want 1", clearCalls)
	}
	if !strings.Contains(stdout, "Enhanced File and Directory Finder") || !strings.Contains(stdout, "Searching in: /tmp/base") {
		t.Fatalf("stdout missing header: %q", stdout)
	}
}

func TestCommandDefaultsAndStateIsolation(t *testing.T) {
	var received []finder.FinderOptions
	run := func(_ context.Context, _, _ string, options finder.FinderOptions) ([]types.FileResult, []string, error) {
		received = append(received, options)
		return nil, nil, nil
	}
	printResults := func([]types.FileResult, []string, ui.ResultsOutputOptions) error { return nil }

	code, _, stderr := runCommand(t, context.Background(), []string{
		"--case-sensitive", "--max-workers", "2", "--no-progress", ".", "*",
	}, run, printResults, func() {})
	if code != 0 || stderr != "" {
		t.Fatalf("first exit/stderr = %d/%q", code, stderr)
	}
	code, _, stderr = runCommand(t, context.Background(), []string{".", "*"}, run, printResults, func() {})
	if code != 0 || stderr != "" {
		t.Fatalf("second exit/stderr = %d/%q", code, stderr)
	}

	if len(received) != 2 {
		t.Fatalf("runner calls = %d, want 2", len(received))
	}
	if !received[0].CaseSensitive || received[0].MaxWorkers != 2 || received[0].ShowProgress {
		t.Fatalf("first options = %+v", received[0])
	}
	if received[1].CaseSensitive || received[1].MaxWorkers != runtime.NumCPU() || !received[1].ShowProgress {
		t.Fatalf("second options leaked state = %+v", received[1])
	}
	if received[1].MinSize != 0 || received[1].MaxSize != 1<<63-1 || received[1].MaxResults != 10000 {
		t.Fatalf("default limits = %+v", received[1])
	}
}

func TestCommandHelpUsesSuppliedStreams(t *testing.T) {
	called := false
	run := func(context.Context, string, string, finder.FinderOptions) ([]types.FileResult, []string, error) {
		called = true
		return nil, nil, nil
	}

	code, stdout, stderr := runCommand(t, context.Background(), []string{"--help"}, run, noOpPrinter, func() {})
	if code != 0 || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q, want 0/empty", code, stderr)
	}
	if called {
		t.Fatal("runner called for help")
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "--large-results-action") {
		t.Fatalf("help output = %q", stdout)
	}
}

func TestCommandErrorsRenderOnceOnStderr(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"host-process", "--host-only"}
	t.Cleanup(func() { os.Args = originalArgs })

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing args", args: nil, want: "accepts 2 arg(s)"},
		{name: "unknown flag", args: []string{"--unknown"}, want: "unknown flag"},
		{name: "invalid minimum size", args: []string{"--min-size", "bad", ".", "*"}, want: "error parsing min-size"},
		{name: "invalid action", args: []string{"--large-results-action", "bad", ".", "*"}, want: "large-results-action must be one of"},
		{name: "display conflict", args: []string{"--display-all", "--output", "results.txt", ".", "*"}, want: "--display-all conflicts with --output"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			run := func(context.Context, string, string, finder.FinderOptions) ([]types.FileResult, []string, error) {
				called = true
				return nil, nil, nil
			}
			code, stdout, stderr := runCommand(t, context.Background(), tt.args, run, noOpPrinter, func() {})
			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if called {
				t.Fatal("runner called for invalid command")
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if strings.Count(stderr, "Error:") != 1 || !strings.Contains(stderr, tt.want) {
				t.Fatalf("stderr = %q, want one error containing %q", stderr, tt.want)
			}
			if strings.Contains(stderr, "Usage:") {
				t.Fatalf("stderr unexpectedly contains usage: %q", stderr)
			}
		})
	}
}

func TestCommandRunnerAndPrinterErrors(t *testing.T) {
	tests := []struct {
		name         string
		run          searchRunner
		printResults resultsPrinter
		want         string
	}{
		{
			name: "runner",
			run: func(context.Context, string, string, finder.FinderOptions) ([]types.FileResult, []string, error) {
				return nil, nil, errors.New("search failed")
			},
			printResults: noOpPrinter,
			want:         "Error: search failed\n",
		},
		{
			name: "printer",
			run: func(context.Context, string, string, finder.FinderOptions) ([]types.FileResult, []string, error) {
				return nil, nil, nil
			},
			printResults: func([]types.FileResult, []string, ui.ResultsOutputOptions) error {
				return errors.New("render failed")
			},
			want: "Error: render failed\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCommand(t, context.Background(), []string{".", "*"}, tt.run, tt.printResults, func() {})
			if code != 1 || stderr != tt.want {
				t.Fatalf("exit/stderr = %d/%q, want 1/%q", code, stderr, tt.want)
			}
			if strings.Count(stderr, "Error:") != 1 {
				t.Fatalf("stderr contains duplicate error: %q", stderr)
			}
			if !strings.Contains(stdout, "Enhanced File and Directory Finder") {
				t.Fatalf("stdout missing pre-run header: %q", stdout)
			}
		})
	}
}

func TestCommandUsesContextAndInjectedWriterForSearch(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "match.txt")
	if err := os.WriteFile(target, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	code, stdout, stderr := runCommand(t, ctx, []string{directory, "*.txt"}, runFinder, ui.PrintResults, func() {})
	if code != 0 || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q", code, stderr)
	}
	if !strings.Contains(stdout, target) || !strings.Contains(stdout, "Search Results:") {
		t.Fatalf("stdout does not contain search result: %q", stdout)
	}
}

func TestExecuteContextAcceptsNilContextAndWriters(t *testing.T) {
	run := func(ctx context.Context, _, _ string, _ finder.FinderOptions) ([]types.FileResult, []string, error) {
		if ctx == nil {
			t.Fatal("runner received nil context")
		}
		return nil, nil, nil
	}
	if code := executeContext(nil, []string{".", "*"}, nil, nil, run, noOpPrinter, func() {}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{input: "0", want: 0},
		{input: "512B", want: 512},
		{input: "1.5KB", want: 1536},
		{input: "2MB", want: 2 * 1024 * 1024},
		{input: "inf", want: 1<<63 - 1},
	}
	for _, tt := range tests {
		got, err := parseSize(tt.input)
		if err != nil || got != tt.want {
			t.Fatalf("parseSize(%q) = %d, %v; want %d, nil", tt.input, got, err, tt.want)
		}
	}
	if _, err := parseSize("bad"); err == nil {
		t.Fatal("parseSize(bad) returned nil error")
	}
}

func runCommand(t *testing.T, ctx context.Context, args []string, run searchRunner, printResults resultsPrinter, clearScreen func()) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeContext(ctx, args, &stdout, &stderr, run, printResults, clearScreen)
	return code, stdout.String(), stderr.String()
}

func noOpPrinter([]types.FileResult, []string, ui.ResultsOutputOptions) error {
	return nil
}

type testContextKey struct{}
