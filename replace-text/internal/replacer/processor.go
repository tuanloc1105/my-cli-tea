package replacer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxStoredFailures = 16

type tempFile interface {
	io.Writer
	Name() string
	Sync() error
	Close() error
}

type fileOps interface {
	Lstat(string) (os.FileInfo, error)
	Open(string) (*os.File, error)
	CreateTemp(string, string) (tempFile, error)
	Remove(string) error
	Rename(string, string) error
	Chown(string, int, int) error
	Chmod(string, os.FileMode) error
	Chtimes(string, time.Time, time.Time) error
	SyncParent(string) error
	WalkDir(string, fs.WalkDirFunc) error
}

type osFileOps struct{}

func (osFileOps) Lstat(path string) (os.FileInfo, error) { return os.Lstat(path) }
func (osFileOps) Open(path string) (*os.File, error)     { return os.Open(path) }
func (osFileOps) CreateTemp(dir, pattern string) (tempFile, error) {
	return os.CreateTemp(dir, pattern)
}
func (osFileOps) Remove(path string) error                  { return os.Remove(path) }
func (osFileOps) Rename(oldPath, newPath string) error      { return os.Rename(oldPath, newPath) }
func (osFileOps) Chown(path string, uid, gid int) error     { return os.Chown(path, uid, gid) }
func (osFileOps) Chmod(path string, mode os.FileMode) error { return os.Chmod(path, mode) }
func (osFileOps) Chtimes(path string, atime, mtime time.Time) error {
	return os.Chtimes(path, atime, mtime)
}
func (osFileOps) SyncParent(path string) error { return syncParentDirectory(path) }
func (osFileOps) WalkDir(root string, walkFn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, walkFn)
}

type processor struct {
	ops fileOps
}

// Run processes a single file or recursively processes a directory. Reporter
// calls and summary aggregation are serialized by the coordinator.
func Run(ctx context.Context, options Options, reporter Reporter) (Summary, error) {
	return processor{ops: osFileOps{}}.run(ctx, options, reporter)
}

func (p processor) run(ctx context.Context, options Options, reporter Reporter) (Summary, error) {
	if err := validateOptions(ctx, options); err != nil {
		return Summary{}, err
	}
	options.Search = append([]byte(nil), options.Search...)
	options.Replacement = append([]byte(nil), options.Replacement...)

	info, err := p.ops.Lstat(options.Target)
	if err != nil {
		return Summary{}, fmt.Errorf("inspect target %q: %w", options.Target, err)
	}

	if bytes.Equal(options.Search, options.Replacement) {
		outcome := Outcome{
			Path:   options.Target,
			Kind:   OutcomeNoMatch,
			Detail: "search and replacement text are identical; nothing to do",
		}
		return finishOutcome(outcome, reporter, info.IsDir())
	}

	if !info.IsDir() {
		outcome := p.processFile(ctx, options.Target, options)
		return finishOutcome(outcome, reporter, false)
	}
	return p.processDirectory(ctx, options, reporter)
}

func validateOptions(ctx context.Context, options Options) error {
	if ctx == nil {
		return errors.New("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if options.Target == "" {
		return errors.New("target is empty")
	}
	if len(options.Search) == 0 {
		return errors.New("search text is empty")
	}
	if options.MaxInputSize < 0 {
		return errors.New("maximum input size must not be negative")
	}
	if options.MaxOutputSize < 0 {
		return errors.New("maximum output size must not be negative")
	}
	if options.Workers < 1 {
		return errors.New("worker count must be at least 1")
	}
	return nil
}

func (p processor) processDirectory(ctx context.Context, options Options, reporter Reporter) (Summary, error) {
	jobs := make(chan string, options.Workers)
	results := make(chan Outcome, options.Workers)

	var workers sync.WaitGroup
	for range options.Workers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for path := range jobs {
				if err := ctx.Err(); err != nil {
					results <- failedOutcome(path, err)
					continue
				}
				results <- p.processFile(ctx, path, options)
			}
		}()
	}

	go func() {
		walkErr := p.ops.WalkDir(options.Target, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				results <- failedOutcome(path, fmt.Errorf("traverse: %w", walkErr))
				if entry != nil && entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if path != options.Target {
					switch entry.Name() {
					case ".git", ".svn", ".hg":
						return fs.SkipDir
					}
				}
				return nil
			}
			if strings.HasSuffix(entry.Name(), ".bak") {
				results <- Outcome{
					Path:   path,
					Kind:   OutcomeSkipped,
					Reason: SkipBackupFile,
					Detail: "backup files found during recursive traversal are not processed",
				}
				return nil
			}

			select {
			case jobs <- path:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(jobs)
		workers.Wait()
		if walkErr != nil {
			results <- failedOutcome(options.Target, fmt.Errorf("walk directory: %w", walkErr))
		}
		close(results)
	}()

	accumulator := outcomeAccumulator{
		summary:           Summary{TargetIsDirectory: true},
		reporter:          reporter,
		targetIsDirectory: true,
	}
	for outcome := range results {
		if err := accumulator.record(outcome); err != nil {
			return accumulator.summary, err
		}
	}
	return accumulator.finish()
}

