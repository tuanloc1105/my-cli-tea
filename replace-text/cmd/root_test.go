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

	"replace-text/internal/replacer"
)

func TestCommandPassesFlagsAndLiteralArguments(t *testing.T) {
	target := writeFixture(t, "unchanged")
	var received replacer.Options
	run := func(_ context.Context, options replacer.Options, _ replacer.Reporter) (replacer.Summary, error) {
		received = options
		return replacer.Summary{}, nil
	}

	code, stdout, stderr := runCommand(t, []string{
		"--backup",
		"--dry-run",
		"--literal",
		"--max-size", "123",
		"--max-output-size", "456",
		"--max-workers", "2",
		`\n`,
		`\t`,
		target,
	}, run)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("unexpected output: stdout = %q, stderr = %q", stdout, stderr)
	}
	if !received.Backup || !received.DryRun {
		t.Fatalf("boolean flags were not forwarded: %+v", received)
	}
	if received.MaxInputSize != 123 || received.MaxOutputSize != 456 || received.Workers != 2 {
		t.Fatalf("numeric flags were not forwarded: %+v", received)
	}
	if string(received.Search) != `\n` || string(received.Replacement) != `\t` {
		t.Fatalf("literal arguments changed: search = %q, replacement = %q", received.Search, received.Replacement)
	}
}

func TestCommandUnescapesArgumentsByDefault(t *testing.T) {
	target := writeFixture(t, "unchanged")
	var received replacer.Options
	run := func(_ context.Context, options replacer.Options, _ replacer.Reporter) (replacer.Summary, error) {
		received = options
		return replacer.Summary{}, nil
	}

	code, _, stderr := runCommand(t, []string{`a\nb\tc\rd\\e\x`, `\\\n`, target}, run)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if got, want := string(received.Search), "a\nb\tc\rd\\e\\x"; got != want {
		t.Fatalf("search = %q, want %q", got, want)
	}
	if got, want := string(received.Replacement), "\\\n"; got != want {
		t.Fatalf("replacement = %q, want %q", got, want)
	}
}

func TestCommandAcceptsFlagLikeTextAfterSeparator(t *testing.T) {
	target := writeFixture(t, "unchanged")
	var received replacer.Options
	run := func(_ context.Context, options replacer.Options, _ replacer.Reporter) (replacer.Summary, error) {
		received = options
		return replacer.Summary{}, nil
	}

	code, _, stderr := runCommand(t, []string{"--literal", "--", "--old", "--new", target}, run)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if string(received.Search) != "--old" || string(received.Replacement) != "--new" {
		t.Fatalf("separator arguments not preserved: %+v", received)
	}
}

func TestCommandDefaults(t *testing.T) {
	target := writeFixture(t, "unchanged")
	var received replacer.Options
	run := func(_ context.Context, options replacer.Options, _ replacer.Reporter) (replacer.Summary, error) {
		received = options
		return replacer.Summary{}, nil
	}

	code, _, stderr := runCommand(t, []string{"old", "new", target}, run)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if received.MaxInputSize != replacer.DefaultMaxInputSize {
		t.Fatalf("MaxInputSize = %d, want %d", received.MaxInputSize, replacer.DefaultMaxInputSize)
	}
	if received.MaxOutputSize != 0 {
		t.Fatalf("MaxOutputSize = %d, want 0", received.MaxOutputSize)
	}
	wantWorkers := min(runtime.NumCPU(), 8)
	if wantWorkers < 1 {
		wantWorkers = 1
	}
	if received.Workers != wantWorkers {
		t.Fatalf("Workers = %d, want %d", received.Workers, wantWorkers)
	}
}

func TestCommandRejectsInvalidUsageWithoutCallingRunner(t *testing.T) {
	target := writeFixture(t, "unchanged")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "too few arguments", args: []string{"old", "new"}, want: "accepts 3 arg(s)"},
		{name: "empty old text", args: []string{"", "new", target}, want: "old-text must not be empty"},
		{name: "negative max input", args: []string{"--max-size", "-1", "old", "new", target}, want: "--max-size must not be negative"},
		{name: "negative max output", args: []string{"--max-output-size", "-1", "old", "new", target}, want: "--max-output-size must not be negative"},
		{name: "zero workers", args: []string{"--max-workers", "0", "old", "new", target}, want: "--max-workers must be at least 1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			run := func(context.Context, replacer.Options, replacer.Reporter) (replacer.Summary, error) {
				calls++
				return replacer.Summary{}, nil
			}

			code, _, stderr := runCommand(t, test.args, run)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr = %q, want substring %q", stderr, test.want)
			}
			if calls != 0 {
				t.Fatalf("runner called %d times", calls)
			}
		})
	}
}

