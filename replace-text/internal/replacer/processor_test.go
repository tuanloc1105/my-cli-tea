package replacer

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestRunSingleFileModifyPreservesMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeTestFile(t, path, "old and old", 0o751)
	wantTime := time.Unix(1_700_000_000, 123_000_000)
	if err := os.Chtimes(path, wantTime, wantTime); err != nil {
		t.Fatal(err)
	}
	wantMode := snapshotPathMetadata(t, path).mode.Perm()

	var outcomes []Outcome
	summary, err := Run(context.Background(), testOptions(path), ReporterFunc(func(outcome Outcome) {
		outcomes = append(outcomes, outcome)
	}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := readTestFile(t, path); got != "new and new" {
		t.Fatalf("content = %q", got)
	}
	if summary.Scanned != 1 || summary.Modified != 1 || summary.Replacements != 2 || len(outcomes) != 1 {
		t.Fatalf("summary=%+v outcomes=%+v", summary, outcomes)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != wantMode {
		t.Fatalf("mode = %v, want %v", info.Mode().Perm(), wantMode)
	}
	if !info.ModTime().Equal(wantTime) {
		t.Fatalf("mtime = %v, want %v", info.ModTime(), wantTime)
	}
	assertNoTemps(t, dir)
}

func TestRunDryRunCreatesNothingAndChangesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	backupPath := path + ".bak"
	writeTestFile(t, path, "old", 0o640)
	writeTestFile(t, backupPath, "existing backup", 0o600)
	wantTime := time.Unix(1_700_000_100, 0)
	if err := os.Chtimes(path, wantTime, wantTime); err != nil {
		t.Fatal(err)
	}
	beforeMetadata := snapshotPathMetadata(t, path)
	options := testOptions(path)
	options.Backup = true
	options.DryRun = true

	summary, err := Run(context.Background(), options, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.WouldModify != 1 || summary.Replacements != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	afterMetadata := snapshotPathMetadata(t, path)
	if !afterMetadata.accessTime.Equal(beforeMetadata.accessTime) ||
		!afterMetadata.modificationTime.Equal(beforeMetadata.modificationTime) ||
		afterMetadata.mode != beforeMetadata.mode {
		t.Fatalf("metadata changed: before=%+v after=%+v", beforeMetadata, afterMetadata)
	}
	if got := readTestFile(t, path); got != "old" {
		t.Fatalf("source = %q", got)
	}
	if got := readTestFile(t, backupPath); got != "existing backup" {
		t.Fatalf("backup = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != beforeMetadata.mode.Perm() {
		t.Fatalf("mode = %v, want %v", info.Mode().Perm(), beforeMetadata.mode.Perm())
	}
	if !info.ModTime().Equal(wantTime) {
		t.Fatalf("mtime = %v, want %v", info.ModTime(), wantTime)
	}
	assertNoTemps(t, dir)
}

func TestRunCanceledContextDoesNotModifySingleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	writeTestFile(t, path, "old", 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	summary, err := Run(ctx, testOptions(path), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if summary.Scanned != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := readTestFile(t, path); got != "old" {
		t.Fatalf("source = %q", got)
	}
}

func TestCancellationBeforeCommitPreservesSourceAndCleansTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeTestFile(t, path, "old", 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	ops := &faultFileOps{afterChtimes: func(tempPath string) {
		if tempPath != path {
			cancel()
		}
	}}

	_, err := (processor{ops: ops}).run(ctx, testOptions(path), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if got := readTestFile(t, path); got != "old" {
		t.Fatalf("source = %q", got)
	}
	assertNoTemps(t, dir)
}

func TestRunBackupReplacesExistingBackupOnlyAfterItIsComplete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	backupPath := path + ".bak"
	writeTestFile(t, path, "old", 0o640)
	writeTestFile(t, backupPath, "previous backup", 0o600)
	wantBackupMode := snapshotPathMetadata(t, path).mode.Perm()
	options := testOptions(path)
	options.Backup = true

	var outcome Outcome
	summary, err := Run(context.Background(), options, ReporterFunc(func(got Outcome) {
		outcome = got
	}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Modified != 1 || outcome.BackupPath != backupPath {
		t.Fatalf("summary=%+v outcome=%+v", summary, outcome)
	}
	if got := readTestFile(t, path); got != "new" {
		t.Fatalf("source = %q", got)
	}
	if got := readTestFile(t, backupPath); got != "old" {
		t.Fatalf("backup = %q", got)
	}
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if backupInfo.Mode().Perm() != wantBackupMode {
		t.Fatalf("backup mode = %v, want %v", backupInfo.Mode().Perm(), wantBackupMode)
	}
	assertNoTemps(t, dir)
}

func TestRunPolicySkipsAndLimits(t *testing.T) {
	tests := []struct {
		name      string
		content   []byte
		configure func(*Options)
		reason    SkipReason
	}{
		{name: "NUL after first buffer", content: append(bytes.Repeat([]byte("a"), streamBufferSize+1), 0), reason: SkipBinaryNUL},
		{name: "invalid UTF-8 after first buffer", content: append(bytes.Repeat([]byte("a"), streamBufferSize+1), 0xff), reason: SkipInvalidUTF8},
		{name: "input over cap", content: []byte("old"), configure: func(options *Options) { options.MaxInputSize = 2 }, reason: SkipInputTooLarge},
		{name: "output over cap", content: []byte("old"), configure: func(options *Options) { options.MaxOutputSize = 2 }, reason: SkipOutputTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "file.txt")
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			options := testOptions(path)
			if test.configure != nil {
				test.configure(&options)
			}
			before := append([]byte(nil), test.content...)
			summary, err := Run(context.Background(), options, nil)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if summary.Skipped[test.reason] != 1 {
				t.Fatalf("summary = %+v, want skip %q", summary, test.reason)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatal("policy skip changed source")
			}
			assertNoTemps(t, filepath.Dir(path))
		})
	}
}

func TestRunAllowsExactInputAndOutputCaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	writeTestFile(t, path, "old", 0o600)
	options := testOptions(path)
	options.MaxInputSize = 3
	options.MaxOutputSize = 3

	if _, err := Run(context.Background(), options, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := readTestFile(t, path); got != "new" {
		t.Fatalf("content = %q", got)
	}
}

func TestRunDirectoryContinuesAfterPartialFailureAndSkipsBackups(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	bad := filepath.Join(dir, "bad.txt")
	backup := filepath.Join(dir, "ignored.bak")
	writeTestFile(t, good, "old", 0o600)
	writeTestFile(t, bad, "old", 0o600)
	writeTestFile(t, backup, "old", 0o600)

	boom := errors.New("injected open failure")
	ops := &faultFileOps{openFailures: map[string]error{bad: boom}}
	var outcomes []Outcome
	summary, err := processor{ops: ops}.run(context.Background(), testOptions(dir), ReporterFunc(func(outcome Outcome) {
		outcomes = append(outcomes, outcome)
	}))
	var partial *PartialError
	if !errors.As(err, &partial) || partial.Total != 1 || !errors.Is(err, boom) {
		t.Fatalf("error = %#v, want one partial failure wrapping %v", err, boom)
	}
	if summary.Scanned != 3 || summary.Modified != 1 || summary.Failed != 1 || summary.Skipped[SkipBackupFile] != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := readTestFile(t, good); got != "new" {
		t.Fatalf("good file = %q", got)
	}
	if got := readTestFile(t, bad); got != "old" {
		t.Fatalf("bad file = %q", got)
	}
	if got := readTestFile(t, backup); got != "old" {
		t.Fatalf("backup file = %q", got)
	}
	if len(outcomes) != 3 {
		t.Fatalf("outcomes = %+v", outcomes)
	}
}

func TestRunWorkerCountsProduceSameSummaryAndContent(t *testing.T) {
	makeFixture := func() string {
		dir := t.TempDir()
		for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
			writeTestFile(t, filepath.Join(dir, name), "old old", 0o600)
		}
		writeTestFile(t, filepath.Join(dir, "skip.bak"), "old", 0o600)
		return dir
	}
	one := makeFixture()
	four := makeFixture()
	optionsOne := testOptions(one)
	optionsOne.Workers = 1
	optionsFour := testOptions(four)
	optionsFour.Workers = 4

	summaryOne, err := Run(context.Background(), optionsOne, nil)
	if err != nil {
		t.Fatal(err)
	}
	summaryFour, err := Run(context.Background(), optionsFour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(summaryOne, summaryFour) {
		t.Fatalf("summaries differ: one=%+v four=%+v", summaryOne, summaryFour)
	}
	if got, want := directoryContents(t, one), directoryContents(t, four); !reflect.DeepEqual(got, want) {
		t.Fatalf("contents differ: one=%v four=%v", got, want)
	}
}

func TestRunEmptyDirectoryRetainsAuthoritativeTargetKind(t *testing.T) {
	dir := t.TempDir()
	summary, err := Run(context.Background(), testOptions(dir), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !summary.TargetIsDirectory || summary.Scanned != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestRunSameTextChecksTargetButDoesNotTraverse(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "file.txt"), "old", 0o600)
	ops := &faultFileOps{walkErr: errors.New("walk must not run")}
	options := testOptions(dir)
	options.Replacement = []byte("old")

	summary, err := processor{ops: ops}.run(context.Background(), options, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.Scanned != 1 || summary.NoMatch != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := readTestFile(t, filepath.Join(dir, "file.txt")); got != "old" {
		t.Fatalf("content = %q", got)
	}

	options.Target = filepath.Join(dir, "missing")
	if _, err := (processor{ops: ops}).run(context.Background(), options, nil); err == nil {
		t.Fatal("same-text run succeeded for missing target")
	}
}

func TestRunDirectBackupFileIsProcessed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "explicit.bak")
	writeTestFile(t, path, "old", 0o600)
	if _, err := Run(context.Background(), testOptions(path), nil); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, path); got != "new" {
		t.Fatalf("content = %q", got)
	}
}

func TestRunSurfacesTraversalError(t *testing.T) {
	dir := t.TempDir()
	walkErr := errors.New("injected walk failure")
	ops := &faultFileOps{walkErr: walkErr}
	summary, err := processor{ops: ops}.run(context.Background(), testOptions(dir), nil)
	if !errors.Is(err, walkErr) {
		t.Fatalf("error = %v, want %v", err, walkErr)
	}
	if summary.Failed != 1 || summary.Scanned != 1 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestInjectedFailuresBeforeMainCommitPreserveSourceAndCleanTemps(t *testing.T) {
	stages := []string{"create", "write", "sync", "close", "rename"}
	if runtime.GOOS != "windows" {
		stages = append(stages, "chown", "chmod", "chtimes")
	}

	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file.txt")
			writeTestFile(t, path, "old", 0o600)
			injected := errors.New("injected " + stage + " failure")
			ops := &faultFileOps{failStage: stage, failErr: injected}

			summary, err := processor{ops: ops}.run(context.Background(), testOptions(path), nil)
			if !errors.Is(err, injected) {
				t.Fatalf("error = %v, want %v", err, injected)
			}
			if summary.Failed != 1 {
				t.Fatalf("summary = %+v", summary)
			}
			if got := readTestFile(t, path); got != "old" {
				t.Fatalf("source = %q", got)
			}
			assertNoTemps(t, dir)
		})
	}
}

