package finder

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"find-everything/internal/types"
	"find-everything/internal/ui"
)

var pathSep = string(os.PathSeparator)

func (ff *FileFinder) FindFilesAndDirs() ([]types.FileResult, []string) {
	defer ff.cancel()

	if ff.showProgress {
		fmt.Printf("%sStarting search...%s\n", ui.ColorOKBlue, ui.ColorEndC)
	}

	// Start progress updater goroutine
	var progressTicker *time.Ticker
	if ff.showProgress {
		progressTicker = time.NewTicker(100 * time.Millisecond)
		defer progressTicker.Stop()
		go func() {
			for {
				select {
				case <-progressTicker.C:
					ff.progressTracker.PrintProgress()
				case <-ff.ctx.Done():
					return
				}
			}
		}()
	}

	var matchedFiles []types.FileResult
	var matchedDirs []string
	var resultsMu sync.Mutex

	// Use a channel for directories to process
	dirQueue := make(chan string, 10000)

	// WaitGroup to track active tasks (files/dirs being processed)
	var processingWg sync.WaitGroup
	var workerWg sync.WaitGroup

	// Atomic counters
	var totalDirs int64
	var skippedDirs int64

	hasExcludePatterns := len(ff.excludePatterns) > 0
	hasSizeFilter := ff.minSize > 0 || ff.maxSize < (1<<63-1)

	// Start workers
	for i := 0; i < ff.maxWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()

			localFiles := make([]types.FileResult, 0, 100)
			localDirs := make([]string, 0, 100)

			// Helper to flush local results
			flush := func() {
				if len(localFiles) > 0 || len(localDirs) > 0 {
					resultsMu.Lock()
					matchedFiles = append(matchedFiles, localFiles...)
					matchedDirs = append(matchedDirs, localDirs...)
					newCount := len(matchedFiles) + len(matchedDirs)
					resultsMu.Unlock()

					// Check max results limit
					if newCount >= ff.maxResults {
						ff.cancel()
					}

					localFiles = localFiles[:0]
					localDirs = localDirs[:0]
				}
			}

			// Ensure final flush
			defer flush()

			for path := range dirQueue {
				processDir(ff, path, dirQueue, &processingWg, &localFiles, &localDirs, &totalDirs, &skippedDirs, hasExcludePatterns, hasSizeFilter)

				// Flush periodically
				if len(localFiles)+len(localDirs) > 100 {
					flush()
				}

				// Task done
				processingWg.Done()
			}
		}()
	}

	// Initial seed
	atomic.AddInt64(&totalDirs, 1)
	ff.progressTracker.SetTotalDirs(1)
	processingWg.Add(1)
	dirQueue <- ff.basePath

	// Monitor completion
	go func() {
		processingWg.Wait()
		close(dirQueue)
	}()

	// Wait for all workers to finish
	workerWg.Wait()

	if ff.showProgress {
		fmt.Println() // New line after progress
	}

	if skipped := atomic.LoadInt64(&skippedDirs); skipped > 0 {
		fmt.Printf("%sWarning: %d directories could not be read (permission denied or other errors)%s\n",
			ui.ColorWarning, skipped, ui.ColorEndC)
	}

	return matchedFiles, matchedDirs
}

func processDir(ff *FileFinder, path string, dirQueue chan string, wg *sync.WaitGroup, localFiles *[]types.FileResult, localDirs *[]string, totalDirs *int64, skippedDirs *int64, hasExcludePatterns bool, hasSizeFilter bool) {
	entries, err := os.ReadDir(path)
	if err != nil {
		atomic.AddInt64(skippedDirs, 1)
		return
	}

	ff.progressTracker.UpdateProcessedDirs(1)

	var newDirCount int64

	for _, entry := range entries {
		entryName := entry.Name()
		isDir := entry.IsDir()

		// Exclude dirs: fast map lookup on entry name only
		if isDir {
			if ff.ShouldExcludeDir(entryName) {
				continue
			}
		}

		// Phase 3a: Avoid filepath.Join — direct string concat
		fullPath := path + pathSep + entryName

		// Exclude patterns (regex): applies to both files and directories
		if hasExcludePatterns {
			if ff.ShouldExcludeByPattern(fullPath) {
				continue
			}
		}

		// Check for match
		if ff.MatchesPattern(entryName) {
			if isDir {
				*localDirs = append(*localDirs, fullPath)
				ff.progressTracker.Update(0, 1)
			} else {
				shouldAdd := true

				// Phase 3c: CheckFileType uses entryName instead of fullPath
				if !ff.CheckFileType(entryName) {
					shouldAdd = false
				} else if hasSizeFilter {
					size, ok := ff.CheckFileSize(entry, fullPath)
					if !ok {
						shouldAdd = false
					} else if shouldAdd {
						*localFiles = append(*localFiles, types.FileResult{Path: fullPath, Size: size})
						ff.progressTracker.Update(1, 0)
						shouldAdd = false // already added
					}
				}

				if shouldAdd {
					// No size filter — get size for display
					size, _ := ff.GetFileSizeFromEntry(entry, fullPath)
					*localFiles = append(*localFiles, types.FileResult{Path: fullPath, Size: size})
					ff.progressTracker.Update(1, 0)
				}
			}
		}

		// If directory, queue for traversal
		if isDir {
			select {
			case <-ff.ctx.Done():
				return
			default:
				newDirCount++

				wg.Add(1)

				// Non-blocking send to prevent deadlock
				select {
				case dirQueue <- fullPath:
				default:
					go func(p string) {
						dirQueue <- p
					}(fullPath)
				}
			}
		}
	}

	// Phase 4a: Batch update progress counter
	if newDirCount > 0 {
		newTotal := atomic.AddInt64(totalDirs, newDirCount)
		ff.progressTracker.SetTotalDirs(int(newTotal))
	}
}
