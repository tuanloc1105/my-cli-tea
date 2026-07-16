package cmd

import (
	"fmt"
	"io"
	"strconv"

	"find-content/internal/searcher"
)

type renderer struct {
	stdout           io.Writer
	stderr           io.Writer
	showLineNumbers  bool
	showFilePath     bool
	multiline        bool
	suppressWarnings bool
}

func (r renderer) handle(event searcher.Event) error {
	if event.Diagnostic != nil {
		if r.suppressWarnings {
			return nil
		}
		if _, err := fmt.Fprintf(r.stderr, "Warning: %s: %v\n", event.Diagnostic.Path, event.Diagnostic.Err); err != nil {
			return err
		}
		return nil
	}
	if event.Result == nil {
		return nil
	}

	result := event.Result
	if r.showFilePath {
		if _, err := io.WriteString(r.stdout, result.Path+":"); err != nil {
			return err
		}
	}
	if r.showLineNumbers {
		line := strconv.Itoa(result.Line)
		if r.multiline && result.EndLine != result.Line {
			line += ".." + strconv.Itoa(result.EndLine)
		}
		if _, err := io.WriteString(r.stdout, line+":"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(r.stdout, result.Content)
	return err
}

func renderListEntry(stdout io.Writer, entry searcher.ListEntry) error {
	entryType := "file"
	size := fmt.Sprintf(" (%d bytes)", entry.Size)
	if entry.IsDir {
		entryType = "directory"
		size = ""
	}
	_, err := fmt.Fprintf(stdout, "%10s %s%s\n", entryType, entry.Name, size)
	return err
}