func TestCommandHelpHasCleanNewlineAndRequiredExamples(t *testing.T) {
	code, stdout, stderr := runCommand(t, []string{"--help"}, func(
		context.Context,
		replacer.Options,
		replacer.Reporter,
	) (replacer.Summary, error) {
		t.Fatal("runner should not be called for help")
		return replacer.Summary{}, nil
	})

	if code != 0 || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	for _, required := range []string{
		"--literal '\\n'",
		"--dry-run 'old' 'new'",
		"replace-text -- '--old'",
	} {
		if !strings.Contains(stdout, required) {
			t.Errorf("help is missing %q:\n%s", required, stdout)
		}
	}
	if !strings.HasSuffix(stdout, "\n") || strings.HasSuffix(stdout, "\n\n") {
		t.Fatalf("help must end in exactly one newline, got suffix %q", stdout[max(0, len(stdout)-4):])
	}
}

func TestSingleTargetOutput(t *testing.T) {
	target := writeFixture(t, "unchanged")
	tests := []struct {
		name        string
		outcome     replacer.Outcome
		summary     replacer.Summary
		wantStdout  string
		wantStderr  string
		returnedErr error
		wantCode    int
	}{
		{
			name: "modified with backup",
			outcome: replacer.Outcome{
				Path: target, Kind: replacer.OutcomeModified, Replacements: 2, BackupPath: target + ".bak",
			},
			wantStdout: fmt.Sprintf(
				"Successfully replaced text in '%s'.\nBackup file created at '%s.bak'.\n",
				target,
				target,
			),
		},
		{
			name:       "dry run",
			outcome:    replacer.Outcome{Path: target, Kind: replacer.OutcomeWouldModify, Replacements: 3},
			wantStdout: fmt.Sprintf("Would replace text in '%s' (3 replacements).\n", target),
		},
		{
			name:       "no match",
			outcome:    replacer.Outcome{Path: target, Kind: replacer.OutcomeNoMatch, Detail: "search text was not found"},
			wantStdout: fmt.Sprintf("No replacement made in '%s': search text was not found.\n", target),
		},
		{
			name: "policy skip",
			outcome: replacer.Outcome{
				Path: target, Kind: replacer.OutcomeSkipped, Reason: replacer.SkipBinaryNUL, Detail: "input contains a NUL byte",
			},
			wantStdout: fmt.Sprintf("Skipped '%s' (binary-nul): input contains a NUL byte.\n", target),
		},
		{
			name:        "operational failure",
			outcome:     replacer.Outcome{Path: target, Kind: replacer.OutcomeFailed, Err: errors.New("boom")},
			returnedErr: &replacer.PartialError{Total: 1, Failures: []replacer.PathError{{Path: target, Err: errors.New("boom")}}},
			wantCode:    1,
			wantStderr:  fmt.Sprintf("Error: 1 path failed: %s: boom\n", target),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := func(_ context.Context, _ replacer.Options, reporter replacer.Reporter) (replacer.Summary, error) {
				reporter.Report(test.outcome)
				return test.summary, test.returnedErr
			}
			code, stdout, stderr := runCommand(t, []string{"old", "new", target}, run)
			if code != test.wantCode || stdout != test.wantStdout || stderr != test.wantStderr {
				t.Fatalf(
					"exit/stdout/stderr = %d/%q/%q, want %d/%q/%q",
					code,
					stdout,
					stderr,
					test.wantCode,
					test.wantStdout,
					test.wantStderr,
				)
			}
		})
	}
}

