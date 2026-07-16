package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"find-content/internal/searcher"
)

type commandOptions struct {
	useRegex         bool
	caseSensitive    bool
	multiline        bool
	extensions       string
	excludeDirs      string
	excludeFiles     string
	noDefaultExclude bool
	noLineNumbers    bool
	noFilePath       bool
	maxResults       int
	maxWorkers       int
	maxLineSize      int64
	maxMultilineSize int64
	listMode         bool
	showHidden       bool
	suppressWarnings bool
	searchAll        bool
}

func defaultCommandOptions() commandOptions {
	return commandOptions{
		maxWorkers:       searcher.DefaultWorkers(),
		maxLineSize:      searcher.DefaultMaxLineSize,
		maxMultilineSize: searcher.DefaultMaxMultilineSize,
	}
}

func validateOptions(options commandOptions) error {
	if options.maxResults < 0 {
		return fmt.Errorf("--max-results must not be negative")
	}
	if options.maxWorkers < 1 {
		return fmt.Errorf("--max-workers must be at least 1")
	}
	if options.maxLineSize <= 0 {
		return fmt.Errorf("--max-line-size must be greater than 0")
	}
	if options.maxMultilineSize <= 0 {
		return fmt.Errorf("--max-multiline-size must be greater than 0")
	}
	if options.searchAll && options.extensions != "" {
		return fmt.Errorf("--all cannot be used with --extensions")
	}
	return nil
}

func validateModeFlags(command *cobra.Command, options commandOptions) error {
	if options.listMode {
		searchOnly := []string{
			"regex", "case-sensitive", "multiline", "extensions", "exclude-dirs",
			"exclude-files", "no-default-excludes", "no-line-numbers", "no-file-path",
			"max-results", "max-workers", "max-line-size", "max-multiline-size", "all",
			"suppress-warnings",
		}
		for _, name := range searchOnly {
			if command.Flags().Changed(name) {
				return fmt.Errorf("--%s cannot be used with --list", name)
			}
		}
		return nil
	}
	if command.Flags().Changed("show-hidden") {
		return fmt.Errorf("--show-hidden can only be used with --list")
	}
	return nil
}

func normalizeCSV(value string) []string {
	seen := make(map[string]struct{})
	var values []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		values = append(values, item)
	}
	return values
}
