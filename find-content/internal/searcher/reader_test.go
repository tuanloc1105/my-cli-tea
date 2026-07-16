package searcher

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestReadLinesBoundariesCRLFAndRecovery(t *testing.T) {
	matcher, err := newMatcher("needle", false, true, false)
	if err != nil {
		t.Fatal(err)
	}

	exact := collectReaderEvents(func(events chan<- Event) {
		readLines(context.Background(), "exact.txt", strings.NewReader("needle\r\n"), 6, matcher, events)
	})
	if len(exact) != 1 || exact[0].Result == nil || exact[0].Result.Content != "needle" || exact[0].Result.Line != 1 {
		t.Fatalf("exact boundary events = %+v", exact)
	}

	recovered := collectReaderEvents(func(events chan<- Event) {
		readLines(context.Background(), "long.txt", strings.NewReader("1234567\nneedle\n"), 6, matcher, events)
	})
	if len(recovered) != 2 || recovered[0].Diagnostic == nil || recovered[1].Result == nil || recovered[1].Result.Line != 2 {
		t.Fatalf("oversized recovery events = %+v", recovered)
	}
	if !strings.Contains(recovered[0].Diagnostic.Err.Error(), "line 1 exceeds") {
		t.Fatalf("oversized diagnostic = %v", recovered[0].Diagnostic.Err)
	}

	boundaryContent := strings.Repeat("a", 15) + "\r\n"
	line, tooLong, done, err := readBoundedLine(
		context.Background(),
		bufio.NewReaderSize(strings.NewReader(boundaryContent), 16),
		15,
	)
	if err != nil || tooLong || done || string(line) != strings.Repeat("a", 15) {
		t.Fatalf("split CRLF line/tooLong/done/error = %q/%v/%v/%v", line, tooLong, done, err)
	}
}

func TestReadLinesStreamsFilesLargerThanLineLimit(t *testing.T) {
	matcher, err := newMatcher("needle", false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("short\n", 1000) + "needle\n"
	events := collectReaderEvents(func(events chan<- Event) {
		readLines(context.Background(), "large.txt", strings.NewReader(content), 6, matcher, events)
	})
	if len(events) != 1 || events[0].Result == nil || events[0].Result.Line != 1001 {
		t.Fatalf("streamed events = %+v", events)
	}
}

func TestReadMultilineLimitCRLFAndOccurrences(t *testing.T) {
	matcher, err := newMatcher(`a\nb`, false, true, true)
	if err != nil {
		t.Fatal(err)
	}
	content := "a\r\nb"
	exact := collectReaderEvents(func(events chan<- Event) {
		readMultiline(context.Background(), "exact.txt", strings.NewReader(content), int64(len(content)), 0, matcher, events)
	})
	if len(exact) != 1 || exact[0].Result == nil || exact[0].Result.Line != 1 || exact[0].Result.EndLine != 2 || exact[0].Result.Content != `a\nb` {
		t.Fatalf("exact multiline events = %+v", exact)
	}

	over := collectReaderEvents(func(events chan<- Event) {
		readMultiline(context.Background(), "over.txt", strings.NewReader(content+"x"), int64(len(content)), 0, matcher, events)
	})
	if len(over) != 1 || over[0].Diagnostic == nil || !strings.Contains(over[0].Diagnostic.Err.Error(), "exceeds --max-multiline-size") {
		t.Fatalf("oversized multiline events = %+v", over)
	}

	repeatedMatcher, err := newMatcher("aa", false, true, true)
	if err != nil {
		t.Fatal(err)
	}
	repeated := collectReaderEvents(func(events chan<- Event) {
		readMultiline(context.Background(), "repeat.txt", strings.NewReader("aaaa"), 4, 0, repeatedMatcher, events)
	})
	if len(repeated) != 2 || repeated[0].Result.ByteOffset != 0 || repeated[1].Result.ByteOffset != 2 {
		t.Fatalf("non-overlapping multiline events = %+v", repeated)
	}
}

func TestReadMultilineZeroWidthAtUTF8Boundaries(t *testing.T) {
	matcher, err := newMatcher(`a*`, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	events := collectReaderEvents(func(events chan<- Event) {
		readMultiline(context.Background(), "utf8.txt", strings.NewReader("é"), 2, 0, matcher, events)
	})
	if len(events) != 2 || events[0].Result.ByteOffset != 0 || events[1].Result.ByteOffset != 2 {
		t.Fatalf("zero-width UTF-8 events = %+v", events)
	}
}

func BenchmarkReader(b *testing.B) {
	line := strings.Repeat("a", 4096) + "\n"
	content := []byte(strings.Repeat(line, 128))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reader := bufio.NewReaderSize(bytes.NewReader(content), 64<<10)
		for {
			_, _, done, err := readBoundedLine(context.Background(), reader, 8192)
			if err != nil {
				b.Fatal(err)
			}
			if done {
				break
			}
		}
	}
}

func collectReaderEvents(run func(chan<- Event)) []Event {
	events := make(chan Event)
	done := make(chan struct{})
	go func() {
		run(events)
		close(events)
		close(done)
	}()
	var collected []Event
	for event := range events {
		collected = append(collected, event)
	}
	<-done
	return collected
}
