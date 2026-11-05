package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitIgnoreChoicesAndScopes(t *testing.T) {
	a, p := gitTestRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	os.MkdirAll(filepath.Join(p.Path, "logs", "nested"), 0700)
	gitWrite(t, p.Path, "logs/nested/a.log", "a")
	gitWrite(t, p.Path, "logs/nested/b.log", "b")
	gitWrite(t, p.Path, "logs/nested/c.txt", "c")
	for _, tc := range []struct{ mode, value, scope, pattern string }{
		{"exact", "", "project", "/logs/nested/a.log"},
		{"extension", "", "local", "*.log"},
		{"directory", "logs", "project", "/logs/"},
		{"custom", "**/cache-*", "global", "**/cache-*"},
	} {
		rule, e := a.GitIgnorePreview(p.ID, "logs/nested/a.log", tc.mode, tc.value, tc.scope)
		if e != nil || rule.Pattern != tc.pattern {
			t.Fatal(rule, e)
		}
		if _, e = os.Stat(rule.Location); e == nil && tc.scope == "global" {
			t.Fatal("preview mutated global file")
		}
	}
	if e := a.GitIgnoreSave(p.ID, "logs/nested/a.log", "extension", "", "local"); e != nil {
		t.Fatal(e)
	}
	files, e := gitFiles(p.Path)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".log") {
			t.Fatal("extension not ignored", f)
		}
	}
	if _, e = os.Stat(filepath.Join(p.Path, ".gitignore")); !os.IsNotExist(e) {
		t.Fatal("local modified project ignore")
	}
	if e := a.GitIgnoreSave(p.ID, "logs/nested/c.txt", "custom", "*.tmp", "global"); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(home, "config", "git", "ignore"))
	if e != nil || string(b) != "*.tmp\n" {
		t.Fatal(string(b), e)
	}
	if e := a.GitIgnoreSave(p.ID, "logs/nested/c.txt", "custom", "*.tmp", "global"); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(filepath.Join(home, "config", "git", "ignore"))
	if string(b) != "*.tmp\n" {
		t.Fatal("duplicate rule")
	}
	if _, e := a.GitIgnorePreview(p.ID, "logs/nested/c.txt", "directory", "elsewhere", "project"); e == nil {
		t.Fatal("accepted unrelated directory")
	}
	if _, e := a.GitIgnorePreview(p.ID, "logs/nested/c.txt", "custom", "*.x\n*.y", "project"); e == nil {
		t.Fatal("accepted multiline")
	}
}
func TestGitIgnoreConfiguredGlobalAndDirectory(t *testing.T) {
	a, p := gitTestRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	gitMust(t, p.Path, "config", "--global", "core.excludesFile", "~/my-ignore")
	os.MkdirAll(filepath.Join(p.Path, "cache"), 0700)
	gitWrite(t, p.Path, "cache/a", "a")
	gitWrite(t, p.Path, "cache/b", "b")
	rule, e := a.GitIgnorePreview(p.ID, "cache/a", "exact", "", "global")
	if e != nil || rule.Location != filepath.Join(home, "my-ignore") {
		t.Fatal(rule, e)
	}
	if e := a.GitIgnoreSave(p.ID, "cache/a", "directory", "cache", "project"); e != nil {
		t.Fatal(e)
	}
	files, e := gitFiles(p.Path)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Path, "cache/") {
			t.Fatal(f)
		}
	}
}
