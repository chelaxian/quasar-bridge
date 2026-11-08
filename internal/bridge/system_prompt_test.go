package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemPromptExtraReadsFileEachTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.txt")
	e := &Engine{SystemPromptFile: path}
	if got := e.systemPromptExtra(); got != "" {
		t.Fatalf("missing file = %q", got)
	}
	if err := os.WriteFile(path, []byte(" без ссылок \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := e.systemPromptExtra(); got != "без ссылок" {
		t.Fatalf("got %q", got)
	}
	if err := os.WriteFile(path, []byte("другое"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := e.systemPromptExtra(); got != "другое" {
		t.Fatalf("not reloaded: %q", got)
	}
}

func TestStripSpokenLinks(t *testing.T) {
	got := stripSpokenLinks("Смотрите https://a.example/x, а также [сайт](https://b.example). Всё.")
	if strings.Contains(got, "example") || !strings.Contains(got, "сайт") || !strings.Contains(got, "Всё") {
		t.Fatalf("got %q", got)
	}
}
