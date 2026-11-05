package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitOCRDelegation(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "ocr")
	script := `#!/bin/sh
if [ "$2" = preview ]; then
 echo '{"schema_version":"1","reviewable_files":[{"path":"keep.go"}]}'
else
 [ "$5" = "--" ] || exit 2
 echo '{"schema_version":"1","groups":[{"files":["keep.go"],"rule":"Check nil dereferences"}]}'
fi
`
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	s := GitReviewSnapshot{Scope: "all", Files: []GitReviewFile{{"keep.go", "diff"}, {"skip.bin", "diff"}}}
	if err := enrichOCRSnapshotWith(context.Background(), cli, dir, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Files) != 1 || s.Files[0].Path != "keep.go" || len(s.Skipped) != 1 || !strings.Contains(s.Rules, "nil dereferences") {
		t.Fatalf("bad snapshot: %+v", s)
	}
	// Preview must not erase staged/worktree-only changes absent from HEAD diff.
	for _, scope := range []string{"staged", "worktree"} {
		s = GitReviewSnapshot{Scope: scope, Files: []GitReviewFile{{"skip.bin", "diff"}}}
		if err := enrichOCRSnapshotWith(context.Background(), cli, dir, &s); err != nil || len(s.Files) != 1 {
			t.Fatalf("%s: %+v %v", scope, s, err)
		}
	}
}
func TestGitOCRFailures(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "ocr")
	for _, body := range []string{"echo invalid", `echo '{"schema_version":"2","groups":[]}'`, "exit 1"} {
		os.WriteFile(cli, []byte("#!/bin/sh\n"+body+"\n"), 0700)
		s := GitReviewSnapshot{Scope: "staged", Files: []GitReviewFile{{"file.go", "diff"}}}
		if err := enrichOCRSnapshotWith(context.Background(), cli, dir, &s); err == nil {
			t.Fatal("expected error", body)
		}
	}
	os.WriteFile(cli, []byte("#!/bin/sh\nsleep 20\n"), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := runOCR(ctx, cli, dir, "delegate", "preview"); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("cancellation failed: %v", err)
	}
}

func TestGitOCRBundledResolution(t *testing.T) {
	root := t.TempDir()
	if _, err := bundledOCR(root); err == nil {
		t.Fatal("missing bundle accepted")
	}
	dir := filepath.Join(root, "open-code-review")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "opencodereview")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := bundledOCR(root); err == nil {
		t.Fatal("non-executable bundle accepted")
	}
	if err := os.Chmod(binary, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	got, err := bundledOCR(root)
	if err != nil || got != binary {
		t.Fatalf("bundle should resolve without globally installed CLI: %q %v", got, err)
	}
}
