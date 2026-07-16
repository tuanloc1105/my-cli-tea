package finder

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"find-everything/internal/types"
)

func TestNewFileFinderValidatesConfiguration(t *testing.T) {
	base := t.TempDir()
	valid := testFinderOptions()

	tests := []struct {
		name    string
		base    string
		pattern string
		mutate  func(*FinderOptions)
	}{
		{name: "empty base", base: "", pattern: "*"},
		{name: "empty pattern", base: base, pattern: ""},
		{name: "zero workers", base: base, pattern: "*", mutate: func(o *FinderOptions) { o.MaxWorkers = 0 }},
		{name: "zero result limit", base: base, pattern: "*", mutate: func(o *FinderOptions) { o.MaxResults = 0 }},
		{name: "negative minimum", base: base, pattern: "*", mutate: func(o *FinderOptions) { o.MinSize = -1 }},
		{name: "negative maximum", base: base, pattern: "*", mutate: func(o *FinderOptions) { o.MaxSize = -1 }},
		{name: "reversed sizes", base: base, pattern: "*", mutate: func(o *FinderOptions) { o.MinSize, o.MaxSize = 2, 1 }},
		{name: "invalid exclude regex", base: base, pattern: "*", mutate: func(o *FinderOptions) { o.ExcludePatterns = []string{"["} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := valid
			if tt.mutate != nil {
				tt.mutate(&opts)
			}
			if _, err := NewFileFinder(tt.base, tt.pattern, opts); err == nil {
				t.Fatal("NewFileFinder() error = nil")
			}
		})
	}
}

func TestFindRejectsInvalidBaseBeforeStartingProgress(t *testing.T) {
	tests := []struct {
		name string
		base func(*testing.T) string
	}{
		{
			name: "missing",
			base: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") },
		},
		{
			name: "not directory",
			base: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "file")
				writeTestFile(t, path, 1)
				return path
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var callbacks atomic.Int64
			opts := testFinderOptions()
			opts.Progress = func(types.ProgressSnapshot) { callbacks.Add(1) }
			finder := mustNewFinder(t, tt.base(t), "*", opts)
			if _, err := finder.FindFilesAndDirs(context.Background()); err == nil {
				t.Fatal("FindFilesAndDirs() error = nil")
			}
			if callbacks.Load() != 0 {
				t.Fatalf("progress callbacks = %d, want 0", callbacks.Load())
			}
		})
	}

	t.Run("unreadable", func(t *testing.T) {
		base := t.TempDir()
		wantErr := errors.New("read denied")
		setReadDir(t, func(path string) ([]os.DirEntry, error) {
			if path == base {
				return nil, wantErr
			}
			return os.ReadDir(path)
		})

		finder := mustNewFinder(t, base, "*", testFinderOptions())
		_, err := finder.FindFilesAndDirs(context.Background())
		if !errors.Is(err, wantErr) {
			t.Fatalf("FindFilesAndDirs() error = %v, want %v", err, wantErr)
		}
	})
}

func TestFindPreservesRelativeAndAbsolutePathForms(t *testing.T) {
	base := t.TempDir()
	writeTestFile(t, filepath.Join(base, "main.go"), 1)

	t.Run("absolute", func(t *testing.T) {
		results := runTestFinder(t, base, "*.go", testFinderOptions())
		if len(results.Files) != 1 || !filepath.IsAbs(results.Files[0].Path) {
			t.Fatalf("files = %+v, want one absolute path", results.Files)
		}
	})

	t.Run("relative", func(t *testing.T) {
		workingDirectory, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd(): %v", err)
		}
		relativeBase, err := filepath.Rel(workingDirectory, base)
		if err != nil {
			t.Fatalf("filepath.Rel(): %v", err)
		}
		results := runTestFinder(t, relativeBase, "*.go", testFinderOptions())
		want := filepath.Join(filepath.Clean(relativeBase), "main.go")
		if len(results.Files) != 1 || results.Files[0].Path != want || filepath.IsAbs(results.Files[0].Path) {
			t.Fatalf("files = %+v, want relative path %q", results.Files, want)
		}
	})
}

