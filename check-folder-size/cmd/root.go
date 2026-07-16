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
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type scanRunner func(string, scanner.ScanOptions) (scanner.ScanResult, error)

type clearScreen func()

type terminalDetector func(io.Writer) bool

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
	sizeMode    string
}

var errPartialResult = errors.New("partial scan result")

// ExecuteContext runs check-folder-size with the supplied process arguments and
// streams. It returns the process exit code without terminating the caller.
func ExecuteContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return executeContextWithTerminal(
		ctx,
		args,
		stdout,
		stderr,
		scanner.ScanDirectory,
		utils.CLS,
		writerIsTerminal,
	)
}

func executeContext(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	scan scanRunner,
	clear clearScreen,
) int {
	return executeContextWithTerminal(ctx, args, stdout, stderr, scan, clear, writerIsTerminal)
}

func executeContextWithTerminal(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	scan scanRunner,
	clear clearScreen,
	isTerminal terminalDetector,
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
	if isTerminal == nil {
		isTerminal = writerIsTerminal
	}

	root := newRootCommand(scan, clear, isTerminal, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, errPartialResult) {
			return 1
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCommand(
	scan scanRunner,
	clear clearScreen,
	isTerminal terminalDetector,
	stdout, stderr io.Writer,
) *cobra.Command {
	options := commandOptions{sortBy: "size", sizeMode: string(scanner.SizeModeAllocated)}

	root := &cobra.Command{
		Use:           "check-folder-size [path]",
		Short:         "Calculate folder sizes with improved features",
		Long:          `A tool to analyze folder sizes with progress tracking, exclusion lists, and colored output.`,
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalysis(cmd.Context(), args, options, stdout, stderr, scan, clear, isTerminal)
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
	flags.StringVar(&options.sizeMode, "size-mode", string(scanner.SizeModeAllocated), "Size metric: 'allocated' or 'logical'")

	return root
}

func runAnalysis(
	ctx context.Context,
	args []string,
	command commandOptions,
	stdout, stderr io.Writer,
	scan scanRunner,
	clear clearScreen,
	isTerminal terminalDetector,
) error {
	validated, err := validateCommand(args, command)
	if err != nil {
		return err
	}

	if !command.jsonOutput && !command.noClear && isTerminal(stdout) {
		clear()
	}

	progressWriter := stdout
	if command.jsonOutput {
		progressWriter = stderr
	}
	if !command.jsonOutput || command.progress {
		fmt.Fprintf(progressWriter, "Analyzing: %s\n", validated.parentFolder)
		if len(validated.excludeList) > 0 {
			fmt.Fprintf(progressWriter, "Excluding: %s\n", strings.Join(validated.excludeList, ", "))
		}
	}
	if command.progress {
		fmt.Fprintln(progressWriter, "Calculating sizes (this may take a while for large directories)...")
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
	result, scanErr := scan(validated.parentFolder, scanner.ScanOptions{
		ShowProgress:   command.progress,
		ExcludeList:    validated.excludeList,
		Ctx:            scanCtx,
		MaxDepth:       command.maxDepth,
		ProgressWriter: progressWriter,
		SizeMode:       validated.sizeMode,
	})

	if command.progress {
		fmt.Fprintf(progressWriter, "\nAnalysis completed in %.2f seconds\n", time.Since(startTime).Seconds())
	}

	partial := result.Status == scanner.ScanStatusPartial || result.WarningCount > 0
	if result.Status == scanner.ScanStatusFailed || (scanErr != nil && len(result.Items) == 0 && result.WarningCount == 0) {
		if scanErr != nil {
			return fmt.Errorf("scanning '%s': %w", validated.parentFolder, scanErr)
		}
		return fmt.Errorf("scanning '%s': scan failed", validated.parentFolder)
	}
	if scanErr != nil {
		partial = true
	}

	items := filterAndSortItems(result.Items, validated.minSize, validated.maxSize, command.sortBy, command.asc)
	if command.jsonOutput {
		if err := json.NewEncoder(stdout).Encode(items); err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
	} else {
		ui.PrintResults(stdout, items, validated.parentFolder, validated.sizeMode)
	}

	if partial {
		writePartialWarning(stderr, result, scanErr)
		return errPartialResult
	}
	return nil
}

type validatedCommand struct {
	parentFolder string
	excludeList  []string
	minSize      int64
	maxSize      int64
	sizeMode     scanner.SizeMode
}

func validateCommand(args []string, command commandOptions) (validatedCommand, error) {
	if command.sortBy != "size" && command.sortBy != "name" {
		return validatedCommand{}, fmt.Errorf("--sort must be 'size' or 'name', got %q", command.sortBy)
	}
	if command.maxDepth < 0 {
		return validatedCommand{}, fmt.Errorf("--depth must be non-negative, got %d", command.maxDepth)
	}
	if command.timeout < 0 {
		return validatedCommand{}, fmt.Errorf("--timeout must be non-negative, got %d", command.timeout)
	}
	if int64(command.timeout) > int64(math.MaxInt64/time.Second) {
		return validatedCommand{}, fmt.Errorf("--timeout value %d overflows time.Duration", command.timeout)
	}

	sizeMode, err := scanner.ParseSizeMode(command.sizeMode)
	if err != nil {
		return validatedCommand{}, fmt.Errorf("invalid --size-mode value %q: must be 'allocated' or 'logical'", command.sizeMode)
	}

	minSize := int64(0)
	if command.minSize != "" {
		minSize, err = parseSize(command.minSize)
		if err != nil {
			return validatedCommand{}, fmt.Errorf("invalid --min-size value %q: %w", command.minSize, err)
		}
	}
	maxSize := int64(math.MaxInt64)
	if command.maxSize != "" {
		maxSize, err = parseSize(command.maxSize)
		if err != nil {
			return validatedCommand{}, fmt.Errorf("invalid --max-size value %q: %w", command.maxSize, err)
		}
	}
	if minSize > maxSize {
		return validatedCommand{}, fmt.Errorf("--min-size (%d) must not exceed --max-size (%d)", minSize, maxSize)
	}

	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	parentFolder, err := filepath.Abs(path)
	if err != nil {
		return validatedCommand{}, fmt.Errorf("invalid path %q: %w", path, err)
	}
	info, err := os.Stat(parentFolder)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return validatedCommand{}, fmt.Errorf("path %q does not exist", parentFolder)
		}
		return validatedCommand{}, fmt.Errorf("cannot access path %q: %w", parentFolder, err)
	}
	if !info.IsDir() {
		return validatedCommand{}, fmt.Errorf("path %q is not a directory", parentFolder)
	}
	root, err := os.Open(parentFolder)
	if err != nil {
		return validatedCommand{}, fmt.Errorf("cannot open directory %q: %w", parentFolder, err)
	}
	if err := root.Close(); err != nil {
		return validatedCommand{}, fmt.Errorf("closing directory %q: %w", parentFolder, err)
	}

	return validatedCommand{
		parentFolder: parentFolder,
		excludeList:  parseExcludeList(command.excludeDirs),
		minSize:      minSize,
		maxSize:      maxSize,
		sizeMode:     sizeMode,
	}, nil
}

func parseExcludeList(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func parseSize(value string) (int64, error) {
	if strings.EqualFold(value, "inf") {
		return math.MaxInt64, nil
	}
	if value == "" {
		return 0, errors.New("size must not be empty")
	}

	upper := strings.ToUpper(value)
	units := []struct {
		suffix     string
		multiplier uint64
	}{
		{suffix: "TB", multiplier: 1024 * 1024 * 1024 * 1024},
		{suffix: "GB", multiplier: 1024 * 1024 * 1024},
		{suffix: "MB", multiplier: 1024 * 1024},
		{suffix: "KB", multiplier: 1024},
		{suffix: "B", multiplier: 1},
	}

	number := upper
	multiplier := uint64(1)
	for _, unit := range units {
		if strings.HasSuffix(upper, unit.suffix) {
			number = strings.TrimSuffix(upper, unit.suffix)
			multiplier = unit.multiplier
			break
		}
	}
	whole, fraction, hasFraction := strings.Cut(number, ".")
	if strings.Contains(fraction, ".") || (!hasFraction && whole == "") || (hasFraction && whole == "" && fraction == "") {
		return 0, errors.New("size must include a decimal number")
	}
	for _, digit := range whole + fraction {
		if digit < '0' || digit > '9' {
			return 0, errors.New("size must be a non-negative decimal number with optional B/KB/MB/GB/TB suffix")
		}
	}
	if whole == "" {
		whole = "0"
	}
	digits := whole + fraction
	parsed, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return 0, errors.New("invalid decimal size")
	}
	parsed.Mul(parsed, new(big.Int).SetUint64(multiplier))
	if hasFraction && fraction != "" {
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(fraction))), nil)
		parsed.Quo(parsed, scale)
	}
	if parsed.Cmp(big.NewInt(math.MaxInt64)) > 0 {
		return 0, errors.New("size overflows int64")
	}
	return parsed.Int64(), nil
}

