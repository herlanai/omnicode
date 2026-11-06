package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

func TestAssistantOCRSnapshot(t *testing.T) {
	before := &aiFile{Data: []byte("old\n")}
	after := &aiFile{Data: []byte("new\n")}
	s := assistantReviewSnapshot([]aiChange{
		{Path: "edit.go", Before: before, After: after},
		{Path: "new.go", After: after},
		{Path: "deleted.go", Before: before},
		{Path: ".env", After: after},
		{Path: "binary", After: &aiFile{Data: []byte{0}}},
	})
	if len(s.Files) != 3 || len(s.Skipped) != 2 || !strings.Contains(s.Files[0].Diff, "-old\n+new") || !strings.Contains(s.Files[1].Diff, "+new") || !strings.Contains(s.Files[2].Diff, "-old") {
		t.Fatal(s)
	}
}

func TestAssistantOCRReviewsApprovalCopy(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("uses the bundled macOS arm64 runtime")
	}
	root, temp := t.TempDir(), t.TempDir()
	prefs := defaultPreferences()
	prefs.PiPath = filepath.Join(t.TempDir(), "pi")
	t.Setenv("DEEPSEEK_API_KEY", "test")
	// Check that the delegated reviewer actually receives both the bundled
	// rules and the pending edit, even in a project without Git metadata.
	script := "#!/bin/sh\ngrep -q reviewRules git-review.json && grep -q pending_edit git-review.json || exit 1\necho '{\"type\":\"message_update\",\"assistantMessageEvent\":{\"type\":\"text_delta\",\"delta\":\"reviewed\"}}'\n"
	if err := os.WriteFile(prefs.PiPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	events := make(chan aiEvent, 8)
	err := reviewAssistantChanges(context.Background(), prefs, &aiReview{Root: root, Temp: temp}, []aiChange{{Path: "new.go", After: &aiFile{Data: []byte("pending_edit")}}}, events)
	if err != nil {
		t.Fatal(err)
	}
	close(events)
	var output string
	for ev := range events {
		output += ev.text
	}
	if !strings.Contains(output, "reviewed") {
		t.Fatal(output)
	}
}

func TestCurrentFileSnapshotUsesEditorContents(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("disk"), 0600); err != nil {
		t.Fatal(err)
	}
	doc := newDocument("main.go", "editor line one\neditor line two")
	doc.path = path
	s, err := currentFileSnapshot(root, doc)
	if err != nil || s.Scope != "current-file" || len(s.Files) != 1 || s.Files[0].Path != "main.go" || s.Files[0].Diff != doc.state.Buffer.String() {
		t.Fatalf("snapshot %+v, error %v", s, err)
	}
	doc.path = filepath.Join(root, ".env")
	if _, err := currentFileSnapshot(root, doc); err == nil {
		t.Fatal("sensitive file accepted")
	}
}

func TestCurrentFileReviewStreamsBeforeEngineExit(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires native sandbox")
	}
	a := newApp(t.TempDir())
	dir := t.TempDir()
	a.prefs.PiPath = filepath.Join(dir, "pi")
	ack := filepath.Join(dir, "continue")
	t.Setenv("REVIEW_STREAM_ACK", ack)
	t.Setenv("DEEPSEEK_API_KEY", "test")
	script := `#!/bin/sh
echo '{"type":"message_update","assistantMessageEvent":{"type":"thinking_start"}}'
echo '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"first"}}'
while [ ! -f "$REVIEW_STREAM_ACK" ]; do sleep 0.05; done
echo '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":" second"}}'
`
	if err := os.WriteFile(a.prefs.PiPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r := &gitReviewUI{root: a.root, fileSnapshot: &GitReviewSnapshot{Scope: "current-file", Files: []GitReviewFile{{Path: "file.go", Diff: "package main"}}}}
	a.startGitReview(r)
	defer r.cancel()
	deadline := time.After(10 * time.Second)
	var thinking bool
	var output string
	for {
		select {
		case ev := <-r.events:
			if strings.Contains(ev.status, "思考") {
				thinking = true
			}
			output += ev.text
			if ev.text == "first" {
				if !thinking {
					t.Fatal("engine progress was lost")
				}
				if err := os.WriteFile(ack, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if ev.done {
				if ev.err != nil || output != "first second" {
					t.Fatalf("output %q, error %v", output, ev.err)
				}
				return
			}
		case <-deadline:
			t.Fatal("review did not deliver progress and text before process exit")
		}
	}
}
