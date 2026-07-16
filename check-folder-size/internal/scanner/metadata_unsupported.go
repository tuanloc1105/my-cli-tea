//go:build !darwin && !linux && !windows

package scanner

import (
	"fmt"
	"os"
)

func lookupMetadata(path string) (entryMetadata, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return entryMetadata{}, fmt.Errorf("reading metadata: %w", err)
	}
	return entryMetadata{
		logicalSize: info.Size(),
		kind:        itemTypeFromMode(info.Mode()),
	}, nil
}

func nativeAllocatedSupported() bool {
	return false
}
