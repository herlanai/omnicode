package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitLargeFilesIndexSize(t *testing.T) {
	root := t.TempDir()
	if _, err := runGit(root, "init"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name string
		size int
	}{{"exact.bin", 10 * 1024 * 1024}, {"large file.bin", 10*1024*1024 + 1}} {
		if err := os.WriteFile(filepath.Join(root, item.name), make([]byte, item.size), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runGit(root, "add", "."); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "large file.bin"), []byte("smaller working copy"), 0600)
	files, err := gitLargeFiles(root)
	if err != nil || len(files) != 1 || files[0].Path != "large file.bin" || files[0].Size != 10*1024*1024+1 {
		t.Fatalf("%+v %v", files, err)
	}
}

func TestGitLargeFilesSkipsMissingSubmoduleCommit(t *testing.T) {
	root := t.TempDir()
	if _, err := runGit(root, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(root, "update-index", "--add", "--cacheinfo", "160000,1111111111111111111111111111111111111111,submodule"); err != nil {
		t.Fatal(err)
	}
	files, err := gitLargeFiles(root)
	if err != nil || len(files) != 0 {
		t.Fatalf("%+v %v", files, err)
	}
}
