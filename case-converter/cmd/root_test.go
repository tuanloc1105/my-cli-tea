package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewRootCommandFlags(t *testing.T) {
	root := newRootCommand(
		func(context.Context, string, commandOptions, io.Writer) error { return nil },
		func() {},
		io.Discard,
		io.Discard,
	)

	if root.Use != "case-converter" {
		t.Fatalf("Use = %q, want case-converter", root.Use)
	}
	if !root.SilenceErrors || !root.SilenceUsage {
		t.Fatal("root command must silence Cobra error and usage rendering")
	}

	tests := []struct {
		name      string
		shorthand string
		defValue  string
		usage     string
	}{
		{
			name:      "file",
			shorthand: "f",
			defValue:  "",
			usage:     "Input file containing text to convert",
		},
		{
			name:     "all",
			defValue: "false",
			usage:    "Show all case conversions",
		},
		{
			name:     "format",
			defValue: "",
			usage:    "Specific format to output (normal, upper, lower, snake, kebab, camel, pascal, constant, title, dot, path)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flag := root.Flags().Lookup(test.name)
			if flag == nil {
				t.Fatalf("flag --%s was not registered", test.name)
			}
			if flag.Shorthand != test.shorthand || flag.DefValue != test.defValue || flag.Usage != test.usage {
				t.Fatalf(
					"flag --%s = shorthand %q, default %q, usage %q",
					test.name,
					flag.Shorthand,
					flag.DefValue,
					flag.Usage,
				)
			}
		})
	}
}

func TestExecuteContextForwardsInputAndOptions(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(filePath, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		args        []string
		wantInput   string
		wantOptions commandOptions
	}{
		{
			name:      "positional accepts extras and consumes the first",
			args:      []string{"first", "ignored", "also ignored"},
			wantInput: "first",
		},
		{
			name:      "flags after positional are parsed",
			args:      []string{"first", "--all", "--format", "upper", "ignored"},
			wantInput: "first",
			wantOptions: commandOptions{
				all:    true,
				format: "upper",
			},
		},
		{
			name:      "file takes precedence over positional",
			args:      []string{"--file", filePath, "positional"},
			wantInput: "from file",
			wantOptions: commandOptions{
				file: filePath,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotInput string
			var gotOptions commandOptions
			runCalls := 0
			clearCalls := 0
			run := func(_ context.Context, input string, options commandOptions, _ io.Writer) error {
				runCalls++
				gotInput = input
				gotOptions = options
				return nil
			}

			code, stdout, stderr := executeForTest(test.args, run, func() { clearCalls++ })
			if code != 0 || stdout != "" || stderr != "" {
				t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout, stderr)
			}
			if runCalls != 1 || clearCalls != 1 {
				t.Fatalf("runner/clear calls = %d/%d, want 1/1", runCalls, clearCalls)
			}
			if gotInput != test.wantInput || !reflect.DeepEqual(gotOptions, test.wantOptions) {
				t.Fatalf("input/options = %q/%+v, want %q/%+v", gotInput, gotOptions, test.wantInput, test.wantOptions)
			}
		})
	}
}

func TestExecuteContextOutputModes(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(filePath, []byte("hello_world\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "specific format",
			args: []string{"--format", "snake_case", "Hello World"},
			want: "hello_world\n",
		},
		{
			name: "format takes precedence over all",
			args: []string{"--all", "--format", "upper", "Hello World"},
			want: "HELLO WORLD\n",
		},
		{
			name: "unknown format keeps existing fallback",
			args: []string{"--format", "snake", "Hello World"},
			want: "Hello World\n",
		},
		{
			name: "file input",
			args: []string{"--file", filePath, "--format", "pascal_case", "ignored"},
			want: "HelloWorld\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := executeForTest(test.args, runConversions, func() {})
			if code != 0 || stdout != test.want || stderr != "" {
				t.Fatalf("code/stdout/stderr = %d/%q/%q, want 0/%q/empty", code, stdout, stderr, test.want)
			}
		})
	}
}

func TestExecuteContextLineProcessing(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantOriginals int
		wantContains  []string
		wantAbsent    []string
	}{
		{
			name:          "default uses only the trimmed first line",
			args:          []string{"  First Line  \nSecond Line"},
			wantOriginals: 1,
			wantContains:  []string{"Original \033[0m: First Line"},
			wantAbsent:    []string{"Second Line"},
		},
		{
			name:          "all uses every non-empty line",
			args:          []string{"--all", "First Line\n\nSecond Line"},
			wantOriginals: 2,
			wantContains:  []string{"Original \033[0m: First Line", "Original \033[0m: Second Line"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := executeForTest(test.args, runConversions, func() {})
			if code != 0 || stderr != "" {
				t.Fatalf("code/stderr = %d/%q", code, stderr)
			}
			if got := strings.Count(stdout, "Original \033[0m:"); got != test.wantOriginals {
				t.Fatalf("Original count = %d, want %d; output = %q", got, test.wantOriginals, stdout)
			}
			for _, want := range test.wantContains {
				if !strings.Contains(stdout, want) {
					t.Fatalf("stdout does not contain %q: %q", want, stdout)
				}
			}
			for _, absent := range test.wantAbsent {
				if strings.Contains(stdout, absent) {
					t.Fatalf("stdout unexpectedly contains %q: %q", absent, stdout)
				}
			}
		})
	}
}

