package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/term"
)

// walkTask is a unit of work for the parallel walker.
// Each directory discovered during the walk becomes a task.
type walkTask struct {
	dirPath      string // absolute path of the directory to read
	topLevelName string // which top-level entry this size counts toward
	currentDepth int    // depth relative to the top-level entry (for maxDepth)
}

type ScanOptions struct {
	ShowProgress bool
	ExcludeList  []string
	Ctx          context.Context
	MaxDepth     int // 0 = unlimited
}

type ItemInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

type ScanResult struct {
	Items        []ItemInfo
	WarningCount int64
}

type parallelWalker struct {
	excludeMap map[string]struct{}
	ctx        context.Context
	maxDepth   int
	numWorkers int

	taskCh   chan walkTask
	sizes    map[string]*int64 // topLevelName -> atomic size accumulator
	taskWg   sync.WaitGroup    // tracks outstanding tasks (not goroutines)
	workerWg sync.WaitGroup    // tracks worker goroutines

	warningCount int64 // atomic

	// Progress tracking
	showProgress      bool
	termWidth         int
	totalTopLevel     int
	completedTopLevel int64             // atomic
	pendingTasks      map[string]*int64 // atomic per-top-level task counters
	progressMu        sync.Mutex
}

// getTerminalWidth returns the width of the terminal
func getTerminalWidth() int {
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		return width
	}
	return 80
}

func newParallelWalker(excludeMap map[string]struct{}, opts ScanOptions, numWorkers, topLevelDirCount int) *parallelWalker {
	bufSize := numWorkers * 4
	if bufSize < 64 {
		bufSize = 64
	}

	pw := &parallelWalker{
		excludeMap:    excludeMap,
		ctx:           opts.Ctx,
		maxDepth:      opts.MaxDepth,
		numWorkers:    numWorkers,
		taskCh:        make(chan walkTask, bufSize),
		sizes:         make(map[string]*int64, topLevelDirCount),
		showProgress:  opts.ShowProgress,
		totalTopLevel: topLevelDirCount,
		pendingTasks:  make(map[string]*int64, topLevelDirCount),
	}

	if opts.ShowProgress {
		pw.termWidth = getTerminalWidth()
	}

	return pw
}

// processDirectory reads one directory level and enqueues child directories as new tasks.
func (pw *parallelWalker) processDirectory(task walkTask) {
	if pw.ctx.Err() != nil {
		return
	}

	entries, err := os.ReadDir(task.dirPath)
	if err != nil {
		atomic.AddInt64(&pw.warningCount, 1)
		return
	}

	sizePtr := pw.sizes[task.topLevelName]

	for _, entry := range entries {
		// Exclusion check first: O(1) map lookup, skip entire subtrees early
		if _, excluded := pw.excludeMap[entry.Name()]; excluded {
			continue
		}

		// Skip symlinks to avoid loops
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			// Depth limit check
			if pw.maxDepth > 0 && task.currentDepth+1 > pw.maxDepth {
				continue
			}

			childTask := walkTask{
				dirPath:      filepath.Join(task.dirPath, entry.Name()),
				topLevelName: task.topLevelName,
				currentDepth: task.currentDepth + 1,
			}

			pw.taskWg.Add(1)
			if pw.showProgress {
				atomic.AddInt64(pw.pendingTasks[task.topLevelName], 1)
			}
			pw.enqueueOrProcess(childTask)
		} else {
			info, err := entry.Info()
			if err != nil {
				atomic.AddInt64(&pw.warningCount, 1)
				continue
			}
			atomic.AddInt64(sizePtr, info.Size())
		}
	}
}

// enqueueOrProcess tries to send the task to the channel.
// If the channel is full, it processes inline to avoid deadlock.
// The inline fallback may recurse if child directories also can't be enqueued,
// but recursion depth is bounded by directory tree depth. On Linux (PATH_MAX=4096),
// this means at most ~2048 levels deep, using ~1MB of stack — well within Go's
// goroutine stack limit (1GB default).
func (pw *parallelWalker) enqueueOrProcess(task walkTask) {
	select {
	case pw.taskCh <- task:
		// Offloaded to another worker
	default:
		// Channel full — process inline, then mark complete
		pw.processDirectory(task)
		pw.completeTask(task)
	}
}

