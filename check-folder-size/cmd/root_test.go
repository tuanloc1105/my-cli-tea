package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"check-folder-size/internal/scanner"
)

func TestCommandRejectsInvalidArgumentsAndFlags(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "too many arguments",
			args: []string{dir, dir},
			want: "accepts at most 1 arg",
		},
		{
			name: "invalid sort",
			args: []string{"--sort", "modified", dir},
			want: "--sort must be 'size' or 'name'",
		},
		{
			name: "invalid minimum size",
			args: []string{"--min-size", "large", dir},
			want: "invalid --min-size value 'large'",
		},
		{
			name: "invalid maximum size",
			args: []string{"--max-size", "large", dir},
			want: "invalid --max-size value 'large'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runnerCalls := 0
			clearCalls := 0
			code, stdout, stderr := runCommand(t, test.args, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
				runnerCalls++
				return scanner.ScanResult{}, nil
			}, func() {
				clearCalls++
			})

			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr = %q, want substring %q", stderr, test.want)
			}
			if strings.Count(stderr, "Error:") != 1 {
				t.Fatalf("stderr must contain one rendered error, got %q", stderr)
			}
			if strings.Contains(stdout, "Error:") {
				t.Fatalf("stdout contains an error: %q", stdout)
			}
			if runnerCalls != 0 || clearCalls != 0 {
				t.Fatalf("runner/clear calls = %d/%d, want 0/0", runnerCalls, clearCalls)
			}
		})
	}
}

func TestCommandMissingPathReturnsOneErrorAndClearsScreen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	runnerCalls := 0
	clearCalls := 0

	code, stdout, stderr := runCommand(t, []string{path}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		runnerCalls++
		return scanner.ScanResult{}, nil
	}, func() {
		clearCalls++
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, fmt.Sprintf("Path '%s' does not exist!", path)) {
		t.Fatalf("stderr = %q, want missing path %q", stderr, path)
	}
	if strings.Count(stderr, "Error:") != 1 {
		t.Fatalf("stderr must contain one rendered error, got %q", stderr)
	}
	if runnerCalls != 0 || clearCalls != 1 {
		t.Fatalf("runner/clear calls = %d/%d, want 0/1", runnerCalls, clearCalls)
	}
}

