//go:build !darwin && !windows

package searcher

import (
	"os"
	"path/filepath"
	"strings"
)

func isHidden(path string, _ os.FileInfo) bool {
	return strings.HasPrefix(filepath.Base(path), ".")
}
