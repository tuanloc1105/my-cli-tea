package scanner

import "os"

type fileIdentity struct {
	volume uint64
	file   uint64
}

type entryMetadata struct {
	logicalSize   int64
	allocatedSize int64
	kind          ItemType
	identity      fileIdentity
	linkCount     uint64
	hasIdentity   bool
}

type nativeFilesystem struct{}

func (nativeFilesystem) lookup(path string) (entryMetadata, error) {
	return lookupMetadata(path)
}

func (nativeFilesystem) allocatedSupported() bool {
	return nativeAllocatedSupported()
}

func itemTypeFromMode(mode os.FileMode) ItemType {
	switch {
	case mode&os.ModeSymlink != 0:
		return ItemTypeSymlink
	case mode.IsDir():
		return ItemTypeDirectory
	case mode.IsRegular():
		return ItemTypeFile
	default:
		return ItemTypeOther
	}
}
