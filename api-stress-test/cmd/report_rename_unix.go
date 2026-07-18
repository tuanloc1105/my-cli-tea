//go:build !windows

package cmd

import "os"

func replaceReportFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
