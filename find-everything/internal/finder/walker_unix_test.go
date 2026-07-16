//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package finder

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestUnixUnreadableDescendantProducesPartialResults(t *testing.T) {
	base := t.TempDir()
	blocked := filepath.Join(base, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatalf("Mkdir(%q): %v", blocked, err)
	}
	writeTestFile(t, filepath.Join(base, "kept.txt"), 1)
	writeTestFile(t, filepath.Join(blocked, "unreadable.txt"), 1)

	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatalf("Chmod(%q): %v", blocked, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0o755); err != nil {
			t.Errorf("restore permissions: %v", err)
		}
	})

	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("current user can read mode-000 directories; deterministic readDir coverage remains in TestPartialTraversalErrorsAreCountedAndCapped")
	} else if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("ReadDir(%q) error = %v, want permission denied", blocked, err)
	}

	results := runTestFinder(t, base, "*.txt", testFinderOptions())
	assertPathNames(t, results.Files, "kept.txt")
	if !results.Report.Incomplete || results.Report.TraversalErrorCount != 1 || len(results.Report.TraversalErrors) != 1 {
		t.Fatalf("report = %+v, want one traversal error", results.Report)
	}
	issue := results.Report.TraversalErrors[0]
	if issue.Path != blocked || issue.Operation != "read directory" || !errors.Is(issue.Err, fs.ErrPermission) {
		t.Fatalf("traversal error = %+v, want permission error for %q", issue, blocked)
	}
}
