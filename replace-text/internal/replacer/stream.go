package replacer

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

const (
	streamBufferSize              = 64 * 1024
	maxConsecutiveEmptyStreamRead = 100
)

var (
	errStreamEmptySearch    = errors.New("search text is empty")
	errStreamInputTooLarge  = errors.New("input exceeds maximum size")
	errStreamOutputTooLarge = errors.New("replacement output exceeds maximum size")
	errStreamSizeOverflow   = errors.New("replacement size overflows int64")
	errStreamInvalidUTF8    = errors.New("input is not valid UTF-8")
	errStreamNUL            = errors.New("input contains NUL byte")
)

// streamMetrics describes the raw source and the replacement output. Digest is
// always the SHA-256 of the raw bytes consumed from source.
type streamMetrics struct {
	RawBytes    int64
	OutputBytes int64
	Matches     int64
	Digest      [sha256.Size]byte
	UTF8Valid   bool
	HasNUL      bool
}

// analyzeStream scans source without writing replacement data. maxInput must
// be non-negative; maxOutput == 0 disables the output limit.
func analyzeStream(source io.Reader, search, replacement []byte, maxInput, maxOutput int64) (streamMetrics, error) {
	if maxInput < 0 {
		return streamMetrics{}, errors.New("maximum input size is negative")
	}
	return processStream(source, nil, nil, search, replacement, maxInput, maxOutput)
}

func analyzeUnlimitedStream(source io.Reader, search, replacement []byte) (streamMetrics, error) {
	return processStream(source, nil, nil, search, replacement, -1, 0)
}

// writeReplacedStream writes replacement data to destination and, when backup
// is non-nil, copies the exact raw source bytes to it. The caller is expected to
// compare these metrics with a preceding analyzeStream result before commit.
func writeReplacedStream(source io.Reader, destination, backup io.Writer, search, replacement []byte) (streamMetrics, error) {
	if destination == nil {
		return streamMetrics{}, errors.New("replacement destination is nil")
	}
	return processStream(source, destination, backup, search, replacement, -1, 0)
}

func processStream(source io.Reader, destination, backup io.Writer, search, replacement []byte, maxInput, maxOutput int64) (metrics streamMetrics, err error) {
	if len(search) == 0 {
		return metrics, errStreamEmptySearch
	}

	digest := sha256.New()
	defer func() {
		copy(metrics.Digest[:], digest.Sum(nil))
	}()

	matcher := newStreamMatcher(search)
	var validator utf8StreamValidator
	emit := func(data []byte, matched bool) error {
		if matched {
			if metrics.Matches == math.MaxInt64 {
				return errStreamSizeOverflow
			}
			metrics.Matches++
			if destination != nil {
				return writeAll(destination, replacement)
			}
			return nil
		}
		if destination != nil {
			return writeAll(destination, data)
		}
		return nil
	}

	buffer := make([]byte, streamBufferSize)
	emptyReads := 0
	for {
		n, readErr := source.Read(buffer)
		if n < 0 || n > len(buffer) {
			return metrics, fmt.Errorf("read source: invalid byte count %d", n)
		}
		if n > 0 {
			emptyReads = 0
			chunk := buffer[:n]
			if int64(n) > math.MaxInt64-metrics.RawBytes {
				return metrics, errStreamSizeOverflow
			}
			metrics.RawBytes += int64(n)
			_, _ = digest.Write(chunk)
			validator.Write(chunk)
			if bytes.IndexByte(chunk, 0) >= 0 {
				metrics.HasNUL = true
			}
			if backup != nil {
				if writeErr := writeAll(backup, chunk); writeErr != nil {
					return metrics, fmt.Errorf("write raw backup: %w", writeErr)
				}
			}
			if maxInput >= 0 && metrics.RawBytes > maxInput {
				return metrics, fmt.Errorf("%w: %d > %d", errStreamInputTooLarge, metrics.RawBytes, maxInput)
			}
			if matchErr := matcher.Push(chunk, false, emit); matchErr != nil {
				return metrics, fmt.Errorf("write replacement output: %w", matchErr)
			}
		} else if readErr == nil {
			emptyReads++
			if emptyReads >= maxConsecutiveEmptyStreamRead {
				return metrics, io.ErrNoProgress
			}
		}

		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return metrics, fmt.Errorf("read source: %w", readErr)
			}
			break
		}
	}

	if matchErr := matcher.Push(nil, true, emit); matchErr != nil {
		return metrics, fmt.Errorf("write replacement output: %w", matchErr)
	}
	metrics.UTF8Valid = validator.Valid()

	metrics.OutputBytes, err = checkedOutputSize(metrics.RawBytes, metrics.Matches, len(search), len(replacement))
	if err != nil {
		return metrics, err
	}
	if metrics.HasNUL {
		return metrics, errStreamNUL
	}
	if !metrics.UTF8Valid {
		return metrics, errStreamInvalidUTF8
	}
	if maxOutput > 0 && metrics.OutputBytes > maxOutput {
		return metrics, fmt.Errorf("%w: %d > %d", errStreamOutputTooLarge, metrics.OutputBytes, maxOutput)
	}
	return metrics, nil
}

