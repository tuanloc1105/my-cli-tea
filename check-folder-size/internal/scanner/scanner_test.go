package scanner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetSizesOfSubfoldersReturnsItemTypes(t *testing.T) {
	parent := t.TempDir()

	fileName := "top-level-file.txt"
	fileContent := []byte("hello")
	if err := os.WriteFile(filepath.Join(parent, fileName), fileContent, 0o644); err != nil {
		t.Fatalf("write top-level file: %v", err)
	}

	dirName := "top-level-directory"
	if err := os.Mkdir(filepath.Join(parent, dirName), 0o755); err != nil {
		t.Fatalf("create top-level directory: %v", err)
	}
	nestedContent := []byte("nested")
	if err := os.WriteFile(filepath.Join(parent, dirName, "nested.txt"), nestedContent, 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	emptyDirName := "empty-directory"
	if err := os.Mkdir(filepath.Join(parent, emptyDirName), 0o755); err != nil {
		t.Fatalf("create empty top-level directory: %v", err)
	}

	result, err := GetSizesOfSubfolders(parent, ScanOptions{
		Ctx: context.Background(),
	})
	if err != nil {
		t.Fatalf("GetSizesOfSubfolders() error = %v", err)
	}

	if result.WarningCount != 0 {
		t.Fatalf("WarningCount = %d, want 0", result.WarningCount)
	}

	file := findItem(t, result.Items, fileName)
	if file.Name != fileName || file.Type != "file" || file.Size != int64(len(fileContent)) {
		t.Fatalf("file item = %#v, want name %q, type file, size %d", file, fileName, len(fileContent))
	}

	dir := findItem(t, result.Items, dirName)
	if dir.Name != dirName || dir.Type != "directory" || dir.Size != int64(len(nestedContent)) {
		t.Fatalf("directory item = %#v, want name %q, type directory, size %d", dir, dirName, len(nestedContent))
	}

	emptyDir := findItem(t, result.Items, emptyDirName)
	if emptyDir.Name != emptyDirName || emptyDir.Type != "directory" || emptyDir.Size != 0 {
		t.Fatalf("empty directory item = %#v, want name %q, type directory, size 0", emptyDir, emptyDirName)
	}
}

func TestGetSizesOfSubfoldersReturnsReadError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	_, err := GetSizesOfSubfolders(missing, ScanOptions{Ctx: context.Background()})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("GetSizesOfSubfolders() error = %v, want os.ErrNotExist", err)
	}
}

func TestGetSizesOfSubfoldersWritesProgressToSuppliedWriter(t *testing.T) {
	parent := t.TempDir()
	dirName := "top-level-directory"
	if err := os.Mkdir(filepath.Join(parent, dirName), 0o755); err != nil {
		t.Fatalf("create top-level directory: %v", err)
	}
	var progress bytes.Buffer

	_, err := GetSizesOfSubfolders(parent, ScanOptions{
		ShowProgress:   true,
		Ctx:            context.Background(),
		ProgressWriter: &progress,
	})
	if err != nil {
		t.Fatalf("GetSizesOfSubfolders() error = %v", err)
	}
	if !strings.Contains(progress.String(), "Processing 1/1: "+dirName) {
		t.Fatalf("progress output = %q", progress.String())
	}
	if !strings.HasSuffix(progress.String(), "\n") {
		t.Fatalf("progress output does not end with a newline: %q", progress.String())
	}
}

func TestGetSizesOfSubfoldersReturnsContextError(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "directory"), 0o755); err != nil {
		t.Fatalf("create top-level directory: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := GetSizesOfSubfolders(parent, ScanOptions{Ctx: ctx})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetSizesOfSubfolders() error = %v, want context.Canceled", err)
	}
}

func findItem(t *testing.T, items []ItemInfo, name string) ItemInfo {
	t.Helper()

	for _, item := range items {
		if item.Name == name {
			return item
		}
	}

	t.Fatalf("item %q not found in %#v", name, items)
	return ItemInfo{}
}
