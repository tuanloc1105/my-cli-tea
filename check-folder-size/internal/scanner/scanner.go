package scanner

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/term"
)

type walkTask struct {
	dirPath      string
	topLevelName string
	currentDepth int
}

type scannerFilesystem interface {
	lookup(string) (entryMetadata, error)
	allocatedSupported() bool
}

type scanDependencies struct {
	filesystem scannerFilesystem
	readDir    func(string) ([]os.DirEntry, error)
}

type topLevelItem struct {
	name  string
	kind  ItemType
	total *atomic.Int64
}

type warningTracker struct {
	count atomic.Int64
	mu    sync.Mutex
	first string
}

func (tracker *warningTracker) add(path string, err error) {
	tracker.count.Add(1)
	tracker.mu.Lock()
	if tracker.first == "" {
		tracker.first = fmt.Sprintf("%s: %v", path, err)
	}
	tracker.mu.Unlock()
}

func (tracker *warningTracker) summary() string {
	count := tracker.count.Load()
	if count == 0 {
		return ""
	}
	tracker.mu.Lock()
	first := tracker.first
	tracker.mu.Unlock()
	return fmt.Sprintf("%d filesystem entries could not be scanned; first error: %s", count, first)
}

type hardlinkRecord struct {
	owner string
	size  int64
}

type hardlinkRegistry struct {
	mu      sync.Mutex
	records map[fileIdentity]hardlinkRecord
	totals  map[string]*atomic.Int64
}

func newHardlinkRegistry(totals map[string]*atomic.Int64) *hardlinkRegistry {
	return &hardlinkRegistry{
		records: make(map[fileIdentity]hardlinkRecord),
		totals:  totals,
	}
}

func (registry *hardlinkRegistry) add(identity fileIdentity, owner string, size int64) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	record, exists := registry.records[identity]
	if !exists {
		if err := addSize(registry.totals[owner], size); err != nil {
			return err
		}
		registry.records[identity] = hardlinkRecord{owner: owner, size: size}
		return nil
	}
	if owner >= record.owner {
		return nil
	}
	if err := addSize(registry.totals[owner], record.size); err != nil {
		return err
	}
	registry.totals[record.owner].Add(-record.size)
	record.owner = owner
	registry.records[identity] = record
	return nil
}

func addSize(total *atomic.Int64, size int64) error {
	if size < 0 {
		return fmt.Errorf("negative size %d", size)
	}
	for {
		current := total.Load()
		if size > math.MaxInt64-current {
			return fmt.Errorf("size overflow adding %d to %d", size, current)
		}
		if total.CompareAndSwap(current, current+size) {
			return nil
		}
	}
}

type parallelWalker struct {
	excludeMap map[string]struct{}
	opts       ScanOptions
	deps       scanDependencies
	numWorkers int

	taskCh   chan walkTask
	taskWg   sync.WaitGroup
	workerWg sync.WaitGroup

	totals    map[string]*atomic.Int64
	hardlinks *hardlinkRegistry
	warnings  *warningTracker

	showProgress      bool
	termWidth         int
	totalTopLevel     int
	completedTopLevel atomic.Int64
	pendingTasks      map[string]*atomic.Int64
	progressMu        sync.Mutex
	progressWriter    io.Writer
}

func getTerminalWidth(writer io.Writer) int {
	file, ok := writer.(*os.File)
	if ok {
		if width, _, err := term.GetSize(int(file.Fd())); err == nil && width > 0 {
			return width
		}
	}
	return 80
}

func newParallelWalker(
	excludeMap map[string]struct{},
	opts ScanOptions,
	deps scanDependencies,
	totals map[string]*atomic.Int64,
	warnings *warningTracker,
	topLevelCapacity int,
) *parallelWalker {
	numWorkers := runtime.NumCPU()
	if numWorkers < 1 {
		numWorkers = 1
	}
	bufSize := numWorkers * 4
	if bufSize < 64 {
		bufSize = 64
	}
	progressWriter := opts.ProgressWriter
	if progressWriter == nil {
		progressWriter = io.Discard
	}
	walker := &parallelWalker{
		excludeMap:     excludeMap,
		opts:           opts,
		deps:           deps,
		numWorkers:     numWorkers,
		taskCh:         make(chan walkTask, bufSize),
		totals:         totals,
		hardlinks:      newHardlinkRegistry(totals),
		warnings:       warnings,
		showProgress:   opts.ShowProgress,
		pendingTasks:   make(map[string]*atomic.Int64, topLevelCapacity),
		progressWriter: progressWriter,
	}
	if opts.ShowProgress {
		walker.termWidth = getTerminalWidth(progressWriter)
	}
	return walker
}

