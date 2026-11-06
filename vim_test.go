package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestVimNativeEditing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.txt")
	put(t, path, "一二三\nsecond")
	a := newApp(root)
	a.open(path)
	store, err := openProjectStore(filepath.Join(t.TempDir(), "settings.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	a.projects = store
	a.prefs, err = store.settings()
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1200, 800)
	if err := tt.Click("Vim 关"); err != nil {
		t.Fatal(err)
	}
	saved, err := store.settings()
	if err != nil || !saved.VimMode {
		t.Fatal("status switch did not save preference", err)
	}
	d := a.current()
	typeKeys := func(keys string) {
		for _, r := range keys {
			tt.Type(string(r))
		}
	}
	want := func(text string) {
		t.Helper()
		if got := d.state.Buffer.String(); got != text {
			t.Fatalf("got %q, want %q", got, text)
		}
	}
	typeKeys("lvld")
	want("一\nsecond")
	typeKeys("u")
	want("一二三\nsecond")
	typeKeys("ggyyGp")
	want("一二三\nsecond\n一二三")
	typeKeys("ddggA")
	tt.Type("追加")
	tt.Key(0, ui.KeyEscape)
	want("一二三追加\nsecond")
	if !tt.HasText("NORMAL") {
		t.Fatal("normal mode status missing")
	}
	tt.Advance(2 * time.Second)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != d.state.Buffer.String() {
		t.Fatalf("autosave: %q, %v", data, err)
	}
	if err := tt.Click("NORMAL"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Vim 关") {
		t.Fatal("missing toggle after disabling")
	}
	tt.Type("x")
	want("一二三追x加\nsecond")
}
