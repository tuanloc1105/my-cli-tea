package types

import (
	"errors"
	"fmt"
	"testing"
)

func TestSearchReportCapsIssueDetails(t *testing.T) {
	var report SearchReport
	for i := 0; i < MaxIssueDetails+5; i++ {
		report.AddTraversalError(PathIssue{
			Path:      fmt.Sprintf("traversal-%d", i),
			Operation: "read directory",
			Err:       errors.New("read failed"),
		})
		report.AddSkippedSymlink(PathIssue{
			Path:      fmt.Sprintf("symlink-%d", i),
			Operation: "stat symlink target",
			Err:       errors.New("unsupported target"),
		})
	}

	if !report.Incomplete {
		t.Fatal("traversal errors must mark the report incomplete")
	}
	if report.TraversalErrorCount != MaxIssueDetails+5 || len(report.TraversalErrors) != MaxIssueDetails {
		t.Fatalf("traversal count/details = %d/%d", report.TraversalErrorCount, len(report.TraversalErrors))
	}
	if report.SkippedSymlinkCount != MaxIssueDetails+5 || len(report.SkippedSymlinks) != MaxIssueDetails {
		t.Fatalf("symlink count/details = %d/%d", report.SkippedSymlinkCount, len(report.SkippedSymlinks))
	}
}