func (walker *parallelWalker) metric(metadata entryMetadata) int64 {
	if walker.opts.SizeMode == SizeModeAllocated {
		return metadata.allocatedSize
	}
	return metadata.logicalSize
}

func (walker *parallelWalker) addEntry(path, owner string, metadata entryMetadata) {
	if walker.opts.SizeMode == SizeModeLogical && metadata.kind == ItemTypeDirectory {
		return
	}
	size := walker.metric(metadata)
	var err error
	if walker.opts.SizeMode == SizeModeAllocated && metadata.kind == ItemTypeFile && metadata.hasIdentity && metadata.linkCount > 1 {
		err = walker.hardlinks.add(metadata.identity, owner, size)
	} else {
		err = addSize(walker.totals[owner], size)
	}
	if err != nil {
		walker.warnings.add(path, err)
	}
}

func (walker *parallelWalker) processDirectory(task walkTask) {
	if walker.opts.Ctx.Err() != nil {
		return
	}
	entries, err := walker.deps.readDir(task.dirPath)
	if err != nil {
		walker.warnings.add(task.dirPath, err)
		return
	}
	if walker.opts.Ctx.Err() != nil {
		return
	}

	for _, entry := range entries {
		if walker.opts.Ctx.Err() != nil {
			return
		}
		if _, excluded := walker.excludeMap[entry.Name()]; excluded {
			continue
		}

		path := filepath.Join(task.dirPath, entry.Name())
		metadata, err := walker.deps.filesystem.lookup(path)
		if err != nil {
			walker.warnings.add(path, err)
			continue
		}
		if walker.opts.Ctx.Err() != nil {
			return
		}
		walker.addEntry(path, task.topLevelName, metadata)

		if metadata.kind != ItemTypeDirectory {
			continue
		}
		childDepth := task.currentDepth + 1
		if walker.opts.MaxDepth > 0 && childDepth > walker.opts.MaxDepth {
			continue
		}
		childTask := walkTask{
			dirPath:      path,
			topLevelName: task.topLevelName,
			currentDepth: childDepth,
		}
		walker.taskWg.Add(1)
		if walker.showProgress {
			walker.pendingTasks[task.topLevelName].Add(1)
		}
		walker.enqueueOrProcess(childTask)
	}
}

func (walker *parallelWalker) enqueueOrProcess(task walkTask) {
	if walker.opts.Ctx.Err() != nil {
		walker.completeTask(task)
		return
	}
	select {
	case walker.taskCh <- task:
	default:
		walker.processDirectory(task)
		walker.completeTask(task)
	}
}

func (walker *parallelWalker) completeTask(task walkTask) {
	walker.taskWg.Done()
	if !walker.showProgress {
		return
	}
	remaining := walker.pendingTasks[task.topLevelName].Add(-1)
	if remaining != 0 || walker.opts.Ctx.Err() != nil {
		return
	}

	count := walker.completedTopLevel.Add(1)
	progressMessage := fmt.Sprintf("Processing %d/%d: %s", count, walker.totalTopLevel, task.topLevelName)
	runes := []rune(progressMessage)
	if len(runes) > walker.termWidth-1 {
		progressMessage = string(runes[:walker.termWidth-4]) + "..."
	}
	paddedMessage := fmt.Sprintf("%-*s", walker.termWidth-1, progressMessage)
	walker.progressMu.Lock()
	fmt.Fprintf(walker.progressWriter, "\r%s", paddedMessage)
	walker.progressMu.Unlock()
}

