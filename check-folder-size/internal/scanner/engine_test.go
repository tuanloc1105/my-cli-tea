package scanner

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestScanDirectoryIncludesHiddenEntriesAndHonorsExclude(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ".hidden-file"), "hidden")
	writeTestFile(t, filepath.Join(root, ".hidden-dir", "nested.txt"), "nested")
	writeTestFile(t, filepath.Join(root, "visible", ".excluded", "ignored.txt"), "ignored")
	writeTestFile(t, filepath.Join(root, "visible", "kept.txt"), "kept")

	result, err := ScanDirectory(root, ScanOptions{
		SizeMode:    SizeModeLogical,
		ExcludeList: []string{".excluded"},
	})
	if err != nil {
		t.Fatalf("ScanDirectory() error = %v", err)
	}
	if result.Status != ScanStatusComplete {
		t.Fatalf("Status = %q, want complete", result.Status)
	}
	if item := findItem(t, result.Items, ".hidden-file"); item.Type != ItemTypeFile || item.Size != 6 {
		t.Fatalf("hidden file = %#v", item)
	}
	if item := findItem(t, result.Items, ".hidden-dir"); item.Type != ItemTypeDirectory || item.Size != 6 {
		t.Fatalf("hidden directory = %#v", item)
	}
	if item := findItem(t, result.Items, "visible"); item.Size != 4 {
		t.Fatalf("visible directory = %#v, want excluded subtree omitted", item)
	}
}

func TestScanDirectoryDepthBoundary(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "top", "direct.txt"), "a")
	writeTestFile(t, filepath.Join(root, "top", "child", "nested.txt"), "bc")
	writeTestFile(t, filepath.Join(root, "top", "child", "grandchild", "deep.txt"), "defg")

	shallow, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeLogical, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	deep, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeLogical, MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := findItem(t, shallow.Items, "top").Size; got != 3 {
		t.Fatalf("depth 1 size = %d, want 3", got)
	}
	if got := findItem(t, deep.Items, "top").Size; got != 7 {
		t.Fatalf("depth 2 size = %d, want 7", got)
	}
}

func TestScanDirectoryDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	writeTestFile(t, target, strings.Repeat("x", 1024))
	if err := os.Symlink("target.txt", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink("missing.txt", filepath.Join(root, "broken")); err != nil {
		t.Skipf("broken symlink unavailable: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "target-dir", "payload"), strings.Repeat("y", 2048))
	container := filepath.Join(root, "container")
	if err := os.MkdirAll(container, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../target-dir", filepath.Join(container, "nested-link")); err != nil {
		t.Skipf("nested symlink unavailable: %v", err)
	}

	result, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeLogical})
	if err != nil {
		t.Fatal(err)
	}
	link := findItem(t, result.Items, "link")
	if link.Type != ItemTypeSymlink || link.Size == 1024 {
		t.Fatalf("link = %#v, want link metadata rather than target size", link)
	}
	if broken := findItem(t, result.Items, "broken"); broken.Type != ItemTypeSymlink {
		t.Fatalf("broken link = %#v, want symlink", broken)
	}
	if nested := findItem(t, result.Items, "container"); nested.Size >= 2048 {
		t.Fatalf("nested link target was followed: %#v", nested)
	}
}

func TestScanDirectoryAcceptsRootSymlinkToDirectory(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	writeTestFile(t, filepath.Join(realRoot, "file"), "data")
	rootLink := filepath.Join(base, "root-link")
	if err := os.Symlink("real", rootLink); err != nil {
		t.Skipf("root symlink unavailable: %v", err)
	}

	result, err := ScanDirectory(rootLink, ScanOptions{SizeMode: SizeModeLogical})
	if err != nil {
		t.Fatal(err)
	}
	if item := findItem(t, result.Items, "file"); item.Size != 4 {
		t.Fatalf("root symlink item = %#v", item)
	}
}