// completeTask decrements the task counter and updates progress when a top-level entry finishes.
func (pw *parallelWalker) completeTask(task walkTask) {
	pw.taskWg.Done()

	if pw.showProgress {
		remaining := atomic.AddInt64(pw.pendingTasks[task.topLevelName], -1)
		if remaining == 0 && pw.ctx.Err() == nil {
			count := atomic.AddInt64(&pw.completedTopLevel, 1)
			progressMsg := fmt.Sprintf("Processing %d/%d: %s", count, pw.totalTopLevel, task.topLevelName)

			runes := []rune(progressMsg)
			if len(runes) > pw.termWidth-1 {
				progressMsg = string(runes[:pw.termWidth-4]) + "..."
			}

			paddedMsg := fmt.Sprintf("%-*s", pw.termWidth-1, progressMsg)
			pw.progressMu.Lock()
			fmt.Printf("\r%s", paddedMsg)
			pw.progressMu.Unlock()
		}
	}
}

// run starts workers, enqueues initial tasks, and blocks until all work is done.
func (pw *parallelWalker) run(initialTasks []walkTask) {
	// Pre-register all initial tasks in WaitGroup and pending counters
	// BEFORE starting workers, so taskWg.Wait() can't return prematurely.
	pw.taskWg.Add(len(initialTasks))
	if pw.showProgress {
		for _, task := range initialTasks {
			atomic.AddInt64(pw.pendingTasks[task.topLevelName], 1)
		}
	}

	// Start workers (they immediately begin consuming from taskCh)
	for i := 0; i < pw.numWorkers; i++ {
		pw.workerWg.Add(1)
		go func() {
			defer pw.workerWg.Done()
			for task := range pw.taskCh {
				if pw.ctx.Err() != nil {
					// Drain without processing — still decrement counters
					pw.completeTask(task)
					continue
				}
				pw.processDirectory(task)
				pw.completeTask(task)
			}
		}()
	}

	// Enqueue initial tasks in a goroutine (may block if buffer fills,
	// but workers are already running and consuming, so no deadlock)
	go func() {
		for _, task := range initialTasks {
			pw.taskCh <- task
		}
	}()

	// Closer goroutine: when all tasks are done, close the channel
	go func() {
		pw.taskWg.Wait()
		close(pw.taskCh)
	}()

	// Block until all workers exit
	pw.workerWg.Wait()
}

// GetSizesOfSubfolders calculates sizes of immediate subfolders/files
func GetSizesOfSubfolders(parentFolder string, opts ScanOptions) ScanResult {
	var items []ItemInfo

	entries, err := os.ReadDir(parentFolder)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", parentFolder, err)
		return ScanResult{Items: items, WarningCount: 1}
	}

	// Build exclude map for O(1) lookup
	excludeMap := make(map[string]struct{})
	for _, item := range opts.ExcludeList {
		excludeMap[item] = struct{}{}
	}

	// Separate top-level files (stat directly) and directories (parallel walk)
	var initialTasks []walkTask
	var fileWarnings int64

	for _, entry := range entries {
		if _, excluded := excludeMap[entry.Name()]; excluded {
			continue
		}

		fullPath := filepath.Join(parentFolder, entry.Name())

		if entry.IsDir() {
			initialTasks = append(initialTasks, walkTask{
				dirPath:      fullPath,
				topLevelName: entry.Name(),
				currentDepth: 0,
			})
		} else {
			if info, err := os.Stat(fullPath); err == nil {
				name := entry.Name()
				items = append(items, ItemInfo{Name: name, Size: info.Size(), Type: "file"})
			} else {
				fileWarnings++
			}
		}
	}

	if len(initialTasks) == 0 {
		return ScanResult{Items: items, WarningCount: fileWarnings}
	}

	// Create parallel walker — NumCPU workers regardless of top-level count,
	// because subdirectories become tasks that benefit from more workers.
	numWorkers := runtime.NumCPU()
	pw := newParallelWalker(excludeMap, opts, numWorkers, len(initialTasks))

	// Allocate atomic size accumulators for each top-level directory
	for _, task := range initialTasks {
		size := int64(0)
		pw.sizes[task.topLevelName] = &size
		if opts.ShowProgress {
			pending := int64(0)
			pw.pendingTasks[task.topLevelName] = &pending
		}
	}

	// Run the parallel walker (blocks until complete)
	pw.run(initialTasks)

	// Collect directory sizes into result
	for name, sizePtr := range pw.sizes {
		items = append(items, ItemInfo{Name: name, Size: atomic.LoadInt64(sizePtr), Type: "directory"})
	}

	if opts.ShowProgress {
		fmt.Println()
	}

	totalWarnings := fileWarnings + atomic.LoadInt64(&pw.warningCount)

	if opts.Ctx.Err() != nil {
		fmt.Fprintf(os.Stderr, "\nScan cancelled: %v (partial results returned)\n", opts.Ctx.Err())
	}

	return ScanResult{
		Items:        items,
		WarningCount: totalWarnings,
	}
}
