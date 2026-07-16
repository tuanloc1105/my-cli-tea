package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"check-folder-size/internal/scanner"
)

func TestCommandRejectsInvalidArgumentsFlagsAndSizesBeforeScan(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "too many arguments", args: []string{dir, dir}, want: "accepts at most 1 arg"},
		{name: "unknown flag", args: []string{"--unknown", dir}, want: "unknown flag"},
		{name: "invalid sort", args: []string{"--sort", "modified", dir}, want: "--sort must be 'size' or 'name'"},
		{name: "invalid size mode", args: []string{"--size-mode", "physical", dir}, want: "invalid --size-mode"},
		{name: "negative depth", args: []string{"--depth=-1", dir}, want: "--depth must be non-negative"},
		{name: "negative timeout", args: []string{"--timeout=-1", dir}, want: "--timeout must be non-negative"},
		{name: "overflowing timeout", args: []string{"--timeout=9223372037", dir}, want: "overflows time.Duration"},
		{name: "negative minimum", args: []string{"--min-size=-1KB", dir}, want: "non-negative decimal number"},
		{name: "nan maximum", args: []string{"--max-size=NaN", dir}, want: "decimal number"},
		{name: "positive infinity maximum", args: []string{"--max-size=+Inf", dir}, want: "decimal number"},
		{name: "negative infinity maximum", args: []string{"--max-size=-Inf", dir}, want: "decimal number"},
		{name: "overflowing maximum", args: []string{"--max-size=8388608TB", dir}, want: "overflows int64"},
		{name: "minimum exceeds maximum", args: []string{"--min-size=2KB", "--max-size=1KB", dir}, want: "must not exceed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runnerCalls := 0
			clearCalls := 0
			code, stdout, stderr := runCommand(t, test.args, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
				runnerCalls++
				return completeResult(nil), nil
			}, func() {
				clearCalls++
			})

			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, test.want) || strings.Count(stderr, "Error:") != 1 {
				t.Fatalf("stderr = %q, want one error containing %q", stderr, test.want)
			}
			if runnerCalls != 0 || clearCalls != 0 {
				t.Fatalf("runner/clear calls = %d/%d, want 0/0", runnerCalls, clearCalls)
			}
		})
	}
}

func TestCommandValidatesDirectoryAndAcceptsRootSymlink(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "missing path", path: filepath.Join(root, "missing"), want: "does not exist"},
		{name: "file input", path: filePath, want: "is not a directory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			code, stdout, stderr := runCommand(t, []string{"--json", test.path}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
				calls++
				return completeResult(nil), nil
			}, func() { t.Fatal("clear must not run") })
			if code != 1 || stdout != "" || calls != 0 {
				t.Fatalf("code/stdout/calls = %d/%q/%d, want 1/empty/0", code, stdout, calls)
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr = %q, want %q", stderr, test.want)
			}
		})
	}

	linkPath := filepath.Join(root, "directory-link")
	if err := os.Symlink(root, linkPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating symlink requires additional Windows privileges: %v", err)
		}
		t.Fatal(err)
	}
	var receivedPath string
	code, stdout, stderr := runCommand(t, []string{"--json", linkPath}, func(path string, _ scanner.ScanOptions) (scanner.ScanResult, error) {
		receivedPath = path
		return completeResult(nil), nil
	}, func() { t.Fatal("JSON must not clear") })
	wantPath, err := filepath.Abs(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || stderr != "" || strings.TrimSpace(stdout) != "[]" || receivedPath != wantPath {
		t.Fatalf("code/stdout/stderr/path = %d/%q/%q/%q, want 0/[]/empty/%q", code, stdout, stderr, receivedPath, wantPath)
	}
}

