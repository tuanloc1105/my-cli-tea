package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

const (
	// Only check the first 8KB to determine if a file is binary.
	binaryCheckSize = 8192
	// Default max file size: 512MB. Files larger than this are skipped.
	defaultMaxFileSize int64 = 512 * 1024 * 1024
)

// processFile checks if a file is text and performs the replacement.
func processFile(filename string, oldText, newText []byte, createBackup bool, maxFileSize int64) error {
	// Stat to get permission and size
	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if info.Size() > maxFileSize {
		return errNoChange
	}

	// Read the entire file content
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Check if it's a valid UTF-8 text file (only check first N bytes).
	// Trim back to the last valid rune boundary to avoid cutting multi-byte characters.
	checkLen := len(content)
	if checkLen > binaryCheckSize {
		checkLen = binaryCheckSize
		for checkLen > 0 && !utf8.RuneStart(content[checkLen-1]) {
			checkLen--
		}
		if checkLen > 0 {
			checkLen-- // drop the potentially incomplete leading byte
		}
	}
	if checkLen == 0 || !utf8.Valid(content[:checkLen]) {
		return errNoChange
	}

	// If oldText is not in the file, there is nothing to do
	if !bytes.Contains(content, oldText) {
		return errNoChange
	}

	perm := info.Mode().Perm()

	var backupFilename string
	if createBackup {
		backupFilename = filename + ".bak"
		os.Remove(backupFilename)
		if err := os.Rename(filename, backupFilename); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	newContent := bytes.ReplaceAll(content, oldText, newText)

	// Atomic write: write to temp file then rename
	dir := filepath.Dir(filename)
	tmp, err := os.CreateTemp(dir, ".replace-text-*.tmp")
	if err != nil {
		if createBackup {
			os.Rename(backupFilename, filename)
		}
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(newContent); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		if createBackup {
			os.Rename(backupFilename, filename)
		}
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		if createBackup {
			os.Rename(backupFilename, filename)
		}
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Preserve original file permissions
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		if createBackup {
			os.Rename(backupFilename, filename)
		}
		return fmt.Errorf("failed to set file permissions: %w", err)
	}

	// Atomically replace the original file
	if err := os.Rename(tmpName, filename); err != nil {
		os.Remove(tmpName)
		if createBackup {
			os.Rename(backupFilename, filename)
		}
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	fmt.Printf("Successfully replaced text in '%s'.\n", filename)
	return nil
}

// errNoChange is a sentinel error indicating the file was not modified.
var errNoChange = fmt.Errorf("no change")

// findAndReplace finds and replaces all occurrences of oldText with newText.
func findAndReplace(path string, oldText, newText []byte, createBackup bool, maxFileSize int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("path '%s' not found or is not a valid file/directory: %w", path, err)
	}

	if !info.IsDir() {
		err := processFile(path, oldText, newText, createBackup, maxFileSize)
		if err == errNoChange {
			return nil
		}
		if err != nil {
			return err
		}
		if createBackup {
			fmt.Printf("Backup file created at '%s.bak'.\n", path)
		}
		return nil
	}

	fmt.Printf("Processing directory: %s\n", path)

	// Collect file paths first, then process in parallel
	var files []string
	err = filepath.WalkDir(path, func(walkPath string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				fmt.Fprintf(os.Stderr, "Warning: Skipping directory '%s' due to error: %v\n", walkPath, err)
				return filepath.SkipDir
			}
			fmt.Fprintf(os.Stderr, "Warning: Skipping file '%s' due to error: %v\n", walkPath, err)
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".svn" || name == ".hg" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip .bak files to avoid processing backups
		if strings.HasSuffix(d.Name(), ".bak") {
			return nil
		}

		files = append(files, walkPath)
		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking directory: %w", err)
	}

	// Process files in parallel using a worker pool
	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}
	if len(files) < numWorkers {
		numWorkers = len(files)
	}

	var errCount atomic.Int64
	fileCh := make(chan string, numWorkers)
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range fileCh {
				if err := processFile(f, oldText, newText, createBackup, maxFileSize); err != nil && err != errNoChange {
					fmt.Fprintf(os.Stderr, "Error processing '%s': %v\n", f, err)
					errCount.Add(1)
				}
			}
		}()
	}

	for _, f := range files {
		fileCh <- f
	}
	close(fileCh)
	wg.Wait()

	fmt.Printf("\nFinished processing directory '%s'.\n", path)
	if errCount.Load() > 0 {
		fmt.Fprintf(os.Stderr, "%d file(s) had errors during processing.\n", errCount.Load())
	}
	if createBackup {
		fmt.Println("Backup files (.bak) were created for all modified files.")
	}

	return nil
}

// unescapeString converts escaped sequences like \n to actual characters.
// Processes character-by-character to handle \\ correctly.
func unescapeString(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
			case 't':
				b.WriteByte('\t')
				i++
			case 'r':
				b.WriteByte('\r')
				i++
			case '\\':
				b.WriteByte('\\')
				i++
			default:
				b.WriteByte(s[i])
			}
		} else {
			b.WriteByte(s[i])
		}
	}

	return b.String()
}

func main() {
	var createBackup bool
	var maxFileSize int64

	var rootCmd = &cobra.Command{
		Use:   "replace-text [old-text] [new-text] [file-or-directory-path]",
		Short: "Find and replace text in files or directories",
		Long: `A tool to find and replace text in files or directories.
Supports both single files and recursive directory processing.
Optionally creates backup files (.bak) for all modified files with --backup flag.

Examples:
  replace-text 'hello' 'goodbye' /path/to/file.txt
  replace-text 'hello' 'goodbye' /path/to/your_folder
  replace-text 'hello' 'goodbye' /path/to/file.txt --backup
  replace-text '\\n' '\\r\\n' /path/to/file.txt  # Replace newlines with CRLF`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldText := []byte(unescapeString(args[0]))
			newText := []byte(unescapeString(args[1]))
			path := args[2]

			return findAndReplace(path, oldText, newText, createBackup, maxFileSize)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	rootCmd.Flags().BoolVar(&createBackup, "backup", false, "Create backup files (.bak) before replacing")
	rootCmd.Flags().Int64Var(&maxFileSize, "max-size", defaultMaxFileSize, "Max file size in bytes to process (default 512MB)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
