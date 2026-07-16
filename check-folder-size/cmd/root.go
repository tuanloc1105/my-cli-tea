package cmd

import (
	"check-folder-size/internal/scanner"
	"check-folder-size/internal/ui"
	"common-module/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type scanRunner func(string, scanner.ScanOptions) (scanner.ScanResult, error)

type clearScreen func()

type commandOptions struct {
	sortBy      string
	asc         bool
	progress    bool
	noClear     bool
	excludeDirs string
	timeout     int
	maxDepth    int
	jsonOutput  bool
	minSize     string
	maxSize     string
}

// ExecuteContext runs check-folder-size with the supplied process arguments and
// streams. It returns the process exit code without terminating the caller.
func ExecuteContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return executeContext(ctx, args, stdout, stderr, scanner.GetSizesOfSubfolders, utils.CLS)
}

func executeContext(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	scan scanRunner,
	clear clearScreen,
) int {
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

	root := newRootCommand(scan, clear, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCommand(scan scanRunner, clear clearScreen, stdout, stderr io.Writer) *cobra.Command {
	options := commandOptions{sortBy: "size"}

	root := &cobra.Command{
		Use:           "check-folder-size [path]",
		Short:         "Calculate folder sizes with improved features",
		Long:          `A tool to analyze folder sizes with progress tracking, exclusion lists, and colored output.`,
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalysis(cmd.Context(), args, options, stdout, stderr, scan, clear)
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.Flags()
	flags.StringVarP(&options.sortBy, "sort", "s", "size", "Sort by 'size' or 'name'")
	flags.BoolVarP(&options.asc, "asc", "a", false, "Sort in ascending order")
	flags.BoolVarP(&options.progress, "progress", "p", false, "Show progress during calculation")
	flags.BoolVarP(&options.noClear, "no-clear", "n", false, "Don't clear screen before output")
	flags.StringVarP(&options.excludeDirs, "exclude-dirs", "e", "", "Comma-separated list of folders/files to exclude (e.g., node_modules,.git,target)")
	flags.IntVar(&options.timeout, "timeout", 0, "Timeout in seconds (0 = no timeout)")
	flags.IntVar(&options.maxDepth, "depth", 0, "Maximum recursion depth (0 = unlimited)")
	flags.BoolVar(&options.jsonOutput, "json", false, "Output results in JSON format")
	flags.StringVar(&options.minSize, "min-size", "", "Minimum size filter (e.g., 1KB, 10MB, 1GB)")
	flags.StringVar(&options.maxSize, "max-size", "", "Maximum size filter (e.g., 100MB, 1GB)")

	return root
}

func runAnalysis(
	ctx context.Context,
	args []string,
	command commandOptions,
	stdout, stderr io.Writer,
	scan scanRunner,
	clear clearScreen,
) error {
	if command.sortBy != "size" && command.sortBy != "name" {
		return fmt.Errorf("--sort must be 'size' or 'name', got '%s'", command.sortBy)
	}

	var excludeList []string
	if command.excludeDirs != "" {
		excludeList = strings.Split(command.excludeDirs, ",")
		for i, item := range excludeList {
			excludeList[i] = strings.TrimSpace(item)
		}
	}

	var minSizeBytes, maxSizeBytes int64
	if command.minSize != "" {
		var err error
		minSizeBytes, err = parseSize(command.minSize)
		if err != nil {
			return fmt.Errorf("invalid --min-size value '%s': %w", command.minSize, err)
		}
	}
	if command.maxSize != "" {
		var err error
		maxSizeBytes, err = parseSize(command.maxSize)
		if err != nil {
			return fmt.Errorf("invalid --max-size value '%s': %w", command.maxSize, err)
		}
	} else {
		maxSizeBytes = 1<<63 - 1
	}

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	if !command.noClear {
		clear()
	}

	parentFolder, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("Invalid path '%s': %w", path, err)
	}

	if _, err := os.Stat(parentFolder); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Path '%s' does not exist!", parentFolder)
		}
		return fmt.Errorf("cannot access path '%s': %w", parentFolder, err)
	}

	fmt.Fprintf(stdout, "Analyzing: %s\n", parentFolder)
	if len(excludeList) > 0 {
		fmt.Fprintf(stdout, "Excluding: %s\n", strings.Join(excludeList, ", "))
	}
	if command.progress {
		fmt.Fprintln(stdout, "Calculating sizes (this may take a while for large directories)...")
	}

	scanCtx := ctx
	var cancel context.CancelFunc
	if command.timeout > 0 {
		scanCtx, cancel = context.WithTimeout(ctx, time.Duration(command.timeout)*time.Second)
	} else {
		scanCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	startTime := time.Now()
	result, scanErr := scan(parentFolder, scanner.ScanOptions{
		ShowProgress:   command.progress,
		ExcludeList:    excludeList,
		Ctx:            scanCtx,
		MaxDepth:       command.maxDepth,
		ProgressWriter: stdout,
	})

	if command.progress {
		fmt.Fprintf(stdout, "\nAnalysis completed in %.2f seconds\n", time.Since(startTime).Seconds())
	}

	if result.WarningCount > 0 {
		fmt.Fprintf(stderr, "Warning: %d files/folders could not be accessed\n", result.WarningCount)
	}

	filteredItems := result.Items
	if minSizeBytes > 0 || maxSizeBytes < (1<<63-1) {
		filteredItems = make([]scanner.ItemInfo, 0, len(result.Items))
		for _, item := range result.Items {
			if item.Size >= minSizeBytes && item.Size <= maxSizeBytes {
				filteredItems = append(filteredItems, item)
			}
		}
	}

	if command.jsonOutput {
		sort.Slice(filteredItems, func(i, j int) bool {
			return filteredItems[i].Name < filteredItems[j].Name
		})
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(filteredItems); err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
	} else {
		ui.PrintResults(stdout, filteredItems, parentFolder, command.sortBy, !command.asc)
	}
	if scanErr != nil {
		return fmt.Errorf("scanning '%s': %w", parentFolder, scanErr)
	}

	return nil
}

func parseSize(sizeStr string) (int64, error) {
	if strings.ToLower(sizeStr) == "inf" {
		return 1<<63 - 1, nil
	}

	sizeStr = strings.ToUpper(sizeStr)

	// Ordered from longest suffix to shortest to avoid "KB" matching "B" first
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

	return strconv.ParseInt(sizeStr, 10, 64)
}
