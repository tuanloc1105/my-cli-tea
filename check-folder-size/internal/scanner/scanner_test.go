package scanner

import (
	"context"
	"os"
	"path/filepath"
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

	result := GetSizesOfSubfolders(parent, ScanOptions{
		Ctx: context.Background(),
	})

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
