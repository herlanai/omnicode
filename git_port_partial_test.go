package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func partialDiff(t *testing.T, a *gitService, p GitProject, path string, staged bool) string {
	t.Helper()
	d, e := a.GitDiff(p.ID, path, staged, "")
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func pickLines(diff string, values ...string) []int {
	var ids []int
	for i, l := range strings.Split(diff, "\n") {
		for _, v := range values {
			if l == v {
				ids = append(ids, i)
			}
		}
	}
	return ids
}
func TestGitPartialStageDiscardAndUnstage(t *testing.T) {
	a, p := gitTestRepo(t)
	path := "a file.txt"
	base := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve\n"
	gitWrite(t, p.Path, path, base)
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	updated := strings.ReplaceAll(strings.ReplaceAll(base, "two\n", "TWO\nextra\n"), "eleven\n", "ELEVEN\n")
	gitWrite(t, p.Path, path, updated)
	d := partialDiff(t, a, p, path, false)
	if e := a.GitApplyLines(p.ID, path, d, pickLines(d, "-two", "+TWO"), "stage"); e != nil {
		t.Fatal(e)
	}
	want := strings.ReplaceAll(base, "two\n", "TWO\n")
	if got := gitMust(t, p.Path, "show", ":"+path); got != want {
		t.Fatalf("index %q want %q", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(p.Path, path)); string(b) != updated {
		t.Fatal("stage modified worktree")
	}
	d = partialDiff(t, a, p, path, false)
	if e := a.GitApplyLines(p.ID, path, d, pickLines(d, "+extra"), "discard"); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(p.Path, path))
	if string(b) != strings.ReplaceAll(updated, "extra\n", "") {
		t.Fatal(string(b))
	}
	d = partialDiff(t, a, p, path, true)
	if e := a.GitApplyLines(p.ID, path, d, pickLines(d, "-two", "+TWO"), "unstage"); e != nil {
		t.Fatal(e)
	}
	if got := gitMust(t, p.Path, "show", ":"+path); got != base {
		t.Fatal(got)
	}
	d = partialDiff(t, a, p, path, false)
	gitWrite(t, p.Path, path, "external change\n")
	if e := a.GitApplyLines(p.ID, path, d, pickLines(d, "+TWO"), "discard"); e == nil {
		t.Fatal("accepted stale diff")
	}
}
func TestGitPartialHunksAndNoNewline(t *testing.T) {
	for _, path := range []string{"normal", "文件\tquoted.txt"} {
		t.Run(path, func(t *testing.T) {
			a, p := gitTestRepo(t)
			base := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm"
			gitWrite(t, p.Path, path, base)
			gitMust(t, p.Path, "add", ".")
			gitMust(t, p.Path, "commit", "-m", "base")
			gitWrite(t, p.Path, path, strings.ReplaceAll(strings.ReplaceAll(base, "b\n", "B\nnew\n"), "m", "M"))
			d := partialDiff(t, a, p, path, false)
			ids := pickLines(d, "-b", "+B", "+new", "-m", "+M")
			if e := a.GitApplyLines(p.ID, path, d, ids, "discard"); e != nil {
				t.Fatal(e)
			}
			b, _ := os.ReadFile(filepath.Join(p.Path, path))
			if string(b) != base {
				t.Fatalf("got %q", b)
			}
		})
	}
}
func TestGitPartialEmptyFile(t *testing.T) {
	a, p := gitTestRepo(t)
	gitWrite(t, p.Path, "empty", "")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	gitWrite(t, p.Path, "empty", "one\ntwo\n")
	d := partialDiff(t, a, p, "empty", false)
	if e := a.GitApplyLines(p.ID, "empty", d, pickLines(d, "+two"), "stage"); e != nil {
		t.Fatal(e)
	}
	if got := gitMust(t, p.Path, "show", ":empty"); got != "two\n" {
		t.Fatal(got)
	}
}
