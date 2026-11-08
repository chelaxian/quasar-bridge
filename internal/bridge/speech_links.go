package bridge

import (
	"regexp"
	"strings"
	"unicode"
)

var spokenURL = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s]*`)

// speechLinks filters URL tokens from streaming text before it reaches the
// sentence buffer. It retains at most the unfinished token between deltas.
type speechLinks struct{ pending string }

func (f *speechLinks) add(delta string) string {
	f.pending += delta
	var out strings.Builder
	start := 0
	for i, r := range f.pending {
		if !unicode.IsSpace(r) {
			continue
		}
		out.WriteString(filterSpokenToken(f.pending[start:i]))
		out.WriteRune(r)
		start = i + len(string(r))
	}
	f.pending = f.pending[start:]
	return out.String()
}

func (f *speechLinks) flush() string {
	return f.flushAll()
}

func stripSpokenLinks(text string) string {
	var f speechLinks
	return f.add(text) + f.flush()
}

func (f *speechLinks) flushAll() string {
	result := filterSpokenToken(f.pending)
	f.pending = ""
	return result
}

func filterSpokenToken(token string) string {
	if loc := spokenURL.FindStringIndex(token); loc != nil {
		prefix := token[:loc[0]]
		if strings.HasSuffix(prefix, "](") {
			prefix = strings.TrimPrefix(strings.TrimSuffix(prefix, "]("), "[")
		} else {
			prefix = strings.TrimRight(prefix, "([{<")
		}
		trailing := strings.TrimRight(token, ")]}>.,!?;:")
		if len(trailing) < len(token) {
			suffix := token[len(trailing):]
			prefix += strings.Trim(suffix, ")]}>")
		}
		return prefix
	}
	return token
}
