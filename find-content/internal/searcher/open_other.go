//go:build !darwin && !linux && !windows

package searcher

import "os"

func openRegularFile(path string) (fileHandle, error) {
	return os.Open(path)
}

func readDirectoryNoFollow(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}
