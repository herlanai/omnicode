package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestExplorerFilenameFilter(t *testing.T) {
	a := newApp(t.TempDir())
	for _, rel := range []string{"api/Repo.go", "ui/repository.ts", "repo/other.go", ".git/repo.txt"} {
		path := filepath.Join(a.root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		put(t, path, "package example")
	}
	a.refresh()
	v := ui.NewTester(a.view, 1100, 720)
	if err := v.Click("按文件名筛选"); err != nil {
		t.Fatal(err)
	}
	v.Type("REPO")
	end := time.Now().Add(3 * time.Second)
	for len(a.fileFilter.matches) == 0 && time.Now().Before(end) {
		v.Frame()
		time.Sleep(time.Millisecond)
	}
	if len(a.fileFilter.matches) != 2 {
		t.Fatalf("query %q matched %v", a.fileFilter.query, a.fileFilter.matches)
	}
	if err := v.Click("api/Repo.go"); err != nil {
		t.Fatal(err)
	}
	if a.current() == nil || a.current().path != filepath.Join(a.root, "api/Repo.go") {
		t.Fatal("did not open filtered file")
	}
	if err := v.Click("按文件名筛选"); err != nil {
		t.Fatal(err)
	}
	if a.fileFilter.open || a.fileFilter.query != "" || len(a.expanded) != 0 {
		t.Fatal("closing filter did not restore original tree")
	}
}
