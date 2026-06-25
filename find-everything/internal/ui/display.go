package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"find-everything/internal/types"

	"golang.org/x/term"
)

// Colors for terminal output
const (
	ColorHeader    = "\033[95m"
	ColorOKBlue    = "\033[94m"
	ColorOKCyan    = "\033[96m"
	ColorOKGreen   = "\033[92m"
	ColorWarning   = "\033[93m"
	ColorFail      = "\033[91m"
	ColorEndC      = "\033[0m"
	ColorBold      = "\033[1m"
	ColorUnderline = "\033[4m"
)

const (
	LargeResultsActionAsk     = "ask"
	LargeResultsActionSave    = "save"
	LargeResultsActionDisplay = "display"
)

// ResultsOutputOptions controls how search results are printed or saved.
type ResultsOutputOptions struct {
	ShowDetails        bool
	Pattern            string
	BasePath           string
	NoSort             bool
	LargeResultsAction string
	OutputPath         string
	PromptReader       io.Reader
	PromptWriter       io.Writer
}

// ProgressTracker tracks search progress
type ProgressTracker struct {
	totalDirs     int64
	processedDirs int64
	foundFiles    int64
	foundDirs     int64
	startTime     time.Time
}

func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		startTime: time.Now(),
	}
}

func (pt *ProgressTracker) Update(filesCount, dirsCount int) {
	atomic.AddInt64(&pt.foundFiles, int64(filesCount))
	atomic.AddInt64(&pt.foundDirs, int64(dirsCount))
}

func (pt *ProgressTracker) UpdateProcessedDirs(count int) {
	atomic.AddInt64(&pt.processedDirs, int64(count))
}

func (pt *ProgressTracker) SetTotalDirs(total int) {
	atomic.StoreInt64(&pt.totalDirs, int64(total))
}

func (pt *ProgressTracker) PrintProgress() {
	elapsed := time.Since(pt.startTime).Seconds()
	processedDirs := atomic.LoadInt64(&pt.processedDirs)
	foundFiles := atomic.LoadInt64(&pt.foundFiles)
	foundDirs := atomic.LoadInt64(&pt.foundDirs)
	fmt.Printf("\r%sProcessed: %d | Found: %d files, %d dirs | Time: %.1fs%s",
		ColorOKCyan, processedDirs, foundFiles, foundDirs, elapsed, ColorEndC)
}

func FormatSize(sizeBytes int64) string {
	const unit = 1024
	if sizeBytes < unit {
		return fmt.Sprintf("%d B", sizeBytes)
	}
	div, exp := int64(unit), 0
	for n := sizeBytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(sizeBytes)/float64(div), "KMGTPE"[exp])
}

// sortResults sorts files and dirs in parallel.
func sortResults(files []types.FileResult, dirs []string) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	}()
	go func() {
		defer wg.Done()
		sort.Strings(dirs)
	}()
	wg.Wait()
}

