//go:build darwin || linux

package scanner

import (
	"fmt"
	"math"
	"os"
	"syscall"
)

func lookupMetadata(path string) (entryMetadata, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return entryMetadata{}, fmt.Errorf("reading metadata: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return entryMetadata{}, fmt.Errorf("reading metadata: unexpected stat type %T", info.Sys())
	}
	if stat.Blocks < 0 || stat.Blocks > math.MaxInt64/512 {
		return entryMetadata{}, fmt.Errorf("allocated size overflow for %d blocks", stat.Blocks)
	}

	metadata := entryMetadata{
		logicalSize:   info.Size(),
		allocatedSize: stat.Blocks * 512,
		kind:          itemTypeFromMode(info.Mode()),
		identity:      fileIdentity{volume: uint64(stat.Dev), file: uint64(stat.Ino)},
		linkCount:     uint64(stat.Nlink),
	}
	metadata.hasIdentity = metadata.kind == ItemTypeFile
	return metadata, nil
}

func nativeAllocatedSupported() bool {
	return true
}
