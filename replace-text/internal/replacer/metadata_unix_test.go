//go:build darwin || linux

package replacer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnixSnapshotIncludesOwnershipAndNanosecondTimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantAtime := time.Unix(1_700_000_000, 123_456_000)
	wantMtime := time.Unix(1_700_000_100, 654_321_000)
	if err := os.Chtimes(path, wantAtime, wantMtime); err != nil {
		t.Fatal(err)
	}

	metadata := snapshotPathMetadata(t, path)
	if !metadata.owner.available {
		t.Fatal("Unix snapshot did not include ownership")
	}
	if !metadata.requiresExactPreservation {
		t.Fatal("Unix snapshot did not require exact metadata preservation")
	}
	if !metadata.accessTime.Equal(wantAtime) {
		t.Fatalf("accessTime = %v, want %v", metadata.accessTime, wantAtime)
	}
	if !metadata.modificationTime.Equal(wantMtime) {
		t.Fatalf("modificationTime = %v, want %v", metadata.modificationTime, wantMtime)
	}
}

func TestUnixSyncParentDirectoryReportsOpenFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "file.txt")
	if err := syncParentDirectory(path); err == nil {
		t.Fatal("syncParentDirectory succeeded for a missing parent")
	}
}
