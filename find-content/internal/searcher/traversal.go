package searcher

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type walkItem struct {
	path     string
	relative string
	entry    os.DirEntry
}

type walkQueue []walkItem

func (q walkQueue) Len() int           { return len(q) }
func (q walkQueue) Less(i, j int) bool { return q[i].relative < q[j].relative }
func (q walkQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }

func (q *walkQueue) Push(value any) {
	*q = append(*q, value.(walkItem))
}

func (q *walkQueue) Pop() any {
	old := *q
	last := len(old) - 1
	item := old[last]
	old[last] = walkItem{}
	*q = old[:last]
	return item
}

func walkCandidates(ctx context.Context, options Options, fs fileSystem, output chan<- candidate) error {
	defer close(output)
	defaultExcludes := stringSet(defaultExcludeDirs)
	userExcludeDirs := stringSet(options.ExcludeDirs)
	userExcludeFiles := stringSet(options.ExcludeFiles)
	classifier := newClassifier(options.SearchAll, options.Extensions)

	entries, err := fs.readDir(options.Root)
	if err != nil {
		return fmt.Errorf("read root directory %q: %w", options.Root, err)
	}
	queue := make(walkQueue, 0, len(entries))
	for _, entry := range entries {
		queue = append(queue, walkItem{
			path:     filepath.Join(options.Root, entry.Name()),
			relative: filepath.ToSlash(entry.Name()),
			entry:    entry,
		})
	}
	heap.Init(&queue)

	for queue.Len() > 0 {
		if ctx.Err() != nil {
			return nil
		}
		item := heap.Pop(&queue).(walkItem)
		entry := item.entry
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		var info os.FileInfo
		if entry.Type() == 0 {
			info, err = entry.Info()
			if err != nil {
				if !sendCandidate(ctx, output, candidate{path: item.path, err: fmt.Errorf("inspect entry: %w", err)}) {
					return nil
				}
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
		}
		isDirectory := entry.IsDir()
		if info != nil {
			isDirectory = info.IsDir()
		}
		if isDirectory {
			_, userExcluded := userExcludeDirs[entry.Name()]
			_, defaultExcluded := defaultExcludes[entry.Name()]
			if userExcluded || (!options.NoDefaultExclude && defaultExcluded) {
				continue
			}
			children, err := fs.readDir(item.path)
			if err != nil {
				if errors.Is(err, errUnsafeFileType) {
					continue
				}
				if !sendCandidate(ctx, output, candidate{path: item.path, err: fmt.Errorf("read directory: %w", err)}) {
					return nil
				}
				continue
			}
			for _, child := range children {
				relative := filepath.ToSlash(filepath.Join(item.relative, child.Name()))
				heap.Push(&queue, walkItem{
					path:     filepath.Join(item.path, child.Name()),
					relative: relative,
					entry:    child,
				})
			}
			continue
		}
		if _, excluded := userExcludeFiles[entry.Name()]; excluded {
			continue
		}
		if !classifier.accepts(item.path) {
			continue
		}
		if info == nil {
			info, err = entry.Info()
			if err != nil {
				if !sendCandidate(ctx, output, candidate{path: item.path, err: fmt.Errorf("inspect candidate: %w", err)}) {
					return nil
				}
				continue
			}
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if !sendCandidate(ctx, output, candidate{path: item.path, info: info}) {
			return nil
		}
	}
	return nil
}

func sendCandidate(ctx context.Context, output chan<- candidate, item candidate) bool {
	select {
	case output <- item:
		return true
	case <-ctx.Done():
		return false
	}
}
