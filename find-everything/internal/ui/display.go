package ui

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"find-everything/internal/types"
)

// Colors for terminal output.
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
	PromptTTY          bool
	Stdout             io.Writer
	Stderr             io.Writer
	StdoutTTY          bool
	StderrTTY          bool
}

// Renderer writes terminal-aware output without inspecting global terminal state.
type Renderer struct {
	stdout    io.Writer
	stderr    io.Writer
	stdoutTTY bool
	stderrTTY bool
}

func NewRenderer(stdout, stderr io.Writer, stdoutTTY, stderrTTY bool) *Renderer {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &Renderer{
		stdout:    stdout,
		stderr:    stderr,
		stdoutTTY: stdoutTTY,
		stderrTTY: stderrTTY,
	}
}

// RenderHeader writes the search heading and safely encoded user input.
func (r *Renderer) RenderHeader(basePath, pattern string) error {
	writer := newCheckedWriter(r.stdout)
	fmt.Fprintf(writer, "%s\n", style(r.stdoutTTY, ColorBold+ColorHeader, "Enhanced File and Directory Finder"))
	fmt.Fprintf(writer, "%s %s\n", style(r.stdoutTTY, ColorOKBlue, "Searching in:"), safeText(basePath))
	fmt.Fprintf(writer, "%s %s\n", style(r.stdoutTTY, ColorOKBlue, "Pattern:"), safeText(pattern))
	return writer.result("render search header")
}

// RenderProgress writes a carriage-return progress line only for a TTY stream.
func (r *Renderer) RenderProgress(snapshot types.ProgressSnapshot) error {
	if !r.stdoutTTY {
		return nil
	}

	writer := newCheckedWriter(r.stdout)
	fmt.Fprintf(
		writer,
		"\r%s",
		style(true, ColorOKCyan, fmt.Sprintf(
			"Processed: %d | Found: %d files, %d dirs | Time: %.1fs",
			snapshot.ProcessedDirectories,
			snapshot.FoundFiles,
			snapshot.FoundDirectories,
			snapshot.Elapsed.Seconds(),
		)),
	)
	return writer.result("render progress")
}

// RenderResults writes result data to stdout and search notices to stderr.
func (r *Renderer) RenderResults(results types.SearchResults, options ResultsOutputOptions) error {
	totalResults := len(results.Files) + len(results.Directories)
	if err := r.renderResultsSummary(results, totalResults); err != nil {
		return err
	}
	if err := r.renderReport(results.Report); err != nil {
		return err
	}

	if totalResults <= 100 {
		return r.renderResultDetails(results.Files, results.Directories, options.ShowDetails, options.NoSort)
	}

	action := strings.ToLower(strings.TrimSpace(options.LargeResultsAction))
	if action == "" {
		action = LargeResultsActionAsk
	}
	if action == LargeResultsActionAsk {
		promptOutput := newCheckedWriter(promptWriter(options, r.stderr))
		var err error
		action, err = resolvePromptedLargeResultsAction(
			promptReader(options),
			promptOutput,
			options.PromptTTY,
		)
		if err != nil {
			return err
		}
		if err := promptOutput.result("render large result prompt"); err != nil {
			return err
		}
	}

	switch action {
	case LargeResultsActionDisplay:
		return r.renderResultDetails(results.Files, results.Directories, options.ShowDetails, options.NoSort)
	case LargeResultsActionSave:
		filename, err := saveSearchResultsToFile(
			results,
			options.Pattern,
			options.BasePath,
			options.ShowDetails,
			options.NoSort,
			options.OutputPath,
			defaultSaveDependencies(),
		)
		if err != nil {
			return fmt.Errorf("save results: %w", err)
		}

		writer := newCheckedWriter(r.stdout)
		fmt.Fprintf(writer, "%s %s\n", style(r.stdoutTTY, ColorOKCyan, "Results saved to:"), safeText(filename))
		return writer.result("render saved result path")
	default:
		return fmt.Errorf("unknown large results action %q", action)
	}
}

func (r *Renderer) renderResultsSummary(results types.SearchResults, totalResults int) error {
	writer := newCheckedWriter(r.stdout)
	fmt.Fprintf(writer, "\n%s\n", style(r.stdoutTTY, ColorBold+ColorHeader, "Search Results:"))
	fmt.Fprintf(writer, "%s %d\n", style(r.stdoutTTY, ColorOKGreen, "Files found:"), len(results.Files))
	fmt.Fprintf(writer, "%s %d\n", style(r.stdoutTTY, ColorOKBlue, "Directories found:"), len(results.Directories))

	totalLabel := fmt.Sprintf("%d", totalResults)
	if totalResults > 100 {
		totalLabel += " (exceeds 100)"
	}
	fmt.Fprintf(writer, "%s %s\n", style(r.stdoutTTY, ColorWarning, "Total results:"), totalLabel)
	fmt.Fprintf(writer, "%s %s\n", style(r.stdoutTTY, statusColor(results.Report), "Status:"), searchStatus(results.Report, totalResults))
	return writer.result("render result summary")
}

