package replacer

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math"
	"testing"
	"unicode/utf8"
)

func FuzzStreamReplace(f *testing.F) {
	f.Add([]byte("prefix-needle-suffix"), []byte("needle"), []byte("replacement"), uint8(3))
	f.Add([]byte("aaaaa"), []byte("aa"), []byte("a-aa"), uint8(1))
	f.Add([]byte("delete me delete me"), []byte("delete "), []byte{}, uint8(4))
	f.Add([]byte("trước € giữa € sau"), []byte("€"), []byte("đồng"), uint8(1))
	f.Add([]byte{'a', 0xe2, 0x82}, []byte("a"), []byte("b"), uint8(2))
	f.Add([]byte{'a', 0, 'b'}, []byte("a"), []byte("b"), uint8(1))

	f.Fuzz(func(t *testing.T, input, search, replacement []byte, rawChunkSize uint8) {
		if len(search) == 0 || len(search) > 128 || len(replacement) > 128 || len(input) > 16*1024 {
			t.Skip()
		}
		chunkSize := int(rawChunkSize)%64 + 1
		wantOutput := bytes.ReplaceAll(input, search, replacement)
		wantMatches := int64(bytes.Count(input, search))
		wantDigest := sha256.Sum256(input)

		analysis, analyzeErr := analyzeStream(
			&maxChunkReader{reader: bytes.NewReader(input), max: chunkSize},
			search,
			replacement,
			math.MaxInt64,
			0,
		)
		assertValidationError(t, analyzeErr, input)
		assertStreamMetrics(t, analysis, input, wantOutput, wantMatches, wantDigest)

		var output bytes.Buffer
		var backup bytes.Buffer
		written, writeErr := writeReplacedStream(
			&maxChunkReader{reader: bytes.NewReader(input), max: chunkSize},
			&output,
			&backup,
			search,
			replacement,
		)
		assertValidationError(t, writeErr, input)
		assertStreamMetrics(t, written, input, wantOutput, wantMatches, wantDigest)
		if !bytes.Equal(output.Bytes(), wantOutput) {
			t.Fatalf("output = %q, want %q", output.Bytes(), wantOutput)
		}
		if !bytes.Equal(backup.Bytes(), input) {
			t.Fatalf("backup = %q, want %q", backup.Bytes(), input)
		}
	})
}

func assertValidationError(t *testing.T, err error, input []byte) {
	t.Helper()
	switch {
	case bytes.IndexByte(input, 0) >= 0:
		if !errors.Is(err, errStreamNUL) {
			t.Fatalf("error = %v, want %v", err, errStreamNUL)
		}
	case !utf8.Valid(input):
		if !errors.Is(err, errStreamInvalidUTF8) {
			t.Fatalf("error = %v, want %v", err, errStreamInvalidUTF8)
		}
	case err != nil:
		t.Fatalf("unexpected error: %v", err)
	}
}
