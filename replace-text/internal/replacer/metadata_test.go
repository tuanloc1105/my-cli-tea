package replacer

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSnapshotFileMetadataRejectsNilFile(t *testing.T) {
	if _, err := snapshotFileMetadata(nil); err == nil {
		t.Fatal("snapshotFileMetadata(nil) succeeded, want error")
	}
}

func TestSnapshotFileMetadataIdentityAndState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.txt")
	otherPath := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(path, []byte("source"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("other"), 0o640); err != nil {
		t.Fatal(err)
	}

	first := snapshotPathMetadata(t, path)
	second := snapshotPathMetadata(t, path)
	other := snapshotPathMetadata(t, otherPath)

	if !first.sameIdentity(second) {
		t.Fatal("snapshots of the same file have different identities")
	}
	if !first.unchangedForCommit(second) {
		t.Fatalf("unchanged snapshots do not match:\nfirst:  %+v\nsecond: %+v", first, second)
	}
	if first.sameIdentity(other) {
		t.Fatal("different files have the same identity")
	}
	if first.size != int64(len("source")) {
		t.Fatalf("size = %d, want %d", first.size, len("source"))
	}
	if first.linkCount != 1 {
		t.Fatalf("linkCount = %d, want 1", first.linkCount)
	}
	if first.hasMultipleLinks() {
		t.Fatal("single-link file reported as hardlinked")
	}

	// Removing every write bit also toggles Windows' read-only attribute, so
	// this metadata change is observable on every supported platform.
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	changedMode := snapshotPathMetadata(t, path)
	if !first.sameIdentity(changedMode) {
		t.Fatal("chmod changed file identity")
	}
	if first.unchangedForCommit(changedMode) {
		t.Fatal("chmod was not detected as a metadata change")
	}
}

func TestSnapshotFileMetadataReportsHardlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.txt")
	linkPath := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(path, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, linkPath); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}

	metadata := snapshotPathMetadata(t, path)
	if metadata.linkCount < 2 {
		t.Fatalf("linkCount = %d, want at least 2", metadata.linkCount)
	}
	if !metadata.hasMultipleLinks() {
		t.Fatal("hardlinked file was not reported as having multiple links")
	}
}

func TestApplyFileMetadataOrder(t *testing.T) {
	atime := time.Unix(1_700_000_000, 123)
	mtime := time.Unix(1_700_000_100, 456)
	metadata := fileMetadata{
		mode:             os.ModeSetuid | os.ModeSetgid | 0o640,
		accessTime:       atime,
		modificationTime: mtime,
		owner: fileOwner{
			uid:       12,
			gid:       34,
			available: true,
		},
		requiresExactPreservation: true,
	}
	ops := &recordingMetadataOps{}

	if err := applyFileMetadata(ops, "target", metadata); err != nil {
		t.Fatalf("applyFileMetadata: %v", err)
	}
	if want := []string{"chown", "chmod", "chtimes"}; !reflect.DeepEqual(ops.calls, want) {
		t.Fatalf("calls = %v, want %v", ops.calls, want)
	}
	if ops.uid != 12 || ops.gid != 34 {
		t.Fatalf("owner = %d:%d, want 12:34", ops.uid, ops.gid)
	}
	wantMode := os.ModeSetuid | os.ModeSetgid | 0o640
	if ops.mode != wantMode {
		t.Fatalf("mode = %v, want %v", ops.mode, wantMode)
	}
	if !ops.atime.Equal(atime) || !ops.mtime.Equal(mtime) {
		t.Fatalf("times = (%v, %v), want (%v, %v)", ops.atime, ops.mtime, atime, mtime)
	}
}

func TestApplyFileMetadataStrictFailureStopsSequence(t *testing.T) {
	operationError := errors.New("operation failed")
	metadata := fileMetadata{
		mode:                      0o600,
		accessTime:                time.Unix(1_700_000_000, 0),
		modificationTime:          time.Unix(1_700_000_100, 0),
		owner:                     fileOwner{available: true},
		requiresExactPreservation: true,
	}
	tests := []struct {
		name      string
		fail      string
		wantCalls []string
	}{
		{name: "chown", fail: "chown", wantCalls: []string{"chown"}},
		{name: "chmod", fail: "chmod", wantCalls: []string{"chown", "chmod"}},
		{name: "chtimes", fail: "chtimes", wantCalls: []string{"chown", "chmod", "chtimes"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ops := &recordingMetadataOps{failOperation: test.fail, err: operationError}
			err := applyFileMetadata(ops, "target", metadata)
			if !errors.Is(err, operationError) {
				t.Fatalf("error = %v, want %v", err, operationError)
			}
			if !reflect.DeepEqual(ops.calls, test.wantCalls) {
				t.Fatalf("calls = %v, want %v", ops.calls, test.wantCalls)
			}
		})
	}
}

func TestApplyFileMetadataBestEffortContinues(t *testing.T) {
	metadata := fileMetadata{
		mode:                      0o600,
		accessTime:                time.Unix(1_700_000_000, 0),
		modificationTime:          time.Unix(1_700_000_100, 0),
		requiresExactPreservation: false,
	}
	ops := &recordingMetadataOps{
		failOperation: "chmod,chtimes",
		err:           errors.New("unsupported"),
	}

	if err := applyFileMetadata(ops, "target", metadata); err != nil {
		t.Fatalf("applyFileMetadata returned best-effort error: %v", err)
	}
	if want := []string{"chmod", "chtimes"}; !reflect.DeepEqual(ops.calls, want) {
		t.Fatalf("calls = %v, want %v", ops.calls, want)
	}
}

func TestSyncParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncParentDirectory(path); err != nil {
		t.Fatalf("syncParentDirectory: %v", err)
	}
}

func snapshotPathMetadata(t *testing.T, path string) fileMetadata {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	metadata, snapshotErr := snapshotFileMetadata(file)
	closeErr := file.Close()
	if snapshotErr != nil {
		t.Fatalf("snapshotFileMetadata: %v", snapshotErr)
	}
	if closeErr != nil {
		t.Fatalf("close snapshot file: %v", closeErr)
	}
	return metadata
}

type recordingMetadataOps struct {
	calls         []string
	failOperation string
	err           error
	uid           int
	gid           int
	mode          os.FileMode
	atime         time.Time
	mtime         time.Time
}

func (o *recordingMetadataOps) Chown(_ string, uid, gid int) error {
	o.calls = append(o.calls, "chown")
	o.uid = uid
	o.gid = gid
	if o.failOperation == "chown" {
		return o.err
	}
	return nil
}

func (o *recordingMetadataOps) Chmod(_ string, mode os.FileMode) error {
	o.calls = append(o.calls, "chmod")
	o.mode = mode
	if o.failOperation == "chmod" || o.failOperation == "chmod,chtimes" {
		return o.err
	}
	return nil
}

func (o *recordingMetadataOps) Chtimes(_ string, atime, mtime time.Time) error {
	o.calls = append(o.calls, "chtimes")
	o.atime = atime
	o.mtime = mtime
	if o.failOperation == "chtimes" || o.failOperation == "chmod,chtimes" {
		return o.err
	}
	return nil
}
