package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitFileDiscardPreservesIndexAndOtherFiles(t *testing.T) {
	a, p := gitTestRepo(t)
	gitWrite(t, p.Path, ":(glob)*", "base")
	gitWrite(t, p.Path, "other", "base")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	gitWrite(t, p.Path, ":(glob)*", "staged")
	if err := a.GitStage(p.ID, ":(glob)*", true); err != nil {
		t.Fatal(err)
	}
	gitWrite(t, p.Path, ":(glob)*", "unstaged")
	gitWrite(t, p.Path, "other", "keep")
	if err := a.GitFileAction(p.ID, ":(glob)*", "discard"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(p.Path, ":(glob)*"))
	if string(b) != "staged" {
		t.Fatal(string(b))
	}
	b, _ = os.ReadFile(filepath.Join(p.Path, "other"))
	if string(b) != "keep" {
		t.Fatal("touched other file")
	}
	if err := a.GitFileAction(p.ID, "../outside", "remove"); err == nil {
		t.Fatal("accepted outside path")
	}
}
func TestGitFileIgnoreEscapesAndProtectsSymlinks(t *testing.T) {
	a, p := gitTestRepo(t)
	gitWrite(t, p.Path, "file*.txt", "data")
	gitWrite(t, p.Path, "file-other.txt", "keep")
	if err := a.GitFileAction(p.ID, "file*.txt", "ignore"); err != nil {
		t.Fatal(err)
	}
	files, err := gitFiles(p.Path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.Path == "file*.txt" {
			t.Fatal("not ignored")
		}
		if f.Path == "file-other.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("ignored wildcard matches")
	}
	contents, _ := os.ReadFile(filepath.Join(p.Path, ".gitignore"))
	if !strings.Contains(string(contents), "/file\\*.txt") {
		t.Fatal(string(contents))
	}
	os.Remove(filepath.Join(p.Path, ".gitignore"))
	target := filepath.Join(t.TempDir(), "external")
	os.WriteFile(target, []byte("untouched"), 0600)
	os.Symlink(target, filepath.Join(p.Path, ".gitignore"))
	if err := a.GitFileAction(p.ID, "file-other.txt", "ignore"); err == nil {
		t.Fatal("wrote through symlink")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "untouched" {
		t.Fatal("modified external")
	}
}
func TestGitFileRemoveUsesTrash(t *testing.T) {
	a, p := gitTestRepo(t)
	gitWrite(t, p.Path, "new", "keep")
	old := gitTrashFile
	defer func() { gitTrashFile = old }()
	var got string
	gitTrashFile = func(path string) error { got = path; return nil }
	if err := a.GitFileAction(p.ID, "new", "remove"); err != nil || got != filepath.Join(p.Path, "new") {
		t.Fatal(got, err)
	}
	got = ""
	if err := a.GitFileAction(p.ID, "new", "discard"); err != nil || got == "" {
		t.Fatal(got, err)
	}
}
