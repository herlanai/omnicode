package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestRecentEditedFilesPersistenceAndScope(t *testing.T) {
	root := t.TempDir()
	store, err := openProjectStore(filepath.Join(t.TempDir(), "cache.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	a := newApp(root)
	root = a.root
	a.projects = store
	for i := 0; i < 12; i++ {
		path := filepath.Join(root, fmt.Sprintf("%02d.txt", i))
		put(t, path, "text")
		a.rememberEditedFile(path)
	}
	newest := filepath.Join(root, "00.txt")
	a.rememberEditedFile(newest)
	external := filepath.Join(t.TempDir(), "other.txt")
	put(t, external, "other")
	a.rememberEditedFile(external)
	b := newApp(root)
	b.projects = store
	b.loadRecentFiles()
	paths := b.visibleRecentFiles()
	if len(paths) != 10 || paths[0] != newest {
		t.Fatalf("wrong recents: %v", paths)
	}
	if err := os.Remove(newest); err != nil {
		t.Fatal(err)
	}
	if b.visibleRecentFiles()[0] == newest {
		t.Fatal("missing file still visible")
	}
}

func TestRecentEditedFileSaveAndOpen(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "note.txt")
	put(t, path, "old")
	a := newApp(root)
	a.open(path)
	if len(a.recentFiles) != 0 {
		t.Fatal("opening is not editing")
	}
	if !a.save(a.current(), false) {
		t.Fatal(a.notice)
	}
	a.docs = nil
	a.active = -1
	view := ui.NewTester(a.view, 1440, 900)
	if err := view.Click("打开最近编辑文件 " + path); err != nil {
		t.Fatal(err)
	}
	if a.current() == nil || a.current().path != path {
		t.Fatal("file not opened")
	}
	if os.Getenv("OMNICODE_RECENT_FILES_SCREENSHOT") != "" {
		os.MkdirAll("artifacts", 0755)
		f, err := os.Create("artifacts/recent-files.png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, view.Image()); err != nil {
			t.Fatal(err)
		}
	}
	if err := view.Click("最近编辑"); err != nil {
		t.Fatal(err)
	}
	if _, ok := view.Find("打开最近编辑文件 " + path); ok {
		t.Fatal("collapse failed")
	}
}
