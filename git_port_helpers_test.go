package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func gitTestRepo(t *testing.T) (*gitService, GitProject) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	gitOK(t, root, "init", "-b", "main")
	gitOK(t, root, "config", "user.name", "Test")
	gitOK(t, root, "config", "user.email", "test@example.invalid")
	gitOK(t, root, "config", "commit.gpgsign", "false")
	return &gitService{ctx: context.Background(), root: root}, GitProject{ID: root, Name: filepath.Base(root), Path: root}
}
func gitMust(t *testing.T, root string, args ...string) string { return gitOK(t, root, args...) }
func gitWrite(t *testing.T, root, path, text string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); e != nil {
		t.Fatal(e)
	}
	writeGit(t, root, path, text)
}
func gitReviewSnapshot(root, scope string) (GitReviewSnapshot, error) {
	return (&gitService{ctx: context.Background(), root: root}).gitReviewSnapshot(root, scope)
}
func gitLargeFiles(root string) ([]GitLargeFile, error) {
	return (&gitService{ctx: context.Background(), root: root}).gitLargeFiles(root)
}
func gitPendingOperation(root string) string {
	return (&gitService{ctx: context.Background(), root: root}).gitPendingOperation(root)
}
func gitFiles(root string) ([]GitFile, error) {
	return (&gitService{ctx: context.Background(), root: root}).gitFiles(root)
}
func runGit(root string, args ...string) (string, error) {
	return repositoryGit(context.Background(), root, args...)
}
