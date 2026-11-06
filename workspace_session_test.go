package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReopenWorkspace(t *testing.T) {
	base := t.TempDir()
	first, second := filepath.Join(base, "alpha"), filepath.Join(base, "beta")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	one, missing, two := filepath.Join(first, "one.go"), filepath.Join(first, "deleted.go"), filepath.Join(second, "two.go")
	for _, path := range []string{one, missing, two} {
		if err := os.WriteFile(path, []byte("saved"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	dbPath := filepath.Join(base, "cache.sqlite")
	start := func(root string) *app {
		a := newApp(root)
		s, err := openProjectStore(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		a.projects = s
		if _, err = s.settings(); err != nil {
			t.Fatal(err)
		}
		return a
	}
	a := start(first)
	f, err := readWorkspaceFolder(second)
	if err != nil {
		t.Fatal(err)
	}
	a.addFolder(f)
	for _, path := range []string{one, missing, two, one} {
		a.open(path)
	}
	a.trackWorkspace = true
	a.checkpointWorkspace()
	a.projects.db.Close()
	if err = os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	next := start("")
	defer next.projects.db.Close()
	if err = next.restoreWorkspace(); err != nil {
		t.Fatal(err)
	}
	if len(next.folders) != 2 || next.root != a.root || len(next.docs) != 2 || next.docs[0].path != a.docs[0].path || next.docs[1].name != "two.go" || next.current() != next.docs[0] {
		t.Fatalf("restored folders %v, root %q, tabs %v, active %d", next.folders, next.root, next.docs, next.active)
	}
}