func (r *Renderer) renderResultDetails(files []types.FileResult, dirs []string, showDetails, noSort bool) error {
	files = append([]types.FileResult(nil), files...)
	dirs = append([]string(nil), dirs...)
	if !noSort {
		sortResults(files, dirs)
	}

	writer := newCheckedWriter(r.stdout)
	if len(files) > 0 {
		fmt.Fprintf(writer, "\n%s\n", style(r.stdoutTTY, ColorBold+ColorOKGreen, "Matching Files:"))
		for _, file := range files {
			if showDetails {
				fmt.Fprintf(writer, "  %s (%s)\n", safeText(file.Path), FormatSize(file.Size))
			} else {
				fmt.Fprintf(writer, "  %s\n", safeText(file.Path))
			}
		}
	}

	if len(dirs) > 0 {
		fmt.Fprintf(writer, "\n%s\n", style(r.stdoutTTY, ColorBold+ColorOKBlue, "Matching Directories:"))
		for _, dirPath := range dirs {
			fmt.Fprintf(writer, "  %s\n", safeText(dirPath))
		}
	}
	return writer.result("render result details")
}

func (r *Renderer) renderReport(report types.SearchReport) error {
	writer := newCheckedWriter(r.stderr)
	traversalCount := max(report.TraversalErrorCount, len(report.TraversalErrors))
	if report.Incomplete {
		if traversalCount == 0 {
			fmt.Fprintf(writer, "%s\n", style(r.stderrTTY, ColorWarning, "Warning: search incomplete."))
		} else {
			fmt.Fprintf(writer, "%s\n", style(r.stderrTTY, ColorWarning, fmt.Sprintf(
				"Warning: search incomplete after %d traversal error(s).",
				traversalCount,
			)))
		}
	}
	for _, issue := range report.TraversalErrors {
		fmt.Fprintf(writer, "  %s\n", formatIssue(issue))
	}
	if omitted := traversalCount - len(report.TraversalErrors); omitted > 0 {
		fmt.Fprintf(writer, "  ... %d additional traversal error(s) omitted\n", omitted)
	}

	skippedCount := max(report.SkippedSymlinkCount, len(report.SkippedSymlinks))
	if skippedCount > 0 {
		fmt.Fprintf(writer, "%s\n", style(r.stderrTTY, ColorWarning, fmt.Sprintf(
			"Notice: skipped %d directory or broken symlink(s).",
			skippedCount,
		)))
	}
	for _, issue := range report.SkippedSymlinks {
		fmt.Fprintf(writer, "  %s\n", formatIssue(issue))
	}
	if omitted := skippedCount - len(report.SkippedSymlinks); omitted > 0 {
		fmt.Fprintf(writer, "  ... %d additional skipped symlink(s) omitted\n", omitted)
	}
	return writer.result("render search report")
}

func formatIssue(issue types.PathIssue) string {
	operation := strings.TrimSpace(issue.Operation)
	if operation == "" {
		operation = "process"
	}
	message := "unknown error"
	if issue.Err != nil {
		message = issue.Err.Error()
	}
	return fmt.Sprintf("%s %s: %s", safeText(operation), safeText(issue.Path), safeText(message))
}

func searchStatus(report types.SearchReport, totalResults int) string {
	switch {
	case report.Incomplete && report.LimitReached:
		return "incomplete (result limit reached)"
	case report.Incomplete:
		return "incomplete"
	case report.LimitReached:
		return "limit reached"
	case totalResults == 0:
		return "complete (no matches)"
	default:
		return "complete"
	}
}

func statusColor(report types.SearchReport) string {
	if report.Incomplete || report.LimitReached {
		return ColorWarning
	}
	return ColorOKGreen
}

func style(enabled bool, codes, text string) string {
	if !enabled {
		return text
	}
	return codes + text + ColorEndC
}

func safeText(value string) string {
	if !utf8.ValidString(value) {
		return strconv.QuoteToASCII(value)
	}
	for _, char := range value {
		if unicode.IsControl(char) || char == '\u2028' || char == '\u2029' {
			return strconv.QuoteToGraphic(value)
		}
	}
	return value
}