func TestWindowsMetadataFailuresAreBestEffort(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific metadata contract")
	}

	for _, stage := range []string{"chown", "chmod", "chtimes"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file.txt")
			writeTestFile(t, path, "old", 0o600)
			ops := &faultFileOps{
				failStage: stage,
				failErr:   errors.New("injected " + stage + " failure"),
			}

			summary, err := processor{ops: ops}.run(context.Background(), testOptions(path), nil)
			if err != nil {
				t.Fatalf("run returned best-effort metadata error: %v", err)
			}
			if summary.Modified != 1 || summary.Failed != 0 {
				t.Fatalf("summary = %+v", summary)
			}
			if got := readTestFile(t, path); got != "new" {
				t.Fatalf("source = %q", got)
			}
			assertNoTemps(t, dir)
		})
	}
}

func TestBackupAndCommitFailureStates(t *testing.T) {
	t.Run("backup rename failure keeps source and old backup", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		writeTestFile(t, path, "old", 0o600)
		writeTestFile(t, path+".bak", "previous", 0o600)
		injected := errors.New("backup rename failed")
		ops := &faultFileOps{failErr: injected, failRenameAt: 1}
		options := testOptions(path)
		options.Backup = true

		if _, err := (processor{ops: ops}).run(context.Background(), options, nil); !errors.Is(err, injected) {
			t.Fatalf("error = %v", err)
		}
		if got := readTestFile(t, path); got != "old" {
			t.Fatalf("source = %q", got)
		}
		if got := readTestFile(t, path+".bak"); got != "previous" {
			t.Fatalf("backup = %q", got)
		}
		assertNoTemps(t, dir)
	})

	t.Run("replacement rename failure leaves committed backup and source", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		writeTestFile(t, path, "old", 0o600)
		injected := errors.New("replacement rename failed")
		ops := &faultFileOps{failErr: injected, failRenameAt: 2}
		options := testOptions(path)
		options.Backup = true

		if _, err := (processor{ops: ops}).run(context.Background(), options, nil); !errors.Is(err, injected) {
			t.Fatalf("error = %v", err)
		}
		if got := readTestFile(t, path); got != "old" {
			t.Fatalf("source = %q", got)
		}
		if got := readTestFile(t, path+".bak"); got != "old" {
			t.Fatalf("backup = %q", got)
		}
		assertNoTemps(t, dir)
	})

	t.Run("post-commit sync failure reports durability error without rollback", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		writeTestFile(t, path, "old", 0o600)
		injected := errors.New("directory sync failed")
		ops := &faultFileOps{failErr: injected, failSyncParentAt: 1}

		_, err := processor{ops: ops}.run(context.Background(), testOptions(path), nil)
		if !errors.Is(err, injected) || !errors.Is(err, ErrPostCommitDurability) {
			t.Fatalf("error = %v", err)
		}
		if got := readTestFile(t, path); got != "new" {
			t.Fatalf("visible replacement was rolled back: %q", got)
		}
		assertNoTemps(t, dir)
	})

	t.Run("backup sync failure leaves source and committed backup", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		writeTestFile(t, path, "old", 0o600)
		writeTestFile(t, path+".bak", "previous", 0o600)
		injected := errors.New("backup directory sync failed")
		ops := &faultFileOps{failErr: injected, failSyncParentAt: 1}
		options := testOptions(path)
		options.Backup = true

		_, err := (processor{ops: ops}).run(context.Background(), options, nil)
		if !errors.Is(err, injected) {
			t.Fatalf("error = %v", err)
		}
		if got := readTestFile(t, path); got != "old" {
			t.Fatalf("source = %q", got)
		}
		if got := readTestFile(t, path+".bak"); got != "old" {
			t.Fatalf("backup = %q", got)
		}
		assertNoTemps(t, dir)
	})
}

