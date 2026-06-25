package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// matchResult represents a single search match
type matchResult struct {
	lineNum int
	endLine int
	content string
}

// searchMatcher holds pre-compiled search state to avoid per-line/per-file recomputation
type searchMatcher struct {
	regex         *regexp.Regexp
	keyword       string
	lowerKeyword  string
	searchPattern string // multiline: \n converted to actual newlines
	lowerPattern  string // multiline case-insensitive
	caseSensitive bool
}

func newSearchMatcher(keyword string, useRegex, caseSensitive, multiline bool) (*searchMatcher, error) {
	sm := &searchMatcher{
		keyword:       keyword,
		caseSensitive: caseSensitive,
	}

	if multiline {
		sm.searchPattern = strings.ReplaceAll(keyword, "\\n", "\n")
		if !caseSensitive {
			sm.lowerPattern = strings.ToLower(sm.searchPattern)
		}
		if useRegex {
			flags := ""
			if !caseSensitive {
				flags = "(?i)"
			}
			re, err := regexp.Compile(flags + sm.searchPattern)
			if err != nil {
				return nil, err
			}
			sm.regex = re
		}
	} else {
		if useRegex {
			flags := ""
			if !caseSensitive {
				flags = "(?i)"
			}
			re, err := regexp.Compile(flags + keyword)
			if err != nil {
				return nil, err
			}
			sm.regex = re
		} else if !caseSensitive {
			sm.lowerKeyword = strings.ToLower(keyword)
		}
	}

	return sm, nil
}

// FileSearcher handles file content searching operations
type FileSearcher struct {
	caseSensitive    bool
	fileExtensions   map[string]bool
	excludeDirs      map[string]bool
	excludeFiles     map[string]bool
	textExtensions   map[string]bool
	suppressWarnings bool
	searchAll        bool
}

// NewFileSearcher creates a new FileSearcher instance
func NewFileSearcher(caseSensitive, suppressWarnings, searchAll bool, fileExtensions, excludeDirs, excludeFiles []string) *FileSearcher {
	fs := &FileSearcher{
		caseSensitive:    caseSensitive,
		suppressWarnings: suppressWarnings,
		searchAll:        searchAll,
		fileExtensions:   make(map[string]bool),
		excludeDirs:      make(map[string]bool),
		excludeFiles:     make(map[string]bool),
		textExtensions:   make(map[string]bool),
	}

	// Set default excluded directories
	defaultExcludeDirs := []string{".git", "__pycache__", "node_modules", ".vscode", ".idea", "target", "build", "dist"}
	for _, dir := range defaultExcludeDirs {
		fs.excludeDirs[dir] = true
	}

	for _, dir := range excludeDirs {
		fs.excludeDirs[dir] = true
	}

	for _, file := range excludeFiles {
		fs.excludeFiles[file] = true
	}

	for _, ext := range fileExtensions {
		e := strings.ToLower(ext)
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		fs.fileExtensions[e] = true
	}

	// Common text file extensions
	textExts := []string{
		".txt", ".md", ".py", ".js", ".ts", ".html", ".css", ".scss", ".json", ".xml",
		".yaml", ".yml", ".ini", ".cfg", ".conf", ".sh", ".bash", ".sql", ".java",
		".cpp", ".c", ".h", ".hpp", ".cs", ".php", ".rb", ".go", ".rs", ".swift",
		".kt", ".scala", ".r", ".m", ".pl", ".lua", ".dart", ".vue", ".jsx", ".tsx", ".properties", ".log",
	}
	for _, ext := range textExts {
		fs.textExtensions[ext] = true
	}

	return fs
}

// isTextFile checks if a file is likely a text file
func (fs *FileSearcher) isTextFile(filePath string) bool {
	if fs.searchAll {
		return true
	}

	ext := strings.ToLower(filepath.Ext(filePath))

	// If explicit extensions specified, use only those
	if len(fs.fileExtensions) > 0 {
		return fs.fileExtensions[ext]
	}

	// Otherwise fall back to known text extensions
	return fs.textExtensions[ext]
}

