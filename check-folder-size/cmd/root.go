package cmd

import (
	"check-folder-size/internal/scanner"
	"check-folder-size/internal/ui"
	"common-module/utils"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
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
)

var RootCmd = &cobra.Command{
	Use:   "check-folder-size [path]",
	Short: "Calculate folder sizes with improved features",
	Long:  `A tool to analyze folder sizes with progress tracking, exclusion lists, and colored output.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// Validate sort flag
		if sortBy != "size" && sortBy != "name" {
			fmt.Fprintf(os.Stderr, "Error: --sort must be 'size' or 'name', got '%s'\n", sortBy)
			os.Exit(1)
		}

		// Parse exclude list
		var excludeList []string
		if excludeDirs != "" {
			excludeList = strings.Split(excludeDirs, ",")
			for i, item := range excludeList {
				excludeList[i] = strings.TrimSpace(item)
			}
		}

		// Parse size filters
		var minSizeBytes, maxSizeBytes int64
		if minSize != "" {
			var err error
			minSizeBytes, err = parseSize(minSize)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid --min-size value '%s': %v\n", minSize, err)
				os.Exit(1)
			}
		}
		if maxSize != "" {
			var err error
			maxSizeBytes, err = parseSize(maxSize)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid --max-size value '%s': %v\n", maxSize, err)
				os.Exit(1)
			}
		} else {
			maxSizeBytes = 1<<63 - 1
		}

		// Determine path to analyze
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		// Clear screen unless disabled
		if !noClear {
			utils.CLS()
		}

		// Validate path
		parentFolder, err := filepath.Abs(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Invalid path '%s': %v\n", path, err)
			os.Exit(1)
		}

		if _, err := os.Stat(parentFolder); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: Path '%s' does not exist!\n", parentFolder)
			os.Exit(1)
		}

		fmt.Printf("Analyzing: %s\n", parentFolder)
		if len(excludeList) > 0 {
			fmt.Printf("Excluding: %s\n", strings.Join(excludeList, ", "))
		}
		if progress {
			fmt.Println("Calculating sizes (this may take a while for large directories)...")
		}

		// Build context
		var ctx context.Context
		var cancel context.CancelFunc
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		defer cancel()

		startTime := time.Now()

		// Get folder sizes
		result := scanner.GetSizesOfSubfolders(parentFolder, scanner.ScanOptions{
			ShowProgress: progress,
			ExcludeList:  excludeList,
			Ctx:          ctx,
			MaxDepth:     maxDepth,
		})

		elapsed := time.Since(startTime)

		if progress {
			fmt.Printf("\nAnalysis completed in %.2f seconds\n", elapsed.Seconds())
		}

		if result.WarningCount > 0 {
			fmt.Fprintf(os.Stderr, "Warning: %d files/folders could not be accessed\n", result.WarningCount)
		}

		// Apply size filters
		filteredItems := result.Items
		if minSizeBytes > 0 || maxSizeBytes < (1<<63-1) {
			filteredItems = make([]scanner.ItemInfo, 0, len(result.Items))
			for _, item := range result.Items {
				if item.Size >= minSizeBytes && item.Size <= maxSizeBytes {
					filteredItems = append(filteredItems, item)
				}
			}
		}

		// Output results
		if jsonOutput {
			sort.Slice(filteredItems, func(i, j int) bool {
				return filteredItems[i].Name < filteredItems[j].Name
			})
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(filteredItems); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
				os.Exit(1)
			}
		} else {
			ui.PrintResults(filteredItems, parentFolder, sortBy, !asc)
		}
	},
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	RootCmd.Flags().StringVarP(&sortBy, "sort", "s", "size", "Sort by 'size' or 'name'")
	RootCmd.Flags().BoolVarP(&asc, "asc", "a", false, "Sort in ascending order")
	RootCmd.Flags().BoolVarP(&progress, "progress", "p", false, "Show progress during calculation")
	RootCmd.Flags().BoolVarP(&noClear, "no-clear", "n", false, "Don't clear screen before output")
	RootCmd.Flags().StringVarP(&excludeDirs, "exclude-dirs", "e", "", "Comma-separated list of folders/files to exclude (e.g., node_modules,.git,target)")
	RootCmd.Flags().IntVar(&timeout, "timeout", 0, "Timeout in seconds (0 = no timeout)")
	RootCmd.Flags().IntVar(&maxDepth, "depth", 0, "Maximum recursion depth (0 = unlimited)")
	RootCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results in JSON format")
	RootCmd.Flags().StringVar(&minSize, "min-size", "", "Minimum size filter (e.g., 1KB, 10MB, 1GB)")
	RootCmd.Flags().StringVar(&maxSize, "max-size", "", "Maximum size filter (e.g., 100MB, 1GB)")
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
