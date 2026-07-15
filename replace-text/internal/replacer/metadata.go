package replacer

import (
	"fmt"
	"os"
	"time"
)

type fileIdentity struct {
	device uint64
	file   uint64
}

type fileOwner struct {
	uid       int
	gid       int
	available bool
}

// fileMetadata is captured from an open file handle. accessTime is preserved
// on replacement files, but is deliberately not part of unchangedForCommit:
// reading the source may legitimately update its access time.
type fileMetadata struct {
	identity                  fileIdentity
	size                      int64
	mode                      os.FileMode
	accessTime                time.Time
	modificationTime          time.Time
	owner                     fileOwner
	linkCount                 uint64
	requiresExactPreservation bool
}

func (m fileMetadata) hasMultipleLinks() bool {
	return m.linkCount > 1
}

func (m fileMetadata) sameIdentity(other fileMetadata) bool {
	return m.identity == other.identity
}

func (m fileMetadata) unchangedForCommit(other fileMetadata) bool {
	return m.sameIdentity(other) &&
		m.size == other.size &&
		m.mode == other.mode &&
		m.modificationTime.Equal(other.modificationTime) &&
		m.owner == other.owner &&
		m.linkCount == other.linkCount
}

// metadataOps is the subset of filesystem operations needed to restore
// metadata. The processing layer's injected fileOps can satisfy this interface.
type metadataOps interface {
	Chown(string, int, int) error
	Chmod(string, os.FileMode) error
	Chtimes(string, time.Time, time.Time) error
}

// applyFileMetadata restores ownership before permissions because chown may
// clear setuid/setgid bits. Timestamps are restored last. Darwin and Linux
// require every operation to succeed; Windows records best-effort metadata and
// therefore attempts mode and timestamps without making either a guarantee.
func applyFileMetadata(ops metadataOps, path string, metadata fileMetadata) error {
	if metadata.owner.available {
		if err := ops.Chown(path, metadata.owner.uid, metadata.owner.gid); err != nil && metadata.requiresExactPreservation {
			return fmt.Errorf("preserve ownership: %w", err)
		}
	}

	mode := metadata.mode & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
	if err := ops.Chmod(path, mode); err != nil && metadata.requiresExactPreservation {
		return fmt.Errorf("preserve mode: %w", err)
	}
	if err := ops.Chtimes(path, metadata.accessTime, metadata.modificationTime); err != nil && metadata.requiresExactPreservation {
		return fmt.Errorf("preserve timestamps: %w", err)
	}
	return nil
}