func TestDirectoryOutputAndPartialFailure(t *testing.T) {
	directory := t.TempDir()
	modified := filepath.Join(directory, "modified.txt")
	wouldModify := filepath.Join(directory, "dry-run.txt")
	noMatch := filepath.Join(directory, "no-match.txt")
	skipped := filepath.Join(directory, "binary.dat")
	failed := filepath.Join(directory, "failed.txt")
	outcomes := []replacer.Outcome{
		{Path: modified, Kind: replacer.OutcomeModified, Replacements: 1, BackupPath: modified + ".bak"},
		{Path: wouldModify, Kind: replacer.OutcomeWouldModify, Replacements: 2},
		{Path: noMatch, Kind: replacer.OutcomeNoMatch, Detail: "search text was not found"},
		{Path: skipped, Kind: replacer.OutcomeSkipped, Reason: replacer.SkipBinaryNUL, Detail: "input contains a NUL byte"},
		{Path: failed, Kind: replacer.OutcomeFailed, Err: errors.New("permission denied")},
	}
	summary := replacer.Summary{
		Scanned:           5,
		Modified:          1,
		WouldModify:       1,
		Replacements:      3,
		NoMatch:           1,
		Skipped:           map[replacer.SkipReason]int64{replacer.SkipBinaryNUL: 1},
		Failed:            1,
		TargetIsDirectory: true,
	}
	partial := &replacer.PartialError{
		Total:    1,
		Failures: []replacer.PathError{{Path: failed, Err: errors.New("permission denied")}},
	}
	run := func(_ context.Context, _ replacer.Options, reporter replacer.Reporter) (replacer.Summary, error) {
		for _, outcome := range outcomes {
			outcome.TargetIsDirectory = true
			reporter.Report(outcome)
		}
		return summary, partial
	}

	code, stdout, stderr := runCommand(t, []string{"old", "new", directory}, run)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	wantStdout := fmt.Sprintf(
		"Processing directory: %s\n"+
			"Successfully replaced text in '%s'.\n"+
			"Backup file created at '%s.bak'.\n"+
			"Would replace text in '%s' (2 replacements).\n"+
			"Finished processing directory '%s'.\n"+
			"Summary: scanned=5 modified=1 would-modify=1 replacements=3 no-match=1 skipped=1 failed=1\n"+
			"Skipped: backup-file=0 binary-nul=1 hardlink=0 input-too-large=0 invalid-utf8=0 non-regular=0 output-too-large=0 symlink=0\n",
		directory,
		modified,
		modified,
		wouldModify,
		directory,
	)
	if stdout != wantStdout {
		t.Fatalf("stdout mismatch:\n--- got ---\n%s--- want ---\n%s", stdout, wantStdout)
	}
	wantStderr := fmt.Sprintf(
		"Error processing '%s': permission denied\nError: 1 path failed: %s: permission denied\n",
		failed,
		failed,
	)
	if stderr != wantStderr {
		t.Fatalf("stderr = %q, want %q", stderr, wantStderr)
	}
	if strings.Contains(stdout, noMatch) || strings.Contains(stdout, skipped) {
		t.Fatalf("directory output should summarize no-match and skips instead of logging each path: %s", stdout)
	}
}

func TestCommandUsesRunnerTargetKindForOutput(t *testing.T) {
	target := writeFixture(t, "unchanged")
	run := func(_ context.Context, _ replacer.Options, reporter replacer.Reporter) (replacer.Summary, error) {
		reporter.Report(replacer.Outcome{
			Path:              target,
			Kind:              replacer.OutcomeNoMatch,
			Detail:            "search text was not found",
			TargetIsDirectory: true,
		})
		return replacer.Summary{Scanned: 1, NoMatch: 1, TargetIsDirectory: true}, nil
	}

	code, stdout, stderr := runCommand(t, []string{"old", "new", target}, run)
	if code != 0 || stderr != "" {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Processing directory:") ||
		!strings.Contains(stdout, "Summary: scanned=1") ||
		strings.Contains(stdout, "No replacement made") {
		t.Fatalf("output did not follow authoritative runner target kind: %q", stdout)
	}
}

func TestExecuteContextIntegrationSmoke(t *testing.T) {
	target := writeFixture(t, "hello\nhello")
	var stdout, stderr bytes.Buffer

	code := ExecuteContext(context.Background(), []string{"--", `\n`, `--`, target}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(content), "hello--hello"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	wantStdout := fmt.Sprintf("Successfully replaced text in '%s'.\n", target)
	if stdout.String() != wantStdout || stderr.String() != "" {
		t.Fatalf("stdout/stderr = %q/%q, want %q/empty", stdout.String(), stderr.String(), wantStdout)
	}
}

func TestExecuteContextNoMatchAndDryRunExitZero(t *testing.T) {
	t.Run("no match", func(t *testing.T) {
		target := writeFixture(t, "original")
		var stdout, stderr bytes.Buffer
		code := ExecuteContext(context.Background(), []string{"missing", "new", target}, &stdout, &stderr)
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
		}
		want := fmt.Sprintf("No replacement made in '%s': search text was not found.\n", target)
		if stdout.String() != want {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	})

	t.Run("dry run", func(t *testing.T) {
		target := writeFixture(t, "old old")
		var stdout, stderr bytes.Buffer
		code := ExecuteContext(context.Background(), []string{"--dry-run", "old", "new", target}, &stdout, &stderr)
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
		}
		content, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "old old" {
			t.Fatalf("dry run changed content to %q", content)
		}
		want := fmt.Sprintf("Would replace text in '%s' (2 replacements).\n", target)
		if stdout.String() != want {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	})
}

func TestExecuteContextOperationalErrorExitOne(t *testing.T) {
	target := filepath.Join(t.TempDir(), "missing.txt")
	var stdout, stderr bytes.Buffer

	code := ExecuteContext(context.Background(), []string{"same", "same", target}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "inspect target") {
		t.Fatalf("stderr = %q, want target inspection error", stderr.String())
	}
}

func runCommand(t *testing.T, args []string, run runner) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeContext(context.Background(), args, &stdout, &stderr, run)
	return code, stdout.String(), stderr.String()
}

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