func TestFileTypesAcceptLeadingDotOrBareExtension(t *testing.T) {
	base := t.TempDir()
	writeTestFile(t, filepath.Join(base, "main.go"), 1)
	writeTestFile(t, filepath.Join(base, "notes.TXT"), 1)
	writeTestFile(t, filepath.Join(base, "image.png"), 1)

	opts := testFinderOptions()
	opts.FileTypes = []string{"go", ".TXT"}
	results := runTestFinder(t, base, "*", opts)
	assertPathNames(t, results.Files, "main.go", "notes.TXT")
}

func TestLimitIsExactCombinedHardCap(t *testing.T) {
	base := t.TempDir()
	for i := 0; i < 30; i++ {
		dir := filepath.Join(base, "dir-"+twoDigits(i))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("Mkdir(%q): %v", dir, err)
		}
		for j := 0; j < 4; j++ {
			writeTestFile(t, filepath.Join(dir, "file-"+twoDigits(j)), 1)
		}
	}

	opts := testFinderOptions()
	opts.MaxWorkers = 32
	opts.MaxResults = 17
	finder := mustNewFinder(t, base, "*", opts)
	results, err := finder.FindFilesAndDirs(context.Background())
	if err != nil {
		t.Fatalf("FindFilesAndDirs() error = %v", err)
	}
	if got := len(results.Files) + len(results.Directories); got != opts.MaxResults {
		t.Fatalf("combined results = %d, want %d", got, opts.MaxResults)
	}
	if !results.Report.LimitReached {
		t.Fatal("LimitReached = false")
	}
}

func TestQueueCapacityWideAndDeepTrees(t *testing.T) {
	for _, capacity := range []int{1, 2} {
		t.Run("capacity-"+twoDigits(capacity), func(t *testing.T) {
			setQueueCapacity(t, capacity)
			base := t.TempDir()
			const branches = 12
			for branch := 0; branch < branches; branch++ {
				path := filepath.Join(base, "branch-"+twoDigits(branch))
				for depth := 0; depth < 8; depth++ {
					path = filepath.Join(path, "depth-"+twoDigits(depth))
					if err := os.MkdirAll(path, 0o755); err != nil {
						t.Fatalf("MkdirAll(%q): %v", path, err)
					}
				}
				writeTestFile(t, filepath.Join(path, "result.match"), 1)
			}

			opts := testFinderOptions()
			opts.MaxWorkers = 4
			finder := mustNewFinder(t, base, "*.match", opts)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			results, err := finder.FindFilesAndDirs(ctx)
			if err != nil {
				t.Fatalf("FindFilesAndDirs() error = %v", err)
			}
			if len(results.Files) != branches {
				t.Fatalf("files = %d, want %d", len(results.Files), branches)
			}
		})
	}
}