type outcomeAccumulator struct {
	summary           Summary
	partial           PartialError
	reporter          Reporter
	targetIsDirectory bool
}

func (a *outcomeAccumulator) record(outcome Outcome) error {
	outcome.TargetIsDirectory = a.targetIsDirectory
	a.summary.TargetIsDirectory = a.targetIsDirectory
	if err := a.summary.record(outcome); err != nil {
		return fmt.Errorf("invalid outcome for %q: %w", outcome.Path, err)
	}
	if a.reporter != nil {
		a.reporter.Report(outcome)
	}
	addFailure(&a.partial, outcome)
	return nil
}

func (a *outcomeAccumulator) finish() (Summary, error) {
	if a.partial.Total > 0 {
		return a.summary, &a.partial
	}
	return a.summary, nil
}

func finishOutcome(outcome Outcome, reporter Reporter, targetIsDirectory bool) (Summary, error) {
	accumulator := outcomeAccumulator{reporter: reporter, targetIsDirectory: targetIsDirectory}
	if err := accumulator.record(outcome); err != nil {
		return accumulator.summary, err
	}
	return accumulator.finish()
}

func addFailure(partial *PartialError, outcome Outcome) {
	if outcome.Kind != OutcomeFailed {
		return
	}
	partial.Total++
	if len(partial.Failures) < maxStoredFailures {
		partial.Failures = append(partial.Failures, PathError{Path: outcome.Path, Err: outcome.Err})
	}
}

