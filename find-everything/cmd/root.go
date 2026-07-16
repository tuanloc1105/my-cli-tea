package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"find-everything/internal/finder"
	"find-everything/internal/types"
	"find-everything/internal/ui"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type searchRunner func(context.Context, string, string, finder.FinderOptions) (types.SearchResults, error)

type headerPrinter func(string, string, ui.ResultsOutputOptions) error

type resultsPrinter func(types.SearchResults, ui.ResultsOutputOptions) error

type ttyDetector func(any) bool

type exitCodeError struct {
	code int
}

func (e exitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", e.code)
}

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
func ExecuteContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return executeContext(ctx, args, stdin, stdout, stderr, runFinder, ui.PrintSearchHeader, ui.PrintSearchResults, isTTY)
}

func executeContext(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	run searchRunner,
	printHeader headerPrinter,
	printResults resultsPrinter,
	detectTTY ttyDetector,
) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdin == nil {
		stdin = strings.NewReader("")
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
	if detectTTY == nil {
		detectTTY = func(any) bool { return false }
	}

	root := newRootCommand(run, printHeader, printResults, detectTTY, stdin, stdout, stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "Search canceled.")
		return 130
	}
	if err == nil {
		return 0
	}

	var exitErr exitCodeError
	if errors.As(err, &exitErr) {
		if exitErr.code == 130 {
			fmt.Fprintln(stderr, "Search canceled.")
		}
		return exitErr.code
	}
	fmt.Fprintf(stderr, "Error: %s\n", ui.SafeText(err.Error()))
	return 1
}

func newRootCommand(
	run searchRunner,
	printHeader headerPrinter,
	printResults resultsPrinter,
	detectTTY ttyDetector,
	stdin io.Reader,
	stdout, stderr io.Writer,
) *cobra.Command {
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
		Example: `  find-everything "C:\" "*.txt" --file-types txt,log
	find-everything "/home/user" "*.py" --exclude-dirs node_modules,.git
	find-everything "D:\" "zalo*" --min-size 1MB --max-size 100MB
	find-everything "." "*.jpg" --case-sensitive --show-details`,
		Args:          cobra.ExactArgs(2),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Context().Err() != nil {
				return exitCodeError{code: 130}
			}
			basePath := args[0]
			pattern := args[1]
			if pattern == "" {
				return fmt.Errorf("pattern must not be empty")
			}
			if options.maxWorkers <= 0 {
				return fmt.Errorf("max-workers must be greater than zero")
			}
			if options.maxResults <= 0 {
				return fmt.Errorf("max-results must be greater than zero")
			}

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
			if minSizeBytes > maxSizeBytes {
				return fmt.Errorf("min-size must not exceed max-size")
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

			stdoutTTY := detectTTY(stdout)
			stderrTTY := detectTTY(stderr)
			outputOptions := ui.ResultsOutputOptions{
				ShowDetails:        options.showDetails,
				Pattern:            pattern,
				BasePath:           basePath,
				NoSort:             options.noSort,
				LargeResultsAction: resolvedLargeResultsAction,
				OutputPath:         options.outputPath,
				PromptReader:       stdin,
				PromptWriter:       stderr,
				Stdout:             stdout,
				Stderr:             stderr,
				StdoutTTY:          stdoutTTY,
				StderrTTY:          stderrTTY,
				PromptTTY:          detectTTY(stdin) && stderrTTY,
			}
			if err := printHeader(basePath, pattern, outputOptions); err != nil {
				return err
			}

			var progressErr error
			var progressMu sync.Mutex
			var progressRendered atomic.Bool
			recordProgressError := func(err error) {
				if err == nil {
					return
				}
				progressMu.Lock()
				defer progressMu.Unlock()
				if progressErr == nil {
					progressErr = err
				}
			}
			var progressRenderer *ui.Renderer
			if !options.noProgress && stderrTTY {
				progressRenderer = ui.NewRenderer(stderr, io.Discard, true, false)
			}

			finderOptions := finder.FinderOptions{
				CaseSensitive:   options.caseSensitive,
				MaxWorkers:      options.maxWorkers,
				ExcludeDirs:     processedExcludeDirs,
				ExcludePatterns: options.excludePatterns,
				FileTypes:       options.fileTypes,
				MinSize:         minSizeBytes,
				MaxSize:         maxSizeBytes,
				MaxResults:      options.maxResults,
			}
			if progressRenderer != nil {
				finderOptions.Progress = func(snapshot types.ProgressSnapshot) {
					progressRendered.Store(true)
					recordProgressError(progressRenderer.RenderProgress(snapshot))
				}
			}

			results, searchErr := run(cmd.Context(), basePath, pattern, finderOptions)
			if progressRendered.Load() {
				if _, err := fmt.Fprintln(stderr); err != nil {
					recordProgressError(fmt.Errorf("finish progress output: %w", err))
				}
			}
			if errors.Is(searchErr, context.Canceled) || errors.Is(searchErr, context.DeadlineExceeded) {
				return exitCodeError{code: 130}
			}
			if searchErr != nil {
				return searchErr
			}
			progressMu.Lock()
			deferredProgressErr := progressErr
			progressMu.Unlock()
			if deferredProgressErr != nil {
				return deferredProgressErr
			}
			if err := printResults(results, outputOptions); err != nil {
				return err
			}
			if results.Report.Incomplete {
				return exitCodeError{code: 2}
			}
			return nil
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

func runFinder(ctx context.Context, basePath, pattern string, options finder.FinderOptions) (types.SearchResults, error) {
	f, err := finder.NewFileFinder(basePath, pattern, options)
	if err != nil {
		return types.SearchResults{}, err
	}
	return f.FindFilesAndDirs(ctx)
}

func isTTY(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
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
	sizeStr = strings.TrimSpace(sizeStr)
	if strings.EqualFold(sizeStr, "inf") {
		return 1<<63 - 1, nil // Max int64
	}
	if sizeStr == "" {
		return 0, fmt.Errorf("size must not be empty")
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
			num, ok := new(big.Rat).SetString(numStr)
			if !ok {
				return 0, fmt.Errorf("size must be finite and non-negative")
			}
			if num.Sign() < 0 {
				return 0, fmt.Errorf("size must be finite and non-negative")
			}
			scaled := new(big.Rat).Mul(num, big.NewRat(u.multiplier, 1))
			if scaled.Cmp(big.NewRat(math.MaxInt64, 1)) > 0 {
				return 0, fmt.Errorf("size overflows int64")
			}
			wholeBytes := new(big.Int).Quo(scaled.Num(), scaled.Denom())
			return wholeBytes.Int64(), nil
		}
	}

	// No unit specified, assume bytes
	value, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("size must be non-negative")
	}
	return value, nil
}
