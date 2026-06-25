package finder

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"find-everything/internal/ui"
)

// FinderOptions holds all configuration for FileFinder
type FinderOptions struct {
	CaseSensitive   bool
	MaxWorkers      int
	ExcludeDirs     []string
	ExcludePatterns []string
	FileTypes       []string
	MinSize         int64
	MaxSize         int64
	ShowProgress    bool
	MaxResults      int
	NoSort          bool
}

// FileFinder handles file and directory searching
type FileFinder struct {
	basePath        string
	pattern         string
	caseSensitive   bool
	maxWorkers      int
	excludeDirs     map[string]bool
	excludePatterns []*regexp.Regexp
	fileTypes       map[string]bool
	minSize         int64
	maxSize         int64
	showProgress    bool
	maxResults      int
	noSort          bool
	progressTracker *ui.ProgressTracker
	patternRegex    *regexp.Regexp
	fastMatch       func(string) bool
	ctx             context.Context
	cancel          context.CancelFunc
}

func NewFileFinder(basePath, pattern string, opts FinderOptions) (*FileFinder, error) {
	// Compile pattern regex
	regexPattern := GlobToRegex(pattern)
	if !opts.CaseSensitive {
		regexPattern = "(?i)" + regexPattern
	}
	patternRegex, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %v", err)
	}

	// Compile exclude patterns
	var excludePatterns []*regexp.Regexp
	for _, p := range opts.ExcludePatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid exclude pattern %q: %v", p, err)
		}
		excludePatterns = append(excludePatterns, re)
	}

	// Build exclude dirs set
	excludeDirs := make(map[string]bool)
	for _, dir := range opts.ExcludeDirs {
		excludeDirs[strings.ToLower(dir)] = true
	}

	// Build file types set
	fileTypes := make(map[string]bool)
	for _, ext := range opts.FileTypes {
		fileTypes[strings.ToLower(ext)] = true
	}

	ctx, cancel := context.WithCancel(context.Background())
	maxWorkers := opts.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 1
	}

	// Build fast matcher for simple glob patterns
	fastMatch := buildFastMatcher(pattern, opts.CaseSensitive)

	return &FileFinder{
		basePath:        basePath,
		pattern:         pattern,
		caseSensitive:   opts.CaseSensitive,
		maxWorkers:      maxWorkers,
		excludeDirs:     excludeDirs,
		excludePatterns: excludePatterns,
		fileTypes:       fileTypes,
		minSize:         opts.MinSize,
		maxSize:         opts.MaxSize,
		showProgress:    opts.ShowProgress,
		maxResults:      opts.MaxResults,
		noSort:          opts.NoSort,
		progressTracker: ui.NewProgressTracker(),
		patternRegex:    patternRegex,
		fastMatch:       fastMatch,
		ctx:             ctx,
		cancel:          cancel,
	}, nil
}

// ShouldExcludeDir checks if a directory should be excluded by name.
// Only needs the directory's own name — parent directories were already
// checked during traversal, so excluded parents are never queued.
func (ff *FileFinder) ShouldExcludeDir(dirName string) bool {
	return ff.excludeDirs[strings.ToLower(dirName)]
}

// ShouldExcludeByPattern checks if a file should be excluded via regex patterns.
func (ff *FileFinder) ShouldExcludeByPattern(fullPath string) bool {
	for _, regex := range ff.excludePatterns {
		if regex.MatchString(fullPath) {
			return true
		}
	}
	return false
}

func (ff *FileFinder) MatchesPattern(name string) bool {
	if ff.fastMatch != nil {
		return ff.fastMatch(name)
	}
	return ff.patternRegex.MatchString(name)
}

// GetFileSizeFromEntry gets file size from a DirEntry.
// For symlinks, falls back to os.Stat to follow the link and get the target size.
func (ff *FileFinder) GetFileSizeFromEntry(entry fs.DirEntry, fullPath string) (int64, bool) {
	// Symlink: entry.Info() returns symlink size, not target size
	if entry.Type()&fs.ModeSymlink != 0 {
		info, err := os.Stat(fullPath)
		if err != nil {
			return 0, false
		}
		return info.Size(), true
	}
	info, err := entry.Info()
	if err != nil {
		return 0, false
	}
	return info.Size(), true
}

// CheckFileSize validates file size against min/max bounds using DirEntry.
// Returns (size, passedFilter).
func (ff *FileFinder) CheckFileSize(entry fs.DirEntry, fullPath string) (int64, bool) {
	size, ok := ff.GetFileSizeFromEntry(entry, fullPath)
	if !ok {
		return 0, false
	}
	return size, size >= ff.minSize && size <= ff.maxSize
}

func (ff *FileFinder) CheckFileType(entryName string) bool {
	if len(ff.fileTypes) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(entryName))
	return ff.fileTypes[ext]
}

// Utility functions

func GlobToRegex(pattern string) string {
	pattern = regexp.QuoteMeta(pattern)
	pattern = strings.ReplaceAll(pattern, "\\*", ".*")
	pattern = strings.ReplaceAll(pattern, "\\?", ".")
	return "^" + pattern + "$"
}

// buildFastMatcher detects simple glob patterns and returns a fast
// string-based matcher. Returns nil for complex patterns (fallback to regex).
func buildFastMatcher(pattern string, caseSensitive bool) func(string) bool {
	// Case 1: "*.ext" — suffix match
	if strings.HasPrefix(pattern, "*") && !strings.ContainsAny(pattern[1:], "*?[]{}") {
		suffix := pattern[1:] // e.g. ".txt"
		if !caseSensitive {
			suffix = strings.ToLower(suffix)
			return func(name string) bool {
				return strings.HasSuffix(strings.ToLower(name), suffix)
			}
		}
		return func(name string) bool {
			return strings.HasSuffix(name, suffix)
		}
	}

	// Case 2: "prefix*" — prefix match
	if strings.HasSuffix(pattern, "*") && !strings.ContainsAny(pattern[:len(pattern)-1], "*?[]{}") {
		prefix := pattern[:len(pattern)-1]
		if !caseSensitive {
			prefix = strings.ToLower(prefix)
			return func(name string) bool {
				return strings.HasPrefix(strings.ToLower(name), prefix)
			}
		}
		return func(name string) bool {
			return strings.HasPrefix(name, prefix)
		}
	}

	// Case 3: no wildcards — exact match
	if !strings.ContainsAny(pattern, "*?[]{}") {
		if !caseSensitive {
			lower := strings.ToLower(pattern)
			return func(name string) bool {
				return strings.ToLower(name) == lower
			}
		}
		return func(name string) bool {
			return name == pattern
		}
	}

	return nil // complex pattern, fallback to regex
}

