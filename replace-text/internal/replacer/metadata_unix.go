//go:build darwin || linux

package replacer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func unixOwnerID(id uint32) (int, error) {
	value := int(id)
	if value < 0 || uint32(value) != id {
		return 0, fmt.Errorf("value %d does not fit in int", id)
	}
	return value, nil
}

func syncParentDirectory(path string) error {
	dirPath := filepath.Dir(path)
	dir, err := os.Open(dirPath)
	if err != nil {
		return fmt.Errorf("open parent directory %q: %w", dirPath, err)
	}

	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(
		wrapOperation("sync parent directory", syncErr),
		wrapOperation("close parent directory", closeErr),
	)
}

func restoreSourceTimes(file *os.File, metadata fileMetadata) error {
	if file == nil {
		return errors.New("restore source timestamps: nil file")
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat source before restoring timestamps: %w", err)
	}
	entryInfo, err := os.Lstat(file.Name())
	if err != nil || entryInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(openedInfo, entryInfo) {
		return errors.Join(ErrConcurrentModification, wrapOperation("inspect source before restoring timestamps", err))
	}
	if err := os.Chtimes(file.Name(), metadata.accessTime, metadata.modificationTime); err != nil {
		return fmt.Errorf("restore source timestamps: %w", err)
	}
	return nil
}
