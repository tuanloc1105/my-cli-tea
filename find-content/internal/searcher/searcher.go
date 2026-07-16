package searcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const (
	DefaultMaxLineSize      int64 = 64 << 20
	DefaultMaxMultilineSize int64 = 64 << 20
)

var defaultExcludeDirs = []string{
	".git",
	"__pycache__",
	"node_modules",
	".vscode",
	".idea",
	"target",
	"build",
	"dist",
}

var errUnsafeFileType = errors.New("path changed to a symlink or non-regular entry")

type Options struct {
	Root             string
	Keyword          string
	UseRegex         bool
	CaseSensitive    bool
	Multiline        bool
	Extensions       []string
	ExcludeDirs      []string
	ExcludeFiles     []string
	NoDefaultExclude bool
	SearchAll        bool
	MaxWorkers       int
	MaxLineSize      int64
	MaxMultilineSize int64
	MaxResults       int
}

type Result struct {
	Path       string
	Line       int
	EndLine    int
	ByteOffset int
	Content    string
}

type Diagnostic struct {
	Path string
	Err  error
}

type Event struct {
	Result     *Result
	Diagnostic *Diagnostic
}

type Summary struct {
	Matches       int
	PartialErrors int
	StoppedEarly  bool
}

type EventHandler func(Event) error

type fileHandle interface {
	io.Reader
	io.Closer
	Stat() (os.FileInfo, error)
}

type fileSystem struct {
	lstat   func(string) (os.FileInfo, error)
	open    func(string) (fileHandle, error)
	readDir func(string) ([]os.DirEntry, error)
}

func osFileSystem() fileSystem {
	return fileSystem{
		lstat:   os.Lstat,
		open:    openRegularFile,
		readDir: readDirectoryNoFollow,
	}
}

func DefaultWorkers() int {
	return min(runtime.NumCPU(), 4)
}

func Search(ctx context.Context, options Options, handle EventHandler) (Summary, error) {
	return search(ctx, options, handle, osFileSystem())
}

func search(ctx context.Context, options Options, handle EventHandler, fs fileSystem) (Summary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if handle == nil {
		handle = func(Event) error { return nil }
	}
	if options.Keyword == "" {
		return Summary{}, errors.New("keyword must not be empty")
	}
	if options.MaxWorkers < 1 {
		return Summary{}, errors.New("max workers must be at least 1")
	}
	if options.MaxLineSize <= 0 {
		return Summary{}, errors.New("max line size must be greater than 0")
	}
	if options.MaxMultilineSize <= 0 {
		return Summary{}, errors.New("max multiline size must be greater than 0")
	}
	if options.MaxResults < 0 {
		return Summary{}, errors.New("max results must not be negative")
	}
	if options.SearchAll && len(options.Extensions) > 0 {
		return Summary{}, errors.New("--all cannot be used with --extensions")
	}

	rootInfo, err := fs.lstat(options.Root)
	if err != nil {
		return Summary{}, fmt.Errorf("inspect root %q: %w", options.Root, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return Summary{}, fmt.Errorf("root %q is a symlink", options.Root)
	}
	if !rootInfo.IsDir() {
		return Summary{}, fmt.Errorf("root %q is not a directory", options.Root)
	}

	matcher, err := newMatcher(options.Keyword, options.UseRegex, options.CaseSensitive, options.Multiline)
	if err != nil {
		return Summary{}, fmt.Errorf("invalid search pattern: %w", err)
	}

	return runCoordinator(ctx, options, matcher, handle, fs)
}

func List(ctx context.Context, path string, showHidden bool, handle func(ListEntry) error) error {
	return list(ctx, path, showHidden, handle, osFileSystem())
}

type ListEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

func list(ctx context.Context, path string, showHidden bool, handle func(ListEntry) error, fs fileSystem) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if handle == nil {
		handle = func(ListEntry) error { return nil }
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	info, err := fs.lstat(path)
	if err != nil {
		return fmt.Errorf("inspect list directory %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("list directory %q is a symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("list path %q is not a directory", path)
	}

	entries, err := fs.readDir(path)
	if err != nil {
		return fmt.Errorf("read list directory %q: %w", path, err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect list entry %q: %w", filepath.Join(path, entry.Name()), err)
		}
		if !showHidden && isHidden(filepath.Join(path, entry.Name()), entryInfo) {
			continue
		}
		if err := handle(ListEntry{Name: entry.Name(), IsDir: entryInfo.IsDir(), Size: entryInfo.Size()}); err != nil {
			return fmt.Errorf("render list entry %q: %w", entry.Name(), err)
		}
	}
	return nil
}
