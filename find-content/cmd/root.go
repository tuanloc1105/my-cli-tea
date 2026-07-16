package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type runner func(context.Context, string, string, commandOptions, io.Writer, io.Writer) error

type commandOptions struct {
	useRegex         bool
	caseSensitive    bool
	multiline        bool
	extensions       string
	excludeDirs      string
	excludeFiles     string
	noLineNumbers    bool
	noFilePath       bool
	maxResults       int
	listMode         bool
	showHidden       bool
	suppressWarnings bool
	searchAll        bool
}

// ExecuteContext runs find-content with the supplied process arguments and
// streams. It returns the process exit code without terminating the caller.
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
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCommand(run runner, stdout, stderr io.Writer) *cobra.Command {
	options := commandOptions{}

	root := &cobra.Command{
		Use:   "find-content [directory] [keyword]",
		Short: "Improved file content search utility",
		Long: `A powerful file content search utility that supports recursive search with various options.

Examples:
  find-content /path/to/search "keyword"
  find-content /path/to/search "pattern" --regex
  find-content /path/to/search "text" --extensions py,js,txt
  find-content /path/to/search "version" --case-sensitive
  find-content /path/to/search "error" --exclude-dirs node_modules,.git
  find-content /path/to/search "line1\nline2\nline3" --multiline`,
		Args:          cobra.ExactArgs(2),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), args[0], args[1], options, stdout, stderr)
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.Flags()
	flags.BoolVarP(&options.useRegex, "regex", "r", false, "Treat keyword as regex pattern")
	flags.BoolVarP(&options.caseSensitive, "case-sensitive", "c", false, "Case sensitive search")
	flags.BoolVarP(&options.multiline, "multiline", "M", false, "Enable multiline search with \\n in keyword")
	flags.StringVarP(&options.extensions, "extensions", "e", "", "Comma-separated list of file extensions to search")
	flags.StringVar(&options.excludeDirs, "exclude-dirs", "", "Comma-separated list of directories to exclude")
	flags.StringVar(&options.excludeFiles, "exclude-files", "", "Comma-separated list of files to exclude")
	flags.BoolVar(&options.noLineNumbers, "no-line-numbers", false, "Hide line numbers in output")
	flags.BoolVar(&options.noFilePath, "no-file-path", false, "Hide file paths in output")
	flags.IntVarP(&options.maxResults, "max-results", "m", 0, "Maximum number of results to show")
	flags.BoolVarP(&options.listMode, "list", "l", false, "List directory contents instead of searching")
	flags.BoolVar(&options.showHidden, "show-hidden", false, "Show hidden files when listing")
	flags.BoolVar(&options.suppressWarnings, "suppress-warnings", false, "Suppress warning messages")
	flags.BoolVar(&options.searchAll, "all", false, "Search in all files (not limited by extension)")

	return root
}

func runSearch(
	_ context.Context,
	directory string,
	keyword string,
	options commandOptions,
	stdout io.Writer,
	stderr io.Writer,
) error {
	var fileExtensions, excludeDirs, excludeFiles []string
	if options.extensions != "" {
		fileExtensions = strings.Split(options.extensions, ",")
	}
	if options.excludeDirs != "" {
		excludeDirs = strings.Split(options.excludeDirs, ",")
	}
	if options.excludeFiles != "" {
		excludeFiles = strings.Split(options.excludeFiles, ",")
	}

	searcher := NewFileSearcher(
		options.caseSensitive,
		options.suppressWarnings,
		options.searchAll,
		fileExtensions,
		excludeDirs,
		excludeFiles,
		stdout,
		stderr,
	)

	if options.listMode {
		return searcher.listDirectoryContents(directory, options.showHidden)
	}

	var maxResults *int
	if options.maxResults > 0 {
		maxResults = &options.maxResults
	}

	matches := searcher.grepRecursive(
		directory,
		keyword,
		options.useRegex,
		options.multiline,
		!options.noLineNumbers,
		!options.noFilePath,
		maxResults,
	)
	if matches == 0 {
		fmt.Fprintln(stdout, "No matches found")
	} else {
		fmt.Fprintf(stdout, "\nFound %d match(es)\n", matches)
	}

	return nil
}
