//go:build darwin

package replacer

import (
	"errors"
	"fmt"
	"os"
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
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileMetadata{}, errors.New("snapshot metadata: unsupported stat data")
	}

	uid, err := unixOwnerID(stat.Uid)
	if err != nil {
		return fileMetadata{}, fmt.Errorf("snapshot metadata uid: %w", err)
	}
	gid, err := unixOwnerID(stat.Gid)
	if err != nil {
		return fileMetadata{}, fmt.Errorf("snapshot metadata gid: %w", err)
	}

	return fileMetadata{
		identity: fileIdentity{
			device: uint64(uint32(stat.Dev)),
			file:   stat.Ino,
		},
		size:             stat.Size,
		mode:             info.Mode(),
		accessTime:       time.Unix(stat.Atimespec.Sec, stat.Atimespec.Nsec),
		modificationTime: time.Unix(stat.Mtimespec.Sec, stat.Mtimespec.Nsec),
		owner: fileOwner{
			uid:       uid,
			gid:       gid,
			available: true,
		},
		linkCount:                 uint64(stat.Nlink),
		requiresExactPreservation: true,
	}, nil
}
