package finder

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"find-everything/internal/types"
)

const defaultQueueCapacity = 10000

var (
	queueCapacity = defaultQueueCapacity
	readDir       = os.ReadDir

	errResultLimit       = errors.New("result limit reached")
	errDirectorySymlink  = errors.New("directory symlink is not followed")
	errUnsupportedTarget = errors.New("symlink target is not a regular file")
)

type directoryTask struct {
	path    string
	entries []os.DirEntry
	readErr error
	loaded  bool
}

type progressCounters struct {
	totalDirectories     atomic.Int64
	processedDirectories atomic.Int64
	foundFiles           atomic.Int64
	foundDirectories     atomic.Int64
}

func (c *progressCounters) snapshot(started time.Time) types.ProgressSnapshot {
	return types.ProgressSnapshot{
		TotalDirectories:     c.totalDirectories.Load(),
		ProcessedDirectories: c.processedDirectories.Load(),
		FoundFiles:           c.foundFiles.Load(),
		FoundDirectories:     c.foundDirectories.Load(),
		Elapsed:              time.Since(started),
	}
}

type resultCollector struct {
	mu       sync.Mutex
	results  types.SearchResults
	reserved atomic.Int64
	limit    int64
	cancel   context.CancelCauseFunc
	progress *progressCounters
}

func (c *resultCollector) addFile(result types.FileResult) bool {
	if !c.reserve() {
		return false
	}
	c.mu.Lock()
	c.results.Files = append(c.results.Files, result)
	c.mu.Unlock()
	c.progress.foundFiles.Add(1)
	return true
}

func (c *resultCollector) addDirectory(path string) bool {
	if !c.reserve() {
		return false
	}
	c.mu.Lock()
	c.results.Directories = append(c.results.Directories, path)
	c.mu.Unlock()
	c.progress.foundDirectories.Add(1)
	return true
}

func (c *resultCollector) reserve() bool {
	for {
		current := c.reserved.Load()
		if current >= c.limit {
			return false
		}
		if !c.reserved.CompareAndSwap(current, current+1) {
			continue
		}
		if current+1 == c.limit {
			c.mu.Lock()
			c.results.Report.LimitReached = true
			c.mu.Unlock()
			c.cancel(errResultLimit)
		}
		return true
	}
}

func (c *resultCollector) addTraversalError(path, operation string, err error) {
	c.mu.Lock()
	c.results.Report.AddTraversalError(types.PathIssue{
		Path:      path,
		Operation: operation,
		Err:       err,
	})
	c.mu.Unlock()
}

func (c *resultCollector) addSkippedSymlink(path, operation string, err error) {
	c.mu.Lock()
	c.results.Report.AddSkippedSymlink(types.PathIssue{
		Path:      path,
		Operation: operation,
		Err:       err,
	})
	c.mu.Unlock()
}

func (c *resultCollector) finalResults() types.SearchResults {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.results
}

func (ff *FileFinder) FindFilesAndDirs(ctx context.Context) (types.SearchResults, error) {
	if ctx == nil {
		return types.SearchResults{}, fmt.Errorf("search context is required")
	}
	if err := ctx.Err(); err != nil {
		return types.SearchResults{}, err
	}
	if queueCapacity <= 0 {
		return types.SearchResults{}, fmt.Errorf("directory queue capacity must be greater than zero")
	}

	baseEntries, err := ff.validateBasePath(ctx)
	if err != nil {
		return types.SearchResults{}, err
	}
	if err := ctx.Err(); err != nil {
		return types.SearchResults{}, err
	}

	scanCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	started := time.Now()
	progress := &progressCounters{}
	progress.totalDirectories.Store(1)
	stopProgress := startProgress(ff.progress, progress, started)

	collector := &resultCollector{
		limit:    int64(ff.maxResults),
		cancel:   cancel,
		progress: progress,
	}

	dirQueue := make(chan directoryTask, queueCapacity)
	var pending sync.WaitGroup
	var workers sync.WaitGroup

	pending.Add(1)
	dirQueue <- directoryTask{
		path:    ff.basePath,
		entries: baseEntries,
		loaded:  true,
	}

	for range ff.maxWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range dirQueue {
				if scanCtx.Err() == nil {
					ff.processQueuedTask(scanCtx, task, dirQueue, &pending, collector, progress)
				}
				pending.Done()
			}
		}()
	}

	queueClosed := make(chan struct{})
	go func() {
		pending.Wait()
		close(dirQueue)
		close(queueClosed)
	}()

	workers.Wait()
	<-queueClosed
	stopProgress()

	results := collector.finalResults()
	if err := ctx.Err(); err != nil {
		return results, err
	}
	if cause := context.Cause(scanCtx); cause != nil && !errors.Is(cause, errResultLimit) {
		return results, scanCtx.Err()
	}
	return results, nil
}