func TestCommandPassesOptionsAndFiltersSelectedMetric(t *testing.T) {
	dir := t.TempDir()
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "present")

	var received scanner.ScanOptions
	var timeoutRemaining time.Duration
	run := func(_ string, options scanner.ScanOptions) (scanner.ScanResult, error) {
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
		return completeResult([]scanner.ItemInfo{
			{Name: "selected-two", Size: 2 * 1024, Type: scanner.ItemTypeFile},
			{Name: "too-large", Size: 3 * 1024, Type: scanner.ItemTypeFile},
			{Name: "selected-one", Size: 1024, Type: scanner.ItemTypeDirectory},
			{Name: "too-small", Size: 100, Type: scanner.ItemTypeFile},
		}), nil
	}

	code, stdout, stderr := runCommandContext(t, ctx, []string{
		"--sort", "name",
		"--asc",
		"--exclude-dirs", " node_modules, , .git ",
		"--timeout", "2",
		"--depth", "3",
		"--min-size", "1KB",
		"--max-size", "2KB",
		"--size-mode", "logical",
		"--json",
		dir,
	}, run, func() { t.Fatal("JSON must not clear") })

	if code != 0 || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	items := decodeSingleJSON(t, stdout)
	wantItems := []scanner.ItemInfo{
		{Name: "selected-one", Size: 1024, Type: scanner.ItemTypeDirectory},
		{Name: "selected-two", Size: 2 * 1024, Type: scanner.ItemTypeFile},
	}
	if !reflect.DeepEqual(items, wantItems) {
		t.Fatalf("items = %#v, want %#v", items, wantItems)
	}
	if received.SizeMode != scanner.SizeModeLogical || received.ShowProgress || received.MaxDepth != 3 {
		t.Fatalf("scan options = %+v", received)
	}
	if got := strings.Join(received.ExcludeList, "|"); got != "node_modules|.git" {
		t.Fatalf("exclude list = %q, want node_modules|.git", got)
	}
	if timeoutRemaining < time.Second || timeoutRemaining > 3*time.Second {
		t.Fatalf("timeout remaining = %v, want approximately 2s", timeoutRemaining)
	}
}

func TestCommandDefaultsToAllocatedSizeMode(t *testing.T) {
	dir := t.TempDir()
	var mode scanner.SizeMode
	code, _, stderr := runCommand(t, []string{"--json", dir}, func(_ string, options scanner.ScanOptions) (scanner.ScanResult, error) {
		mode = options.SizeMode
		return completeResult(nil), nil
	}, func() { t.Fatal("JSON must not clear") })
	if code != 0 || stderr != "" || mode != scanner.SizeModeAllocated {
		t.Fatalf("code/stderr/mode = %d/%q/%q, want 0/empty/allocated", code, stderr, mode)
	}
}

