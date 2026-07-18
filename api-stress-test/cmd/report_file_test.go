package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteReportFileAtomicallyPreservesMode(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeReportFile(destination, []byte(`{"schema_version":2}`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"schema_version":2}` {
		t.Fatalf("report = %q", data)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestWriteReportFileReplacesSymlinkWithoutChangingTarget(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.json")
	link := filepath.Join(directory, "report.json")
	if err := os.WriteFile(target, []byte("target-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeReportFile(link, []byte("report-new")); err != nil {
		t.Fatal(err)
	}
	targetData, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(targetData) != "target-old" {
		t.Fatalf("symlink target changed: %q", targetData)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("replacement mode = %s, want regular file", info.Mode())
	}
}

func TestWriteReportFileFailuresPreserveOldDestination(t *testing.T) {
	failure := errors.New("injected failure")
	tests := []struct {
		name   string
		create func(*fakeReportFile) func(string, string) (reportTempFile, error)
		rename func(string, string) error
	}{
		{name: "create", create: func(*fakeReportFile) func(string, string) (reportTempFile, error) {
			return func(string, string) (reportTempFile, error) { return nil, failure }
		}},
		{name: "write", create: fakeReportCreator(func(file *fakeReportFile) { file.writeErr = failure })},
		{name: "sync", create: fakeReportCreator(func(file *fakeReportFile) { file.syncErr = failure })},
		{name: "close", create: fakeReportCreator(func(file *fakeReportFile) { file.closeErr = failure })},
		{name: "rename", create: fakeReportCreator(nil), rename: func(string, string) error { return failure }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "report.json")
			if err := os.WriteFile(destination, []byte("old-report"), 0o644); err != nil {
				t.Fatal(err)
			}
			fake := &fakeReportFile{name: filepath.Join(filepath.Dir(destination), ".report.tmp")}
			removed := false
			create := tt.create(fake)
			rename := tt.rename
			if rename == nil {
				rename = func(string, string) error { return nil }
			}
			deps := reportFileDependencies{
				lstat: os.Lstat, createTemp: create, rename: rename,
				remove: func(string) error { removed = true; return nil },
			}
			if err := writeReportFileWithDependencies(destination, []byte("new-report"), deps); err == nil {
				t.Fatal("write succeeded, want injected error")
			}
			data, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "old-report" {
				t.Fatalf("old report changed: %q", data)
			}
			if tt.name != "create" && !removed {
				t.Fatal("temporary report was not cleaned up")
			}
		})
	}
}

func TestWriteReportFileReportsCleanupFailure(t *testing.T) {
	writeFailure := errors.New("write failed")
	cleanupFailure := errors.New("cleanup failed")
	fake := &fakeReportFile{name: filepath.Join(t.TempDir(), ".report.tmp"), writeErr: writeFailure}
	deps := reportFileDependencies{
		lstat:      os.Lstat,
		createTemp: fakeReportCreator(nil)(fake),
		rename:     func(string, string) error { return nil },
		remove:     func(string) error { return cleanupFailure },
	}
	err := writeReportFileWithDependencies(filepath.Join(t.TempDir(), "report.json"), []byte("report"), deps)
	if !errors.Is(err, writeFailure) || !errors.Is(err, cleanupFailure) {
		t.Fatalf("error = %v, want write and cleanup failures", err)
	}
}

type fakeReportFile struct {
	bytes.Buffer
	name     string
	writeErr error
	syncErr  error
	closeErr error
}

func fakeReportCreator(configure func(*fakeReportFile)) func(*fakeReportFile) func(string, string) (reportTempFile, error) {
	return func(file *fakeReportFile) func(string, string) (reportTempFile, error) {
		if configure != nil {
			configure(file)
		}
		return func(string, string) (reportTempFile, error) { return file, nil }
	}
}

func (f *fakeReportFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.Buffer.Write(p)
}

func (f *fakeReportFile) Name() string            { return f.name }
func (f *fakeReportFile) Chmod(os.FileMode) error { return nil }
func (f *fakeReportFile) Sync() error             { return f.syncErr }
func (f *fakeReportFile) Close() error            { return f.closeErr }
