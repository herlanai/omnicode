package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitReviewScopesAndReadOnly(t *testing.T) {
	_, p := gitTestRepo(t)
	gitWrite(t, p.Path, "app.txt", "base\n")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "initial")
	gitWrite(t, p.Path, "app.txt", "staged\n")
	gitMust(t, p.Path, "add", ".")
	gitWrite(t, p.Path, "app.txt", "working\n")
	gitWrite(t, p.Path, "new.txt", "new content")
	before := gitMust(t, p.Path, "diff", "--cached")
	for _, scope := range []string{"staged", "worktree", "all"} {
		s, e := gitReviewSnapshot(p.ID, scope)
		if e != nil {
			t.Fatal(e)
		}
		want := 2
		if scope == "staged" {
			want = 1
		}
		if len(s.Files) != want {
			t.Fatalf("%s: %+v", scope, s)
		}
		diff := s.Files[0].Diff
		if scope == "staged" && (strings.Contains(diff, "+working") || !strings.Contains(diff, "+staged")) {
			t.Fatal(diff)
		}
		if scope != "staged" && !strings.Contains(diff, "+working") {
			t.Fatal(diff)
		}
	}
	if before != gitMust(t, p.Path, "diff", "--cached") {
		t.Fatal("index changed")
	}
	content, _ := os.ReadFile(filepath.Join(p.Path, "app.txt"))
	if string(content) != "working\n" {
		t.Fatal("worktree changed")
	}
}
func TestGitReviewSkipsAndUnborn(t *testing.T) {
	_, p := gitTestRepo(t)
	gitWrite(t, p.Path, "ok.txt", "hello")
	gitMust(t, p.Path, "add", "ok.txt")
	gitWrite(t, p.Path, ".env", "SECRET=value")
	gitWrite(t, p.Path, "binary.bin", "\x00binary")
	gitWrite(t, p.Path, "large.txt", strings.Repeat("a", 100001))
	if e := os.Symlink(".env", filepath.Join(p.Path, "link")); e != nil {
		t.Fatal(e)
	}
	s, e := gitReviewSnapshot(p.ID, "all")
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Files) != 1 || len(s.Skipped) != 4 || s.Files[0].Path != "ok.txt" {
		t.Fatalf("%+v", s)
	}
	if _, e = gitReviewSnapshot(p.ID, "bad"); e == nil {
		t.Fatal("invalid scope accepted")
	}
}

func TestGitReviewSkipsLargeWithoutMisclassifyingSource(t *testing.T) {
	_, p := gitTestRepo(t)
	gitWrite(t, p.Path, "source.go", `package main // detects "Binary files "`)
	gitWrite(t, p.Path, "large.txt", strings.Repeat("long line\n", 240000))
	gitWrite(t, p.Path, "binary.bin", "\x00binary")
	gitMust(t, p.Path, "add", ".")
	s, e := gitReviewSnapshot(p.ID, "staged")
	if e != nil || len(s.Files) != 1 || s.Files[0].Path != "source.go" || len(s.Skipped) != 2 {
		t.Fatalf("%+v %v", s, e)
	}
}