func (p processor) processFile(ctx context.Context, path string, options Options) Outcome {
	if err := ctx.Err(); err != nil {
		return failedOutcome(path, err)
	}
	info, err := p.ops.Lstat(path)
	if err != nil {
		return failedOutcome(path, fmt.Errorf("inspect file: %w", err))
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return skippedOutcome(path, SkipSymlink, "symbolic links are not processed")
	}
	if !info.Mode().IsRegular() {
		return skippedOutcome(path, SkipNonRegular, "non-regular files are not processed")
	}

	source, metadata, err := p.openSource(path)
	if err != nil {
		return failedOutcome(path, err)
	}
	if metadata.hasMultipleLinks() {
		closeErr := source.Close()
		if closeErr != nil {
			return failedOutcome(path, fmt.Errorf("close source: %w", closeErr))
		}
		return skippedOutcome(path, SkipHardlink, "files with multiple hard links are not processed")
	}

	metrics, analyzeErr := analyzeStream(
		contextReader{ctx: ctx, reader: source},
		options.Search,
		options.Replacement,
		options.MaxInputSize,
		options.MaxOutputSize,
	)
	if err := finishSourceRead(source, metadata); err != nil {
		return failedOutcome(path, fmt.Errorf("finish source analysis: %w", err))
	}
	if analyzeErr != nil {
		if reason, detail, ok := streamSkip(analyzeErr); ok {
			return skippedOutcome(path, reason, detail)
		}
		return failedOutcome(path, fmt.Errorf("analyze source: %w", analyzeErr))
	}
	if err := ctx.Err(); err != nil {
		return failedOutcome(path, err)
	}
	if metrics.Matches == 0 {
		return Outcome{Path: path, Kind: OutcomeNoMatch, Detail: "search text was not found"}
	}
	if options.DryRun {
		return Outcome{
			Path:         path,
			Kind:         OutcomeWouldModify,
			Detail:       "file would be modified",
			Replacements: metrics.Matches,
		}
	}
	return p.rewriteFile(ctx, path, options, metadata, metrics)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func finishSourceRead(source *os.File, before fileMetadata) error {
	after, snapshotErr := snapshotFileMetadata(source)
	var stateErr error
	var restoreErr error
	if snapshotErr == nil {
		if !before.unchangedForCommit(after) {
			stateErr = ErrConcurrentModification
		} else {
			restoreErr = restoreSourceTimes(source, before)
		}
	}
	closeErr := source.Close()
	return errors.Join(
		wrapOperation("snapshot source after read", snapshotErr),
		stateErr,
		restoreErr,
		wrapOperation("close source after read", closeErr),
	)
}

func (p processor) openSource(path string) (*os.File, fileMetadata, error) {
	entryInfo, err := p.ops.Lstat(path)
	if err != nil {
		return nil, fileMetadata{}, fmt.Errorf("inspect source: %w", err)
	}
	if entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() {
		return nil, fileMetadata{}, ErrConcurrentModification
	}

	file, err := p.ops.Open(path)
	if err != nil {
		return nil, fileMetadata{}, fmt.Errorf("open source: %w", err)
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil || !os.SameFile(entryInfo, openedInfo) {
		closeErr := file.Close()
		return nil, fileMetadata{}, errors.Join(
			ErrConcurrentModification,
			wrapOperation("stat opened source", statErr),
			wrapOperation("close source", closeErr),
		)
	}
	metadata, err := snapshotFileMetadata(file)
	if err != nil {
		closeErr := file.Close()
		return nil, fileMetadata{}, errors.Join(err, wrapOperation("close source", closeErr))
	}
	return file, metadata, nil
}

func streamSkip(err error) (SkipReason, string, bool) {
	switch {
	case errors.Is(err, errStreamInputTooLarge):
		return SkipInputTooLarge, err.Error(), true
	case errors.Is(err, errStreamOutputTooLarge), errors.Is(err, errStreamSizeOverflow):
		return SkipOutputTooLarge, err.Error(), true
	case errors.Is(err, errStreamNUL):
		return SkipBinaryNUL, "input contains a NUL byte", true
	case errors.Is(err, errStreamInvalidUTF8):
		return SkipInvalidUTF8, "input is not valid UTF-8", true
	default:
		return "", "", false
	}
}

func skippedOutcome(path string, reason SkipReason, detail string) Outcome {
	return Outcome{Path: path, Kind: OutcomeSkipped, Reason: reason, Detail: detail}
}

func failedOutcome(path string, err error) Outcome {
	return Outcome{Path: path, Kind: OutcomeFailed, Err: err}
}

type tempArtifact struct {
	file tempFile
	path string
}

func (a *tempArtifact) close() error {
	if a.file == nil {
		return nil
	}
	err := a.file.Close()
	a.file = nil
	return err
}

func (a *tempArtifact) cleanup(ops fileOps) error {
	closeErr := a.close()
	var removeErr error
	if a.path != "" {
		removeErr = ops.Remove(a.path)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		a.path = ""
	}
	return errors.Join(
		wrapOperation("close temporary file during cleanup", closeErr),
		wrapOperation("remove temporary file during cleanup", removeErr),
	)
}

func (p processor) rewriteFile(ctx context.Context, path string, options Options, metadata fileMetadata, analyzed streamMetrics) (outcome Outcome) {
	outcome = failedOutcome(path, errors.New("replacement did not complete"))
	var replacementTemp tempArtifact
	var backupTemp tempArtifact
	defer func() {
		cleanupErr := errors.Join(replacementTemp.cleanup(p.ops), backupTemp.cleanup(p.ops))
		if cleanupErr == nil {
			return
		}
		if outcome.Kind == OutcomeFailed {
			outcome.Err = errors.Join(outcome.Err, cleanupErr)
			return
		}
		outcome = failedOutcome(path, cleanupErr)
	}()
	if err := ctx.Err(); err != nil {
		return failedOutcome(path, err)
	}

	var backupPath string
	var backupBefore pathSnapshot
	if options.Backup {
		backupPath = path + ".bak"
		var err error
		backupBefore, err = p.snapshotPath(backupPath)
		if err != nil {
			return failedOutcome(path, fmt.Errorf("snapshot backup target: %w", err))
		}
	}

	replacementFile, err := p.ops.CreateTemp(filepath.Dir(path), ".replace-text-output-*.tmp")
	if err != nil {
		return failedOutcome(path, fmt.Errorf("create replacement temporary file: %w", err))
	}
	replacementTemp = tempArtifact{file: replacementFile, path: replacementFile.Name()}

	if options.Backup {
		backupFile, createErr := p.ops.CreateTemp(filepath.Dir(path), ".replace-text-backup-*.tmp")
		if createErr != nil {
			return failedOutcome(path, fmt.Errorf("create backup temporary file: %w", createErr))
		}
		backupTemp = tempArtifact{file: backupFile, path: backupFile.Name()}
	}

	source, beforeWrite, err := p.openSource(path)
	if err != nil {
		return failedOutcome(path, err)
	}
	if !metadata.unchangedForCommit(beforeWrite) {
		_ = source.Close()
		return failedOutcome(path, fmt.Errorf("%w: metadata changed before write pass", ErrConcurrentModification))
	}

	var backupWriter io.Writer
	if backupTemp.file != nil {
		backupWriter = backupTemp.file
	}
	written, writeErr := writeReplacedStream(
		contextReader{ctx: ctx, reader: source},
		replacementTemp.file,
		backupWriter,
		options.Search,
		options.Replacement,
	)
	readStateErr := finishSourceRead(source, metadata)
	if writeErr != nil || readStateErr != nil {
		return failedOutcome(path, errors.Join(
			wrapOperation("write replacement temporary file", writeErr),
			wrapOperation("finish source write pass", readStateErr),
		))
	}
	if written != analyzed {
		return failedOutcome(path, fmt.Errorf("%w: write metrics differ from analysis", ErrConcurrentModification))
	}

	if err := applyFileMetadata(p.ops, replacementTemp.path, metadata); err != nil {
		return failedOutcome(path, err)
	}
	if backupTemp.file != nil {
		if err := applyFileMetadata(p.ops, backupTemp.path, metadata); err != nil {
			return failedOutcome(path, err)
		}
	}
	if err := replacementTemp.file.Sync(); err != nil {
		return failedOutcome(path, fmt.Errorf("sync replacement temporary file: %w", err))
	}
	if backupTemp.file != nil {
		if err := backupTemp.file.Sync(); err != nil {
			return failedOutcome(path, fmt.Errorf("sync backup temporary file: %w", err))
		}
	}
	if err := errors.Join(
		wrapOperation("close replacement temporary file", replacementTemp.close()),
		wrapOperation("close backup temporary file", backupTemp.close()),
	); err != nil {
		return failedOutcome(path, err)
	}

	if err := ctx.Err(); err != nil {
		return failedOutcome(path, err)
	}
	if err := p.verifySource(ctx, path, metadata, analyzed, options.Search, options.Replacement); err != nil {
		return failedOutcome(path, err)
	}
	if options.Backup {
		unchanged, err := p.pathUnchanged(backupPath, backupBefore)
		if err != nil {
			return failedOutcome(path, fmt.Errorf("verify backup target: %w", err))
		}
		if !unchanged {
			return failedOutcome(path, fmt.Errorf("%w: backup target changed", ErrConcurrentModification))
		}
		if err := ctx.Err(); err != nil {
			return failedOutcome(path, err)
		}
		if err := p.ops.Rename(backupTemp.path, backupPath); err != nil {
			return failedOutcome(path, fmt.Errorf("commit backup: %w", err))
		}
		backupTemp.path = ""
		if err := p.ops.SyncParent(backupPath); err != nil {
			return failedOutcome(path, fmt.Errorf("sync parent after backup commit: %w", err))
		}
		if err := p.verifySource(ctx, path, metadata, analyzed, options.Search, options.Replacement); err != nil {
			return failedOutcome(path, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return failedOutcome(path, err)
	}
	if err := p.ops.Rename(replacementTemp.path, path); err != nil {
		return failedOutcome(path, fmt.Errorf("commit replacement: %w", err))
	}
	replacementTemp.path = ""
	if err := p.ops.SyncParent(path); err != nil {
		return failedOutcome(path, errors.Join(
			ErrPostCommitDurability,
			fmt.Errorf("sync parent after replacement commit: %w", err),
		))
	}

	return Outcome{
		Path:         path,
		Kind:         OutcomeModified,
		Detail:       "file was modified",
		Replacements: analyzed.Matches,
		BackupPath:   backupPath,
	}
}

func (p processor) verifySource(ctx context.Context, path string, metadata fileMetadata, expected streamMetrics, search, replacement []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, beforeVerify, err := p.openSource(path)
	if err != nil {
		return fmt.Errorf("verify source before commit: %w", err)
	}
	if !metadata.unchangedForCommit(beforeVerify) {
		_ = source.Close()
		return ErrConcurrentModification
	}

	metrics, analyzeErr := analyzeUnlimitedStream(contextReader{ctx: ctx, reader: source}, search, replacement)
	readStateErr := finishSourceRead(source, metadata)
	if analyzeErr != nil || readStateErr != nil {
		return errors.Join(
			wrapOperation("verify source content", analyzeErr),
			wrapOperation("finish verified source read", readStateErr),
		)
	}
	if metrics != expected {
		return ErrConcurrentModification
	}
	return nil
}

type pathSnapshot struct {
	exists bool
	info   os.FileInfo
}

func (p processor) snapshotPath(path string) (pathSnapshot, error) {
	info, err := p.ops.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return pathSnapshot{}, nil
	}
	if err != nil {
		return pathSnapshot{}, err
	}
	return pathSnapshot{exists: true, info: info}, nil
}

func (p processor) pathUnchanged(path string, before pathSnapshot) (bool, error) {
	after, err := p.snapshotPath(path)
	if err != nil {
		return false, err
	}
	if before.exists != after.exists {
		return false, nil
	}
	if !before.exists {
		return true, nil
	}
	return os.SameFile(before.info, after.info) &&
		before.info.Mode() == after.info.Mode() &&
		before.info.Size() == after.info.Size() &&
		before.info.ModTime().Equal(after.info.ModTime()), nil
}

func wrapOperation(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
