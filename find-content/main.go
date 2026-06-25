package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	var (
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
	)

	rootCmd := &cobra.Command{
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
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			directory := args[0]
			keyword := args[1]

			// Parse comma-separated arguments
			var fileExtensions, excludeDirsList, excludeFilesList []string
			if extensions != "" {
				fileExtensions = strings.Split(extensions, ",")
			}
			if excludeDirs != "" {
				excludeDirsList = strings.Split(excludeDirs, ",")
			}
			if excludeFiles != "" {
				excludeFilesList = strings.Split(excludeFiles, ",")
			}

			searcher := NewFileSearcher(caseSensitive, suppressWarnings, searchAll, fileExtensions, excludeDirsList, excludeFilesList)

			if listMode {
				if err := searcher.listDirectoryContents(directory, showHidden); err != nil {
					os.Exit(1)
				}
			} else {
				var maxResultsPtr *int
				if maxResults > 0 {
					maxResultsPtr = &maxResults
				}

				matches := searcher.grepRecursive(
					directory,
					keyword,
					useRegex,
					multiline,
					!noLineNumbers,
					!noFilePath,
					maxResultsPtr,
				)

				if matches == 0 {
					fmt.Println("No matches found")
				} else {
					fmt.Printf("\nFound %d match(es)\n", matches)
				}
			}
		},
	}

	// Add flags
	rootCmd.Flags().BoolVarP(&useRegex, "regex", "r", false, "Treat keyword as regex pattern")
	rootCmd.Flags().BoolVarP(&caseSensitive, "case-sensitive", "c", false, "Case sensitive search")
	rootCmd.Flags().BoolVarP(&multiline, "multiline", "M", false, "Enable multiline search with \\n in keyword")
	rootCmd.Flags().StringVarP(&extensions, "extensions", "e", "", "Comma-separated list of file extensions to search")
	rootCmd.Flags().StringVar(&excludeDirs, "exclude-dirs", "", "Comma-separated list of directories to exclude")
	rootCmd.Flags().StringVar(&excludeFiles, "exclude-files", "", "Comma-separated list of files to exclude")
	rootCmd.Flags().BoolVar(&noLineNumbers, "no-line-numbers", false, "Hide line numbers in output")
	rootCmd.Flags().BoolVar(&noFilePath, "no-file-path", false, "Hide file paths in output")
	rootCmd.Flags().IntVarP(&maxResults, "max-results", "m", 0, "Maximum number of results to show")
	rootCmd.Flags().BoolVarP(&listMode, "list", "l", false, "List directory contents instead of searching")
	rootCmd.Flags().BoolVar(&showHidden, "show-hidden", false, "Show hidden files when listing")
	rootCmd.Flags().BoolVar(&suppressWarnings, "suppress-warnings", false, "Suppress warning messages")
	rootCmd.Flags().BoolVar(&searchAll, "all", false, "Search in all files (not limited by extension)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