func TestCommandUsesSameStableOrderForJSONAndTerminal(t *testing.T) {
	dir := t.TempDir()
	input := []scanner.ItemInfo{
		{Name: "charlie", Size: 1, Type: scanner.ItemTypeFile},
		{Name: "bravo", Size: 3, Type: scanner.ItemTypeFile},
		{Name: "alpha", Size: 3, Type: scanner.ItemTypeFile},
		{Name: "delta", Size: 2, Type: scanner.ItemTypeFile},
	}
	run := func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		return completeResult(input), nil
	}
	tests := []struct {
		name  string
		flags []string
		want  []string
	}{
		{name: "default size descending with name tie-break", want: []string{"alpha", "bravo", "delta", "charlie"}},
		{name: "size ascending", flags: []string{"--asc"}, want: []string{"charlie", "delta", "alpha", "bravo"}},
		{name: "name descending", flags: []string{"--sort", "name"}, want: []string{"delta", "charlie", "bravo", "alpha"}},
		{name: "name ascending", flags: []string{"--sort", "name", "--asc"}, want: []string{"alpha", "bravo", "charlie", "delta"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			terminalArgs := append(append([]string{"--no-clear"}, test.flags...), dir)
			terminalCode, terminalOut, terminalErr := runCommand(t, terminalArgs, run, func() { t.Fatal("clear disabled") })
			jsonArgs := append(append([]string{"--json"}, test.flags...), dir)
			jsonCode, jsonOut, jsonErr := runCommand(t, jsonArgs, run, func() { t.Fatal("JSON must not clear") })
			if terminalCode != 0 || jsonCode != 0 || terminalErr != "" || jsonErr != "" {
				t.Fatalf("codes = %d/%d, stderr = %q/%q", terminalCode, jsonCode, terminalErr, jsonErr)
			}
			assertOrder(t, terminalOut, test.want...)
			decoded := decodeSingleJSON(t, jsonOut)
			got := make([]string, len(decoded))
			for i, item := range decoded {
				got[i] = item.Name
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("JSON order = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCommandJSONIsQuietAndEmptyArrayIsNonNull(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runCommand(t, []string{"--json", dir}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		return completeResult(nil), nil
	}, func() { t.Fatal("JSON must not clear") })
	if code != 0 || stderr != "" || stdout != "[]\n" {
		t.Fatalf("code/stdout/stderr = %d/%q/%q, want 0/[]\\n/empty", code, stdout, stderr)
	}
	decodeSingleJSON(t, stdout)
}

func TestCommandRoutesExplicitJSONProgressToStderr(t *testing.T) {
	dir := t.TempDir()
	run := func(_ string, options scanner.ScanOptions) (scanner.ScanResult, error) {
		if !options.ShowProgress {
			t.Error("ShowProgress = false, want true")
		}
		fmt.Fprintln(options.ProgressWriter, "scan-progress")
		return completeResult([]scanner.ItemInfo{{Name: "item", Size: 1, Type: scanner.ItemTypeFile}}), nil
	}
	code, stdout, stderr := runCommand(t, []string{"--json", "--progress", dir}, run, func() { t.Fatal("JSON must not clear") })
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	items := decodeSingleJSON(t, stdout)
	if len(items) != 1 || strings.Contains(stdout, "Analyzing") || strings.Contains(stdout, "scan-progress") || strings.Contains(stdout, "\x1b[") {
		t.Fatalf("stdout is not a clean JSON payload: %q", stdout)
	}
	for _, want := range []string{"Analyzing:", "Calculating sizes", "scan-progress", "Analysis completed"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr does not contain %q: %q", want, stderr)
		}
	}
}

func TestCommandPartialResultsRenderPayloadWarningAndExitOne(t *testing.T) {
	dir := t.TempDir()
	run := func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		return scanner.ScanResult{
			Items:          []scanner.ItemInfo{{Name: "partial", Size: 1, Type: scanner.ItemTypeFile}},
			WarningCount:   2,
			WarningSummary: "metadata unavailable",
			Status:         scanner.ScanStatusPartial,
		}, errors.New("deadline exceeded")
	}

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "JSON", args: []string{"--json", dir}},
		{name: "terminal", args: []string{"--no-clear", dir}},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := runCommand(t, test.args, run, func() { t.Fatal("clear disabled") })
			if code != 1 || !strings.Contains(stdout, "partial") {
				t.Fatalf("code/stdout = %d/%q, want 1 with partial payload", code, stdout)
			}
			if test.name == "JSON" {
				decodeSingleJSON(t, stdout)
			}
			if !strings.Contains(stderr, "Warning:") || !strings.Contains(stderr, "metadata unavailable") || !strings.Contains(stderr, "deadline exceeded") {
				t.Fatalf("stderr = %q, want partial warning", stderr)
			}
			if strings.Contains(stderr, "Error:") {
				t.Fatalf("partial result rendered as fatal error: %q", stderr)
			}
		})
	}
}

func TestCommandWarningOnlyPartialResultExitsOne(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runCommand(t, []string{"--json", dir}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		return scanner.ScanResult{
			Items:          []scanner.ItemInfo{},
			WarningCount:   1,
			WarningSummary: "metadata unavailable",
			Status:         scanner.ScanStatusPartial,
		}, nil
	}, func() { t.Fatal("JSON must not clear") })
	if code != 1 || stdout != "[]\n" || !strings.Contains(stderr, "Warning: metadata unavailable") || strings.Contains(stderr, "Error:") {
		t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
}

