package cmd

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"

	"common-module/utils"
	"find-everything/internal/finder"
	"find-everything/internal/types"
	"find-everything/internal/ui"

	"github.com/spf13/cobra"
)

type searchRunner func(context.Context, string, string, finder.FinderOptions) ([]types.FileResult, []string, error)

type resultsPrinter func([]types.FileResult, []string, ui.ResultsOutputOptions) error

type commandOptions struct {
	caseSensitive      bool
	maxWorkers         int
	excludeDirs        []string
	excludePatterns    []string
	fileTypes          []string
	minSize            string
	maxSize            string
	maxResults         int
	noProgress         bool
	showDetails        bool
	noSort             bool
	displayAll         bool
	outputPath         string
	largeResultsAction string
}

// ExecuteContext runs find-everything with the supplied process arguments and
// streams. It returns the process exit code without terminating the caller.
func ExecuteContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return executeContext(ctx, args, stdout, stderr, runFinder, ui.PrintResults, utils.CLS)
}

func executeContext(ctx context.Context, args []string, stdout, stderr io.Writer, run searchRunner, printResults resultsPrinter, clearScreen func()) int {
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

	root := newRootCommand(run, printResults, clearScreen, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCommand(run searchRunner, printResults resultsPrinter, clearScreen func(), stdout, stderr io.Writer) *cobra.Command {
	options := commandOptions{
		maxWorkers:         runtime.NumCPU(),
		minSize:            "0",
		maxSize:            "inf",
		maxResults:         10000,
		largeResultsAction: ui.LargeResultsActionAsk,
	}

	root := &cobra.Command{
		Use:   "find-everything [base-path] [pattern]",
		Short: "Enhanced file and directory finder with advanced filtering options",
		Long: `Enhanced file and directory finder with advanced filtering options.

This tool provides comprehensive file and directory searching capabilities with
support for glob patterns, size filtering, file type filtering, and exclusion rules.`,
		Example: `  find-everything "C:\" "*.txt" --file-types .txt .log
  find-everything "/home/user" "*.py" --exclude-dirs node_modules .git
  find-everything "D:\" "zalo*" --min-size 1MB --max-size 100MB
  find-everything "." "*.jpg" --case-sensitive --show-details`,
		Args:          cobra.ExactArgs(2),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			basePath := args[0]
			pattern := args[1]

			resolvedLargeResultsAction, err := resolveLargeResultsAction(cmd, options.largeResultsAction, options.displayAll, options.outputPath)
			if err != nil {
				return err
			}

			minSizeBytes, err := parseSize(options.minSize)
			if err != nil {
				return fmt.Errorf("error parsing min-size: %w", err)
			}

			maxSizeBytes, err := parseSize(options.maxSize)
			if err != nil {
				return fmt.Errorf("error parsing max-size: %w", err)
			}

			processedExcludeDirs := []string{}
			for _, item := range options.excludeDirs {
				for _, dir := range strings.Split(item, ",") {
					dir = strings.TrimSpace(dir)
					if dir != "" {
						processedExcludeDirs = append(processedExcludeDirs, dir)
					}
				}
			}

			clearScreen()

			fmt.Fprintf(stdout, "%s%sEnhanced File and Directory Finder%s\n", ui.ColorBold, ui.ColorHeader, ui.ColorEndC)
			fmt.Fprintf(stdout, "%sSearching in: %s%s\n", ui.ColorOKBlue, basePath, ui.ColorEndC)
			fmt.Fprintf(stdout, "%sPattern: %s%s\n", ui.ColorOKBlue, pattern, ui.ColorEndC)

			finderOptions := finder.FinderOptions{
				CaseSensitive:   options.caseSensitive,
				MaxWorkers:      options.maxWorkers,
				ExcludeDirs:     processedExcludeDirs,
				ExcludePatterns: options.excludePatterns,
				FileTypes:       options.fileTypes,
				MinSize:         minSizeBytes,
				MaxSize:         maxSizeBytes,
				MaxResults:      options.maxResults,
				ShowProgress:    !options.noProgress,
				NoSort:          options.noSort,
				Writer:          stdout,
			}

			files, dirs, err := run(cmd.Context(), basePath, pattern, finderOptions)
			if err != nil {
				return err
			}

			return printResults(files, dirs, ui.ResultsOutputOptions{
				ShowDetails:        options.showDetails,
				Pattern:            pattern,
				BasePath:           basePath,
				NoSort:             options.noSort,
				LargeResultsAction: resolvedLargeResultsAction,
				OutputPath:         options.outputPath,
				Writer:             stdout,
			})
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.Flags()
	flags.BoolVarP(&options.caseSensitive, "case-sensitive", "c", false, "Case sensitive search")
	flags.IntVarP(&options.maxWorkers, "max-workers", "w", runtime.NumCPU(), "Maximum number of worker goroutines")
	flags.StringSliceVarP(&options.excludeDirs, "exclude-dirs", "e", []string{}, "Directories to exclude from search")
	flags.StringSliceVarP(&options.excludePatterns, "exclude-patterns", "p", []string{}, "Patterns to exclude (regex)")
	flags.StringSliceVarP(&options.fileTypes, "file-types", "t", []string{}, "File extensions to include")
	flags.StringVar(&options.minSize, "min-size", "0", "Minimum file size (e.g., 1KB, 1MB, 1GB)")
	flags.StringVar(&options.maxSize, "max-size", "inf", "Maximum file size (e.g., 1KB, 1MB, 1GB)")
	flags.IntVar(&options.maxResults, "max-results", 10000, "Maximum number of results to find")
	flags.BoolVar(&options.noProgress, "no-progress", false, "Disable progress display")
	flags.BoolVarP(&options.showDetails, "show-details", "d", false, "Show file sizes and details")
	flags.BoolVar(&options.noSort, "no-sort", false, "Skip sorting results (faster for large result sets)")
	flags.BoolVar(&options.displayAll, "display-all", false, "Display all results in terminal when result count exceeds 100")
	flags.StringVar(&options.outputPath, "output", "", "Save large result output to the specified file path")
	flags.StringVar(&options.largeResultsAction, "large-results-action", ui.LargeResultsActionAsk, "Action for more than 100 results: ask, save, or display")

	return root
}

func runFinder(ctx context.Context, basePath, pattern string, options finder.FinderOptions) ([]types.FileResult, []string, error) {
	options.Context = ctx
	f, err := finder.NewFileFinder(basePath, pattern, options)
	if err != nil {
		return nil, nil, err
	}
	files, dirs := f.FindFilesAndDirs()
	return files, dirs, nil
}

func resolveLargeResultsAction(cmd *cobra.Command, action string, displayAll bool, outputPath string) (string, error) {
	normalizedAction := strings.ToLower(strings.TrimSpace(action))
	if normalizedAction == "" {
		normalizedAction = ui.LargeResultsActionAsk
	}

	switch normalizedAction {
	case ui.LargeResultsActionAsk, ui.LargeResultsActionSave, ui.LargeResultsActionDisplay:
	default:
		return "", fmt.Errorf("large-results-action must be one of: ask, save, display")
	}

	actionChanged := cmd.Flags().Changed("large-results-action")

	if displayAll && actionChanged && normalizedAction == ui.LargeResultsActionSave {
		return "", fmt.Errorf("--display-all conflicts with --large-results-action save")
	}
	if displayAll && outputPath != "" {
		return "", fmt.Errorf("--display-all conflicts with --output")
	}
	if outputPath != "" && actionChanged && normalizedAction == ui.LargeResultsActionDisplay {
		return "", fmt.Errorf("--output conflicts with --large-results-action display")
	}

	if displayAll {
		return ui.LargeResultsActionDisplay, nil
	}
	if outputPath != "" && normalizedAction == ui.LargeResultsActionAsk {
		return ui.LargeResultsActionSave, nil
	}

	return normalizedAction, nil
}

func parseSize(sizeStr string) (int64, error) {
	if strings.ToLower(sizeStr) == "inf" {
		return 1<<63 - 1, nil // Max int64
	}

	sizeStr = strings.ToUpper(sizeStr)

	// Ordered from longest suffix to shortest to avoid ambiguous matching
	// (e.g., "1KB" matching "B" before "KB")
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{"TB", 1024 * 1024 * 1024 * 1024},
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, u := range units {
		if strings.HasSuffix(sizeStr, u.suffix) {
			numStr := strings.TrimSuffix(sizeStr, u.suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, err
			}
			return int64(num * float64(u.multiplier)), nil
		}
	}

	// No unit specified, assume bytes
	return strconv.ParseInt(sizeStr, 10, 64)
}
