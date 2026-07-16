package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandHelpAndFreshState(t *testing.T) {
	code, stdout, stderr := runCommand(t, []string{"--help"}, func(context.Context, []string, commandOptions, io.Writer, io.Writer) error {
		t.Fatal("runner called for help")
		return nil
	})
	if code != 0 || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q", code, stderr)
	}
	for _, required := range []string{
		"Usage:", "--no-default-excludes", "--max-workers", "--max-line-size",
		"--max-multiline-size", "--show-hidden", "exit code 1", "exit code 2",
	} {
		if !strings.Contains(stdout, required) {
			t.Errorf("help is missing %q:\n%s", required, stdout)
		}
	}

	var received []commandOptions
	run := func(_ context.Context, _ []string, options commandOptions, _ io.Writer, _ io.Writer) error {
		received = append(received, options)
		return nil
	}
	firstCode, _, firstErr := runCommand(t, []string{"--regex", "--max-workers=2", "root", "word"}, run)
	secondCode, _, secondErr := runCommand(t, []string{"root", "word"}, run)
	if firstCode != 0 || secondCode != 0 || firstErr != "" || secondErr != "" {
		t.Fatalf("codes/stderr = %d/%d/%q/%q", firstCode, secondCode, firstErr, secondErr)
	}
	if len(received) != 2 || !received[0].useRegex || received[0].maxWorkers != 2 {
		t.Fatalf("received options = %+v", received)
	}
	defaults := defaultCommandOptions()
	if received[1] != defaults {
		t.Fatalf("second invocation leaked state: %+v, want %+v", received[1], defaults)
	}
}

func TestCommandArgumentAndValidationErrorsExitTwo(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing args", want: "accepts 2 arg(s)"},
		{name: "empty keyword", args: []string{"root", ""}, want: "keyword must not be empty"},
		{name: "negative results", args: []string{"--max-results=-1", "root", "word"}, want: "must not be negative"},
		{name: "zero workers", args: []string{"--max-workers=0", "root", "word"}, want: "at least 1"},
		{name: "zero line size", args: []string{"--max-line-size=0", "root", "word"}, want: "greater than 0"},
		{name: "zero multiline size", args: []string{"--max-multiline-size=0", "root", "word"}, want: "greater than 0"},
		{name: "all with extensions", args: []string{"--all", "--extensions=txt", "root", "word"}, want: "cannot be used"},
		{name: "list search flag", args: []string{"--list", "--regex", "root"}, want: "cannot be used with --list"},
		{name: "list suppress warnings", args: []string{"--list", "--suppress-warnings", "root"}, want: "cannot be used with --list"},
		{name: "show hidden in search", args: []string{"--show-hidden", "root", "word"}, want: "only be used with --list"},
		{name: "unknown flag", args: []string{"--unknown", "root", "word"}, want: "unknown flag"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			code, stdout, stderr := runCommand(t, test.args, func(context.Context, []string, commandOptions, io.Writer, io.Writer) error {
				calls++
				return nil
			})
			if code != 2 || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("exit/stdout/stderr = %d/%q/%q, want code 2 and %q", code, stdout, stderr, test.want)
			}
			if calls != 0 || strings.Count(stderr, "Error:") != 1 || strings.Contains(stderr, "Usage:") {
				t.Fatalf("invalid error routing: calls=%d stderr=%q", calls, stderr)
			}
		})
	}
}