func TestCommandFatalScanKeepsJSONStdoutEmpty(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runCommand(t, []string{"--json", dir}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		return scanner.ScanResult{Items: []scanner.ItemInfo{}, Status: scanner.ScanStatusFailed}, errors.New("root open failed")
	}, func() { t.Fatal("JSON must not clear") })
	if code != 1 || stdout != "" {
		t.Fatalf("code/stdout = %d/%q, want 1/empty", code, stdout)
	}
	if !strings.Contains(stderr, "root open failed") || strings.Count(stderr, "Error:") != 1 {
		t.Fatalf("stderr = %q, want one fatal error", stderr)
	}
}

func TestCommandCancellationWithoutDataIsFatal(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runCommand(t, []string{"--json", dir}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		return scanner.ScanResult{Items: []scanner.ItemInfo{}, Status: scanner.ScanStatusPartial}, context.Canceled
	}, func() { t.Fatal("JSON must not clear") })
	if code != 1 || stdout != "" {
		t.Fatalf("code/stdout = %d/%q, want 1/empty", code, stdout)
	}
	if !strings.Contains(stderr, "context canceled") || strings.Count(stderr, "Error:") != 1 {
		t.Fatalf("stderr = %q, want one fatal cancellation error", stderr)
	}
}

func TestCommandClearsOnlyInteractiveTerminalOutput(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name       string
		args       []string
		isTerminal bool
		wantClear  bool
	}{
		{name: "TTY terminal", args: []string{dir}, isTerminal: true, wantClear: true},
		{name: "non-TTY terminal", args: []string{dir}, isTerminal: false, wantClear: false},
		{name: "no-clear TTY terminal", args: []string{"--no-clear", dir}, isTerminal: true, wantClear: false},
		{name: "JSON TTY", args: []string{"--json", dir}, isTerminal: true, wantClear: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			clearCalls := 0
			code := executeContextWithTerminal(
				context.Background(),
				test.args,
				&stdout,
				&stderr,
				func(string, scanner.ScanOptions) (scanner.ScanResult, error) { return completeResult(nil), nil },
				func() {
					clearCalls++
					fmt.Fprint(&stdout, "\x1b[2J")
				},
				func(io.Writer) bool { return test.isTerminal },
			)
			if code != 0 || stderr.Len() != 0 {
				t.Fatalf("code/stderr = %d/%q", code, stderr.String())
			}
			wantCalls := 0
			if test.wantClear {
				wantCalls = 1
			}
			if clearCalls != wantCalls || strings.Contains(stdout.String(), "\x1b[2J") != test.wantClear {
				t.Fatalf("clear calls/output = %d/%q, want clear=%v", clearCalls, stdout.String(), test.wantClear)
			}
		})
	}
}

func TestExecuteContextUsesFreshCommandState(t *testing.T) {
	dir := t.TempDir()
	var modes []scanner.SizeMode
	var progresses []bool
	run := func(_ string, options scanner.ScanOptions) (scanner.ScanResult, error) {
		modes = append(modes, options.SizeMode)
		progresses = append(progresses, options.ShowProgress)
		return completeResult(nil), nil
	}
	firstCode, _, firstStderr := runCommand(t, []string{"--json", "--progress", "--size-mode", "logical", dir}, run, func() { t.Fatal("JSON must not clear") })
	secondCode, _, secondStderr := runCommand(t, []string{"--json", dir}, run, func() { t.Fatal("JSON must not clear") })
	if firstCode != 0 || secondCode != 0 || len(modes) != 2 || modes[0] != scanner.SizeModeLogical || modes[1] != scanner.SizeModeAllocated || !progresses[0] || progresses[1] {
		t.Fatalf("codes=%d/%d modes=%v progresses=%v stderr=%q/%q", firstCode, secondCode, modes, progresses, firstStderr, secondStderr)
	}
}