func checkedOutputSize(inputBytes, matches int64, searchLength, replacementLength int) (int64, error) {
	if inputBytes < 0 || matches < 0 || searchLength <= 0 || replacementLength < 0 {
		return 0, errStreamSizeOverflow
	}

	searchSize := int64(searchLength)
	replacementSize := int64(replacementLength)
	if replacementSize >= searchSize {
		growth := replacementSize - searchSize
		if growth != 0 && matches > (math.MaxInt64-inputBytes)/growth {
			return 0, errStreamSizeOverflow
		}
		return inputBytes + matches*growth, nil
	}

	shrink := searchSize - replacementSize
	if shrink != 0 && matches > inputBytes/shrink {
		return 0, errStreamSizeOverflow
	}
	return inputBytes - matches*shrink, nil
}

type streamMatcher struct {
	search  []byte
	pending []byte
}

func newStreamMatcher(search []byte) streamMatcher {
	capacity := streamBufferSize
	if len(search)-1 <= math.MaxInt-capacity {
		capacity += len(search) - 1
	}
	return streamMatcher{
		search:  search,
		pending: make([]byte, 0, capacity),
	}
}

// Push emits unmatched bytes with matched=false and one event per match with
// matched=true. Matches are left-to-right and non-overlapping.
func (m *streamMatcher) Push(chunk []byte, final bool, emit func([]byte, bool) error) error {
	m.pending = append(m.pending, chunk...)
	processEnd := len(m.pending)
	if !final {
		processEnd -= len(m.search) - 1
		if processEnd < 0 {
			processEnd = 0
		}
	}

	cursor := 0
	for cursor < processEnd {
		relative := bytes.Index(m.pending[cursor:], m.search)
		if relative < 0 {
			break
		}
		matchStart := cursor + relative
		if matchStart >= processEnd {
			break
		}
		if matchStart > cursor {
			if err := emit(m.pending[cursor:matchStart], false); err != nil {
				return err
			}
		}
		if err := emit(m.pending[matchStart:matchStart+len(m.search)], true); err != nil {
			return err
		}
		cursor = matchStart + len(m.search)
	}

	if final {
		if cursor < len(m.pending) {
			if err := emit(m.pending[cursor:], false); err != nil {
				return err
			}
		}
		m.pending = m.pending[:0]
		return nil
	}

	keepFrom := processEnd
	if cursor > keepFrom {
		keepFrom = cursor
	}
	if cursor < keepFrom {
		if err := emit(m.pending[cursor:keepFrom], false); err != nil {
			return err
		}
	}
	copy(m.pending, m.pending[keepFrom:])
	m.pending = m.pending[:len(m.pending)-keepFrom]
	return nil
}

type utf8StreamValidator struct {
	pending    [utf8.UTFMax]byte
	pendingLen int
	invalid    bool
}

func (v *utf8StreamValidator) Write(data []byte) {
	if v.invalid {
		return
	}

	if v.pendingLen > 0 {
		for !utf8.FullRune(v.pending[:v.pendingLen]) && len(data) > 0 {
			v.pending[v.pendingLen] = data[0]
			v.pendingLen++
			data = data[1:]
		}
		if utf8.FullRune(v.pending[:v.pendingLen]) {
			r, size := utf8.DecodeRune(v.pending[:v.pendingLen])
			if r == utf8.RuneError && size == 1 {
				v.invalid = true
				return
			}
			v.pendingLen = 0
		}
	}

	for len(data) > 0 {
		if data[0] < utf8.RuneSelf {
			data = data[1:]
			continue
		}
		if !utf8.FullRune(data) {
			v.pendingLen = copy(v.pending[:], data)
			return
		}
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			v.invalid = true
			return
		}
		data = data[size:]
	}
}

func (v *utf8StreamValidator) Valid() bool {
	return !v.invalid && v.pendingLen == 0
}

func writeAll(destination io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := destination.Write(data)
		if n < 0 || n > len(data) {
			return fmt.Errorf("invalid write count %d", n)
		}
		data = data[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