func TestScanDirectoryAllocatedCountsDirectoryMetadataButNotRoot(t *testing.T) {
	root := t.TempDir()
	top := filepath.Join(root, "top")
	nested := filepath.Join(top, "nested")
	file := filepath.Join(nested, "file.txt")
	writeTestFile(t, file, "content")

	want := allocatedSizeOf(t, top) + allocatedSizeOf(t, nested) + allocatedSizeOf(t, file)
	result, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeAllocated})
	if err != nil {
		t.Fatal(err)
	}
	if got := findItem(t, result.Items, "top").Size; got != want {
		t.Fatalf("allocated top size = %d, want %d without root metadata", got, want)
	}
}

func TestScanDirectoryAllocatedDeduplicatesHardlinksDeterministically(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "a")
	zDir := filepath.Join(root, "z")
	if err := os.MkdirAll(aDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(zDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(zDir, "original")
	writeTestFile(t, original, strings.Repeat("x", 8192))
	if err := os.Link(original, filepath.Join(aDir, "link-1")); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	if err := os.Link(original, filepath.Join(aDir, "link-2")); err != nil {
		t.Fatal(err)
	}

	wantA := allocatedSizeOf(t, aDir) + allocatedSizeOf(t, original)
	wantZ := allocatedSizeOf(t, zDir)
	for i := 0; i < 25; i++ {
		result, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeAllocated})
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if got := findItem(t, result.Items, "a").Size; got != wantA {
			t.Fatalf("run %d: owner a size = %d, want %d", i, got, wantA)
		}
		if got := findItem(t, result.Items, "z").Size; got != wantZ {
			t.Fatalf("run %d: owner z size = %d, want %d", i, got, wantZ)
		}
	}

	logical, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeLogical})
	if err != nil {
		t.Fatal(err)
	}
	if got := findItem(t, logical.Items, "a").Size; got != 2*8192 {
		t.Fatalf("logical owner a size = %d, want both observed links", got)
	}
	if got := findItem(t, logical.Items, "z").Size; got != 8192 {
		t.Fatalf("logical owner z size = %d, want original link", got)
	}
}

func TestHardlinkRegistryIsThreadSafeAndChoosesSmallestOwner(t *testing.T) {
	for run := 0; run < 50; run++ {
		totals := map[string]*atomic.Int64{"a": {}, "m": {}, "z": {}}
		registry := newHardlinkRegistry(totals)
		identity := fileIdentity{volume: 1, file: 2}
		var wg sync.WaitGroup
		for _, owner := range []string{"z", "m", "a", "z", "a"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := registry.add(identity, owner, 4096); err != nil {
					t.Errorf("registry.add() error = %v", err)
				}
			}()
		}
		wg.Wait()
		if totals["a"].Load() != 4096 || totals["m"].Load() != 0 || totals["z"].Load() != 0 {
			t.Fatalf("run %d totals = a:%d m:%d z:%d", run, totals["a"].Load(), totals["m"].Load(), totals["z"].Load())
		}
	}
}

func TestScanDirectorySparseFileModesDifferWhenSupported(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sparse")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64 << 20); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	logical, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeLogical})
	if err != nil {
		t.Fatal(err)
	}
	allocated, err := ScanDirectory(root, ScanOptions{SizeMode: SizeModeAllocated})
	if err != nil {
		t.Fatal(err)
	}
	logicalSize := findItem(t, logical.Items, "sparse").Size
	allocatedSize := findItem(t, allocated.Items, "sparse").Size
	if allocatedSize >= logicalSize {
		t.Skipf("filesystem does not expose sparse allocation: allocated=%d logical=%d", allocatedSize, logicalSize)
	}
}

func TestScanDirectoryMetadataFailureReturnsPartialResult(t *testing.T) {
	root := t.TempDir()
	goodPath := filepath.Join(root, "good")
	badPath := filepath.Join(root, "bad")
	writeTestFile(t, goodPath, "good")
	writeTestFile(t, badPath, "bad")
	deps := scanDependencies{
		filesystem: faultFilesystem{scannerFilesystem: nativeFilesystem{}, failPath: badPath, err: errors.New("metadata denied")},
		readDir:    os.ReadDir,
	}

	result, err := scanDirectory(root, ScanOptions{SizeMode: SizeModeAllocated}, deps)
	if err != nil {
		t.Fatalf("scanDirectory() error = %v", err)
	}
	if result.Status != ScanStatusPartial || result.WarningCount != 1 || !strings.Contains(result.WarningSummary, "metadata denied") {
		t.Fatalf("partial result = %#v", result)
	}
	if len(result.Items) != 1 || result.Items[0].Name != "good" {
		t.Fatalf("Items = %#v, want only good", result.Items)
	}
}