func TestQueueSaturationDoesNotGrowGoroutinesPerDirectory(t *testing.T) {
	setQueueCapacity(t, 1)
	base := t.TempDir()
	for i := 0; i < 200; i++ {
		if err := os.Mkdir(filepath.Join(base, "dir-"+threeDigits(i)), 0o755); err != nil {
			t.Fatalf("Mkdir(): %v", err)
		}
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var blocked atomic.Bool
	setReadDir(t, func(path string) ([]os.DirEntry, error) {
		if path != base && blocked.CompareAndSwap(false, true) {
			close(started)
			<-release
		}
		return os.ReadDir(path)
	})

	opts := testFinderOptions()
	opts.MaxWorkers = 1
	finder := mustNewFinder(t, base, "never-match", opts)
	baseline := runtime.NumGoroutine()
	done := make(chan error, 1)
	go func() {
		_, err := finder.FindFilesAndDirs(context.Background())
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("finder did not reach blocked descendant read")
	}
	if growth := runtime.NumGoroutine() - baseline; growth > 10 {
		t.Fatalf("goroutine growth = %d, want at most 10", growth)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("FindFilesAndDirs() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("finder deadlocked after releasing readDir")
	}
}

func TestCancelBeforeScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	finder := mustNewFinder(t, t.TempDir(), "*", testFinderOptions())
	_, err := finder.FindFilesAndDirs(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FindFilesAndDirs() error = %v, want context.Canceled", err)
	}
}

func TestCancelDuringScan(t *testing.T) {
	base := t.TempDir()
	child := filepath.Join(base, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("Mkdir(): %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	setReadDir(t, func(path string) ([]os.DirEntry, error) {
		if path == child {
			close(started)
			<-release
		}
		return os.ReadDir(path)
	})

	opts := testFinderOptions()
	opts.MaxWorkers = 1
	finder := mustNewFinder(t, base, "never-match", opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := finder.FindFilesAndDirs(ctx)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("finder did not start descendant read")
	}
	cancel()
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("FindFilesAndDirs() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("finder deadlocked after cancellation")
	}
}

func TestPartialTraversalErrorsAreCountedAndCapped(t *testing.T) {
	base := t.TempDir()
	for i := 0; i < types.MaxIssueDetails+5; i++ {
		if err := os.Mkdir(filepath.Join(base, "dir-"+twoDigits(i)), 0o755); err != nil {
			t.Fatalf("Mkdir(): %v", err)
		}
	}

	wantErr := errors.New("descendant read failed")
	setReadDir(t, func(path string) ([]os.DirEntry, error) {
		if path != base {
			return nil, wantErr
		}
		return os.ReadDir(path)
	})

	results := runTestFinder(t, base, "never-match", testFinderOptions())
	if !results.Report.Incomplete {
		t.Fatal("Incomplete = false")
	}
	if results.Report.TraversalErrorCount != types.MaxIssueDetails+5 {
		t.Fatalf("TraversalErrorCount = %d, want %d", results.Report.TraversalErrorCount, types.MaxIssueDetails+5)
	}
	if len(results.Report.TraversalErrors) != types.MaxIssueDetails {
		t.Fatalf("TraversalErrors details = %d, want %d", len(results.Report.TraversalErrors), types.MaxIssueDetails)
	}
	for _, issue := range results.Report.TraversalErrors {
		if !errors.Is(issue.Err, wantErr) || issue.Operation != "read directory" {
			t.Fatalf("unexpected issue: %+v", issue)
		}
	}
}

func TestPartialReadProcessesEntriesBeforeReportingError(t *testing.T) {
	base := t.TempDir()
	child := filepath.Join(base, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("Mkdir(): %v", err)
	}
	writeTestFile(t, filepath.Join(child, "kept.txt"), 3)

	wantErr := errors.New("partial read")
	setReadDir(t, func(path string) ([]os.DirEntry, error) {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		if path == child {
			return entries, wantErr
		}
		return entries, nil
	})

	results := runTestFinder(t, base, "*.txt", testFinderOptions())
	assertPathNames(t, results.Files, "kept.txt")
	if !results.Report.Incomplete || results.Report.TraversalErrorCount != 1 {
		t.Fatalf("report = %+v, want one partial traversal error", results.Report)
	}
}

func TestPatternExcludesTypesAndSizes(t *testing.T) {
	base := t.TempDir()
	writeTestFile(t, filepath.Join(base, "keep.go"), 5)
	writeTestFile(t, filepath.Join(base, "skip.go"), 5)
	writeTestFile(t, filepath.Join(base, "small.go"), 1)
	writeTestFile(t, filepath.Join(base, "keep.txt"), 5)
	writeTestFile(t, filepath.Join(base, "name.go.bin"), 5)

	nested := filepath.Join(base, "nested")
	excluded := filepath.Join(base, "excluded")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	if err := os.MkdirAll(excluded, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	writeTestFile(t, filepath.Join(nested, "nested.go"), 7)
	writeTestFile(t, filepath.Join(excluded, "excluded.go"), 7)

	opts := testFinderOptions()
	opts.ExcludeDirs = []string{"excluded"}
	opts.ExcludePatterns = []string{`skip\.go$`}
	opts.FileTypes = []string{"go"}
	opts.MinSize = 2
	opts.MaxSize = 10
	results := runTestFinder(t, base, "*.go", opts)
	assertPathNames(t, results.Files, "keep.go", "nested.go")
}

func TestPatternMatchesBasenameOnly(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "contains-needle")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatalf("Mkdir(): %v", err)
	}
	writeTestFile(t, filepath.Join(parent, "plain.bin"), 1)

	results := runTestFinder(t, base, "*needle*", testFinderOptions())
	if len(results.Files) != 0 {
		t.Fatalf("files = %+v, path components outside basename must not match", results.Files)
	}
	if len(results.Directories) != 1 || filepath.Base(results.Directories[0]) != "contains-needle" {
		t.Fatalf("directories = %v, want contains-needle", results.Directories)
	}
}