func TestConcurrentSourceChangeIsDetectedBeforeCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeTestFile(t, path, "old", 0o600)
	ops := &faultFileOps{afterChtimes: func(tempPath string) {
		if tempPath != path {
			if err := os.WriteFile(path, []byte("external change"), 0o600); err != nil {
				t.Errorf("external write: %v", err)
			}
		}
	}}

	_, err := processor{ops: ops}.run(context.Background(), testOptions(path), nil)
	if !errors.Is(err, ErrConcurrentModification) {
		t.Fatalf("error = %v, want concurrent modification", err)
	}
	if got := readTestFile(t, path); got != "external change" {
		t.Fatalf("source = %q", got)
	}
	assertNoTemps(t, dir)
}

func testOptions(target string) Options {
	return Options{
		Target:        target,
		Search:        []byte("old"),
		Replacement:   []byte("new"),
		MaxInputSize:  DefaultMaxInputSize,
		MaxOutputSize: 0,
		Workers:       2,
	}
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".replace-text-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func directoryContents(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	contents := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		contents = append(contents, entry.Name()+"="+readTestFile(t, filepath.Join(dir, entry.Name())))
	}
	sort.Strings(contents)
	return contents
}

type faultFileOps struct {
	osFileOps
	failStage        string
	failErr          error
	openFailures     map[string]error
	walkErr          error
	failRenameAt     int
	failSyncParentAt int
	afterChtimes     func(string)

	mu              sync.Mutex
	renameCalls     int
	syncParentCalls int
}