func filterAndSortItems(
	items []scanner.ItemInfo,
	minSize, maxSize int64,
	sortBy string,
	ascending bool,
) []scanner.ItemInfo {
	filtered := make([]scanner.ItemInfo, 0, len(items))
	for _, item := range items {
		if item.Size >= minSize && item.Size <= maxSize {
			filtered = append(filtered, item)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		left, right := filtered[i], filtered[j]
		if sortBy == "size" && left.Size != right.Size {
			if ascending {
				return left.Size < right.Size
			}
			return left.Size > right.Size
		}

		leftName := strings.ToLower(left.Name)
		rightName := strings.ToLower(right.Name)
		if leftName != rightName {
			if sortBy == "name" && !ascending {
				return leftName > rightName
			}
			return leftName < rightName
		}
		if left.Name != right.Name {
			if sortBy == "name" && !ascending {
				return left.Name > right.Name
			}
			return left.Name < right.Name
		}
		return false
	})
	return filtered
}

func writePartialWarning(stderr io.Writer, result scanner.ScanResult, scanErr error) {
	parts := make([]string, 0, 3)
	if result.WarningSummary != "" {
		parts = append(parts, result.WarningSummary)
	} else if result.WarningCount > 0 {
		parts = append(parts, fmt.Sprintf("%d entries could not be accessed", result.WarningCount))
	}
	if scanErr != nil {
		parts = append(parts, scanErr.Error())
	}
	if len(parts) == 0 {
		parts = append(parts, "scan returned incomplete results")
	}
	fmt.Fprintf(stderr, "Warning: %s\n", strings.Join(parts, ": "))
}

func writerIsTerminal(writer io.Writer) bool {
	file, ok := writer.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(file.Fd()))
}
