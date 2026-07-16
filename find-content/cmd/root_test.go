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

func TestCommandHelpAndFlags(t *testing.T) {
	code, stdout, stderr := runCommand(t, []string{"--help"}, func(
		context.Context,
		string,
		string,
		commandOptions,
		io.Writer,
		io.Writer,
	) error {
		t.Fatal("runner should not be called for help")
		return nil
	})

	if code != 0 || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	for _, required := range []string{
		"Usage:",
		"find-content [directory] [keyword]",
		"--case-sensitive",
		"--exclude-dirs",
		"--exclude-files",
		"--max-results",
		"--show-hidden",
		"--suppress-warnings",
	} {
		if !strings.Contains(stdout, required) {
			t.Errorf("help is missing %q:\n%s", required, stdout)
		}
	}

	root := newRootCommand(nil, io.Discard, io.Discard)
	flagNames := []string{
		"all",
		"case-sensitive",
		"exclude-dirs",
		"exclude-files",
		"extensions",
		"list",
		"max-results",
		"multiline",
		"no-file-path",
		"no-line-numbers",
		"regex",
		"show-hidden",
		"suppress-warnings",
	}
	if len(flagNames) != 13 {
		t.Fatalf("test contract has %d flags, want 13", len(flagNames))
	}
	for _, name := range flagNames {
		if root.Flags().Lookup(name) == nil {
			t.Errorf("root command is missing --%s", name)
		}
	}
}

func TestCommandRequiresExactlyTwoArguments(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"host-process", "--host-only"}
	t.Cleanup(func() { os.Args = originalArgs })

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no arguments", want: "accepts 2 arg(s)"},
		{name: "list still needs keyword", args: []string{"--list", "directory"}, want: "accepts 2 arg(s)"},
		{name: "too many arguments", args: []string{"directory", "keyword", "extra"}, want: "accepts 2 arg(s)"},
		{name: "unknown flag", args: []string{"--unknown", "directory", "keyword"}, want: "unknown flag"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			code, stdout, stderr := runCommand(t, test.args, func(
				context.Context,
				string,
				string,
				commandOptions,
				io.Writer,
				io.Writer,
			) error {
				calls++
				return nil
			})

			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("stdout/stderr = %q/%q, want empty/substring %q", stdout, stderr, test.want)
			}
			if strings.Count(stderr, "Error:") != 1 || strings.Contains(stderr, "Usage:") {
				t.Fatalf("error should be rendered once without usage: %q", stderr)
			}
			if calls != 0 {
				t.Fatalf("runner called %d times", calls)
			}
		})
	}
}