func TestSymlinkPolicy(t *testing.T) {
	base := t.TempDir()
	targetFile := filepath.Join(base, "target.bin")
	targetDir := filepath.Join(base, "target-dir")
	writeTestFile(t, targetFile, 9)
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("Mkdir(): %v", err)
	}

	fileLink := filepath.Join(base, "file-link.go")
	dirLink := filepath.Join(base, "dir-link.go")
	brokenLink := filepath.Join(base, "broken-link.go")
	requireTestSymlink(t, targetFile, fileLink, "regular file")
	requireTestSymlink(t, targetDir, dirLink, "directory")
	requireTestSymlink(t, filepath.Join(base, "missing"), brokenLink, "broken target")

	opts := testFinderOptions()
	opts.FileTypes = []string{".go"}
	results := runTestFinder(t, base, "*.go", opts)
	assertPathNames(t, results.Files, "file-link.go")
	if len(results.Directories) != 0 {
		t.Fatalf("directories = %v, want none", results.Directories)
	}
	if results.Report.Incomplete {
		t.Fatal("skipped symlinks must not mark the scan incomplete")
	}
	if results.Report.SkippedSymlinkCount != 2 || len(results.Report.SkippedSymlinks) != 2 {
		t.Fatalf("symlink report = %+v, want two notices", results.Report)
	}
}

func TestHiddenEntriesAndHiddenBaseAreIncluded(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".hidden-base")
	hiddenDir := filepath.Join(base, ".hidden-dir")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	writeTestFile(t, filepath.Join(base, ".dotfile.log"), 1)
	if err := os.WriteFile(filepath.Join(base, ".gitignore"), []byte("ignored-by-gitignore.log\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore): %v", err)
	}
	writeTestFile(t, filepath.Join(base, "ignored-by-gitignore.log"), 1)
	writeTestFile(t, filepath.Join(base, "visible.txt"), 1)
	writeTestFile(t, filepath.Join(hiddenDir, "nested.log"), 1)

	results := runTestFinder(t, base, "*", testFinderOptions())
	assertPathNames(t, results.Files, ".dotfile.log", ".gitignore", "ignored-by-gitignore.log", "nested.log", "visible.txt")
	if len(results.Directories) != 1 || filepath.Base(results.Directories[0]) != ".hidden-dir" {
		t.Fatalf("directories = %v, want .hidden-dir", results.Directories)
	}

	patternResults := runTestFinder(t, base, "*.log", testFinderOptions())
	assertPathNames(t, patternResults.Files, ".dotfile.log", "ignored-by-gitignore.log", "nested.log")

	opts := testFinderOptions()
	opts.ExcludeDirs = []string{".hidden-dir"}
	opts.ExcludePatterns = []string{`\.gitignore$`, `ignored-by-gitignore\.log$`}
	excludedResults := runTestFinder(t, base, "*", opts)
	assertPathNames(t, excludedResults.Files, ".dotfile.log", "visible.txt")
	if len(excludedResults.Directories) != 0 {
		t.Fatalf("excluded hidden directories = %v, want none", excludedResults.Directories)
	}
}

