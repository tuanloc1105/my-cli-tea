//go:build windows

package scanner

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type fileStandardInformation struct {
	AllocationSize int64
	EndOfFile      int64
	NumberOfLinks  uint32
	DeletePending  byte
	Directory      byte
	_              [2]byte
}

func lookupMetadata(path string) (metadata entryMetadata, resultErr error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return entryMetadata{}, fmt.Errorf("encoding path: %w", err)
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return entryMetadata{}, fmt.Errorf("opening metadata handle: %w", err)
	}
	defer func() {
		if err := windows.CloseHandle(handle); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("closing metadata handle: %w", err)
		}
	}()

	var standard fileStandardInformation
	err = windows.GetFileInformationByHandleEx(
		handle,
		windows.FileStandardInfo,
		(*byte)(unsafe.Pointer(&standard)),
		uint32(unsafe.Sizeof(standard)),
	)
	if err != nil {
		return entryMetadata{}, fmt.Errorf("reading standard file information: %w", err)
	}
	var identity windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &identity); err != nil {
		return entryMetadata{}, fmt.Errorf("reading file identity: %w", err)
	}
	if standard.AllocationSize < 0 || standard.EndOfFile < 0 {
		return entryMetadata{}, fmt.Errorf("invalid negative file size")
	}

	kind := ItemTypeFile
	if identity.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		kind = ItemTypeSymlink
	} else if standard.Directory != 0 {
		kind = ItemTypeDirectory
	}
	metadata = entryMetadata{
		logicalSize:   standard.EndOfFile,
		allocatedSize: standard.AllocationSize,
		kind:          kind,
		identity: fileIdentity{
			volume: uint64(identity.VolumeSerialNumber),
			file:   uint64(identity.FileIndexHigh)<<32 | uint64(identity.FileIndexLow),
		},
		linkCount:   uint64(standard.NumberOfLinks),
		hasIdentity: kind == ItemTypeFile,
	}
	return metadata, nil
}

func nativeAllocatedSupported() bool {
	return true
}
