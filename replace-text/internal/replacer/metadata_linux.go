//go:build linux

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
			device: stat.Dev,
			file:   stat.Ino,
		},
		size:             stat.Size,
		mode:             info.Mode(),
		accessTime:       time.Unix(stat.Atim.Sec, stat.Atim.Nsec),
		modificationTime: time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec),
		owner: fileOwner{
			uid:       uid,
			gid:       gid,
			available: true,
		},
		linkCount:                 stat.Nlink,
		requiresExactPreservation: true,
	}, nil
}
