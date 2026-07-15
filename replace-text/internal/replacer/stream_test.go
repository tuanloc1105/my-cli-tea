package replacer

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"math"
	"strconv"
	"testing"
	"unicode/utf8"
)

func TestStreamReplaceMatchesBytesPackage(t *testing.T) {
	tests := []struct {
		name        string
		input       []byte
		search      []byte
		replacement []byte
	}{
		{name: "chunk boundary", input: []byte("prefix-needle-suffix"), search: []byte("needle"), replacement: []byte("X")},
		{name: "overlap is non-overlapping", input: []byte("aaaaa"), search: []byte("aa"), replacement: []byte("X")},
		{name: "deletion", input: []byte("abc--abc--"), search: []byte("--"), replacement: nil},
		{name: "replacement contains search", input: []byte("one one"), search: []byte("one"), replacement: []byte("oneone")},
		{name: "unicode split", input: []byte("trước € giữa € sau"), search: []byte("€"), replacement: []byte("đồng")},
		{name: "single byte search", input: []byte("banana"), search: []byte("a"), replacement: []byte("AA")},
		{name: "no match", input: []byte("unchanged"), search: []byte("missing"), replacement: []byte("new")},
		{name: "empty input", input: nil, search: []byte("x"), replacement: []byte("y")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for chunkSize := 1; chunkSize <= len(test.input)+2; chunkSize++ {
				t.Run("chunk="+strconv.Itoa(chunkSize), func(t *testing.T) {
					wantOutput := bytes.ReplaceAll(test.input, test.search, test.replacement)
					wantMatches := int64(bytes.Count(test.input, test.search))
					wantDigest := sha256.Sum256(test.input)

					analysis, err := analyzeStream(
						&maxChunkReader{reader: bytes.NewReader(test.input), max: chunkSize},
						test.search,
						test.replacement,
						math.MaxInt64,
						0,
					)
					if err != nil {
						t.Fatalf("analyzeStream: %v", err)
					}
					assertStreamMetrics(t, analysis, test.input, wantOutput, wantMatches, wantDigest)

					var output bytes.Buffer
					var backup bytes.Buffer
					written, err := writeReplacedStream(
						&maxChunkReader{reader: bytes.NewReader(test.input), max: chunkSize},
						&output,
						&backup,
						test.search,
						test.replacement,
					)
					if err != nil {
						t.Fatalf("writeReplacedStream: %v", err)
					}
					assertStreamMetrics(t, written, test.input, wantOutput, wantMatches, wantDigest)
					if !bytes.Equal(output.Bytes(), wantOutput) {
						t.Fatalf("output = %q, want %q", output.Bytes(), wantOutput)
					}
					if !bytes.Equal(backup.Bytes(), test.input) {
						t.Fatalf("backup = %q, want raw input %q", backup.Bytes(), test.input)
					}
				})
			}
		})
	}
}

func TestStreamReplaceSearchLargerThanBuffer(t *testing.T) {
	search := bytes.Repeat([]byte("needle"), streamBufferSize/6+257)
	input := append([]byte("prefix-"), search...)
	input = append(input, []byte("-suffix")...)
	replacement := []byte("X")
	wantOutput := bytes.ReplaceAll(input, search, replacement)

	analysis, err := analyzeStream(
		&maxChunkReader{reader: bytes.NewReader(input), max: 1021},
		search,
		replacement,
		int64(len(input)),
		0,
	)
	if err != nil {
		t.Fatalf("analyzeStream: %v", err)
	}
	if analysis.Matches != 1 || analysis.OutputBytes != int64(len(wantOutput)) {
		t.Fatalf("analysis = %+v, want one match and %d output bytes", analysis, len(wantOutput))
	}

	var output bytes.Buffer
	if _, err := writeReplacedStream(
		&maxChunkReader{reader: bytes.NewReader(input), max: 997},
		&output,
		nil,
		search,
		replacement,
	); err != nil {
		t.Fatalf("writeReplacedStream: %v", err)
	}
	if !bytes.Equal(output.Bytes(), wantOutput) {
		t.Fatalf("output differs from bytes.ReplaceAll: got %d bytes, want %d", output.Len(), len(wantOutput))
	}
}

