package searcher

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const binaryPreviewSize = 8 << 10

type candidate struct {
	path string
	info os.FileInfo
	err  error
}

func processCandidate(
	ctx context.Context,
	candidate candidate,
	options Options,
	matcher *matcher,
	fs fileSystem,
	events chan<- Event,
) {
	defer close(events)
	if ctx.Err() != nil {
		return
	}
	if candidate.err != nil {
		sendEvent(ctx, events, diagnosticEvent(candidate.path, candidate.err))
		return
	}

	file, err := fs.open(candidate.path)
	if err != nil {
		if errors.Is(err, errUnsafeFileType) {
			return
		}
		sendEvent(ctx, events, diagnosticEvent(candidate.path, fmt.Errorf("open file: %w", err)))
		return
	}
	stopClose := context.AfterFunc(ctx, func() { _ = file.Close() })
	defer stopClose()

	openedInfo, err := file.Stat()
	if err != nil {
		sendEvent(ctx, events, diagnosticEvent(candidate.path, fmt.Errorf("inspect opened file: %w", err)))
		_ = file.Close()
		return
	}
	if !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		return
	}
	if candidate.info != nil && !os.SameFile(candidate.info, openedInfo) {
		sendEvent(ctx, events, diagnosticEvent(candidate.path, errors.New("file identity changed before read")))
		_ = file.Close()
		return
	}

	var previewBuffer [binaryPreviewSize]byte
	count, readErr := io.ReadFull(file, previewBuffer[:])
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		sendEvent(ctx, events, diagnosticEvent(candidate.path, fmt.Errorf("read binary preview: %w", readErr)))
		_ = file.Close()
		return
	}
	preview := previewBuffer[:count]
	if bytes.IndexByte(preview, 0) >= 0 {
		_ = file.Close()
		return
	}
	reader := io.MultiReader(bytes.NewReader(preview), file)

	if options.Multiline {
		readMultiline(ctx, candidate.path, reader, options.MaxMultilineSize, options.MaxResults, matcher, events)
	} else {
		readLines(ctx, candidate.path, reader, options.MaxLineSize, matcher, events)
	}
	if err := file.Close(); err != nil && ctx.Err() == nil {
		sendEvent(ctx, events, diagnosticEvent(candidate.path, fmt.Errorf("close file: %w", err)))
	}
}

func readLines(
	ctx context.Context,
	path string,
	input io.Reader,
	maxLineSize int64,
	matcher *matcher,
	events chan<- Event,
) {
	reader := bufio.NewReaderSize(input, 64<<10)
	lineNumber := 1
	for {
		line, tooLong, done, err := readBoundedLine(ctx, reader, maxLineSize)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				sendEvent(ctx, events, diagnosticEvent(path, fmt.Errorf("read line %d: %w", lineNumber, err)))
			}
			return
		}
		if done {
			return
		}
		if tooLong {
			if !sendEvent(ctx, events, diagnosticEvent(path, fmt.Errorf("line %d exceeds --max-line-size (%d bytes)", lineNumber, maxLineSize))) {
				return
			}
			lineNumber++
			continue
		}
		match, found := matcher.findFirst(line)
		if found {
			result := Result{
				Path:       path,
				Line:       lineNumber,
				EndLine:    lineNumber,
				ByteOffset: match.start,
				Content:    string(line),
			}
			if !sendEvent(ctx, events, Event{Result: &result}) {
				return
			}
		}
		lineNumber++
	}
}

func readBoundedLine(ctx context.Context, reader *bufio.Reader, limit int64) ([]byte, bool, bool, error) {
	var line []byte
	var size int64
	readAny := false
	var lastContentByte byte
	hasContent := false

	for {
		if err := ctx.Err(); err != nil {
			return nil, false, false, err
		}
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			readAny = true
		}
		data := fragment
		hasNewline := len(data) > 0 && data[len(data)-1] == '\n'
		if hasNewline {
			data = data[:len(data)-1]
			if len(data) > 0 && data[len(data)-1] == '\r' {
				data = data[:len(data)-1]
			} else if len(data) == 0 && size > 0 && hasContent && lastContentByte == '\r' {
				size--
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
			}
		}
		if len(data) > 0 {
			lastContentByte = data[len(data)-1]
			hasContent = true
		}

		terminal := err == nil || errors.Is(err, io.EOF)
		if line == nil && size == 0 && terminal {
			if errors.Is(err, io.EOF) && !readAny {
				return nil, false, true, nil
			}
			if int64(len(data)) > limit {
				return nil, true, false, nil
			}
			return data, false, false, nil
		}

		size += int64(len(data))
		remaining := limit - int64(len(line))
		if remaining > 0 {
			appendCount := minInt64(int64(len(data)), remaining)
			line = append(line, data[:appendCount]...)
		}

		switch {
		case err == nil:
			return line, size > limit, false, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && readAny:
			return line, size > limit, false, nil
		case errors.Is(err, io.EOF):
			return nil, false, true, nil
		default:
			return nil, false, false, err
		}
	}
}

func readMultiline(
	ctx context.Context,
	path string,
	input io.Reader,
	limit int64,
	matchLimit int,
	matcher *matcher,
	events chan<- Event,
) {
	content, tooLarge, err := readBoundedContent(ctx, input, limit)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		sendEvent(ctx, events, diagnosticEvent(path, fmt.Errorf("read multiline content: %w", err)))
		return
	}
	if tooLarge {
		sendEvent(ctx, events, diagnosticEvent(path, fmt.Errorf("file exceeds --max-multiline-size (%d bytes)", limit)))
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}

	if bytes.Contains(content, []byte("\r\n")) {
		content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	}
	line := 1
	lastOffset := 0
	matcher.forEach(content, matchLimit, func(match matchIndex) bool {
		if ctx.Err() != nil {
			return false
		}
		line += bytes.Count(content[lastOffset:match.start], []byte("\n"))
		endLine := line + bytes.Count(content[match.start:match.end], []byte("\n"))
		result := Result{
			Path:       path,
			Line:       line,
			EndLine:    endLine,
			ByteOffset: match.start,
			Content:    strings.ReplaceAll(string(content[match.start:match.end]), "\n", `\n`),
		}
		if !sendEvent(ctx, events, Event{Result: &result}) {
			return false
		}
		lastOffset = match.start
		return true
	})
}

func readBoundedContent(ctx context.Context, input io.Reader, limit int64) ([]byte, bool, error) {
	readLimit := limit
	const maxInt64 = int64(^uint64(0) >> 1)
	if readLimit < maxInt64 {
		readLimit++
	}
	content := make([]byte, 0, minInt64(readLimit, 64<<10))
	buffer := make([]byte, minInt64(readLimit, 64<<10))
	emptyReads := 0
	for int64(len(content)) < readLimit {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		remaining := readLimit - int64(len(content))
		readSize := minInt64(remaining, int64(len(buffer)))
		count, err := input.Read(buffer[:readSize])
		if count > 0 {
			content = append(content, buffer[:count]...)
			emptyReads = 0
		} else if err == nil {
			emptyReads++
			if emptyReads >= 100 {
				return nil, false, io.ErrNoProgress
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, false, ctx.Err()
			}
			return nil, false, err
		}
	}
	return content, int64(len(content)) > limit, nil
}

func diagnosticEvent(path string, err error) Event {
	diagnostic := Diagnostic{Path: path, Err: err}
	return Event{Diagnostic: &diagnostic}
}

func sendEvent(ctx context.Context, events chan<- Event, event Event) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func minInt64(left, right int64) int {
	if left < right {
		return int(left)
	}
	return int(right)
}