func TestScanDirectoryReadFailureReturnsPartialResult(t *testing.T) {
	root := t.TempDir()
	top := filepath.Join(root, "top")
	writeTestFile(t, filepath.Join(top, "file"), "data")
	deps := scanDependencies{
		filesystem: nativeFilesystem{},
		readDir: func(path string) ([]os.DirEntry, error) {
			if path == top {
				return nil, errors.New("read denied")
			}
			return os.ReadDir(path)
		},
	}

	result, err := scanDirectory(root, ScanOptions{SizeMode: SizeModeLogical}, deps)
	if err != nil {
		t.Fatalf("scanDirectory() error = %v", err)
	}
	if result.Status != ScanStatusPartial || result.WarningCount != 1 || !strings.Contains(result.WarningSummary, "read denied") {
		t.Fatalf("partial result = %#v", result)
	}
	if item := findItem(t, result.Items, "top"); item.Size != 0 {
		t.Fatalf("partially read directory = %#v", item)
	}
}

func TestScanDirectoryCancellationReturnsCollectedData(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a-file"), "data")
	dir := filepath.Join(root, "z-dir")
	writeTestFile(t, filepath.Join(dir, "nested"), "nested")
	ctx, cancel := context.WithCancel(context.Background())
	deps := scanDependencies{
		filesystem: nativeFilesystem{},
		readDir: func(path string) ([]os.DirEntry, error) {
			entries, err := os.ReadDir(path)
			if path == dir {
				cancel()
			}
			return entries, err
		},
	}

	result, err := scanDirectory(root, ScanOptions{Ctx: ctx, SizeMode: SizeModeLogical}, deps)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scanDirectory() error = %v, want context.Canceled", err)
	}
	if result.Status != ScanStatusPartial || len(result.Items) != 2 {
		t.Fatalf("cancelled result = %#v", result)
	}
}

func TestScanDirectorySizeOverflowBecomesPartialWarning(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "top")
	writeTestFile(t, filepath.Join(dir, "one"), "1")
	writeTestFile(t, filepath.Join(dir, "two"), "2")
	deps := scanDependencies{
		filesystem: overflowFilesystem{nativeFilesystem{}},
		readDir:    os.ReadDir,
	}

	result, err := scanDirectory(root, ScanOptions{SizeMode: SizeModeAllocated}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScanStatusPartial || result.WarningCount != 1 || !strings.Contains(result.WarningSummary, "overflow") {
		t.Fatalf("overflow result = %#v", result)
	}
	if got := findItem(t, result.Items, "top").Size; got != math.MaxInt64 {
		t.Fatalf("size = %d, want MaxInt64", got)
	}
}

type faultFilesystem struct {
	scannerFilesystem
	failPath string
	err      error
}

func (filesystem faultFilesystem) lookup(path string) (entryMetadata, error) {
	if path == filesystem.failPath {
		return entryMetadata{}, filesystem.err
	}
	return filesystem.scannerFilesystem.lookup(path)
}

type overflowFilesystem struct {
	nativeFilesystem
}

func (filesystem overflowFilesystem) lookup(path string) (entryMetadata, error) {
	metadata, err := filesystem.nativeFilesystem.lookup(path)
	if err != nil {
		return entryMetadata{}, err
	}
	if metadata.kind == ItemTypeFile {
		metadata.allocatedSize = math.MaxInt64
		metadata.linkCount = 1
	}
	if metadata.kind == ItemTypeDirectory {
		metadata.allocatedSize = 0
	}
	return metadata, nil
}

func allocatedSizeOf(t *testing.T, path string) int64 {
	t.Helper()
	metadata, err := nativeFilesystem{}.lookup(path)
	if err != nil {
		t.Fatalf("lookup(%q): %v", path, err)
	}
	return metadata.allocatedSize
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