func TestStreamValidationCoversWholeInput(t *testing.T) {
	validPrefix := bytes.Repeat([]byte("valid text\n"), streamBufferSize/10+1)
	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{name: "NUL after first buffer", input: append(append([]byte(nil), validPrefix...), 0), want: errStreamNUL},
		{name: "invalid byte after first buffer", input: append(append([]byte(nil), validPrefix...), 0xff), want: errStreamInvalidUTF8},
		{name: "truncated rune at EOF", input: []byte{'a', 0xe2, 0x82}, want: errStreamInvalidUTF8},
		{name: "invalid continuation across boundary", input: []byte{0xe2, 'x', 0xac}, want: errStreamInvalidUTF8},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics, err := analyzeStream(
				&maxChunkReader{reader: bytes.NewReader(test.input), max: 1},
				[]byte("not present"),
				[]byte("replacement"),
				math.MaxInt64,
				0,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if metrics.RawBytes != int64(len(test.input)) {
				t.Fatalf("RawBytes = %d, want %d", metrics.RawBytes, len(test.input))
			}
			if metrics.Digest != sha256.Sum256(test.input) {
				t.Fatalf("Digest = %x, want SHA-256 of full input", metrics.Digest)
			}
			if errors.Is(test.want, errStreamNUL) != metrics.HasNUL {
				t.Fatalf("HasNUL = %t", metrics.HasNUL)
			}
			if errors.Is(test.want, errStreamInvalidUTF8) && metrics.UTF8Valid {
				t.Fatal("UTF8Valid = true for invalid input")
			}
		})
	}
}

func TestStreamLimitsAndCheckedSizing(t *testing.T) {
	t.Run("input cap", func(t *testing.T) {
		metrics, err := analyzeStream(bytes.NewReader([]byte("12345")), []byte("x"), []byte("y"), 4, 0)
		if !errors.Is(err, errStreamInputTooLarge) {
			t.Fatalf("error = %v, want %v", err, errStreamInputTooLarge)
		}
		if metrics.RawBytes != 5 {
			t.Fatalf("RawBytes = %d, want 5", metrics.RawBytes)
		}
	})

	t.Run("output cap", func(t *testing.T) {
		metrics, err := analyzeStream(bytes.NewReader([]byte("aaaa")), []byte("a"), []byte("123"), 4, 11)
		if !errors.Is(err, errStreamOutputTooLarge) {
			t.Fatalf("error = %v, want %v", err, errStreamOutputTooLarge)
		}
		if metrics.OutputBytes != 12 {
			t.Fatalf("OutputBytes = %d, want 12", metrics.OutputBytes)
		}
	})

	t.Run("zero output cap is unlimited", func(t *testing.T) {
		if _, err := analyzeStream(bytes.NewReader([]byte("aaaa")), []byte("a"), []byte("123"), 4, 0); err != nil {
			t.Fatalf("analyzeStream: %v", err)
		}
	})

	t.Run("growth overflow", func(t *testing.T) {
		if _, err := checkedOutputSize(math.MaxInt64-1, 1, 1, 3); !errors.Is(err, errStreamSizeOverflow) {
			t.Fatalf("error = %v, want %v", err, errStreamSizeOverflow)
		}
	})

	t.Run("invalid shrink", func(t *testing.T) {
		if _, err := checkedOutputSize(1, 2, 2, 0); !errors.Is(err, errStreamSizeOverflow) {
			t.Fatalf("error = %v, want %v", err, errStreamSizeOverflow)
		}
	})

	t.Run("empty search", func(t *testing.T) {
		if _, err := analyzeStream(bytes.NewReader(nil), nil, []byte("x"), 0, 0); !errors.Is(err, errStreamEmptySearch) {
			t.Fatalf("error = %v, want %v", err, errStreamEmptySearch)
		}
	})
}

