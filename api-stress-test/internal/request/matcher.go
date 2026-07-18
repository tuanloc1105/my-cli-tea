package request

// BodyMatcher is an immutable streaming substring matcher.
// It is safe to reuse concurrently across requests.
type BodyMatcher struct {
	pattern []byte
	prefix  []int
}

// PrepareBodyMatcher prepares an immutable matcher for reuse across requests.
// An empty expectation disables body matching and returns nil.
func PrepareBodyMatcher(expectBody string) *BodyMatcher {
	if expectBody == "" {
		return nil
	}

	pattern := []byte(expectBody)
	prefix := make([]int, len(pattern))
	for i, matched := 1, 0; i < len(pattern); i++ {
		for matched > 0 && pattern[i] != pattern[matched] {
			matched = prefix[matched-1]
		}
		if pattern[i] == pattern[matched] {
			matched++
		}
		prefix[i] = matched
	}

	return &BodyMatcher{pattern: pattern, prefix: prefix}
}

type bodyMatchState struct {
	matcher *BodyMatcher
	matched int
	found   bool
}

func (m *BodyMatcher) newState() bodyMatchState {
	return bodyMatchState{matcher: m}
}

func (s *bodyMatchState) observe(chunk []byte) {
	if s.matcher == nil || s.found {
		return
	}

	for _, b := range chunk {
		for s.matched > 0 && b != s.matcher.pattern[s.matched] {
			s.matched = s.matcher.prefix[s.matched-1]
		}
		if b == s.matcher.pattern[s.matched] {
			s.matched++
		}
		if s.matched == len(s.matcher.pattern) {
			s.found = true
			return
		}
	}
}
