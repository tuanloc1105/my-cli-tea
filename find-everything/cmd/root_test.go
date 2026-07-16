package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	var receivedHeaderOptions, receivedOutputOptions ui.ResultsOutputOptions
	run := func(ctx context.Context, basePath, pattern string, options finder.FinderOptions) (types.SearchResults, error) {
		receivedContext = ctx
		receivedBasePath = basePath
		receivedPattern = pattern
		receivedFinderOptions = options
		return types.SearchResults{
			Files:       []types.FileResult{{Path: "result.txt", Size: 1024}},
			Directories: []string{"result-dir"},
		}, nil
	}
	printHeader := func(_, _ string, options ui.ResultsOutputOptions) error {
		receivedHeaderOptions = options
		return nil
	}
	printResults := func(results types.SearchResults, options ui.ResultsOutputOptions) error {
		if len(results.Files) != 1 || len(results.Directories) != 1 {
			t.Fatalf("results = %+v, want one file and one directory", results)
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
		"--file-types", "go,.txt",
		"--min-size", "1KB",
		"--max-size", "2MB",
		"--max-results", "8",
		"--no-progress",
		"--show-details",
		"--no-sort",
		"--display-all",
		"/tmp/base", "*.go",
	}, run, printHeader, printResults, neverTTY)

	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
	if receivedContext.Value(testContextKey{}) != "value" {
		t.Fatal("runner did not receive command context")
	}
	if receivedBasePath != "/tmp/base" || receivedPattern != "*.go" {
		t.Fatalf("base/pattern = %q/%q", receivedBasePath, receivedPattern)
	}
	if !receivedFinderOptions.CaseSensitive || receivedFinderOptions.MaxWorkers != 3 {
		t.Fatalf("finder booleans/workers = %+v", receivedFinderOptions)
	}
	if receivedFinderOptions.Progress != nil {
		t.Fatal("--no-progress unexpectedly installed a progress callback")
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
	if got := strings.Join(receivedFinderOptions.FileTypes, ","); got != "go,.txt" {
		t.Fatalf("file types = %q", got)
	}
	if !receivedOutputOptions.ShowDetails || !receivedOutputOptions.NoSort || receivedOutputOptions.LargeResultsAction != ui.LargeResultsActionDisplay {
		t.Fatalf("output options = %+v", receivedOutputOptions)
	}
	if receivedOutputOptions.Stdout == nil || receivedOutputOptions.Stderr == nil || receivedOutputOptions.PromptReader == nil {
		t.Fatalf("streams were not injected: %+v", receivedOutputOptions)
	}
	if receivedHeaderOptions.StdoutTTY || receivedHeaderOptions.StderrTTY || receivedHeaderOptions.PromptTTY {
		t.Fatalf("non-TTY flags = %+v", receivedHeaderOptions)
	}
}

func TestCommandDefaultsAndStateIsolation(t *testing.T) {
	var received []finder.FinderOptions
	run := func(_ context.Context, _, _ string, options finder.FinderOptions) (types.SearchResults, error) {
		received = append(received, options)
		return types.SearchResults{}, nil
	}

	code, _, stderr := runCommand(t, context.Background(), []string{
		"--case-sensitive", "--max-workers", "2", "--no-progress", ".", "*",
	}, run, noOpHeader, noOpPrinter, alwaysTTY)
	if code != 0 || stderr != "" {
		t.Fatalf("first exit/stderr = %d/%q", code, stderr)
	}
	code, _, stderr = runCommand(t, context.Background(), []string{".", "*"}, run, noOpHeader, noOpPrinter, alwaysTTY)
	if code != 0 || stderr != "" {
		t.Fatalf("second exit/stderr = %d/%q", code, stderr)
	}

	if len(received) != 2 {
		t.Fatalf("runner calls = %d, want 2", len(received))
	}
	if !received[0].CaseSensitive || received[0].MaxWorkers != 2 || received[0].Progress != nil {
		t.Fatalf("first options = %+v", received[0])
	}
	if received[1].CaseSensitive || received[1].MaxWorkers != runtime.NumCPU() || received[1].Progress == nil {
		t.Fatalf("second options leaked state = %+v", received[1])
	}
	if received[1].MinSize != 0 || received[1].MaxSize != 1<<63-1 || received[1].MaxResults != 10000 {
		t.Fatalf("default limits = %+v", received[1])
	}
}