func TestCommandReturnsJSONEncodingError(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	code := executeContext(
		context.Background(),
		[]string{"--json", dir},
		failingWriter{},
		&stderr,
		func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
			return completeResult([]scanner.ItemInfo{{Name: "item", Size: 1, Type: scanner.ItemTypeFile}}), nil
		},
		func() { t.Fatal("JSON must not clear") },
	)
	if code != 1 || !strings.Contains(stderr.String(), "encoding JSON: write failed") || strings.Count(stderr.String(), "Error:") != 1 {
		t.Fatalf("code/stderr = %d/%q, want one encoding error", code, stderr.String())
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
	code := executeContext(nil, nil, nil, nil, func(path string, options scanner.ScanOptions) (scanner.ScanResult, error) {
		receivedPath = path
		receivedContext = options.Ctx
		receivedWriter = options.ProgressWriter != nil
		return completeResult(nil), nil
	}, func() { t.Fatal("discard writer is not a TTY") })
	if code != 0 || receivedPath != wantPath || receivedContext == nil || !receivedWriter {
		t.Fatalf("code/path/context/writer = %d/%q/%v/%v", code, receivedPath, receivedContext, receivedWriter)
	}
}

func TestCommandHelpExitsZeroWithoutSideEffects(t *testing.T) {
	code, stdout, stderr := runCommand(t, []string{"--help"}, func(string, scanner.ScanOptions) (scanner.ScanResult, error) {
		t.Fatal("scanner should not run for help")
		return scanner.ScanResult{}, nil
	}, func() { t.Fatal("clear should not run for help") })
	if code != 0 || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"Usage:", "--max-size", "--size-mode"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help output is missing %q:\n%s", want, stdout)
		}
	}
}

func TestExecuteContextIntegrationSmoke(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := ExecuteContext(context.Background(), []string{"--json", "--size-mode", "logical", dir}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	items := decodeSingleJSON(t, stdout.String())
	if len(items) != 1 || items[0].Name != "sample.txt" || items[0].Size != 5 {
		t.Fatalf("items = %#v", items)
	}
}

func TestParseSizeUsesExactDecimalBinaryUnitsAndChecksOverflow(t *testing.T) {
	valid := map[string]int64{
		"0":                                  0,
		"1B":                                 1,
		"2kb":                                2 * 1024,
		"3MB":                                3 * 1024 * 1024,
		"4GB":                                4 * 1024 * 1024 * 1024,
		"1TB":                                1024 * 1024 * 1024 * 1024,
		"1.5KB":                              1536,
		".5KB":                               512,
		"1.B":                                1,
		"0.9B":                               0,
		"inf":                                math.MaxInt64,
		"INF":                                math.MaxInt64,
		strconv.FormatInt(math.MaxInt64, 10): math.MaxInt64,
	}
	for input, want := range valid {
		t.Run("valid "+input, func(t *testing.T) {
			got, err := parseSize(input)
			if err != nil || got != want {
				t.Fatalf("parseSize(%q) = %d, %v; want %d, nil", input, got, err, want)
			}
		})
	}
	for _, input := range []string{"", ".", "1.2.3KB", "-1", "+1", "NaN", "+Inf", "-Inf", "1PB", "8388608TB", "9223372036854775808"} {
		t.Run("invalid "+input, func(t *testing.T) {
			if _, err := parseSize(input); err == nil {
				t.Fatalf("parseSize(%q) unexpectedly succeeded", input)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func completeResult(items []scanner.ItemInfo) scanner.ScanResult {
	if items == nil {
		items = []scanner.ItemInfo{}
	}
	return scanner.ScanResult{Items: items, Status: scanner.ScanStatusComplete}
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

func decodeSingleJSON(t *testing.T, output string) []scanner.ItemInfo {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	var items []scanner.ItemInfo
	if err := decoder.Decode(&items); err != nil {
		t.Fatalf("decoding JSON %q: %v", output, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contains more than one JSON value: %q (error %v)", output, err)
	}
	if items == nil {
		t.Fatalf("JSON decoded to nil slice: %q", output)
	}
	return items
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
