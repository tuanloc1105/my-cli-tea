package searcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassifierExtensionsBasenamesAndAll(t *testing.T) {
	tests := []struct {
		name       string
		classifier classifier
		path       string
		want       bool
	}{
		{name: "added extension", classifier: newClassifier(false, nil), path: "config.toml", want: true},
		{name: "known basename", classifier: newClassifier(false, nil), path: "Makefile", want: true},
		{name: "env pattern", classifier: newClassifier(false, nil), path: ".env.local", want: true},
		{name: "unknown extension", classifier: newClassifier(false, nil), path: "archive.bin", want: false},
		{name: "explicit strict accept", classifier: newClassifier(false, []string{" TXT ", "txt"}), path: "a.txt", want: true},
		{name: "explicit strict reject known", classifier: newClassifier(false, []string{"txt"}), path: "a.go", want: false},
		{name: "all", classifier: newClassifier(true, nil), path: "archive.bin", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.classifier.accepts(test.path); got != test.want {
				t.Fatalf("accepts(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}

func TestSearchHiddenDefaultExcludesAndExplicitRoot(t *testing.T) {
	root := t.TempDir()
	writeSearcherFile(t, filepath.Join(root, "a.txt"), "needle\n")
	writeSearcherFile(t, filepath.Join(root, ".hidden.txt"), "needle\n")
	writeSearcherFile(t, filepath.Join(root, ".hidden", "nested.txt"), "needle\n")
	writeSearcherFile(t, filepath.Join(root, "node_modules", "dependency.txt"), "needle\n")

	summary, events, err := collectSearch(defaultOptions(root, "needle"), nil)
	if err != nil || summary.Matches != 3 || summary.PartialErrors != 0 {
		t.Fatalf("default search summary/events/error = %+v/%+v/%v", summary, events, err)
	}
	paths := resultPaths(events)
	if strings.Join(paths, ",") != strings.Join([]string{
		filepath.Join(root, ".hidden.txt"),
		filepath.Join(root, ".hidden", "nested.txt"),
		filepath.Join(root, "a.txt"),
	}, ",") {
		t.Fatalf("default hidden paths = %v", paths)
	}

	options := defaultOptions(root, "needle")
	options.NoDefaultExclude = true
	summary, events, err = collectSearch(options, nil)
	if err != nil || summary.Matches != 4 || !containsResultPath(events, filepath.Join(root, "node_modules", "dependency.txt")) {
		t.Fatalf("no-default-excludes summary/events/error = %+v/%+v/%v", summary, events, err)
	}

	options.ExcludeDirs = []string{"node_modules"}
	summary, events, err = collectSearch(options, nil)
	if err != nil || summary.Matches != 3 || containsResultPath(events, filepath.Join(root, "node_modules", "dependency.txt")) {
		t.Fatalf("user excludes summary/events/error = %+v/%+v/%v", summary, events, err)
	}

	hiddenRoot := filepath.Join(t.TempDir(), ".git")
	writeSearcherFile(t, filepath.Join(hiddenRoot, "config.txt"), "needle\n")
	summary, _, err = collectSearch(defaultOptions(hiddenRoot, "needle"), nil)
	if err != nil || summary.Matches != 1 {
		t.Fatalf("explicit default-excluded root summary/error = %+v/%v", summary, err)
	}
}

func TestSearchSkipsBinaryAndSymlinksAndRejectsRootSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	writeSearcherFile(t, target, "needle\n")
	writeSearcherFile(t, filepath.Join(root, "binary.data"), "needle\x00tail")
	directory := filepath.Join(root, "real-dir")
	writeSearcherFile(t, filepath.Join(directory, "inside.txt"), "absent\n")
	if err := os.Symlink(target, filepath.Join(root, "file-link.txt")); err != nil {
		t.Skipf("file symlink unavailable: %v", err)
	}
	directoryLink := filepath.Join(root, "dir-link")
	if err := os.Symlink(directory, directoryLink); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	if _, err := readDirectoryNoFollow(directoryLink); !errors.Is(err, errUnsafeFileType) {
		t.Fatalf("no-follow directory read error = %v", err)
	}

	options := defaultOptions(root, "needle")
	options.SearchAll = true
	summary, events, err := collectSearch(options, nil)
	if err != nil || summary.Matches != 1 || len(resultPaths(events)) != 1 || resultPaths(events)[0] != target {
		t.Fatalf("file policy summary/events/error = %+v/%+v/%v", summary, events, err)
	}

	rootLink := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Skipf("root symlink unavailable: %v", err)
	}
	options.Root = rootLink
	if _, _, err := collectSearch(options, nil); err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("root symlink error = %v", err)
	}
}

func TestSearchInjectedPathErrorsRemainDeterministic(t *testing.T) {
	root := t.TempDir()
	aPath := filepath.Join(root, "a.txt")
	bPath := filepath.Join(root, "b.txt")
	writeSearcherFile(t, aPath, "needle\n")
	writeSearcherFile(t, bPath, "needle\n")

	fs := osFileSystem()
	fs.open = func(path string) (fileHandle, error) {
		if path == aPath {
			return nil, errors.New("injected open failure")
		}
		return os.Open(path)
	}
	summary, events, err := collectSearch(defaultOptions(root, "needle"), &fs)
	if err != nil || summary.Matches != 1 || summary.PartialErrors != 1 || len(events) != 2 {
		t.Fatalf("summary/events/error = %+v/%+v/%v", summary, events, err)
	}
	if events[0].Diagnostic == nil || events[0].Diagnostic.Path != aPath || events[1].Result == nil || events[1].Result.Path != bPath {
		t.Fatalf("deterministic diagnostic events = %+v", events)
	}

	fs = osFileSystem()
	fs.open = func(string) (fileHandle, error) { return os.Open(bPath) }
	summary, events, err = collectSearch(defaultOptions(root, "needle"), &fs)
	if err != nil || summary.Matches != 1 || summary.PartialErrors != 1 || events[0].Diagnostic == nil || !strings.Contains(events[0].Diagnostic.Err.Error(), "identity changed") {
		t.Fatalf("identity recheck summary/events/error = %+v/%+v/%v", summary, events, err)
	}
}

func TestSearchInjectedDirectoryAndOpenedStatErrors(t *testing.T) {
	root := t.TempDir()
	badDirectory := filepath.Join(root, "a")
	if err := os.MkdirAll(badDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(root, "z.txt")
	writeSearcherFile(t, filePath, "needle\n")

	fs := osFileSystem()
	readDir := fs.readDir
	fs.readDir = func(path string) ([]os.DirEntry, error) {
		if path == badDirectory {
			return nil, errors.New("injected directory failure")
		}
		return readDir(path)
	}
	summary, events, err := collectSearch(defaultOptions(root, "needle"), &fs)
	if err != nil || summary.PartialErrors != 1 || summary.Matches != 1 || events[0].Diagnostic == nil || events[1].Result == nil {
		t.Fatalf("directory error summary/events/error = %+v/%+v/%v", summary, events, err)
	}

	fs = osFileSystem()
	fs.open = func(path string) (fileHandle, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &statErrorFile{File: file}, nil
	}
	summary, events, err = collectSearch(defaultOptions(root, "needle"), &fs)
	if err != nil || summary.PartialErrors != 1 || summary.Matches != 0 || len(events) != 1 || events[0].Diagnostic == nil || !strings.Contains(events[0].Diagnostic.Err.Error(), "inspect opened file") {
		t.Fatalf("stat error summary/events/error = %+v/%+v/%v", summary, events, err)
	}
}

func TestUnknownDirEntryTypeUsesInfoForTraversalAndListing(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "unknown")
	writeSearcherFile(t, filepath.Join(directory, "inside.txt"), "needle\n")

	fs := osFileSystem()
	readDir := fs.readDir
	fs.readDir = func(path string) ([]os.DirEntry, error) {
		entries, err := readDir(path)
		if err != nil {
			return nil, err
		}
		if path == root {
			for index, entry := range entries {
				if entry.Name() == "unknown" {
					entries[index] = unknownTypeEntry{DirEntry: entry}
				}
			}
		}
		return entries, nil
	}
	summary, events, err := collectSearch(defaultOptions(root, "needle"), &fs)
	if err != nil || summary.Matches != 1 || len(resultPaths(events)) != 1 {
		t.Fatalf("unknown traversal summary/events/error = %+v/%+v/%v", summary, events, err)
	}

	var listed []ListEntry
	err = list(context.Background(), root, true, func(entry ListEntry) error {
		listed = append(listed, entry)
		return nil
	}, fs)
	if err != nil || len(listed) != 1 || !listed[0].IsDir {
		t.Fatalf("unknown list entries/error = %+v/%v", listed, err)
	}
}

func TestRootDirectoryReadFailureIsFatal(t *testing.T) {
	root := t.TempDir()
	fs := osFileSystem()
	fs.readDir = func(string) ([]os.DirEntry, error) {
		return nil, errors.New("injected root read failure")
	}
	summary, events, err := collectSearch(defaultOptions(root, "needle"), &fs)
	if err == nil || !strings.Contains(err.Error(), "read root directory") || summary.PartialErrors != 0 || len(events) != 0 {
		t.Fatalf("root read summary/events/error = %+v/%+v/%v", summary, events, err)
	}
}

func TestCoordinatorOrdersOutOfOrderWorkersAndCapsExactly(t *testing.T) {
	root := t.TempDir()
	aPath := filepath.Join(root, "a.txt")
	bPath := filepath.Join(root, "b.txt")
	writeSearcherFile(t, aPath, "needle\n")
	writeSearcherFile(t, bPath, "needle\n")

	bStarted := make(chan struct{})
	var closeB sync.Once
	fs := osFileSystem()
	fs.open = func(path string) (fileHandle, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		handle := &readHookFile{File: file}
		if path == aPath {
			handle.beforeRead = func() { <-bStarted }
		} else if path == bPath {
			handle.beforeRead = func() { closeB.Do(func() { close(bStarted) }) }
		}
		return handle, nil
	}
	summary, events, err := collectSearch(defaultOptions(root, "needle"), &fs)
	if err != nil || summary.Matches != 2 || strings.Join(resultPaths(events), ",") != aPath+","+bPath {
		t.Fatalf("ordered summary/events/error = %+v/%+v/%v", summary, events, err)
	}

	for index := 0; index < 20; index++ {
		writeSearcherFile(t, filepath.Join(root, fmt.Sprintf("c%02d.txt", index)), "needle\n")
	}
	var opens atomic.Int32
	fs = osFileSystem()
	fs.open = func(path string) (fileHandle, error) {
		opens.Add(1)
		return os.Open(path)
	}
	options := defaultOptions(root, "needle")
	options.MaxResults = 1
	options.MaxWorkers = 2
	summary, events, err = collectSearch(options, &fs)
	if err != nil || summary.Matches != 1 || !summary.StoppedEarly || len(resultPaths(events)) != 1 || resultPaths(events)[0] != aPath {
		t.Fatalf("cap summary/events/error = %+v/%+v/%v", summary, events, err)
	}
	if got := opens.Load(); got > 2 {
		t.Fatalf("early cancellation opened %d files, want at most worker window 2", got)
	}
}

func TestCoordinatorUsesBytewiseRelativePathOrderAcrossDirectories(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "a.txt")
	nestedPath := filepath.Join(root, "a", "inside.txt")
	writeSearcherFile(t, filePath, "needle\n")
	writeSearcherFile(t, nestedPath, "needle\n")

	summary, events, err := collectSearch(defaultOptions(root, "needle"), nil)
	if err != nil || summary.Matches != 2 {
		t.Fatalf("summary/events/error = %+v/%+v/%v", summary, events, err)
	}
	if got := strings.Join(resultPaths(events), ","); got != filePath+","+nestedPath {
		t.Fatalf("relative bytewise order = %q, want %q", got, filePath+","+nestedPath)
	}
}

func TestSearchPropagatesHandlerAndContextErrors(t *testing.T) {
	root := t.TempDir()
	writeSearcherFile(t, filepath.Join(root, "a.txt"), "needle\n")
	options := defaultOptions(root, "needle")
	_, err := Search(context.Background(), options, func(Event) error { return errors.New("render failed") })
	if err == nil || !strings.Contains(err.Error(), "emit search result") {
		t.Fatalf("handler error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Search(ctx, options, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
}

func TestMaxResultsCancellationClosesBlockedSiblingRead(t *testing.T) {
	root := t.TempDir()
	aPath := filepath.Join(root, "a.txt")
	bPath := filepath.Join(root, "b.txt")
	writeSearcherFile(t, aPath, "needle\n")
	writeSearcherFile(t, bPath, "needle\n")

	bStarted := make(chan struct{})
	fs := osFileSystem()
	fs.open = func(path string) (fileHandle, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		if path == aPath {
			return &readHookFile{File: file, beforeRead: func() { <-bStarted }}, nil
		}
		return &cancelBlockingFile{File: file, readStarted: bStarted, closed: make(chan struct{})}, nil
	}
	options := defaultOptions(root, "needle")
	options.MaxResults = 1
	options.MaxWorkers = 2
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var events []Event
	summary, err := search(ctx, options, func(event Event) error {
		events = append(events, event)
		return nil
	}, fs)
	if err != nil || ctx.Err() != nil || summary.Matches != 1 || !summary.StoppedEarly || len(resultPaths(events)) != 1 {
		t.Fatalf("cancellation summary/events/context/error = %+v/%+v/%v/%v", summary, events, ctx.Err(), err)
	}
}

func BenchmarkCoordinator(b *testing.B) {
	root := b.TempDir()
	for index := 0; index < 16; index++ {
		path := filepath.Join(root, fmt.Sprintf("%02d.txt", index))
		if err := os.WriteFile(path, []byte("needle\n"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	options := defaultOptions(root, "needle")
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := Search(context.Background(), options, func(Event) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

type readHookFile struct {
	*os.File
	once       sync.Once
	beforeRead func()
}

type statErrorFile struct {
	*os.File
}

type unknownTypeEntry struct {
	os.DirEntry
}

func (entry unknownTypeEntry) Type() os.FileMode { return 0 }
func (entry unknownTypeEntry) IsDir() bool       { return false }

type cancelBlockingFile struct {
	*os.File
	startOnce   sync.Once
	closeOnce   sync.Once
	readStarted chan struct{}
	closed      chan struct{}
}

func (file *cancelBlockingFile) Read([]byte) (int, error) {
	file.startOnce.Do(func() { close(file.readStarted) })
	<-file.closed
	return 0, os.ErrClosed
}

func (file *cancelBlockingFile) Close() error {
	file.closeOnce.Do(func() { close(file.closed) })
	return file.File.Close()
}

func (f *statErrorFile) Stat() (os.FileInfo, error) {
	return nil, errors.New("injected stat failure")
}

func (f *readHookFile) Read(buffer []byte) (int, error) {
	f.once.Do(func() {
		if f.beforeRead != nil {
			f.beforeRead()
		}
	})
	return f.File.Read(buffer)
}

func defaultOptions(root, keyword string) Options {
	return Options{
		Root:             root,
		Keyword:          keyword,
		MaxWorkers:       2,
		MaxLineSize:      DefaultMaxLineSize,
		MaxMultilineSize: DefaultMaxMultilineSize,
	}
}

func collectSearch(options Options, fs *fileSystem) (Summary, []Event, error) {
	var events []Event
	handle := func(event Event) error {
		events = append(events, event)
		return nil
	}
	if fs == nil {
		summary, err := Search(context.Background(), options, handle)
		return summary, events, err
	}
	summary, err := search(context.Background(), options, handle, *fs)
	return summary, events, err
}

func resultPaths(events []Event) []string {
	var paths []string
	for _, event := range events {
		if event.Result != nil {
			paths = append(paths, event.Result.Path)
		}
	}
	return paths
}

func containsResultPath(events []Event, path string) bool {
	for _, resultPath := range resultPaths(events) {
		if resultPath == path {
			return true
		}
	}
	return false
}

func writeSearcherFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

var _ io.Reader = (*readHookFile)(nil)