func (walker *parallelWalker) run(initialTasks []walkTask) {
	if len(initialTasks) == 0 {
		return
	}
	walker.totalTopLevel = len(initialTasks)
	walker.taskWg.Add(len(initialTasks))
	if walker.showProgress {
		for _, task := range initialTasks {
			pending := &atomic.Int64{}
			pending.Store(1)
			walker.pendingTasks[task.topLevelName] = pending
		}
	}
	for range walker.numWorkers {
		walker.workerWg.Add(1)
		go func() {
			defer walker.workerWg.Done()
			for task := range walker.taskCh {
				if walker.opts.Ctx.Err() == nil {
					walker.processDirectory(task)
				}
				walker.completeTask(task)
			}
		}()
	}
	go func() {
		for _, task := range initialTasks {
			walker.taskCh <- task
		}
	}()
	go func() {
		walker.taskWg.Wait()
		close(walker.taskCh)
	}()
	walker.workerWg.Wait()
	if walker.showProgress {
		fmt.Fprintln(walker.progressWriter)
	}
}

// ScanDirectory calculates sizes of the immediate entries in a directory.
func ScanDirectory(parentFolder string, opts ScanOptions) (ScanResult, error) {
	return scanDirectory(parentFolder, opts, scanDependencies{
		filesystem: nativeFilesystem{},
		readDir:    os.ReadDir,
	})
}

func scanDirectory(parentFolder string, opts ScanOptions, deps scanDependencies) (ScanResult, error) {
	result := newScanResult()
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	if opts.SizeMode == "" {
		opts.SizeMode = SizeModeLogical
	}
	if !opts.SizeMode.Valid() {
		result.Status = ScanStatusFailed
		return result, &ScanError{Kind: ErrorKindInvalidOptions, Path: parentFolder, Err: fmt.Errorf("invalid size mode %q", opts.SizeMode)}
	}
	if opts.SizeMode == SizeModeAllocated && !deps.filesystem.allocatedSupported() {
		result.Status = ScanStatusFailed
		return result, &ScanError{Kind: ErrorKindUnsupported, Path: parentFolder, Err: fmt.Errorf("allocated size is not supported on %s", runtime.GOOS)}
	}
	if err := opts.Ctx.Err(); err != nil {
		result.Status = ScanStatusPartial
		return result, &ScanError{Kind: ErrorKindCancelled, Path: parentFolder, Err: err}
	}

	entries, err := deps.readDir(parentFolder)
	if err != nil {
		result.Status = ScanStatusFailed
		return result, &ScanError{Kind: ErrorKindRootOpen, Path: parentFolder, Err: err}
	}

	excludeMap := make(map[string]struct{}, len(opts.ExcludeList))
	for _, item := range opts.ExcludeList {
		excludeMap[item] = struct{}{}
	}
	totals := make(map[string]*atomic.Int64, len(entries))
	warnings := &warningTracker{}
	walker := newParallelWalker(excludeMap, opts, deps, totals, warnings, len(entries))
	items := make([]topLevelItem, 0, len(entries))
	initialTasks := make([]walkTask, 0, len(entries))

	for _, entry := range entries {
		if err := opts.Ctx.Err(); err != nil {
			break
		}
		if _, excluded := excludeMap[entry.Name()]; excluded {
			continue
		}
		path := filepath.Join(parentFolder, entry.Name())
		metadata, err := deps.filesystem.lookup(path)
		if err != nil {
			warnings.add(path, err)
			continue
		}
		if err := opts.Ctx.Err(); err != nil {
			break
		}
		total := &atomic.Int64{}
		totals[entry.Name()] = total
		items = append(items, topLevelItem{name: entry.Name(), kind: metadata.kind, total: total})
		walker.addEntry(path, entry.Name(), metadata)
		if metadata.kind == ItemTypeDirectory {
			initialTasks = append(initialTasks, walkTask{
				dirPath:      path,
				topLevelName: entry.Name(),
				currentDepth: 0,
			})
		}
	}

	walker.run(initialTasks)
	for _, item := range items {
		result.Items = append(result.Items, ItemInfo{Name: item.name, Size: item.total.Load(), Type: item.kind})
	}
	result.WarningCount = warnings.count.Load()
	result.WarningSummary = warnings.summary()
	if result.WarningCount > 0 {
		result.Status = ScanStatusPartial
	}
	if err := opts.Ctx.Err(); err != nil {
		result.Status = ScanStatusPartial
		return result, &ScanError{Kind: ErrorKindCancelled, Path: parentFolder, Err: err}
	}
	return result, nil
}

// GetSizesOfSubfolders preserves the original logical-size API.
func GetSizesOfSubfolders(parentFolder string, opts ScanOptions) (ScanResult, error) {
	opts.SizeMode = SizeModeLogical
	return ScanDirectory(parentFolder, opts)
}
