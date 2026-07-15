//go:build windows

package replacer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func snapshotFileMetadata(file *os.File) (fileMetadata, error) {
	if file == nil {
		return fileMetadata{}, errors.New("snapshot metadata: nil file")
	}
	info, err := file.Stat()
	if err != nil {
		return fileMetadata{}, fmt.Errorf("snapshot metadata: %w", err)
	}

	var handleInfo syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &handleInfo); err != nil {
		return fileMetadata{}, fmt.Errorf("snapshot metadata handle info: %w", err)
	}

	return fileMetadata{
		identity: fileIdentity{
			device: uint64(handleInfo.VolumeSerialNumber),
			file:   uint64(handleInfo.FileIndexHigh)<<32 | uint64(handleInfo.FileIndexLow),
		},
		size:             int64(uint64(handleInfo.FileSizeHigh)<<32 | uint64(handleInfo.FileSizeLow)),
		mode:             info.Mode(),
		accessTime:       time.Unix(0, handleInfo.LastAccessTime.Nanoseconds()),
		modificationTime: time.Unix(0, handleInfo.LastWriteTime.Nanoseconds()),
		linkCount:        uint64(handleInfo.NumberOfLinks),
		// Stdlib does not expose owner SID/ACL restoration. Mode and times are
		// retained only as best-effort inputs to applyFileMetadata.
		requiresExactPreservation: false,
	}, nil
}

// Windows has no stdlib operation that reliably flushes a directory entry.
// Make the best-effort attempt, but do not present failures as a durability
// guarantee that the platform contract cannot make.
func syncParentDirectory(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return nil
	}
	_ = dir.Sync()
	_ = dir.Close()
	return nil
}

func restoreSourceTimes(file *os.File, metadata fileMetadata) error {
	if file == nil {
		return nil
	}
	_ = os.Chtimes(file.Name(), metadata.accessTime, metadata.modificationTime)
	return nil
}
