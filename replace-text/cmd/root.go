package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"replace-text/internal/replacer"
)

type runner func(context.Context, replacer.Options, replacer.Reporter) (replacer.Summary, error)

type commandOptions struct {
	backup        bool
	dryRun        bool
	literal       bool
	maxInputSize  int64
	maxOutputSize int64
	workers       int
}

// ExecuteContext runs replace-text with the supplied process arguments and
// streams. It returns the process exit code without terminating the caller.
func ExecuteContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return executeContext(ctx, args, stdout, stderr, replacer.Run)
}

func executeContext(ctx context.Context, args []string, stdout, stderr io.Writer, run runner) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	root := newRootCommand(run, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCommand(run runner, stdout, stderr io.Writer) *cobra.Command {
	options := commandOptions{
		maxInputSize: replacer.DefaultMaxInputSize,
		workers:      defaultWorkers(),
	}

	root := &cobra.Command{
		Use:   "replace-text [old-text] [new-text] [file-or-directory-path]",
		Short: "Find and replace text in files or directories",
		Long: `Find and replace UTF-8 text in one file or recursively in a directory.

Escape sequences \n, \r, \t, and \\ are interpreted by default. Backups are
created only when --backup is set. Policy skips such as binary files are not
errors, while usage and operational failures return a nonzero exit status.`,
		Example: `  replace-text 'hello' 'goodbye' /path/to/file.txt
  replace-text '\n' '\r\n' /path/to/file.txt
  replace-text --literal '\n' 'line break' /path/to/file.txt
  replace-text --dry-run 'old' 'new' /path/to/directory
  replace-text -- '--old' 'new' /path/to/file.txt`,
		Args:          cobra.ExactArgs(3),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplacement(cmd.Context(), args, options, run, stdout, stderr)
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.Flags()
	flags.BoolVar(&options.backup, "backup", false, "Create a .bak copy for each modified file")
	flags.Int64Var(&options.maxInputSize, "max-size", replacer.DefaultMaxInputSize, "Maximum input file size in bytes")
	flags.BoolVar(&options.literal, "literal", false, "Treat backslashes literally instead of interpreting escape sequences")
	flags.BoolVar(&options.dryRun, "dry-run", false, "Report replacements without changing files")
	flags.IntVar(&options.workers, "max-workers", defaultWorkers(), "Maximum number of files processed concurrently")
	flags.Int64Var(&options.maxOutputSize, "max-output-size", 0, "Maximum output file size in bytes (0 means unlimited)")

	return root
}

func runReplacement(
	ctx context.Context,
	args []string,
	command commandOptions,
	run runner,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if run == nil {
		return errors.New("replacement runner is nil")
	}
	if command.maxInputSize < 0 {
		return errors.New("--max-size must not be negative")
	}
	if command.maxOutputSize < 0 {
		return errors.New("--max-output-size must not be negative")
	}
	if command.workers < 1 {
		return errors.New("--max-workers must be at least 1")
	}

	search, replacement := args[0], args[1]
	if !command.literal {
		search = unescapeString(search)
		replacement = unescapeString(replacement)
	}
	if search == "" {
		return errors.New("old-text must not be empty")
	}

	target := args[2]
	reporter := &outputReporter{
		target: target,
		stdout: stdout,
		stderr: stderr,
	}
	summary, err := run(ctx, replacer.Options{
		Target:        target,
		Search:        []byte(search),
		Replacement:   []byte(replacement),
		Backup:        command.backup,
		DryRun:        command.dryRun,
		MaxInputSize:  command.maxInputSize,
		MaxOutputSize: command.maxOutputSize,
		Workers:       command.workers,
	}, reporter)

	if summary.TargetIsDirectory {
		reporter.beginDirectory()
		writeDirectorySummary(stdout, target, summary)
	}
	return err
}

type outputReporter struct {
	target    string
	directory bool
	started   bool
	stdout    io.Writer
	stderr    io.Writer
}

func (r *outputReporter) Report(outcome replacer.Outcome) {
	if outcome.TargetIsDirectory {
		r.beginDirectory()
	}
	switch outcome.Kind {
	case replacer.OutcomeModified:
		fmt.Fprintf(r.stdout, "Successfully replaced text in '%s'.\n", outcome.Path)
		if outcome.BackupPath != "" {
			fmt.Fprintf(r.stdout, "Backup file created at '%s'.\n", outcome.BackupPath)
		}
	case replacer.OutcomeWouldModify:
		fmt.Fprintf(r.stdout, "Would replace text in '%s' (%d replacements).\n", outcome.Path, outcome.Replacements)
	case replacer.OutcomeNoMatch:
		if !r.directory {
			fmt.Fprintf(r.stdout, "No replacement made in '%s': %s.\n", outcome.Path, outcome.Detail)
		}
	case replacer.OutcomeSkipped:
		if !r.directory {
			fmt.Fprintf(r.stdout, "Skipped '%s' (%s): %s.\n", outcome.Path, outcome.Reason, outcome.Detail)
		}
	case replacer.OutcomeFailed:
		if r.directory {
			fmt.Fprintf(r.stderr, "Error processing '%s': %v\n", outcome.Path, outcome.Err)
		}
	}
}

func (r *outputReporter) beginDirectory() {
	if r.started {
		return
	}
	r.directory = true
	r.started = true
	fmt.Fprintf(r.stdout, "Processing directory: %s\n", r.target)
}

func writeDirectorySummary(w io.Writer, path string, summary replacer.Summary) {
	var skipped int64
	for _, reason := range replacer.SkipReasons() {
		skipped += summary.Skipped[reason]
	}

	fmt.Fprintf(w, "Finished processing directory '%s'.\n", path)
	fmt.Fprintf(
		w,
		"Summary: scanned=%d modified=%d would-modify=%d replacements=%d no-match=%d skipped=%d failed=%d\n",
		summary.Scanned,
		summary.Modified,
		summary.WouldModify,
		summary.Replacements,
		summary.NoMatch,
		skipped,
		summary.Failed,
	)

	parts := make([]string, 0, len(replacer.SkipReasons()))
	for _, reason := range replacer.SkipReasons() {
		parts = append(parts, fmt.Sprintf("%s=%d", reason, summary.Skipped[reason]))
	}
	fmt.Fprintf(w, "Skipped: %s\n", strings.Join(parts, " "))
}

func defaultWorkers() int {
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

// unescapeString converts the escape sequences supported by the historical
// CLI while preserving unknown escapes verbatim.
func unescapeString(value string) string {
	var result strings.Builder
	result.Grow(len(value))

	for index := 0; index < len(value); index++ {
		if value[index] != '\\' || index+1 >= len(value) {
			result.WriteByte(value[index])
			continue
		}

		switch value[index+1] {
		case 'n':
			result.WriteByte('\n')
			index++
		case 'r':
			result.WriteByte('\r')
			index++
		case 't':
			result.WriteByte('\t')
			index++
		case '\\':
			result.WriteByte('\\')
			index++
		default:
			result.WriteByte(value[index])
		}
	}

	return result.String()
}
