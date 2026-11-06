package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestProjectsInOneWindow(t *testing.T) {
	base := t.TempDir()
	folder := func(name string) *workspaceFolder {
		path := filepath.Join(base, name)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(path, name+".go"), "package demo")
		f, err := readWorkspaceFolder(path)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	first, second, third := folder("alpha"), folder("beta"), folder("gamma")
	a := newApp(first.path)
	a.open(filepath.Join(first.path, "alpha.go"))
	original := a.current()
	a.addFolder(second)
	view := ui.NewTester(a.view, 1100, 720)
	if !view.HasText("alpha.go") || !view.HasText("beta.go") {
		t.Fatal("missing sibling trees")
	}
	if err := view.Click("beta.go"); err != nil {
		t.Fatal(err)
	}
	if a.root != second.path || a.current().name != "beta.go" {
		t.Fatal("wrong active project")
	}
	if len(findExplorerFiles(context.Background(), []string{first.path, second.path}, ".go")) != 2 {
		t.Fatal("missing files from workspace search")
	}
	a.chooseFolder(first.path)
	if len(a.folders) != 2 || a.root != first.path {
		t.Fatal("existing project duplicated")
	}
	a.activateFolder(second)
	a.replaceFolder(third)
	if len(a.folders) != 2 || a.folders[0].path != first.path || a.folders[1].path != third.path || a.root != third.path || len(a.docs) != 1 || a.docs[0] != original {
		t.Fatal("replace affected unrelated project")
	}
}
