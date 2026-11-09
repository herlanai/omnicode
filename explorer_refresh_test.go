package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestExplorerExternalRename(t *testing.T) {
	a := newApp(t.TempDir())
	sub := filepath.Join(a.root, "folder")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(sub, "old.go"), "code")
	a.refresh()
	e := a.folders[0].entries[0]
	e.children, _ = children(sub)
	e.loaded = true
	a.expanded[sub] = true
	view := ui.NewTester(a.view, 1200, 780)
	if err := os.Rename(filepath.Join(sub, "old.go"), filepath.Join(sub, "new.go")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view.Frame()
		if len(e.children) == 1 && e.children[0].name == "new.go" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expanded folder did not refresh")
}
