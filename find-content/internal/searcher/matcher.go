package searcher

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

type matchIndex struct {
	start int
	end   int
}

type matcher struct {
	regex        *regexp.Regexp
	contextRegex *regexp.Regexp
}

func newMatcher(keyword string, useRegex, caseSensitive, multiline bool) (*matcher, error) {
	if keyword == "" {
		return nil, errors.New("keyword must not be empty")
	}
	pattern := keyword
	if multiline {
		pattern = strings.ReplaceAll(pattern, `\n`, "\n")
	}
	if !useRegex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !caseSensitive {
		pattern = "(?i:" + pattern + ")"
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	contextRegex, err := regexp.Compile("(?s:.)(?:" + pattern + ")")
	if err != nil {
		return nil, err
	}
	return &matcher{regex: re, contextRegex: contextRegex}, nil
}

func (m *matcher) findFirst(content []byte) (matchIndex, bool) {
	location := m.regex.FindIndex(content)
	if location == nil {
		return matchIndex{}, false
	}
	return matchIndex{start: location[0], end: location[1]}, true
}

func (m *matcher) forEach(content []byte, limit int, yield func(matchIndex) bool) {
	match, found := m.findFirst(content)
	if !found {
		return
	}
	emitted := 0
	for {
		if !yield(match) {
			return
		}
		emitted++
		if limit > 0 && emitted == limit {
			return
		}

		var offset int
		if match.end > match.start {
			offset = match.end
		} else {
			if match.start == len(content) {
				return
			}
			_, size := utf8.DecodeRune(content[match.start:])
			if size == 0 {
				size = 1
			}
			offset = match.start + size
		}

		next, found := m.nextAtOrAfter(content, offset)
		if !found {
			return
		}
		if next.start == next.end && next.start == match.end {
			if next.start == len(content) {
				return
			}
			_, size := utf8.DecodeRune(content[next.start:])
			next, found = m.nextAtOrAfter(content, next.start+size)
			if !found {
				return
			}
		}
		match = next
	}
}

func (m *matcher) nextAtOrAfter(content []byte, offset int) (matchIndex, bool) {
	_, prefixSize := utf8.DecodeLastRune(content[:offset])
	prefixStart := offset - prefixSize
	location := m.contextRegex.FindIndex(content[prefixStart:])
	if location == nil {
		return matchIndex{}, false
	}
	matchStart := prefixStart + location[0]
	_, prefixSize = utf8.DecodeRune(content[matchStart:])
	return matchIndex{start: matchStart + prefixSize, end: prefixStart + location[1]}, true
}
