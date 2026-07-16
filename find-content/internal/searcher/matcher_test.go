package searcher

import (
	"strings"
	"testing"
)

func TestMatcherModesUnicodeAndOffsets(t *testing.T) {
	tests := []struct {
		name          string
		keyword       string
		content       string
		useRegex      bool
		caseSensitive bool
		multiline     bool
		want          []matchIndex
	}{
		{name: "literal sensitive", keyword: "Needle", content: "Needle needle", caseSensitive: true, want: []matchIndex{{0, 6}}},
		{name: "literal insensitive", keyword: "Needle", content: "needle NEEDLE", want: []matchIndex{{0, 6}, {7, 13}}},
		{name: "regex sensitive", keyword: `n.edle`, content: "nXedle", useRegex: true, caseSensitive: true, want: []matchIndex{{0, 6}}},
		{name: "regex insensitive", keyword: `n.edle`, content: "NXEDLE", useRegex: true, want: []matchIndex{{0, 6}}},
		{name: "multiline escape", keyword: `a\nb`, content: "a\nb", caseSensitive: true, multiline: true, want: []matchIndex{{0, 3}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matcher, err := newMatcher(test.keyword, test.useRegex, test.caseSensitive, test.multiline)
			if err != nil {
				t.Fatal(err)
			}
			assertMatchIndices(t, collectMatcherMatches(matcher, []byte(test.content), 0), test.want)
		})
	}

	content := "xⱥy"
	matcher, err := newMatcher("Ⱥ", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(content, "ⱥ")
	assertMatchIndices(t, collectMatcherMatches(matcher, []byte(content), 0), []matchIndex{{start: start, end: start + len("ⱥ")}})
}

func TestMatcherRejectsEmptyAndInvalidRegex(t *testing.T) {
	if _, err := newMatcher("", false, true, false); err == nil {
		t.Fatal("empty keyword was accepted")
	}
	if _, err := newMatcher("[", true, true, false); err == nil {
		t.Fatal("invalid regex was accepted")
	}
}

func TestMatcherZeroWidthProgressIsRuneAware(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    []matchIndex
	}{
		{pattern: `^`, input: "é", want: []matchIndex{{0, 0}}},
		{pattern: `$`, input: "é", want: []matchIndex{{2, 2}}},
		{pattern: `a*`, input: "é", want: []matchIndex{{0, 0}, {2, 2}}},
		{pattern: `(?m)^`, input: "α\nβ", want: []matchIndex{{0, 0}, {3, 3}}},
	}
	for _, test := range tests {
		t.Run(test.pattern, func(t *testing.T) {
			matcher, err := newMatcher(test.pattern, true, true, true)
			if err != nil {
				t.Fatal(err)
			}
			assertMatchIndices(t, collectMatcherMatches(matcher, []byte(test.input), 0), test.want)
		})
	}
}

func TestMatcherIteratorMatchesRegexpSemantics(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
	}{
		{pattern: `^|a`, input: "a"},
		{pattern: `(?m)^|a`, input: "ba"},
		{pattern: `(?m)^aa|a`, input: "aaa"},
		{pattern: `\b`, input: "a b"},
		{pattern: `aa|a`, input: "aaa"},
		{pattern: `a*`, input: "é"},
		{pattern: `0$|`, input: "0"},
	}
	for _, test := range tests {
		t.Run(test.pattern+"/"+test.input, func(t *testing.T) {
			matcher, err := newMatcher(test.pattern, true, true, true)
			if err != nil {
				t.Fatal(err)
			}
			assertMatchesRegexp(t, matcher, []byte(test.input))
		})
	}
}

func FuzzMatcher(f *testing.F) {
	f.Add("needle", "a needle b", false, false)
	f.Add(`^|$`, "é", true, true)
	f.Fuzz(func(t *testing.T, keyword, content string, useRegex, caseSensitive bool) {
		if keyword == "" {
			return
		}
		matcher, err := newMatcher(keyword, useRegex, caseSensitive, true)
		if err != nil {
			return
		}
		contentBytes := []byte(content)
		matches := collectMatcherMatches(matcher, contentBytes, 0)
		lastEnd := 0
		for index, match := range matches {
			if match.start < 0 || match.end < match.start || match.end > len(content) {
				t.Fatalf("invalid match %+v for %d bytes", match, len(content))
			}
			if index > 0 && match.start < lastEnd {
				t.Fatalf("overlapping/out-of-order match %+v after end %d", match, lastEnd)
			}
			lastEnd = match.end
		}
		locations := matcher.regex.FindAllIndex(contentBytes, -1)
		if len(matches) != len(locations) {
			t.Fatalf("iterator matches = %+v, regexp matches = %+v", matches, locations)
		}
		for index, location := range locations {
			if matches[index] != (matchIndex{start: location[0], end: location[1]}) {
				t.Fatalf("iterator matches = %+v, regexp matches = %+v", matches, locations)
			}
		}
	})
}

func BenchmarkMatcher(b *testing.B) {
	matcher, err := newMatcher("Needle", false, false, false)
	if err != nil {
		b.Fatal(err)
	}
	content := []byte(strings.Repeat("haystack ", 128) + "needle")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matcher.forEach(content, 0, func(matchIndex) bool { return true })
	}
}

func collectMatcherMatches(matcher *matcher, content []byte, limit int) []matchIndex {
	var matches []matchIndex
	matcher.forEach(content, limit, func(match matchIndex) bool {
		matches = append(matches, match)
		return true
	})
	return matches
}

func assertMatchIndices(t *testing.T, got, want []matchIndex) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("matches = %+v, want %+v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("matches = %+v, want %+v", got, want)
		}
	}
}

func assertMatchesRegexp(t *testing.T, matcher *matcher, content []byte) {
	t.Helper()
	locations := matcher.regex.FindAllIndex(content, -1)
	want := make([]matchIndex, 0, len(locations))
	for _, location := range locations {
		want = append(want, matchIndex{start: location[0], end: location[1]})
	}
	assertMatchIndices(t, collectMatcherMatches(matcher, content, 0), want)
}