func TestExecuteContextHelp(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"host-process", "--host-only"}
	t.Cleanup(func() { os.Args = originalArgs })

	tests := []struct {
		name      string
		args      []string
		wantClear int
	}{
		{name: "explicit help", args: []string{"--help"}},
		{name: "no input", args: []string{}, wantClear: 1},
		{name: "nil args", args: nil, wantClear: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearCalls := 0
			runCalls := 0
			run := func(context.Context, string, commandOptions, io.Writer) error {
				runCalls++
				return nil
			}
			code, stdout, stderr := executeForTest(test.args, run, func() { clearCalls++ })
			if code != 0 || stderr != "" {
				t.Fatalf("code/stderr = %d/%q", code, stderr)
			}
			if runCalls != 0 || clearCalls != test.wantClear {
				t.Fatalf("runner/clear calls = %d/%d, want 0/%d", runCalls, clearCalls, test.wantClear)
			}
			for _, want := range []string{
				"Case Converter CLI Tool - A command-line tool for text case conversion and transformation.",
				"Usage:\n  case-converter [flags]",
				"-f, --file string",
				"--format string",
			} {
				if !strings.Contains(stdout, want) {
					t.Fatalf("help output does not contain %q: %q", want, stdout)
				}
			}
		})
	}
}

func TestExecuteContextErrors(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		run       conversionRunner
		wantError string
		wantClear int
	}{
		{
			name:      "unknown flag",
			args:      []string{"--unknown"},
			run:       runConversions,
			wantError: "Error: unknown flag: --unknown\n",
		},
		{
			name: "runner error",
			args: []string{"input"},
			run: func(context.Context, string, commandOptions, io.Writer) error {
				return errors.New("conversion failed")
			},
			wantError: "Error: conversion failed\n",
			wantClear: 1,
		},
		{
			name:      "nil runner",
			args:      []string{"input"},
			wantError: "Error: conversion runner is nil\n",
			wantClear: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearCalls := 0
			code, stdout, stderr := executeForTest(test.args, test.run, func() { clearCalls++ })
			if code != 1 || stdout != "" || stderr != test.wantError {
				t.Fatalf("code/stdout/stderr = %d/%q/%q, want 1/empty/%q", code, stdout, stderr, test.wantError)
			}
			if clearCalls != test.wantClear {
				t.Fatalf("clear calls = %d, want %d", clearCalls, test.wantClear)
			}
		})
	}
}

func TestExecuteContextMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	clearCalls := 0
	runCalls := 0
	run := func(context.Context, string, commandOptions, io.Writer) error {
		runCalls++
		return nil
	}

	code, stdout, stderr := executeForTest(
		[]string{"--file", missing, "ignored"},
		run,
		func() { clearCalls++ },
	)
	if code != 1 || stdout != "" {
		t.Fatalf("code/stdout = %d/%q, want 1/empty", code, stdout)
	}
	if runCalls != 0 || clearCalls != 1 {
		t.Fatalf("runner/clear calls = %d/%d, want 0/1", runCalls, clearCalls)
	}
	if !strings.HasPrefix(stderr, "Error: reading file: ") ||
		!strings.Contains(stderr, missing) ||
		strings.Count(stderr, "\n") != 1 {
		t.Fatalf("stderr = %q, want one missing-file error line", stderr)
	}
}

func TestExecuteContextNormalizesContextAndWriters(t *testing.T) {
	runCalls := 0
	run := func(ctx context.Context, _ string, _ commandOptions, stdout io.Writer) error {
		runCalls++
		if ctx == nil {
			t.Fatal("runner received a nil context")
		}
		if stdout == nil {
			t.Fatal("runner received a nil stdout writer")
		}
		return nil
	}

	code := executeContext(nil, []string{"input"}, nil, nil, run, nil)
	if code != 0 || runCalls != 1 {
		t.Fatalf("code/runner calls = %d/%d, want 0/1", code, runCalls)
	}
}

func TestExecuteContextPassesCancellationToRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run := func(ctx context.Context, _ string, _ commandOptions, _ io.Writer) error {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("context error = %v, want context canceled", ctx.Err())
		}
		return nil
	}
	var stdout, stderr bytes.Buffer

	code := executeContext(ctx, []string{"input"}, &stdout, &stderr, run, func() {})
	if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("code/stdout/stderr = %d/%q/%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteContextDoesNotLeakOptionsBetweenInvocations(t *testing.T) {
	var invocations []commandOptions
	run := func(_ context.Context, _ string, options commandOptions, _ io.Writer) error {
		invocations = append(invocations, options)
		return nil
	}

	firstCode, _, firstStderr := executeForTest(
		[]string{"--all", "--format", "upper", "first"},
		run,
		func() {},
	)
	secondCode, _, secondStderr := executeForTest([]string{"second"}, run, func() {})
	if firstCode != 0 || secondCode != 0 || firstStderr != "" || secondStderr != "" {
		t.Fatalf("codes/stderr = %d,%d/%q,%q", firstCode, secondCode, firstStderr, secondStderr)
	}
	want := []commandOptions{{all: true, format: "upper"}, {}}
	if !reflect.DeepEqual(invocations, want) {
		t.Fatalf("invocations = %+v, want %+v", invocations, want)
	}
}

func executeForTest(
	args []string,
	run conversionRunner,
	clearScreen clearScreenFunc,
) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := executeContext(context.Background(), args, &stdout, &stderr, run, clearScreen)
	return code, stdout.String(), stderr.String()
}