func TestCommandPassesScanOptionsAndFormatsFilteredJSON(t *testing.T) {
	dir := t.TempDir()
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "present")

	var receivedPath string
	var received scanner.ScanOptions
	var timeoutRemaining time.Duration
	clearCalls := 0
	run := func(path string, options scanner.ScanOptions) (scanner.ScanResult, error) {
		receivedPath = path
		received = options
		if got := options.Ctx.Value(contextKey("request")); got != "present" {
			t.Errorf("context value = %v, want present", got)
		}
		deadline, ok := options.Ctx.Deadline()
		if !ok {
			t.Error("scan context has no timeout deadline")
		} else {
			timeoutRemaining = time.Until(deadline)
		}
		fmt.Fprintln(options.ProgressWriter, "scan-progress")
		return scanner.ScanResult{Items: []scanner.ItemInfo{
			{Name: "b", Size: 2 * 1024, Type: "file"},
			{Name: "too-large", Size: 3 * 1024, Type: "file"},
			{Name: "a", Size: 1024, Type: "directory"},
			{Name: "too-small", Size: 100, Type: "file"},
		}}, nil
	}

	code, stdout, stderr := runCommandContext(t, ctx, []string{
		"--sort", "name",
		"--asc",
		"--progress",
		"--no-clear",
		"--exclude-dirs", " node_modules, .git ",
		"--timeout", "2",
		"--depth", "3",
		"--min-size", "1KB",
		"--max-size", "2KB",
		"--json",
		dir,
	}, run, func() {
		clearCalls++
	})

	if code != 0 || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	wantPath, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if receivedPath != wantPath {
		t.Fatalf("scan path = %q, want %q", receivedPath, wantPath)
	}
	if got := strings.Join(received.ExcludeList, "|"); got != "node_modules|.git" {
		t.Fatalf("exclude list = %q, want node_modules|.git", got)
	}
	if !received.ShowProgress || received.MaxDepth != 3 {
		t.Fatalf("scan options = %+v, want progress and depth 3", received)
	}
	if timeoutRemaining < time.Second || timeoutRemaining > 3*time.Second {
		t.Fatalf("timeout remaining = %v, want approximately 2s", timeoutRemaining)
	}
	if clearCalls != 0 {
		t.Fatalf("clear calls = %d, want 0", clearCalls)
	}
	for _, want := range []string{
		"Analyzing: " + wantPath,
		"Excluding: node_modules, .git",
		"Calculating sizes (this may take a while for large directories)...",
		"scan-progress",
		`"name": "a"`,
		`"name": "b"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout does not contain %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "too-small") || strings.Contains(stdout, "too-large") {
		t.Fatalf("size filters were not applied:\n%s", stdout)
	}
	if strings.Index(stdout, `"name": "a"`) > strings.Index(stdout, `"name": "b"`) {
		t.Fatalf("JSON items are not sorted by name:\n%s", stdout)
	}
}

func TestCommandSortsResults(t *testing.T) {
	dir := t.TempDir()
	run := func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		return scanner.ScanResult{Items: []scanner.ItemInfo{
			{Name: "z-low", Size: 1, Type: "file"},
			{Name: "a-high", Size: 3, Type: "file"},
			{Name: "m-mid", Size: 2, Type: "file"},
		}}, nil
	}

	tests := []struct {
		name  string
		args  []string
		order []string
	}{
		{
			name:  "size ascending",
			args:  []string{"--no-clear", "--sort", "size", "--asc", dir},
			order: []string{"z-low", "m-mid", "a-high"},
		},
		{
			name:  "name descending",
			args:  []string{"--no-clear", "--sort", "name", dir},
			order: []string{"z-low", "m-mid", "a-high"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := runCommand(t, test.args, run, func() {
				t.Fatal("clear should be disabled")
			})
			if code != 0 || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr)
			}
			assertOrder(t, stdout, test.order...)
		})
	}
}

func TestExecuteContextUsesFreshCommandState(t *testing.T) {
	dir := t.TempDir()
	type invocation struct {
		showProgress bool
		excludeList  string
		maxDepth     int
		hasDeadline  bool
	}
	var invocations []invocation
	clearCalls := 0
	run := func(_ string, options scanner.ScanOptions) (scanner.ScanResult, error) {
		_, hasDeadline := options.Ctx.Deadline()
		invocations = append(invocations, invocation{
			showProgress: options.ShowProgress,
			excludeList:  strings.Join(options.ExcludeList, ","),
			maxDepth:     options.MaxDepth,
			hasDeadline:  hasDeadline,
		})
		return scanner.ScanResult{Items: []scanner.ItemInfo{
			{Name: "low", Size: 5, Type: "file"},
			{Name: "middle", Size: 15, Type: "file"},
			{Name: "high", Size: 25, Type: "file"},
		}}, nil
	}

	firstCode, firstStdout, firstStderr := runCommand(t, []string{
		"--progress",
		"--no-clear",
		"--exclude-dirs", "one,two",
		"--timeout", "1",
		"--depth", "2",
		"--min-size", "10B",
		"--max-size", "20B",
		"--json",
		dir,
	}, run, func() {
		clearCalls++
	})
	secondCode, secondStdout, secondStderr := runCommand(t, []string{dir}, run, func() {
		clearCalls++
	})

	if firstCode != 0 || secondCode != 0 || firstStderr != "" || secondStderr != "" {
		t.Fatalf("codes = %d/%d, stderr = %q/%q", firstCode, secondCode, firstStderr, secondStderr)
	}
	if len(invocations) != 2 {
		t.Fatalf("scanner calls = %d, want 2", len(invocations))
	}
	if !invocations[0].showProgress || invocations[0].excludeList != "one,two" || invocations[0].maxDepth != 2 || !invocations[0].hasDeadline {
		t.Fatalf("first invocation = %+v", invocations[0])
	}
	if invocations[1].showProgress || invocations[1].excludeList != "" || invocations[1].maxDepth != 0 || invocations[1].hasDeadline {
		t.Fatalf("second invocation leaked state: %+v", invocations[1])
	}
	if clearCalls != 1 {
		t.Fatalf("clear calls = %d, want 1 for second invocation", clearCalls)
	}
	if !strings.Contains(firstStdout, `"name": "middle"`) || strings.Contains(firstStdout, `"name": "low"`) || strings.Contains(firstStdout, `"name": "high"`) {
		t.Fatalf("first invocation filters/JSON are wrong:\n%s", firstStdout)
	}
	for _, name := range []string{"low", "middle", "high"} {
		if !strings.Contains(secondStdout, name) {
			t.Fatalf("second invocation is missing %q:\n%s", name, secondStdout)
		}
	}
	if strings.Contains(secondStdout, `"name"`) {
		t.Fatalf("second invocation leaked JSON mode:\n%s", secondStdout)
	}
	assertOrder(t, secondStdout, "high", "middle", "low")
}

func TestCommandRoutesScannerErrorsAndWarnings(t *testing.T) {
	dir := t.TempDir()

	t.Run("scanner error", func(t *testing.T) {
		code, stdout, stderr := runCommand(t, []string{"--no-clear", dir}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
			return scanner.ScanResult{Items: []scanner.ItemInfo{{Name: "partial", Size: 1, Type: "file"}}}, errors.New("scan failed")
		}, func() {
			t.Fatal("clear should be disabled")
		})

		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if strings.Contains(stdout, "scan failed") || strings.Contains(stdout, "Error:") {
			t.Fatalf("stdout contains scanner error: %q", stdout)
		}
		if !strings.Contains(stdout, "partial") {
			t.Fatalf("stdout does not contain partial scanner results: %q", stdout)
		}
		if !strings.Contains(stderr, "scan failed") || strings.Count(stderr, "Error:") != 1 {
			t.Fatalf("stderr = %q, want one scanner error", stderr)
		}
	})

	t.Run("scanner warning", func(t *testing.T) {
		code, stdout, stderr := runCommand(t, []string{"--no-clear", dir}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
			return scanner.ScanResult{WarningCount: 2}, nil
		}, func() {
			t.Fatal("clear should be disabled")
		})

		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		if strings.Contains(stdout, "Warning:") {
			t.Fatalf("stdout contains warning: %q", stdout)
		}
		if stderr != "Warning: 2 files/folders could not be accessed\n" {
			t.Fatalf("stderr = %q", stderr)
		}
	})
}

func TestCommandReturnsJSONEncodingError(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	code := executeContext(
		context.Background(),
		[]string{"--json", "--no-clear", dir},
		failingWriter{},
		&stderr,
		func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
			return scanner.ScanResult{Items: []scanner.ItemInfo{{Name: "item", Size: 1, Type: "file"}}}, nil
		},
		func() {
			t.Fatal("clear should be disabled")
		},
	)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "encoding JSON: write failed") || strings.Count(stderr.String(), "Error:") != 1 {
		t.Fatalf("stderr = %q, want one encoding error", stderr.String())
	}
}

func TestCommandDefaultsPathAndNormalizesNilInputs(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"host-process", "--host-only"}
	t.Cleanup(func() { os.Args = originalArgs })

	wantPath, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	var receivedPath string
	var receivedContext context.Context
	var receivedWriter bool
	clearCalls := 0

	code := executeContext(nil, nil, nil, nil, func(path string, options scanner.ScanOptions) (scanner.ScanResult, error) {
		receivedPath = path
		receivedContext = options.Ctx
		receivedWriter = options.ProgressWriter != nil
		return scanner.ScanResult{}, nil
	}, func() {
		clearCalls++
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if receivedPath != wantPath {
		t.Fatalf("scan path = %q, want %q", receivedPath, wantPath)
	}
	if receivedContext == nil || !receivedWriter {
		t.Fatalf("context/writer were not normalized: context=%v writer=%v", receivedContext, receivedWriter)
	}
	if clearCalls != 1 {
		t.Fatalf("clear calls = %d, want 1", clearCalls)
	}
}

func TestCommandHelpExitsZeroWithoutSideEffects(t *testing.T) {
	code, stdout, stderr := runCommand(t, []string{"--help"}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		t.Fatal("scanner should not run for help")
		return scanner.ScanResult{}, nil
	}, func() {
		t.Fatal("clear should not run for help")
	})

	if code != 0 || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "--max-size") {
		t.Fatalf("help output is incomplete:\n%s", stdout)
	}
}

func TestExecuteContextIntegrationSmoke(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := ExecuteContext(context.Background(), []string{"--json", "--no-clear", dir}, &stdout, &stderr)

	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	want := fmt.Sprintf(`Analyzing: %s
[
  {
    "name": "sample.txt",
    "size": 5,
    "type": "file"
  }
]
`, dir)
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func runCommand(t *testing.T, args []string, run scanRunner, clear clearScreen) (int, string, string) {
	t.Helper()
	return runCommandContext(t, context.Background(), args, run, clear)
}

func runCommandContext(
	t *testing.T,
	ctx context.Context,
	args []string,
	run scanRunner,
	clear clearScreen,
) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeContext(ctx, args, &stdout, &stderr, run, clear)
	return code, stdout.String(), stderr.String()
}

func assertOrder(t *testing.T, output string, values ...string) {
	t.Helper()
	previous := -1
	for _, value := range values {
		index := strings.Index(output, value)
		if index < 0 {
			t.Fatalf("output does not contain %q:\n%s", value, output)
		}
		if index <= previous {
			t.Fatalf("output does not order %v:\n%s", values, output)
		}
		previous = index
	}
}
