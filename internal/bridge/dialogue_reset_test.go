package bridge

import (
	"os"
	"path/filepath"
	"testing"

	"quasarbridge/internal/dialogue"
)

func TestDialogueResetFileClearsContextOnce(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "reset")
	store := dialogue.New(dialogue.DefaultConfig())
	e := &Engine{Dialogue: store, DialogueResetFile: marker}
	ctx := dialogue.WithSpeaker(t.Context(), "device:test")
	ctx, _, ticket := store.Start(ctx)
	if !store.Commit(ctx, ticket, "вопрос", "ответ", false) {
		t.Fatal("commit failed")
	}
	e.Prepare(dialogue.WithSpeaker(t.Context(), "device:test"))
	if len(store.View(dialogue.WithSpeaker(t.Context(), "device:test")).Messages) == 0 {
		t.Fatal("context cleared without marker")
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e.Prepare(dialogue.WithSpeaker(t.Context(), "device:test"))
	if len(store.View(dialogue.WithSpeaker(t.Context(), "device:test")).Messages) != 0 {
		t.Fatal("context survived marker")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("marker not consumed")
	}
}