func SaveResultsToFile(files []types.FileResult, dirs []string, pattern, basePath string, showDetails bool, noSort bool, outputPath string) (string, error) {
	filename := outputPath
	if filename == "" {
		timestamp := time.Now().Format("20060102_150405")
		filename = fmt.Sprintf("search_results_%s.txt", timestamp)
	}

	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	fmt.Fprintf(writer, "Enhanced File and Directory Finder Results\n")
	fmt.Fprintf(writer, "%s\n", strings.Repeat("=", 80))
	fmt.Fprintf(writer, "Search Date: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(writer, "Base Path: %s\n", basePath)
	fmt.Fprintf(writer, "Search Pattern: %s\n", pattern)
	fmt.Fprintf(writer, "Files found: %d\n", len(files))
	fmt.Fprintf(writer, "Directories found: %d\n", len(dirs))
	fmt.Fprintf(writer, "Total results: %d\n", len(files)+len(dirs))
	fmt.Fprintf(writer, "%s\n\n", strings.Repeat("=", 80))

	if !noSort {
		sortResults(files, dirs)
	}

	if len(files) > 0 {
		fmt.Fprintf(writer, "MATCHING FILES:\n")
		fmt.Fprintf(writer, "%s\n", strings.Repeat("-", 40))
		for _, f := range files {
			if showDetails {
				fmt.Fprintf(writer, "  %s (%s)\n", f.Path, FormatSize(f.Size))
			} else {
				fmt.Fprintf(writer, "  %s\n", f.Path)
			}
		}
		fmt.Fprintf(writer, "\n")
	}

	if len(dirs) > 0 {
		fmt.Fprintf(writer, "MATCHING DIRECTORIES:\n")
		fmt.Fprintf(writer, "%s\n", strings.Repeat("-", 40))
		for _, dirPath := range dirs {
			fmt.Fprintf(writer, "  %s\n", dirPath)
		}
		fmt.Fprintf(writer, "\n")
	}

	if err := writer.Flush(); err != nil {
		return "", err
	}

	return filename, nil
}

func PrintResults(files []types.FileResult, dirs []string, options ResultsOutputOptions) error {
	totalResults := len(files) + len(dirs)

	if totalResults <= 100 {
		printResultsSummary(len(files), len(dirs), totalResults, false)
		printResultDetails(files, dirs, options.ShowDetails, options.NoSort)
		return nil
	}

	printResultsSummary(len(files), len(dirs), totalResults, true)

	action := strings.ToLower(strings.TrimSpace(options.LargeResultsAction))
	if action == "" {
		action = LargeResultsActionAsk
	}

	if action == LargeResultsActionAsk {
		action = resolvePromptedLargeResultsAction(promptReader(options), promptWriter(options))
	}

	if action == LargeResultsActionDisplay {
		printResultDetails(files, dirs, options.ShowDetails, options.NoSort)
		return nil
	}

	filename, err := SaveResultsToFile(files, dirs, options.Pattern, options.BasePath, options.ShowDetails, options.NoSort, options.OutputPath)
	if err != nil {
		return fmt.Errorf("save results: %w", err)
	}

	fmt.Printf("%sResults saved to: %s%s\n", ColorOKCyan, filename, ColorEndC)
	return nil
}

func printResultsSummary(filesCount, dirsCount, totalResults int, exceededLimit bool) {
	fmt.Printf("\n%s%sSearch Results:%s\n", ColorBold, ColorHeader, ColorEndC)
	fmt.Printf("%sFiles found: %d%s\n", ColorOKGreen, filesCount, ColorEndC)
	fmt.Printf("%sDirectories found: %d%s\n", ColorOKBlue, dirsCount, ColorEndC)
	if exceededLimit {
		fmt.Printf("%sTotal results: %d (exceeds 100)%s\n", ColorWarning, totalResults, ColorEndC)
	}
}

func printResultDetails(files []types.FileResult, dirs []string, showDetails bool, noSort bool) {
	if !noSort {
		sortResults(files, dirs)
	}

	if len(files) > 0 {
		fmt.Printf("\n%s%sMatching Files:%s\n", ColorBold, ColorOKGreen, ColorEndC)
		for _, f := range files {
			if showDetails {
				fmt.Printf("  %s (%s)\n", f.Path, FormatSize(f.Size))
			} else {
				fmt.Printf("  %s\n", f.Path)
			}
		}
	}

	if len(dirs) > 0 {
		fmt.Printf("\n%s%sMatching Directories:%s\n", ColorBold, ColorOKBlue, ColorEndC)
		for _, dirPath := range dirs {
			fmt.Printf("  %s\n", dirPath)
		}
	}
}

func promptReader(options ResultsOutputOptions) io.Reader {
	if options.PromptReader != nil {
		return options.PromptReader
	}
	return os.Stdin
}

func promptWriter(options ResultsOutputOptions) io.Writer {
	if options.PromptWriter != nil {
		return options.PromptWriter
	}
	return os.Stdout
}

func resolvePromptedLargeResultsAction(reader io.Reader, writer io.Writer) string {
	if !canPrompt(reader, writer) {
		fmt.Fprintf(writer, "%sNon-interactive terminal detected; saving results to file.%s\n", ColorWarning, ColorEndC)
		return LargeResultsActionSave
	}

	return promptLargeResultsAction(reader, writer)
}

func canPrompt(reader io.Reader, writer io.Writer) bool {
	input, inputOK := reader.(*os.File)
	output, outputOK := writer.(*os.File)
	return inputOK && outputOK && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
}

func promptLargeResultsAction(reader io.Reader, writer io.Writer) string {
	restoreTerminal := enableSingleKeyInput(reader)
	if restoreTerminal != nil {
		defer restoreTerminal()
	}

	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprint(writer, "Choose output: press [s] save to file / [d] display in terminal: ")

		answer, ok := readPromptChoice(reader)
		if !ok {
			return LargeResultsActionSave
		}

		switch answer {
		case "", "s":
			fmt.Fprint(writer, "\r\n")
			return LargeResultsActionSave
		case "d":
			fmt.Fprint(writer, "\r\n")
			return LargeResultsActionDisplay
		default:
			fmt.Fprint(writer, "\r\nInvalid choice. Please press s or d.\r\n")
		}
	}

	return LargeResultsActionSave
}

func enableSingleKeyInput(reader io.Reader) func() {
	input, ok := reader.(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return nil
	}

	state, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return nil
	}

	return func() {
		_ = term.Restore(int(input.Fd()), state)
	}
}

func readPromptChoice(reader io.Reader) (string, bool) {
	var buffer [1]byte
	for {
		n, err := reader.Read(buffer[:])
		if err != nil {
			return "", false
		}
		if n == 0 {
			continue
		}

		switch buffer[0] {
		case '\r', '\n':
			return "", true
		default:
			return strings.ToLower(string(buffer[0])), true
		}
	}
}