func (o *faultFileOps) Open(path string) (*os.File, error) {
	if err := o.openFailures[path]; err != nil {
		return nil, err
	}
	return o.osFileOps.Open(path)
}

func (o *faultFileOps) CreateTemp(dir, pattern string) (tempFile, error) {
	if o.failStage == "create" {
		return nil, o.failErr
	}
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return &faultTempFile{File: file, ops: o}, nil
}

func (o *faultFileOps) Chown(path string, uid, gid int) error {
	if o.failStage == "chown" {
		return o.failErr
	}
	return os.Chown(path, uid, gid)
}

func (o *faultFileOps) Chmod(path string, mode os.FileMode) error {
	if o.failStage == "chmod" {
		return o.failErr
	}
	return os.Chmod(path, mode)
}

func (o *faultFileOps) Chtimes(path string, atime, mtime time.Time) error {
	if o.failStage == "chtimes" {
		return o.failErr
	}
	err := os.Chtimes(path, atime, mtime)
	if err == nil && o.afterChtimes != nil {
		o.afterChtimes(path)
	}
	return err
}

func (o *faultFileOps) Rename(oldPath, newPath string) error {
	o.mu.Lock()
	o.renameCalls++
	call := o.renameCalls
	o.mu.Unlock()
	if o.failStage == "rename" || o.failRenameAt == call {
		return o.failErr
	}
	return os.Rename(oldPath, newPath)
}

func (o *faultFileOps) SyncParent(path string) error {
	o.mu.Lock()
	o.syncParentCalls++
	call := o.syncParentCalls
	o.mu.Unlock()
	if o.failStage == "sync-dir" || o.failSyncParentAt == call {
		return o.failErr
	}
	return syncParentDirectory(path)
}

func (o *faultFileOps) WalkDir(root string, walkFn fs.WalkDirFunc) error {
	if o.walkErr != nil {
		return o.walkErr
	}
	return filepath.WalkDir(root, walkFn)
}

type faultTempFile struct {
	*os.File
	ops *faultFileOps
}

func (f *faultTempFile) Write(data []byte) (int, error) {
	if f.ops.failStage == "write" {
		return 0, f.ops.failErr
	}
	return f.File.Write(data)
}

func (f *faultTempFile) Sync() error {
	if f.ops.failStage == "sync" {
		return f.ops.failErr
	}
	return f.File.Sync()
}

func (f *faultTempFile) Close() error {
	err := f.File.Close()
	if f.ops.failStage == "close" {
		return errors.Join(err, f.ops.failErr)
	}
	return err
}

var _ fileOps = (*faultFileOps)(nil)
var _ tempFile = (*faultTempFile)(nil)