// shouldSkipDirectory checks if directory should be skipped
func (fs *FileSearcher) shouldSkipDirectory(dirName string) bool {
	return fs.excludeDirs[dirName]
}

// shouldSkipFile checks if file should be skipped
func (fs *FileSearcher) shouldSkipFile(fileName string) bool {
	return fs.excludeFiles[fileName]
}

// searchInFile searches for keyword in a single file using a pre-compiled matcher
func (fs *FileSearcher) searchInFile(filePath string, matcher *searchMatcher, multiline bool) []matchResult {
	file, err := os.Open(filePath)
	if err != nil {
		if !fs.suppressWarnings {
			fmt.Fprintf(os.Stderr, "Warning: Could not read %s: %v\n", filePath, err)
		}
		return nil
	}
	defer file.Close()

	if multiline {
		return fs.searchInFileMultiline(filePath, file, matcher)
	}

	// Binary file detection for --all mode (stack-allocated buffer)
	if fs.searchAll {
		var preview [512]byte
		n, err := file.Read(preview[:])
		if err != nil && err != io.EOF {
			return nil
		}
		if bytes.IndexByte(preview[:n], 0) != -1 {
			return nil // binary file, skip
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil
		}
	}

	var matches []matchResult
	scanner := bufio.NewScanner(file)
	lineNum := 1

	for scanner.Scan() {
		line := scanner.Text()
		var matched bool

		if matcher.regex != nil {
			matched = matcher.regex.MatchString(line)
		} else if matcher.caseSensitive {
			matched = strings.Contains(line, matcher.keyword)
		} else {
			matched = strings.Contains(strings.ToLower(line), matcher.lowerKeyword)
		}

		if matched {
			matches = append(matches, matchResult{lineNum, lineNum, line})
		}
		lineNum++
	}

	if err := scanner.Err(); err != nil {
		if !fs.suppressWarnings {
			fmt.Fprintf(os.Stderr, "Warning: Error reading %s: %v\n", filePath, err)
		}
	}

	return matches
}

// searchInFileMultiline searches for multiline keyword in a single file
func (fs *FileSearcher) searchInFileMultiline(filePath string, file *os.File, matcher *searchMatcher) []matchResult {
	contentBytes, err := io.ReadAll(file)
	if err != nil {
		if !fs.suppressWarnings {
			fmt.Fprintf(os.Stderr, "Warning: Could not read %s: %v\n", filePath, err)
		}
		return nil
	}

	// Binary detection for --all mode (check already-read content, no double read)
	if fs.searchAll && bytes.IndexByte(contentBytes, 0) != -1 {
		return nil
	}

	// Normalize Windows line endings to Unix line endings
	content := strings.ReplaceAll(string(contentBytes), "\r\n", "\n")

	type position struct {
		start, end int
	}
	var foundPositions []position

	if matcher.regex != nil {
		for _, m := range matcher.regex.FindAllStringIndex(content, -1) {
			foundPositions = append(foundPositions, position{m[0], m[1]})
		}
	} else {
		searchContent := content
		pattern := matcher.searchPattern
		if !matcher.caseSensitive {
			searchContent = strings.ToLower(content)
			pattern = matcher.lowerPattern
		}
		patternLen := len(pattern)
		idx := strings.Index(searchContent, pattern)
		for idx != -1 {
			foundPositions = append(foundPositions, position{idx, idx + patternLen})
			nextStart := idx + patternLen
			if nextStart >= len(searchContent) {
				break
			}
			nextIdx := strings.Index(searchContent[nextStart:], pattern)
			if nextIdx == -1 {
				break
			}
			idx = nextStart + nextIdx
		}
	}

	if len(foundPositions) == 0 {
		return nil
	}

	// Incremental line number calculation: O(n) total instead of O(n*m)
	matches := make([]matchResult, 0, len(foundPositions))
	lastPos := 0
	lastLine := 1
	for _, pos := range foundPositions {
		lastLine += strings.Count(content[lastPos:pos.start], "\n")
		startLineNum := lastLine
		endLineNum := startLineNum + strings.Count(content[pos.start:pos.end], "\n")
		matchedContent := strings.ReplaceAll(content[pos.start:pos.end], "\n", "\\n")
		matches = append(matches, matchResult{startLineNum, endLineNum, matchedContent})
		lastPos = pos.start
	}

	return matches
}

