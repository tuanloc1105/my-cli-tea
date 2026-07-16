package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestParseSizeMode(t *testing.T) {
	for _, value := range []string{string(SizeModeLogical), string(SizeModeAllocated)} {
		mode, err := ParseSizeMode(value)
		if err != nil {
			t.Fatalf("ParseSizeMode(%q) error = %v", value, err)
		}
		if string(mode) != value || !mode.Valid() {
			t.Fatalf("ParseSizeMode(%q) = %q, want valid mode", value, mode)
		}
	}

	_, err := ParseSizeMode("physical")
	if kind, ok := ErrorKindOf(err); !ok || kind != ErrorKindInvalidOptions {
		t.Fatalf("ParseSizeMode() error kind = %q, %v, want %q, true", kind, ok, ErrorKindInvalidOptions)
	}
}

func TestItemTypeContract(t *testing.T) {
	want := []ItemType{ItemTypeFile, ItemTypeDirectory, ItemTypeSymlink, ItemTypeOther}
	got := []ItemType{"file", "directory", "symlink", "other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("item types = %v, want %v", got, want)
	}
}

func TestScanDirectoryEmptyResultIsInitialized(t *testing.T) {
	result, err := ScanDirectory(t.TempDir(), ScanOptions{SizeMode: SizeModeLogical})
	if err != nil {
		t.Fatalf("ScanDirectory() error = %v", err)
	}
	if result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("Items = %#v, want initialized empty slice", result.Items)
	}
	if result.Status != ScanStatusComplete {
		t.Fatalf("Status = %q, want %q", result.Status, ScanStatusComplete)
	}
}

func TestGetSizesOfSubfoldersDelegatesToLogicalScan(t *testing.T) {
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	wrapper, err := GetSizesOfSubfolders(parent, ScanOptions{SizeMode: SizeModeAllocated})
	if err != nil {
		t.Fatalf("GetSizesOfSubfolders() error = %v", err)
	}
	direct, err := ScanDirectory(parent, ScanOptions{SizeMode: SizeModeLogical})
	if err != nil {
		t.Fatalf("ScanDirectory() error = %v", err)
	}
	sort.Slice(wrapper.Items, func(i, j int) bool { return wrapper.Items[i].Name < wrapper.Items[j].Name })
	sort.Slice(direct.Items, func(i, j int) bool { return direct.Items[i].Name < direct.Items[j].Name })
	if !reflect.DeepEqual(wrapper, direct) {
		t.Fatalf("wrapper result = %#v, direct result = %#v", wrapper, direct)
	}
}

func TestScanDirectoryClassifiesErrors(t *testing.T) {
	t.Run("root open", func(t *testing.T) {
		result, err := ScanDirectory(filepath.Join(t.TempDir(), "missing"), ScanOptions{SizeMode: SizeModeLogical})
		assertScanError(t, result, err, ErrorKindRootOpen, os.ErrNotExist, ScanStatusFailed)
	})

	t.Run("cancelled", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "directory"), 0o755); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := ScanDirectory(root, ScanOptions{Ctx: ctx, SizeMode: SizeModeLogical})
		assertScanError(t, result, err, ErrorKindCancelled, context.Canceled, ScanStatusPartial)
	})

	t.Run("invalid mode", func(t *testing.T) {
		result, err := ScanDirectory(t.TempDir(), ScanOptions{SizeMode: "physical"})
		assertScanError(t, result, err, ErrorKindInvalidOptions, nil, ScanStatusFailed)
	})

	t.Run("unsupported mode", func(t *testing.T) {
		result, err := scanDirectory(t.TempDir(), ScanOptions{SizeMode: SizeModeAllocated}, scanDependencies{
			filesystem: unsupportedFilesystem{nativeFilesystem{}},
			readDir:    os.ReadDir,
		})
		assertScanError(t, result, err, ErrorKindUnsupported, nil, ScanStatusFailed)
	})
}

type unsupportedFilesystem struct {
	nativeFilesystem
}

func (unsupportedFilesystem) allocatedSupported() bool {
	return false
}

func assertScanError(t *testing.T, result ScanResult, err error, wantKind ErrorKind, wantCause error, wantStatus ScanStatus) {
	t.Helper()
	if result.Items == nil {
		t.Fatal("Items is nil")
	}
	if result.Status != wantStatus {
		t.Fatalf("Status = %q, want %q", result.Status, wantStatus)
	}
	if kind, ok := ErrorKindOf(err); !ok || kind != wantKind {
		t.Fatalf("error kind = %q, %v, want %q, true (error %v)", kind, ok, wantKind, err)
	}
	if wantCause != nil && !errors.Is(err, wantCause) {
		t.Fatalf("error = %v, want cause %v", err, wantCause)
	}
}
