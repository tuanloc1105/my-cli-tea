package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"find-content/internal/searcher"
)

type runner func(context.Context, []string, commandOptions, io.Writer, io.Writer) error

type exitError struct {
	code    int
	message string
}

func (e *exitError) Error() string { return e.message }

func ExecuteContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return executeContext(ctx, args, stdout, stderr, runSearch)
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
	if args == nil {
		args = []string{}
	}

	root := newRootCommand(run, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		code := 2
		var status *exitError
		if errors.As(err, &status) {
			code = status.code
		}
		if err.Error() != "" {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		}
		return code
	}
	return 0
}

func newRootCommand(run runner, stdout, stderr io.Writer) *cobra.Command {
	options := defaultCommandOptions()
	root := &cobra.Command{
		Use:   "find-content [directory] [keyword]",
		Short: "Search text files recursively with deterministic output",
		Long: `Search regular text files recursively without following symlinks.

Search includes hidden entries. List mode hides hidden entries unless
--show-hidden is set. Clean searches with no matches return exit code 1;
usage, validation, root, writer, and partial-search errors return exit code 2.`,
		Example: `  find-content /path/to/search "keyword"
  find-content /path/to/search "pattern" --regex
  find-content /path/to/search "text" --extensions py,js,txt
  find-content --list /path/to/search
  find-content /path/to/search -- "--keyword"`,
		Args: func(command *cobra.Command, args []string) error {
			if options.listMode {
				if len(args) == 1 || len(args) == 2 {
					return nil
				}
				return fmt.Errorf("--list accepts 1 arg(s), or 2 in the deprecated legacy form")
			}
			return cobra.ExactArgs(2)(command, args)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, args []string) error {
			if err := validateModeFlags(command, options); err != nil {
				return err
			}
			if err := validateOptions(options); err != nil {
				return err
			}
			if !options.listMode && args[1] == "" {
				return errors.New("keyword must not be empty")
			}
			return run(command.Context(), args, options, stdout, stderr)
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.Flags()
	flags.BoolVarP(&options.useRegex, "regex", "r", false, "Treat keyword as a regular expression")
	flags.BoolVarP(&options.caseSensitive, "case-sensitive", "c", false, "Use case-sensitive matching")
	flags.BoolVarP(&options.multiline, "multiline", "M", false, "Allow matches across lines; \\n in the keyword means newline")
	flags.StringVarP(&options.extensions, "extensions", "e", "", "Comma-separated authoritative file extensions")
	flags.StringVar(&options.excludeDirs, "exclude-dirs", "", "Comma-separated directory basenames to exclude")
	flags.StringVar(&options.excludeFiles, "exclude-files", "", "Comma-separated file basenames to exclude")
	flags.BoolVar(&options.noDefaultExclude, "no-default-excludes", false, "Disable built-in directory excludes")
	flags.BoolVar(&options.noLineNumbers, "no-line-numbers", false, "Hide line numbers in output")
	flags.BoolVar(&options.noFilePath, "no-file-path", false, "Hide file paths in output")
	flags.IntVarP(&options.maxResults, "max-results", "m", 0, "Maximum results to emit (0 means unlimited)")
	flags.IntVar(&options.maxWorkers, "max-workers", options.maxWorkers, "Maximum files searched concurrently")
	flags.Int64Var(&options.maxLineSize, "max-line-size", options.maxLineSize, "Maximum bytes in one normal-mode line")
	flags.Int64Var(&options.maxMultilineSize, "max-multiline-size", options.maxMultilineSize, "Maximum bytes in one multiline-mode file")
	flags.BoolVarP(&options.listMode, "list", "l", false, "List one directory instead of searching")
	flags.BoolVar(&options.showHidden, "show-hidden", false, "Show hidden entries in list mode")
	flags.BoolVar(&options.suppressWarnings, "suppress-warnings", false, "Hide per-path warnings without changing exit status")
	flags.BoolVar(&options.searchAll, "all", false, "Search every filename while still skipping NUL-binary files")
	return root
}

func runSearch(
	ctx context.Context,
	args []string,
	options commandOptions,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if options.listMode {
		if len(args) == 2 {
			if _, err := fmt.Fprintln(stderr, "Warning: the two-argument --list form is deprecated; use find-content --list <directory>"); err != nil {
				return fmt.Errorf("write deprecation warning: %w", err)
			}
		}
		return searcher.List(ctx, args[0], options.showHidden, func(entry searcher.ListEntry) error {
			return renderListEntry(stdout, entry)
		})
	}
	if args[1] == "" {
		return errors.New("keyword must not be empty")
	}

	render := renderer{
		stdout:           stdout,
		stderr:           stderr,
		showLineNumbers:  !options.noLineNumbers,
		showFilePath:     !options.noFilePath,
		multiline:        options.multiline,
		suppressWarnings: options.suppressWarnings,
	}
	summary, err := searcher.Search(ctx, searcher.Options{
		Root:             args[0],
		Keyword:          args[1],
		UseRegex:         options.useRegex,
		CaseSensitive:    options.caseSensitive,
		Multiline:        options.multiline,
		Extensions:       normalizeCSV(options.extensions),
		ExcludeDirs:      normalizeCSV(options.excludeDirs),
		ExcludeFiles:     normalizeCSV(options.excludeFiles),
		NoDefaultExclude: options.noDefaultExclude,
		SearchAll:        options.searchAll,
		MaxWorkers:       options.maxWorkers,
		MaxLineSize:      options.maxLineSize,
		MaxMultilineSize: options.maxMultilineSize,
		MaxResults:       options.maxResults,
	}, render.handle)
	if err != nil {
		return err
	}
	if summary.Matches > 0 {
		if _, err := fmt.Fprintf(stdout, "\nFound %d match(es)\n", summary.Matches); err != nil {
			return fmt.Errorf("write match summary: %w", err)
		}
	}
	if summary.PartialErrors > 0 {
		return &exitError{code: 2, message: fmt.Sprintf("search incomplete: %d diagnostic(s)", summary.PartialErrors)}
	}
	if summary.Matches == 0 {
		if _, err := fmt.Fprintln(stdout, "No matches found"); err != nil {
			return fmt.Errorf("write no-match summary: %w", err)
		}
		return &exitError{code: 1}
	}
	return nil
}
