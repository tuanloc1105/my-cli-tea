//go:build darwin

package searcher

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const ufHidden = 0x00008000

func isHidden(path string, info os.FileInfo) bool {
	if strings.HasPrefix(filepath.Base(path), ".") {
		return true
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Flags&ufHidden != 0
}