func TestProgressEmitsPeriodicAndFinalSnapshotsThenStops(t *testing.T) {
	base := t.TempDir()
	child := filepath.Join(base, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("Mkdir(): %v", err)
	}
	writeTestFile(t, filepath.Join(child, "result.txt"), 2)

	releaseRead := make(chan struct{})
	setReadDir(t, func(path string) ([]os.DirEntry, error) {
		if path == child {
			<-releaseRead
		}
		return os.ReadDir(path)
	})

	var mu sync.Mutex
	var snapshots []types.ProgressSnapshot
	var releaseOnce sync.Once
	opts := testFinderOptions()
	opts.Progress = func(snapshot types.ProgressSnapshot) {
		mu.Lock()
		snapshots = append(snapshots, snapshot)
		mu.Unlock()
		releaseOnce.Do(func() { close(releaseRead) })
	}
	results := runTestFinder(t, base, "*", opts)
	if got := len(results.Files) + len(results.Directories); got != 2 {
		t.Fatalf("results = %d, want 2", got)
	}

	mu.Lock()
	countAtReturn := len(snapshots)
	if countAtReturn < 2 {
		mu.Unlock()
		t.Fatalf("progress snapshots = %d, want periodic and final snapshots", countAtReturn)
	}
	final := snapshots[countAtReturn-1]
	mu.Unlock()

	if final.TotalDirectories != 2 || final.ProcessedDirectories != 2 || final.FoundFiles != 1 || final.FoundDirectories != 1 {
		t.Fatalf("final snapshot = %+v", final)
	}
}

func testFinderOptions() FinderOptions {
	return FinderOptions{
		MaxWorkers: 4,
		MaxResults: 10000,
		MaxSize:    math.MaxInt64,
	}
}

func mustNewFinder(t *testing.T, base, pattern string, opts FinderOptions) *FileFinder {
	t.Helper()
	finder, err := NewFileFinder(base, pattern, opts)
	if err != nil {
		t.Fatalf("NewFileFinder() error = %v", err)
	}
	return finder
}

func runTestFinder(t *testing.T, base, pattern string, opts FinderOptions) types.SearchResults {
	t.Helper()
	finder := mustNewFinder(t, base, pattern, opts)
	results, err := finder.FindFilesAndDirs(context.Background())
	if err != nil {
		t.Fatalf("FindFilesAndDirs() error = %v", err)
	}
	return results
}

func writeTestFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func requireTestSymlink(t *testing.T, target, link, kind string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Fatalf("Symlink(%s) prerequisite unavailable: %v; enable Windows Developer Mode or grant SeCreateSymbolicLinkPrivilege", kind, err)
		}
		t.Fatalf("Symlink(%s): %v", kind, err)
	}
}

func assertPathNames(t *testing.T, files []types.FileResult, want ...string) {
	t.Helper()
	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, filepath.Base(file.Path))
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("file names = %v, want %v", got, want)
	}
}

func setQueueCapacity(t *testing.T, capacity int) {
	t.Helper()
	old := queueCapacity
	queueCapacity = capacity
	t.Cleanup(func() { queueCapacity = old })
}

func setReadDir(t *testing.T, fn func(string) ([]os.DirEntry, error)) {
	t.Helper()
	old := readDir
	readDir = fn
	t.Cleanup(func() { readDir = old })
}

func twoDigits(value int) string {
	return string([]byte{'0' + byte(value/10), '0' + byte(value%10)})
}

func threeDigits(value int) string {
	return string([]byte{'0' + byte(value/100), '0' + byte((value/10)%10), '0' + byte(value%10)})
}