func TestCommandForwardsAllFlags(t *testing.T) {
	var received commandOptions
	var directory, keyword string
	code, stdout, stderr := runCommand(t, []string{
		"--regex",
		"--case-sensitive",
		"--multiline",
		"--extensions", "go, txt",
		"--exclude-dirs", "vendor, node_modules",
		"--exclude-files", "skip.go, other.go",
		"--no-line-numbers",
		"--no-file-path",
		"--max-results", "7",
		"--list",
		"--show-hidden",
		"--suppress-warnings",
		"--all",
		"fixture-dir",
		"needle",
	}, func(
		_ context.Context,
		gotDirectory string,
		gotKeyword string,
		options commandOptions,
		stdout io.Writer,
		stderr io.Writer,
	) error {
		directory = gotDirectory
		keyword = gotKeyword
		received = options
		fmt.Fprint(stdout, "stdout")
		fmt.Fprint(stderr, "stderr")
		return nil
	})

	if code != 0 || stdout != "stdout" || stderr != "stderr" {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
	if directory != "fixture-dir" || keyword != "needle" {
		t.Fatalf("directory/keyword = %q/%q", directory, keyword)
	}
	want := commandOptions{
		useRegex:         true,
		caseSensitive:    true,
		multiline:        true,
		extensions:       "go, txt",
		excludeDirs:      "vendor, node_modules",
		excludeFiles:     "skip.go, other.go",
		noLineNumbers:    true,
		noFilePath:       true,
		maxResults:       7,
		listMode:         true,
		showHidden:       true,
		suppressWarnings: true,
		searchAll:        true,
	}
	if received != want {
		t.Fatalf("options = %+v, want %+v", received, want)
	}
}

func TestCommandStateDoesNotLeakBetweenInvocations(t *testing.T) {
	var received []commandOptions
	run := func(
		_ context.Context,
		_ string,
		_ string,
		options commandOptions,
		_ io.Writer,
		_ io.Writer,
	) error {
		received = append(received, options)
		return nil
	}

	firstCode, _, firstStderr := runCommand(t, []string{
		"--regex", "--extensions", "go", "--max-results", "3", "first", "needle",
	}, run)
	secondCode, _, secondStderr := runCommand(t, []string{"second", "needle"}, run)
	if firstCode != 0 || secondCode != 0 || firstStderr != "" || secondStderr != "" {
		t.Fatalf("codes/stderr = %d/%d/%q/%q", firstCode, secondCode, firstStderr, secondStderr)
	}
	if len(received) != 2 {
		t.Fatalf("runner calls = %d, want 2", len(received))
	}
	if !received[0].useRegex || received[0].extensions != "go" || received[0].maxResults != 3 {
		t.Fatalf("first options = %+v", received[0])
	}
	if received[1] != (commandOptions{}) {
		t.Fatalf("second invocation inherited state: %+v", received[1])
	}
}

func TestCommandRoutesStreamsAndRendersRunnerErrorOnce(t *testing.T) {
	code, stdout, stderr := runCommand(t, []string{"directory", "keyword"}, func(
		context.Context,
		string,
		string,
		commandOptions,
		io.Writer,
		io.Writer,
	) error {
		return errors.New("boom")
	})

	if code != 1 || stdout != "" || stderr != "Error: boom\n" {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
	if strings.Count(stderr, "Error:") != 1 {
		t.Fatalf("error rendered more than once: %q", stderr)
	}
}

func TestCommandContextAndNilStreams(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout, stderr bytes.Buffer
	code := executeContext(ctx, []string{"directory", "keyword"}, &stdout, &stderr, func(
		ctx context.Context,
		_ string,
		_ string,
		_ commandOptions,
		_ io.Writer,
		_ io.Writer,
	) error {
		return ctx.Err()
	})
	if code != 1 || stdout.Len() != 0 || stderr.String() != "Error: context canceled\n" {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout.String(), stderr.String())
	}

	called := false
	code = executeContext(nil, []string{"directory", "keyword"}, nil, nil, func(
		ctx context.Context,
		_ string,
		_ string,
		_ commandOptions,
		stdout io.Writer,
		stderr io.Writer,
	) error {
		called = true
		if ctx == nil {
			t.Fatal("nil context reached runner")
		}
		fmt.Fprint(stdout, "discarded stdout")
		fmt.Fprint(stderr, "discarded stderr")
		return nil
	})
	if code != 0 || !called {
		t.Fatalf("exit/called = %d/%v", code, called)
	}
}

func TestExecuteContextSearchFormattingAndModes(t *testing.T) {
	fixture := newSearchFixture(t)

	t.Run("plain", func(t *testing.T) {
		code, stdout, stderr := runIntegration(t, []string{
			"--extensions", "go",
			"--no-file-path",
			"--no-line-numbers",
			fixture.root,
			"needle",
		})
		if code != 0 || stderr != "" || stdout != "beta needle\n\nFound 1 match(es)\n" {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
	})

	t.Run("regex", func(t *testing.T) {
		code, stdout, stderr := runIntegration(t, []string{
			"--regex",
			"--case-sensitive",
			"--extensions", "txt",
			"--exclude-dirs", "sub",
			fixture.root,
			"^Alpha needle$",
		})
		want := fmt.Sprintf("%s:1:Alpha needle\n\nFound 1 match(es)\n", fixture.textFile)
		if code != 0 || stderr != "" || stdout != want {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q, want 0/%q/empty", code, stdout, stderr, want)
		}
	})

	t.Run("multiline", func(t *testing.T) {
		code, stdout, stderr := runIntegration(t, []string{
			"--multiline",
			"--extensions", "txt",
			"--exclude-dirs", "sub",
			"--no-file-path",
			fixture.root,
			`multi start\nmulti end`,
		})
		want := "3..4:multi start\\nmulti end\n\nFound 1 match(es)\n"
		if code != 0 || stderr != "" || stdout != want {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q, want 0/%q/empty", code, stdout, stderr, want)
		}
	})
}

func TestExecuteContextFiltersAndUnlimitedMaxResults(t *testing.T) {
	fixture := newSearchFixture(t)

	for _, maxResults := range []string{"0", "-1"} {
		t.Run("max-results "+maxResults, func(t *testing.T) {
			code, stdout, stderr := runIntegration(t, []string{
				"--extensions", "txt",
				"--exclude-dirs", "sub",
				"--no-file-path",
				"--no-line-numbers",
				"--max-results=" + maxResults,
				fixture.root,
				"needle",
			})
			if code != 0 || stderr != "" || !strings.Contains(stdout, "Found 2 match(es)") {
				t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
			}
			for _, line := range []string{"Alpha needle\n", "second needle\n"} {
				if !strings.Contains(stdout, line) {
					t.Fatalf("stdout is missing %q: %q", line, stdout)
				}
			}
		})
	}

	t.Run("comma values are not trimmed", func(t *testing.T) {
		code, stdout, stderr := runIntegration(t, []string{
			"--extensions", "txt, go",
			"--exclude-dirs", "sub",
			"--no-file-path",
			"--no-line-numbers",
			fixture.root,
			"needle",
		})
		if code != 0 || stderr != "" || !strings.Contains(stdout, "Found 2 match(es)") {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
		if strings.Contains(stdout, "beta needle") {
			t.Fatalf("raw split unexpectedly trimmed the second extension: %q", stdout)
		}
	})

	t.Run("excluded file", func(t *testing.T) {
		code, stdout, stderr := runIntegration(t, []string{
			"--extensions", "txt",
			"--exclude-dirs", "sub",
			"--exclude-files", filepath.Base(fixture.textFile),
			fixture.root,
			"needle",
		})
		if code != 0 || stderr != "" || stdout != "No matches found\n" {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
	})
}

func TestExecuteContextSearchErrorPolicies(t *testing.T) {
	fixture := newSearchFixture(t)

	t.Run("no match", func(t *testing.T) {
		code, stdout, stderr := runIntegration(t, []string{fixture.root, "absent"})
		if code != 0 || stdout != "No matches found\n" || stderr != "" {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
	})

	t.Run("invalid regex remains successful", func(t *testing.T) {
		code, stdout, stderr := runIntegration(t, []string{"--regex", fixture.root, "["})
		if code != 0 || stdout != "No matches found\n" {
			t.Fatalf("exit/stdout = %d/%q", code, stdout)
		}
		if !strings.Contains(stderr, "Error: Invalid regex pattern:") || strings.Count(stderr, "Error:") != 1 {
			t.Fatalf("stderr = %q", stderr)
		}
	})

	t.Run("missing search directory remains successful", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		code, stdout, stderr := runIntegration(t, []string{missing, "needle"})
		if code != 0 || stdout != "No matches found\n" {
			t.Fatalf("exit/stdout = %d/%q", code, stdout)
		}
		want := fmt.Sprintf("Error: Directory does not exist: %s\n", missing)
		if stderr != want {
			t.Fatalf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("missing search directory warning can be suppressed", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		code, stdout, stderr := runIntegration(t, []string{"--suppress-warnings", missing, "needle"})
		if code != 0 || stdout != "No matches found\n" || stderr != "" {
			t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
		}
	})
}

func TestExecuteContextListModeAndFailure(t *testing.T) {
	fixture := newSearchFixture(t)

	code, stdout, stderr := runIntegration(t, []string{"--list", fixture.root, "unused"})
	if code != 0 || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	for _, visible := range []string{filepath.Base(fixture.textFile), filepath.Base(fixture.goFile), "sub"} {
		if !strings.Contains(stdout, visible) {
			t.Errorf("listing is missing %q: %q", visible, stdout)
		}
	}
	if strings.Contains(stdout, ".hidden.txt") {
		t.Fatalf("hidden file was listed without --show-hidden: %q", stdout)
	}

	code, stdout, stderr = runIntegration(t, []string{"--list", "--show-hidden", fixture.root, "unused"})
	if code != 0 || stderr != "" || !strings.Contains(stdout, ".hidden.txt") {
		t.Fatalf("show-hidden exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	code, stdout, stderr = runIntegration(t, []string{"--list", missing, "unused"})
	if code != 1 || stdout != "" {
		t.Fatalf("list failure exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
	if !strings.Contains(stderr, missing) || strings.Count(stderr, "Error:") != 1 || strings.Contains(stderr, "Usage:") {
		t.Fatalf("list error should be rendered once without usage: %q", stderr)
	}
}

type searchFixture struct {
	root     string
	textFile string
	goFile   string
}

func newSearchFixture(t *testing.T) searchFixture {
	t.Helper()
	root := t.TempDir()
	textFile := filepath.Join(root, "a.txt")
	goFile := filepath.Join(root, "b.go")
	writeTestFile(t, textFile, "Alpha needle\nsecond needle\nmulti start\nmulti end\n")
	writeTestFile(t, goFile, "beta needle\n")
	writeTestFile(t, filepath.Join(root, ".hidden.txt"), "hidden without target\n")
	writeTestFile(t, filepath.Join(root, "sub", "nested.txt"), "nested needle\n")
	writeTestFile(t, filepath.Join(root, "node_modules", "dependency.txt"), "dependency needle\n")
	return searchFixture{root: root, textFile: textFile, goFile: goFile}
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

func runIntegration(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := ExecuteContext(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}
