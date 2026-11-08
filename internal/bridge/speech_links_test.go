package bridge

import (
	"strings"
	"testing"
)

func TestSpeechLinksFiltersAcrossDeltas(t *testing.T) {
	var filter speechLinks
	var b strings.Builder
	for _, s := range []string{"См. ht", "tps://example.", "org/a?q=1", " и [источник](https://example.org/page).", " Готово."} {
		b.WriteString(filter.add(s))
	}
	b.WriteString(filter.flush())
	got := b.String()
	if strings.Contains(got, "https") || strings.Contains(got, "example.org") || !strings.Contains(got, "источник") || !strings.Contains(got, "Готово") {
		t.Fatalf("filtered = %q", got)
	}
}