func TestWriteReplacedStreamOptionalBackupAndWriteErrors(t *testing.T) {
	t.Run("backup is optional", func(t *testing.T) {
		var output bytes.Buffer
		if _, err := writeReplacedStream(bytes.NewReader([]byte("old")), &output, nil, []byte("old"), []byte("new")); err != nil {
			t.Fatalf("writeReplacedStream: %v", err)
		}
		if output.String() != "new" {
			t.Fatalf("output = %q, want new", output.String())
		}
	})

	t.Run("destination error", func(t *testing.T) {
		writeErr := errors.New("destination failed")
		_, err := writeReplacedStream(bytes.NewReader([]byte("old")), errorWriter{err: writeErr}, nil, []byte("old"), []byte("new"))
		if !errors.Is(err, writeErr) {
			t.Fatalf("error = %v, want %v", err, writeErr)
		}
	})

	t.Run("backup error", func(t *testing.T) {
		writeErr := errors.New("backup failed")
		_, err := writeReplacedStream(bytes.NewReader([]byte("old")), io.Discard, errorWriter{err: writeErr}, []byte("old"), []byte("new"))
		if !errors.Is(err, writeErr) {
			t.Fatalf("error = %v, want %v", err, writeErr)
		}
	})

	t.Run("short write", func(t *testing.T) {
		_, err := writeReplacedStream(bytes.NewReader([]byte("old")), zeroWriter{}, nil, []byte("old"), []byte("new"))
		if !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("error = %v, want %v", err, io.ErrShortWrite)
		}
	})
}

func TestStreamAllocationsDoNotScaleWithInputSize(t *testing.T) {
	search := []byte("needle")
	replacement := []byte("replacement")
	small := bytes.Repeat([]byte("plain text "), 128)
	large := bytes.Repeat([]byte("plain text "), 256*1024)

	allocations := func(input []byte) float64 {
		return testing.AllocsPerRun(10, func() {
			metrics, err := analyzeStream(bytes.NewReader(input), search, replacement, int64(len(input)), 0)
			if err != nil || metrics.RawBytes != int64(len(input)) {
				panic("unexpected stream analysis result")
			}
		})
	}

	smallAllocs := allocations(small)
	largeAllocs := allocations(large)
	if largeAllocs > smallAllocs+1 {
		t.Fatalf("allocations scale with input: small=%0.1f large=%0.1f", smallAllocs, largeAllocs)
	}
	t.Logf("allocation evidence: small=%0.1f large=%0.1f allocations/run", smallAllocs, largeAllocs)
}

func BenchmarkStreamReplace(b *testing.B) {
	for _, size := range []int{1 << 10, 1 << 20, 16 << 20} {
		input := bytes.Repeat([]byte("plain text with a needle\n"), size/25+1)
		input = input[:size]
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for range b.N {
				if _, err := analyzeStream(bytes.NewReader(input), []byte("needle"), []byte("replacement"), int64(len(input)), 0); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func assertStreamMetrics(t *testing.T, got streamMetrics, input, output []byte, matches int64, digest [sha256.Size]byte) {
	t.Helper()
	if got.RawBytes != int64(len(input)) {
		t.Errorf("RawBytes = %d, want %d", got.RawBytes, len(input))
	}
	if got.OutputBytes != int64(len(output)) {
		t.Errorf("OutputBytes = %d, want %d", got.OutputBytes, len(output))
	}
	if got.Matches != matches {
		t.Errorf("Matches = %d, want %d", got.Matches, matches)
	}
	if got.Digest != digest {
		t.Errorf("Digest = %x, want %x", got.Digest, digest)
	}
	if got.UTF8Valid != utf8.Valid(input) {
		t.Errorf("UTF8Valid = %t, want %t", got.UTF8Valid, utf8.Valid(input))
	}
	if got.HasNUL != bytes.ContainsRune(input, 0) {
		t.Errorf("HasNUL = %t, want %t", got.HasNUL, bytes.ContainsRune(input, 0))
	}
}

type maxChunkReader struct {
	reader io.Reader
	max    int
}

func (r *maxChunkReader) Read(data []byte) (int, error) {
	if len(data) > r.max {
		data = data[:r.max]
	}
	return r.reader.Read(data)
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) {
	return 0, nil
}