// SafeText quotes strings that could inject terminal controls or forged lines.
func SafeText(value string) string {
	return safeText(value)
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

// PrintSearchHeader renders the heading using injected streams and TTY flags.
func PrintSearchHeader(basePath, pattern string, options ResultsOutputOptions) error {
	renderer := NewRenderer(stdoutWriter(options), stderrWriter(options), options.StdoutTTY, options.StderrTTY)
	return renderer.RenderHeader(basePath, pattern)
}

// PrintSearchResults renders the full finder result contract.
func PrintSearchResults(results types.SearchResults, options ResultsOutputOptions) error {
	renderer := NewRenderer(stdoutWriter(options), stderrWriter(options), options.StdoutTTY, options.StderrTTY)
	return renderer.RenderResults(results, options)
}

func stdoutWriter(options ResultsOutputOptions) io.Writer {
	if options.Stdout != nil {
		return options.Stdout
	}
	return os.Stdout
}

func stderrWriter(options ResultsOutputOptions) io.Writer {
	if options.Stderr != nil {
		return options.Stderr
	}
	return os.Stderr
}

func promptReader(options ResultsOutputOptions) io.Reader {
	if options.PromptReader != nil {
		return options.PromptReader
	}
	return os.Stdin
}

func promptWriter(options ResultsOutputOptions, fallback io.Writer) io.Writer {
	if options.PromptWriter != nil {
		return options.PromptWriter
	}
	return fallback
}

func resolvePromptedLargeResultsAction(reader io.Reader, writer io.Writer, interactive bool) (string, error) {
	if !interactive {
		fmt.Fprintln(writer, "Non-interactive terminal detected; saving results to file.")
		return LargeResultsActionSave, nil
	}
	return promptLargeResultsAction(reader, writer)
}

func promptLargeResultsAction(reader io.Reader, writer io.Writer) (string, error) {
	scanner := bufio.NewScanner(reader)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprint(writer, "Choose output: enter [s] to save to file or [d] to display in terminal: ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", fmt.Errorf("read output choice: %w", err)
			}
			return LargeResultsActionSave, nil
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))

		switch answer {
		case "", "s", "save":
			fmt.Fprintln(writer)
			return LargeResultsActionSave, nil
		case "d", "display":
			fmt.Fprintln(writer)
			return LargeResultsActionDisplay, nil
		default:
			fmt.Fprintln(writer, "\nInvalid choice. Please enter s or d.")
		}
	}
	return LargeResultsActionSave, nil
}

func sortResults(files []types.FileResult, dirs []string) {
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	}()
	go func() {
		defer waitGroup.Done()
		sort.Strings(dirs)
	}()
	waitGroup.Wait()
}

// SaveResultsToFile atomically writes search results to an explicit or generated path.
func SaveResultsToFile(files []types.FileResult, dirs []string, pattern, basePath string, showDetails, noSort bool, outputPath string) (string, error) {
	return saveSearchResultsToFile(
		types.SearchResults{Files: files, Directories: dirs},
		pattern,
		basePath,
		showDetails,
		noSort,
		outputPath,
		defaultSaveDependencies(),
	)
}

type outputFile interface {
	io.Writer
	io.Closer
	Chmod(os.FileMode) error
	Name() string
}

type destinationState struct {
	exists bool
	mode   os.FileMode
}

type saveDependencies struct {
	now        func() time.Time
	random     io.Reader
	lstat      func(string) (os.FileInfo, error)
	createTemp func(string, string) (outputFile, error)
	rename     func(string, string) error
	remove     func(string) error
}

func defaultSaveDependencies() saveDependencies {
	return saveDependencies{
		now:    time.Now,
		random: rand.Reader,
		lstat:  os.Lstat,
		createTemp: func(directory, pattern string) (outputFile, error) {
			return os.CreateTemp(directory, pattern)
		},
		rename: os.Rename,
		remove: os.Remove,
	}
}