func (ff *FileFinder) validateBasePath(ctx context.Context) ([]os.DirEntry, error) {
	info, err := os.Stat(ff.basePath)
	if err != nil {
		return nil, fmt.Errorf("inspect base path %q: %w", ff.basePath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("base path %q is not a directory", ff.basePath)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := readDir(ff.basePath)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		return nil, fmt.Errorf("read base path %q: %w", ff.basePath, err)
	}
	return entries, nil
}

func startProgress(callback types.ProgressFunc, counters *progressCounters, started time.Time) func() {
	if callback == nil {
		return func() {}
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				callback(counters.snapshot(started))
			case <-stop:
				return
			}
		}
	}()

	return func() {
		close(stop)
		<-done
		callback(counters.snapshot(started))
	}
}

func (ff *FileFinder) processQueuedTask(
	ctx context.Context,
	root directoryTask,
	dirQueue chan<- directoryTask,
	pending *sync.WaitGroup,
	collector *resultCollector,
	progress *progressCounters,
) {
	stack := []directoryTask{root}
	for len(stack) > 0 {
		if ctx.Err() != nil {
			return
		}

		last := len(stack) - 1
		task := stack[last]
		stack = stack[:last]

		children := ff.processDirectory(ctx, task, collector, progress)
		for _, child := range children {
			if ctx.Err() != nil {
				return
			}
			progress.totalDirectories.Add(1)

			pending.Add(1)
			select {
			case dirQueue <- child:
			default:
				pending.Done()
				stack = append(stack, child)
			}
		}
	}
}

func (ff *FileFinder) processDirectory(
	ctx context.Context,
	task directoryTask,
	collector *resultCollector,
	progress *progressCounters,
) []directoryTask {
	if ctx.Err() != nil {
		return nil
	}

	entries := task.entries
	readErr := task.readErr
	if !task.loaded {
		entries, readErr = readDir(task.path)
	}
	progress.processedDirectories.Add(1)

	children := make([]directoryTask, 0)
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}

		entryName := entry.Name()
		fullPath := filepath.Join(task.path, entryName)
		isDirectory := entry.IsDir()

		if isDirectory && ff.ShouldExcludeDir(entryName) {
			continue
		}
		if ff.ShouldExcludeByPattern(fullPath) {
			continue
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			ff.processSymlink(entryName, fullPath, collector)
			continue
		}

		if ff.MatchesPattern(entryName) {
			if isDirectory {
				collector.addDirectory(fullPath)
			} else {
				ff.processFile(entry, entryName, fullPath, collector)
			}
		}

		if isDirectory && ctx.Err() == nil {
			children = append(children, directoryTask{path: fullPath})
		}
	}

	if readErr != nil {
		collector.addTraversalError(task.path, "read directory", readErr)
	}
	return children
}

func (ff *FileFinder) processFile(entry fs.DirEntry, entryName, fullPath string, collector *resultCollector) {
	if !ff.CheckFileType(entryName) {
		return
	}
	info, err := entry.Info()
	if err != nil {
		collector.addTraversalError(fullPath, "stat file", err)
		return
	}
	if !ff.matchesFileSize(info.Size()) {
		return
	}
	collector.addFile(types.FileResult{Path: fullPath, Size: info.Size()})
}

func (ff *FileFinder) processSymlink(entryName, fullPath string, collector *resultCollector) {
	info, err := os.Stat(fullPath)
	if err != nil {
		collector.addSkippedSymlink(fullPath, "stat symlink target", err)
		return
	}
	if info.IsDir() {
		collector.addSkippedSymlink(fullPath, "skip directory symlink", errDirectorySymlink)
		return
	}
	if !info.Mode().IsRegular() {
		collector.addSkippedSymlink(fullPath, "skip unsupported symlink", errUnsupportedTarget)
		return
	}
	if !ff.MatchesPattern(entryName) || !ff.CheckFileType(entryName) {
		return
	}
	if !ff.matchesFileSize(info.Size()) {
		return
	}
	collector.addFile(types.FileResult{Path: fullPath, Size: info.Size()})
}