func TestCommandHelpUsesSuppliedStreams(t *testing.T) {
	called := false
	run := func(context.Context, string, string, finder.FinderOptions) (types.SearchResults, error) {
		called = true
		return types.SearchResults{}, nil
	}

	code, stdout, stderr := runCommand(t, context.Background(), []string{"--help"}, run, noOpHeader, noOpPrinter, neverTTY)
	if code != 0 || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q, want 0/empty", code, stderr)
	}
	if called {
		t.Fatal("runner called for help")
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "--large-results-action") {
		t.Fatalf("help output = %q", stdout)
	}
	if strings.Contains(stdout, "*.{") || strings.Contains(stdout, "--progress") {
		t.Fatalf("help advertises unsupported syntax: %q", stdout)
	}
}

func TestCommandValidationErrorsRenderOnce(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing args", args: nil, want: "accepts 2 arg(s)"},
		{name: "unknown flag", args: []string{"--unknown"}, want: "unknown flag"},
		{name: "empty pattern", args: []string{".", ""}, want: "pattern must not be empty"},
		{name: "workers", args: []string{"--max-workers", "0", ".", "*"}, want: "max-workers must be greater than zero"},
		{name: "results", args: []string{"--max-results", "0", ".", "*"}, want: "max-results must be greater than zero"},
		{name: "negative size", args: []string{"--min-size", "-1", ".", "*"}, want: "non-negative"},
		{name: "not finite", args: []string{"--min-size", "NaNMB", ".", "*"}, want: "finite and non-negative"},
		{name: "overflow", args: []string{"--max-size", "999999999999TB", ".", "*"}, want: "overflows int64"},
		{name: "size order", args: []string{"--min-size", "2MB", "--max-size", "1MB", ".", "*"}, want: "min-size must not exceed max-size"},
		{name: "invalid action", args: []string{"--large-results-action", "bad", ".", "*"}, want: "large-results-action must be one of"},
		{name: "display conflict", args: []string{"--display-all", "--output", "results.txt", ".", "*"}, want: "--display-all conflicts with --output"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			run := func(context.Context, string, string, finder.FinderOptions) (types.SearchResults, error) {
				called = true
				return types.SearchResults{}, nil
			}
			code, stdout, stderr := runCommand(t, context.Background(), tt.args, run, ui.PrintSearchHeader, ui.PrintSearchResults, neverTTY)
			if code != 1 || called || stdout != "" {
				t.Fatalf("exit/called/stdout = %d/%v/%q", code, called, stdout)
			}
			if strings.Count(stderr, "Error:") != 1 || !strings.Contains(stderr, tt.want) || strings.Contains(stderr, "Usage:") {
				t.Fatalf("stderr = %q, want one error containing %q", stderr, tt.want)
			}
		})
	}
}

func TestCommandExitCodeSemanticsAndPriority(t *testing.T) {
	t.Run("canceled before execution", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		called := false
		run := func(context.Context, string, string, finder.FinderOptions) (types.SearchResults, error) {
			called = true
			return types.SearchResults{}, nil
		}
		code, stdout, stderr := runCommand(t, ctx, []string{".", "*"}, run, ui.PrintSearchHeader, noOpPrinter, neverTTY)
		if code != 130 || called || stdout != "" || stderr != "Search canceled.\n" {
			t.Fatalf("exit/called/stdout/stderr = %d/%v/%q/%q", code, called, stdout, stderr)
		}
	})

	t.Run("limit", func(t *testing.T) {
		run := resultRunner(types.SearchResults{Report: types.SearchReport{LimitReached: true}}, nil)
		code, _, stderr := runCommand(t, context.Background(), []string{".", "*"}, run, noOpHeader, noOpPrinter, neverTTY)
		if code != 0 || stderr != "" {
			t.Fatalf("exit/stderr = %d/%q", code, stderr)
		}
	})

	t.Run("partial", func(t *testing.T) {
		run := resultRunner(types.SearchResults{Report: types.SearchReport{Incomplete: true}}, nil)
		code, _, stderr := runCommand(t, context.Background(), []string{".", "*"}, run, noOpHeader, noOpPrinter, neverTTY)
		if code != 2 || stderr != "" {
			t.Fatalf("exit/stderr = %d/%q", code, stderr)
		}
	})

	t.Run("output fatal beats partial", func(t *testing.T) {
		run := resultRunner(types.SearchResults{Report: types.SearchReport{Incomplete: true}}, nil)
		printer := func(types.SearchResults, ui.ResultsOutputOptions) error { return errors.New("render failed") }
		code, _, stderr := runCommand(t, context.Background(), []string{".", "*"}, run, noOpHeader, printer, neverTTY)
		if code != 1 || stderr != "Error: render failed\n" {
			t.Fatalf("exit/stderr = %d/%q", code, stderr)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		run := resultRunner(types.SearchResults{}, context.Canceled)
		code, _, stderr := runCommand(t, context.Background(), []string{".", "*"}, run, noOpHeader, noOpPrinter, neverTTY)
		if code != 130 || stderr != "Search canceled.\n" {
			t.Fatalf("exit/stderr = %d/%q", code, stderr)
		}
	})

	t.Run("external cancellation beats output failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		run := func(context.Context, string, string, finder.FinderOptions) (types.SearchResults, error) {
			cancel()
			return types.SearchResults{Report: types.SearchReport{Incomplete: true}}, nil
		}
		printer := func(types.SearchResults, ui.ResultsOutputOptions) error { return errors.New("render failed") }
		code, _, stderr := runCommand(t, ctx, []string{".", "*"}, run, noOpHeader, printer, neverTTY)
		if code != 130 || stderr != "Search canceled.\n" {
			t.Fatalf("exit/stderr = %d/%q", code, stderr)
		}
	})
}