func saveSearchResultsToFile(results types.SearchResults, pattern, basePath string, showDetails, noSort bool, outputPath string, dependencies saveDependencies) (string, error) {
	filename, err := resolveOutputPath(outputPath, dependencies)
	if err != nil {
		return "", err
	}
	destination, err := validateDestination(filename, dependencies.lstat)
	if err != nil {
		return "", err
	}

	directory := filepath.Dir(filename)
	temp, err := dependencies.createTemp(directory, "."+filepath.Base(filename)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temporary output: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = dependencies.remove(tempPath)
		}
	}()
	if destination.exists {
		if err := temp.Chmod(destination.mode.Perm()); err != nil {
			_ = temp.Close()
			return "", fmt.Errorf("preserve output permissions: %w", err)
		}
	}

	buffered := bufio.NewWriter(temp)
	if err := writeResultsFile(buffered, results, pattern, basePath, showDetails, noSort, dependencies.now()); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("write temporary output: %w", err)
	}
	if err := buffered.Flush(); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("flush temporary output: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close temporary output: %w", err)
	}
	if err := dependencies.rename(tempPath, filename); err != nil {
		return "", fmt.Errorf("replace output destination: %w", err)
	}

	cleanup = false
	return filename, nil
}

func resolveOutputPath(outputPath string, dependencies saveDependencies) (string, error) {
	if outputPath != "" {
		return outputPath, nil
	}

	for attempt := 0; attempt < 10; attempt++ {
		suffix := make([]byte, 16)
		if _, err := io.ReadFull(dependencies.random, suffix); err != nil {
			return "", fmt.Errorf("generate output filename: %w", err)
		}
		candidate := fmt.Sprintf(
			"search_results_%s_%s.txt",
			dependencies.now().Format("20060102_150405"),
			hex.EncodeToString(suffix),
		)
		_, err := dependencies.lstat(candidate)
		switch {
		case os.IsNotExist(err):
			return candidate, nil
		case err != nil:
			return "", fmt.Errorf("inspect generated output path %q: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("generate unique output filename after 10 attempts")
}

func validateDestination(filename string, lstat func(string) (os.FileInfo, error)) (destinationState, error) {
	info, err := lstat(filename)
	if os.IsNotExist(err) {
		return destinationState{}, nil
	}
	if err != nil {
		return destinationState{}, fmt.Errorf("inspect output destination %q: %w", filename, err)
	}
	if info.IsDir() {
		return destinationState{}, fmt.Errorf("output destination %q is a directory", filename)
	}
	if !info.Mode().IsRegular() {
		return destinationState{}, fmt.Errorf("output destination %q is not a regular file", filename)
	}
	return destinationState{exists: true, mode: info.Mode()}, nil
}

func writeResultsFile(writer io.Writer, results types.SearchResults, pattern, basePath string, showDetails, noSort bool, now time.Time) error {
	files := append([]types.FileResult(nil), results.Files...)
	dirs := append([]string(nil), results.Directories...)
	if !noSort {
		sortResults(files, dirs)
	}

	checked := newCheckedWriter(writer)
	fmt.Fprintln(checked, "Enhanced File and Directory Finder Results")
	fmt.Fprintln(checked, strings.Repeat("=", 80))
	fmt.Fprintf(checked, "Search Date: %s\n", now.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(checked, "Base Path: %s\n", safeText(basePath))
	fmt.Fprintf(checked, "Search Pattern: %s\n", safeText(pattern))
	fmt.Fprintf(checked, "Files found: %d\n", len(files))
	fmt.Fprintf(checked, "Directories found: %d\n", len(dirs))
	fmt.Fprintf(checked, "Total results: %d\n", len(files)+len(dirs))
	fmt.Fprintf(checked, "Status: %s\n", searchStatus(results.Report, len(files)+len(dirs)))
	fmt.Fprintf(checked, "%s\n\n", strings.Repeat("=", 80))

	if len(files) > 0 {
		fmt.Fprintln(checked, "MATCHING FILES:")
		fmt.Fprintln(checked, strings.Repeat("-", 40))
		for _, file := range files {
			if showDetails {
				fmt.Fprintf(checked, "  %s (%s)\n", safeText(file.Path), FormatSize(file.Size))
			} else {
				fmt.Fprintf(checked, "  %s\n", safeText(file.Path))
			}
		}
		fmt.Fprintln(checked)
	}

	if len(dirs) > 0 {
		fmt.Fprintln(checked, "MATCHING DIRECTORIES:")
		fmt.Fprintln(checked, strings.Repeat("-", 40))
		for _, dirPath := range dirs {
			fmt.Fprintf(checked, "  %s\n", safeText(dirPath))
		}
		fmt.Fprintln(checked)
	}
	return checked.result("write output content")
}

type checkedWriter struct {
	writer io.Writer
	err    error
}

func newCheckedWriter(writer io.Writer) *checkedWriter {
	return &checkedWriter{writer: writer}
}

func (w *checkedWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	written, err := w.writer.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return written, err
}

func (w *checkedWriter) result(operation string) error {
	if w.err != nil {
		return fmt.Errorf("%s: %w", operation, w.err)
	}
	return nil
}