// grepRecursive recursively searches for keyword in files using parallel workers
func (fs *FileSearcher) grepRecursive(rootDir, keyword string, useRegex, multiline bool, showLineNumbers, showFilePath bool, maxResults *int) int {
	info, err := os.Stat(rootDir)
	if err != nil {
		if !fs.suppressWarnings {
			fmt.Fprintf(os.Stderr, "Error: Directory does not exist: %s\n", rootDir)
		}
		return 0
	}

	if !info.IsDir() {
		if !fs.suppressWarnings {
			fmt.Fprintf(os.Stderr, "Error: Path is not a directory: %s\n", rootDir)
		}
		return 0
	}

	// Pre-compile search matcher once (regex + lowercase keyword)
	matcher, err := newSearchMatcher(keyword, useRegex, fs.caseSensitive, multiline)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Invalid regex pattern: %v\n", err)
		return 0
	}

	// Buffered output to reduce syscalls
	out := bufio.NewWriterSize(os.Stdout, 64*1024)
	defer out.Flush()

	// Parallel search with worker pool
	numWorkers := runtime.NumCPU()
	paths := make(chan string, numWorkers*4)
	var totalMatches atomic.Int64
	var maxReached atomic.Bool
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range paths {
				if maxReached.Load() {
					continue // drain channel
				}

				matches := fs.searchInFile(path, matcher, multiline)
				if len(matches) == 0 {
					continue
				}

				mu.Lock()
				for _, match := range matches {
					if maxResults != nil && int(totalMatches.Load()) >= *maxResults {
						maxReached.Store(true)
						break
					}

					if showFilePath {
						out.WriteString(path)
						out.WriteByte(':')
					}
					if showLineNumbers {
						if multiline && match.lineNum != match.endLine {
							out.WriteString(strconv.Itoa(match.lineNum))
							out.WriteString("..")
							out.WriteString(strconv.Itoa(match.endLine))
						} else {
							out.WriteString(strconv.Itoa(match.lineNum))
						}
						out.WriteByte(':')
					}
					out.WriteString(match.content)
					out.WriteByte('\n')
					totalMatches.Add(1)
				}
				mu.Unlock()
			}
		}()
	}

	// Walk directory tree and dispatch file paths to workers
	filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				if !fs.suppressWarnings {
					fmt.Fprintf(os.Stderr, "Warning: Permission denied: %s\n", path)
				}
				return nil
			}
			if !fs.suppressWarnings {
				fmt.Fprintf(os.Stderr, "Warning: Error accessing %s: %v\n", path, err)
			}
			return nil
		}

		if maxReached.Load() {
			return filepath.SkipAll
		}

		if d.IsDir() {
			if fs.shouldSkipDirectory(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if fs.shouldSkipFile(d.Name()) {
			return nil
		}

		if !fs.isTextFile(path) {
			return nil
		}

		paths <- path
		return nil
	})
	close(paths)
	wg.Wait()

	return int(totalMatches.Load())
}

// listDirectoryContents lists directory contents
func (fs *FileSearcher) listDirectoryContents(path string, showHidden bool) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	for _, entry := range entries {
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		entryType := "file"
		if entry.IsDir() {
			entryType = "directory"
		}

		sizeStr := ""
		if entryType == "file" {
			sizeStr = fmt.Sprintf(" (%d bytes)", info.Size())
		}

		fmt.Printf("%10s %s%s\n", entryType, entry.Name(), sizeStr)
	}

	return nil
}
