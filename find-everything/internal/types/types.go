package types

import "time"

const MaxIssueDetails = 20

// FileResult holds a matched file path along with its metadata.
type FileResult struct {
	Path string
	Size int64
}

// PathIssue describes a path that could not be processed normally.
type PathIssue struct {
	Path      string
	Operation string
	Err       error
}

// SearchReport describes non-fatal conditions encountered during a search.
type SearchReport struct {
	LimitReached        bool
	Incomplete          bool
	TraversalErrorCount int
	TraversalErrors     []PathIssue
	SkippedSymlinkCount int
	SkippedSymlinks     []PathIssue
}

func (r *SearchReport) AddTraversalError(issue PathIssue) {
	r.Incomplete = true
	r.TraversalErrorCount++
	if len(r.TraversalErrors) < MaxIssueDetails {
		r.TraversalErrors = append(r.TraversalErrors, issue)
	}
}

func (r *SearchReport) AddSkippedSymlink(issue PathIssue) {
	r.SkippedSymlinkCount++
	if len(r.SkippedSymlinks) < MaxIssueDetails {
		r.SkippedSymlinks = append(r.SkippedSymlinks, issue)
	}
}

type SearchResults struct {
	Files       []FileResult
	Directories []string
	Report      SearchReport
}

// ProgressSnapshot is an immutable view of finder progress.
type ProgressSnapshot struct {
	TotalDirectories     int64
	ProcessedDirectories int64
	FoundFiles           int64
	FoundDirectories     int64
	Elapsed              time.Duration
}

type ProgressFunc func(ProgressSnapshot)