func TestCommandForwardsArgumentsOptionsAndStreams(t *testing.T) {
	var gotArgs []string
	var gotOptions commandOptions
	code, stdout, stderr := runCommand(t, []string{
		"--regex", "--case-sensitive", "--multiline", "--extensions=go, txt",
		"--exclude-dirs=vendor", "--exclude-files=skip.go", "--no-default-excludes",
		"--no-line-numbers", "--no-file-path", "--max-results=7", "--max-workers=2",
		"--max-line-size=99", "--max-multiline-size=101", "--suppress-warnings",
		"fixture", "needle",
	}, func(_ context.Context, args []string, options commandOptions, stdout, stderr io.Writer) error {
		gotArgs = append([]string(nil), args...)
		gotOptions = options
		fmt.Fprint(stdout, "out")
		fmt.Fprint(stderr, "err")
		return nil
	})
	if code != 0 || stdout != "out" || stderr != "err" {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
	if strings.Join(gotArgs, ",") != "fixture,needle" || !gotOptions.useRegex || !gotOptions.caseSensitive || !gotOptions.multiline || gotOptions.maxWorkers != 2 || gotOptions.maxLineSize != 99 || gotOptions.maxMultilineSize != 101 {
		t.Fatalf("args/options = %v/%+v", gotArgs, gotOptions)
	}
}

func TestListRejectsEverySearchOnlyFlag(t *testing.T) {
	flags := [][]string{
		{"--regex"},
		{"--case-sensitive"},
		{"--multiline"},
		{"--extensions=txt"},
		{"--exclude-dirs=skip"},
		{"--exclude-files=skip.txt"},
		{"--no-default-excludes"},
		{"--no-line-numbers"},
		{"--no-file-path"},
		{"--max-results=1"},
		{"--max-workers=1"},
		{"--max-line-size=1"},
		{"--max-multiline-size=1"},
		{"--all"},
		{"--suppress-warnings"},
	}
	for _, flag := range flags {
		t.Run(flag[0], func(t *testing.T) {
			args := append([]string{"--list"}, flag...)
			args = append(args, "root")
			code, stdout, stderr := runCommand(t, args, func(context.Context, []string, commandOptions, io.Writer, io.Writer) error {
				t.Fatal("runner called for incompatible list flag")
				return nil
			})
			if code != 2 || stdout != "" || !strings.Contains(stderr, "cannot be used with --list") {
				t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
			}
		})
	}
}

func TestExecuteContextSearchExitCodesAndFormatting(t *testing.T) {
	fixture := newSearchFixture(t)

	t.Run("match", func(t *testing.T) {
		code, stdout, stderr := runIntegration([]string{"--extensions=txt", "--exclude-dirs=sub,.hidden", "--exclude-files=.hidden.txt", fixture.root, "needle"})
		want := fmt.Sprintf("%s:1:Alpha needle\n%s:2:second needle\n\nFound 2 match(es)\n", fixture.textFile, fixture.textFile)
		if code != 0 || stdout != want || stderr != "" {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q, want 0/%q/empty", code, stdout, stderr, want)
		}
	})

	t.Run("clean no match", func(t *testing.T) {
		code, stdout, stderr := runIntegration([]string{fixture.root, "absent"})
		if code != 1 || stdout != "No matches found\n" || stderr != "" {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
	})

	t.Run("invalid regex", func(t *testing.T) {
		code, stdout, stderr := runIntegration([]string{"--regex", fixture.root, "["})
		if code != 2 || stdout != "" || !strings.Contains(stderr, "Error: invalid search pattern:") {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
	})

	t.Run("missing root", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		code, stdout, stderr := runIntegration([]string{missing, "needle"})
		if code != 2 || stdout != "" || !strings.Contains(stderr, "inspect root") {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
	})

	t.Run("keyword begins with dash", func(t *testing.T) {
		code, stdout, stderr := runIntegration([]string{"--no-file-path", "--no-line-numbers", fixture.root, "--", "--token"})
		if code != 0 || !strings.Contains(stdout, "literal --token") || stderr != "" {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
	})

	t.Run("multiline range", func(t *testing.T) {
		code, stdout, stderr := runIntegration([]string{
			"--multiline", "--extensions=txt", "--exclude-dirs=sub,.hidden", "--exclude-files=.hidden.txt",
			"--no-file-path", fixture.root, `multi start\nmulti end`,
		})
		if code != 0 || stdout != "3..4:multi start\\nmulti end\n\nFound 1 match(es)\n" || stderr != "" {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
	})
}

func TestExecuteContextHiddenListAndLegacySyntax(t *testing.T) {
	fixture := newSearchFixture(t)

	code, stdout, stderr := runIntegration([]string{"--no-file-path", "--no-line-numbers", fixture.root, "hidden needle"})
	if code != 0 || stdout != "hidden needle\nhidden needle\n\nFound 2 match(es)\n" || stderr != "" {
		t.Fatalf("hidden search = %d/%q/%q", code, stdout, stderr)
	}

	code, stdout, stderr = runIntegration([]string{"--list", fixture.root})
	if code != 0 || stderr != "" || strings.Contains(stdout, ".hidden.txt") || !strings.Contains(stdout, "a.txt") {
		t.Fatalf("canonical list = %d/%q/%q", code, stdout, stderr)
	}

	code, stdout, stderr = runIntegration([]string{"--list", "--show-hidden", fixture.root})
	if code != 0 || stderr != "" || !strings.Contains(stdout, ".hidden.txt") {
		t.Fatalf("show hidden list = %d/%q/%q", code, stdout, stderr)
	}

	code, _, stderr = runIntegration([]string{"--list", fixture.root, "unused"})
	if code != 0 || stderr != "Warning: the two-argument --list form is deprecated; use find-content --list <directory>\n" {
		t.Fatalf("legacy list = %d/%q", code, stderr)
	}
}

func TestExecuteContextMaxResultsNormalizationAndBinaryPolicy(t *testing.T) {
	fixture := newSearchFixture(t)
	code, stdout, stderr := runIntegration([]string{
		"--extensions", " txt, txt, ,go ", "--no-file-path", "--no-line-numbers",
		"--exclude-dirs=.hidden", "--exclude-files=.hidden.txt",
		"--max-results=2", fixture.root, "needle",
	})
	if code != 0 || stderr != "" || stdout != "Alpha needle\nsecond needle\n\nFound 2 match(es)\n" {
		t.Fatalf("normalized exact cap = %d/%q/%q", code, stdout, stderr)
	}

	code, stdout, stderr = runIntegration([]string{"--all", fixture.root, "binary needle"})
	if code != 1 || stdout != "No matches found\n" || stderr != "" {
		t.Fatalf("binary policy = %d/%q/%q", code, stdout, stderr)
	}
}

func TestExecuteContextPartialErrorsAndSuppression(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.txt"), "1234567\nneedle\n")

	for _, suppress := range []bool{false, true} {
		args := []string{"--max-line-size=6", "--no-file-path", "--no-line-numbers", root, "needle"}
		if suppress {
			args = append([]string{"--suppress-warnings"}, args...)
		}
		code, stdout, stderr := runIntegration(args)
		if code != 2 || stdout != "needle\n\nFound 1 match(es)\n" || !strings.Contains(stderr, "Error: search incomplete: 1 diagnostic(s)") {
			t.Fatalf("suppress=%v exit/stdout/stderr = %d/%q/%q", suppress, code, stdout, stderr)
		}
		if suppress == strings.Contains(stderr, "Warning:") {
			t.Fatalf("suppress=%v warning routing = %q", suppress, stderr)
		}
		if strings.Contains(stdout, "No matches found") {
			t.Fatalf("partial search printed clean no-match: %q", stdout)
		}
	}
}

func TestExecuteContextWriterAndContextErrorsExitTwo(t *testing.T) {
	fixture := newSearchFixture(t)
	var stderr bytes.Buffer
	code := ExecuteContext(context.Background(), []string{fixture.root, "needle"}, failingWriter{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "emit search result") {
		t.Fatalf("writer failure = %d/%q", code, stderr.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout bytes.Buffer
	stderr.Reset()
	code = ExecuteContext(ctx, []string{fixture.root, "needle"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "context canceled") {
		t.Fatalf("context failure = %d/%q/%q", code, stdout.String(), stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type searchFixture struct {
	root     string
	textFile string
}

func newSearchFixture(t *testing.T) searchFixture {
	t.Helper()
	root := t.TempDir()
	textFile := filepath.Join(root, "a.txt")
	writeTestFile(t, textFile, "Alpha needle\nsecond needle\nmulti start\nmulti end\nliteral --token\n")
	writeTestFile(t, filepath.Join(root, "b.go"), "beta needle\n")
	writeTestFile(t, filepath.Join(root, ".hidden.txt"), "hidden needle\n")
	writeTestFile(t, filepath.Join(root, ".hidden", "nested.txt"), "hidden needle\n")
	writeTestFile(t, filepath.Join(root, "sub", "nested.txt"), "nested needle\n")
	writeTestFile(t, filepath.Join(root, "node_modules", "dependency.txt"), "dependency needle\n")
	writeTestFile(t, filepath.Join(root, "binary.bin"), "binary needle\x00tail")
	return searchFixture{root: root, textFile: textFile}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runCommand(t *testing.T, args []string, run runner) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeContext(context.Background(), args, &stdout, &stderr, run)
	return code, stdout.String(), stderr.String()
}

func runIntegration(args []string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := ExecuteContext(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}