func TestCommandActualSearchAndRedirectedOutput(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "match\nname.txt")
	if err := os.WriteFile(target, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runActualCommand(t, context.Background(), []string{"--no-progress", directory, "*.txt"})
	if code != 0 || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q", code, stderr)
	}
	if !strings.Contains(stdout, `"`+strings.ReplaceAll(target, "\n", `\n`)+`"`) || !strings.Contains(stdout, "Status: complete") {
		t.Fatalf("stdout does not contain escaped result: %q", stdout)
	}
	if strings.Contains(stdout+stderr, "\x1b[") || strings.Contains(stdout+stderr, "\r") || strings.Contains(stdout+stderr, "\033c") {
		t.Fatalf("redirected output contains terminal controls: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCommandActualNoMatchAndInvalidBase(t *testing.T) {
	code, stdout, stderr := runActualCommand(t, context.Background(), []string{"--no-progress", t.TempDir(), "*.missing"})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Status: complete (no matches)") {
		t.Fatalf("no-match exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	code, stdout, stderr = runActualCommand(t, context.Background(), []string{"--no-progress", missing, "*"})
	if code != 1 || !strings.Contains(stderr, "inspect base path") || strings.Contains(stderr, "Usage:") {
		t.Fatalf("invalid-base exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
}

func TestCommandEscapesFatalErrorText(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing\nforged\t\x1b")
	code, _, stderr := runActualCommand(t, context.Background(), []string{"--no-progress", missing, "*"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "\x1b") {
		t.Fatalf("stderr contains raw control characters: %q", stderr)
	}
	for _, escaped := range []string{`\n`, `\t`, `\x1b`} {
		if !strings.Contains(stderr, escaped) {
			t.Fatalf("stderr does not contain %q: %q", escaped, stderr)
		}
	}
}

func TestCommandPromptRequiresInputAndStderrTTY(t *testing.T) {
	stdin := strings.NewReader("d\n")
	var stdout, stderr bytes.Buffer
	promptTTY := true
	printer := func(_ types.SearchResults, options ui.ResultsOutputOptions) error {
		promptTTY = options.PromptTTY
		return nil
	}
	detectTTY := func(stream any) bool { return stream == stdin }
	code := executeContext(
		context.Background(),
		[]string{"--no-progress", ".", "*"},
		stdin,
		&stdout,
		&stderr,
		resultRunner(types.SearchResults{}, nil),
		noOpHeader,
		printer,
		detectTTY,
	)
	if code != 0 || promptTTY {
		t.Fatalf("exit/promptTTY = %d/%v, want 0/false", code, promptTTY)
	}
}

func TestCommandPartialWarningsStayOnStderr(t *testing.T) {
	results := types.SearchResults{
		Files: []types.FileResult{{Path: "match.txt", Size: 1}},
		Report: types.SearchReport{
			Incomplete:          true,
			TraversalErrorCount: 1,
			TraversalErrors: []types.PathIssue{{
				Path:      "bad\npath",
				Operation: "read directory",
				Err:       errors.New("denied"),
			}},
		},
	}
	code, stdout, stderr := runCommand(
		t,
		context.Background(),
		[]string{"--no-progress", ".", "*"},
		resultRunner(results, nil),
		ui.PrintSearchHeader,
		ui.PrintSearchResults,
		neverTTY,
	)
	if code != 2 || !strings.Contains(stdout, "match.txt") || !strings.Contains(stdout, "Status: incomplete") {
		t.Fatalf("exit/stdout = %d/%q", code, stdout)
	}
	if strings.Contains(stdout, "Warning:") || !strings.Contains(stderr, "Warning: search incomplete") || !strings.Contains(stderr, `"bad\npath"`) {
		t.Fatalf("stream separation failed: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCommandOutputFailureReturnsOne(t *testing.T) {
	files := make([]types.FileResult, 101)
	for i := range files {
		files[i] = types.FileResult{Path: fmt.Sprintf("file-%d.txt", i)}
	}
	destination := t.TempDir()
	code, _, stderr := runCommand(
		t,
		context.Background(),
		[]string{"--no-progress", "--output", destination, ".", "*"},
		resultRunner(types.SearchResults{Files: files}, nil),
		ui.PrintSearchHeader,
		ui.PrintSearchResults,
		neverTTY,
	)
	if code != 1 || !strings.Contains(stderr, "is a directory") || strings.Count(stderr, "Error:") != 1 {
		t.Fatalf("exit/stderr = %d/%q", code, stderr)
	}
}

func TestCommandProgressRequiresStderrTTY(t *testing.T) {
	run := func(_ context.Context, _, _ string, options finder.FinderOptions) (types.SearchResults, error) {
		if options.Progress == nil {
			t.Fatal("TTY execution did not install progress callback")
		}
		options.Progress(types.ProgressSnapshot{ProcessedDirectories: 1, FoundFiles: 1})
		return types.SearchResults{Files: []types.FileResult{{Path: "match.txt"}}}, nil
	}
	code, stdout, stderr := runCommand(t, context.Background(), []string{".", "*"}, run, ui.PrintSearchHeader, ui.PrintSearchResults, alwaysTTY)
	if code != 0 || !strings.Contains(stdout, "\x1b[") || !strings.Contains(stderr, "\r\x1b[") {
		t.Fatalf("TTY exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
}

func TestExecuteContextAcceptsNilContextAndStreams(t *testing.T) {
	run := func(ctx context.Context, _, _ string, _ finder.FinderOptions) (types.SearchResults, error) {
		if ctx == nil {
			t.Fatal("runner received nil context")
		}
		return types.SearchResults{}, nil
	}
	if code := executeContext(nil, []string{".", "*"}, nil, nil, nil, run, noOpHeader, noOpPrinter, nil); code != 0 {
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
		{input: "9223372036854775807B", want: 1<<63 - 1},
		{input: " inf ", want: 1<<63 - 1},
	}
	for _, tt := range tests {
		got, err := parseSize(tt.input)
		if err != nil || got != tt.want {
			t.Fatalf("parseSize(%q) = %d, %v; want %d, nil", tt.input, got, err, tt.want)
		}
	}
	for _, input := range []string{"", "bad", "-1", "NaNMB", "999999999999TB", "9223372036854775808", "9223372036854775808B"} {
		if _, err := parseSize(input); err == nil {
			t.Fatalf("parseSize(%q) returned nil error", input)
		}
	}
}

func runActualCommand(t *testing.T, ctx context.Context, args []string) (int, string, string) {
	t.Helper()
	return runCommand(t, ctx, args, runFinder, ui.PrintSearchHeader, ui.PrintSearchResults, neverTTY)
}

func runCommand(
	t *testing.T,
	ctx context.Context,
	args []string,
	run searchRunner,
	printHeader headerPrinter,
	printResults resultsPrinter,
	detectTTY ttyDetector,
) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeContext(
		ctx,
		args,
		strings.NewReader("d\n"),
		&stdout,
		&stderr,
		run,
		printHeader,
		printResults,
		detectTTY,
	)
	return code, stdout.String(), stderr.String()
}

func resultRunner(results types.SearchResults, err error) searchRunner {
	return func(context.Context, string, string, finder.FinderOptions) (types.SearchResults, error) {
		return results, err
	}
}

func noOpHeader(string, string, ui.ResultsOutputOptions) error {
	return nil
}

func noOpPrinter(types.SearchResults, ui.ResultsOutputOptions) error {
	return nil
}

func neverTTY(any) bool { return false }

func alwaysTTY(any) bool { return true }

type testContextKey struct{}
