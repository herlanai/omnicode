package main

import (
	"context"
	"github.com/HyperparameterSI/golet/backend/ui"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAIReviewApprovalConflictAndDelete(t *testing.T) {
	root := t.TempDir()
	temp := t.TempDir()
	path := filepath.Join(root, "main.go")
	os.WriteFile(path, []byte("before\n"), 0644)
	r, err := prepareAIReview(root, temp, true)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(r.Work, "main.go"), []byte("after\n"), 0644)
	os.WriteFile(filepath.Join(r.Work, "new.go"), []byte("new\n"), 0644)
	changes, err := r.scan()
	if err != nil || len(changes) != 2 {
		t.Fatal("changes missing", err)
	}
	original, _ := os.ReadFile(path)
	if string(original) != "before\n" {
		t.Fatal("unapproved write")
	}
	os.WriteFile(path, []byte("user edit\n"), 0644)
	if err := applyAIChange(root, changes[0]); err == nil {
		t.Fatal("conflict overwritten")
	}
	os.WriteFile(path, []byte("before\n"), 0644)
	for _, c := range changes {
		if err := applyAIChange(root, c); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadFile(path)
	if string(after) != "after\n" {
		t.Fatal("approval not applied")
	}
	deleteChange := aiChange{Path: "main.go", Before: changes[0].After}
	if err := applyAIChange(root, deleteChange); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("delete not applied")
	}
}
func TestAIReviewRejectAndSymlink(t *testing.T) {
	root := t.TempDir()
	a := newApp(root)
	r, err := prepareAIReview(root, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(r.Work, "new.txt"), []byte("pending"), 0644)
	r.Changes, _ = r.scan()
	a.ai.review = r
	a.discardAIChanges()
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("rejected file applied")
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(root, "link"))
	c := aiChange{Path: "link/escape", After: &aiFile{Data: []byte("bad"), Mode: 0644}}
	if applyAIChange(root, c) == nil {
		t.Fatal("symlink escape allowed")
	}
	if _, err := prepareAIReview(root, t.TempDir(), true); err == nil {
		t.Fatal("snapshot followed symlink")
	}
}
func TestAIIsolatedApprovalEndToEnd(t *testing.T) {
	if os.Getenv("OMNICODE_AI_SANDBOX_TEST") != "1" {
		t.Skip("native sandbox requires an unsandboxed test host")
	}
	root := t.TempDir()
	engine := filepath.Join(t.TempDir(), "fake-agent")
	// Try both normal relative writes and an absolute write to the real workspace.
	script := "#!/bin/sh\nprintf 'proposal' > proposed.txt\nif printf 'escaped' > '" + filepath.Join(root, "escape.txt") + "'; then exit 9; fi\nprintf 'ready'\n"
	os.WriteFile(engine, []byte(script), 0700)
	a := newApp(root)
	a.prefs.Engine = "CodeBuddy CLI"
	a.prefs.BuddyPath = engine
	a.ai.draft = "edit"
	a.sendAI()
	if !a.ai.running {
		t.Fatal(a.ai.err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for a.ai.running && time.Now().Before(deadline) {
		a.drainAI()
		time.Sleep(10 * time.Millisecond)
	}
	if a.ai.running || a.ai.err != "" {
		t.Fatal("isolated task failed:", a.ai.err)
	}
	if !a.hasPendingAI() {
		t.Fatal("missing approval")
	}
	if _, err := os.Stat(filepath.Join(root, "proposed.txt")); !os.IsNotExist(err) {
		t.Fatal("applied before approval")
	}
	if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("sandbox escape")
	}
	a.approveAIChanges(map[string]bool{"proposed.txt": true})
	data, err := os.ReadFile(filepath.Join(root, "proposed.txt"))
	if err != nil || string(data) != "proposal" {
		t.Fatal("approval failed", err)
	}
}

func TestDSHLiveApproval(t *testing.T) {
	if os.Getenv("OMNICODE_DSH_APPROVAL_TEST") != "1" {
		t.Skip("opt-in DSH review flow")
	}
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	os.WriteFile(path, []byte("before\n"), 0644)
	a := newApp(root)
	a.prefs.Engine = "DSH"
	a.ai.draft = "把当前工作目录的 note.txt 内容替换为 after，文件末尾保留换行。只修改这个文件，完成后简短回复。"
	a.sendAI()
	if !a.ai.running {
		t.Fatal(a.ai.err)
	}
	defer func() {
		if a.ai.cancel != nil {
			a.ai.cancel()
		}
		a.discardAIChanges()
	}()
	deadline := time.Now().Add(55 * time.Second)
	for a.ai.running && time.Now().Before(deadline) {
		a.drainAI()
		time.Sleep(20 * time.Millisecond)
	}
	if a.ai.running || a.ai.err != "" {
		t.Fatal("DSH review task failed", a.ai.err)
	}
	original, _ := os.ReadFile(path)
	if string(original) != "before\n" {
		t.Fatal("original changed without approval")
	}
	if !a.hasPendingAI() {
		// Diagnostics contain model output only in this opt-in test with no configured Omnicode key.
		t.Fatalf("no proposal from DSH: %s", a.ai.messages[len(a.ai.messages)-1].text)
	}
	a.approveAIChanges(map[string]bool{"note.txt": true})
	after, _ := os.ReadFile(path)
	if string(after) != "after\n" {
		t.Fatal("approved content not applied")
	}
	t.Log("DSH wrote its isolated copy; original changed only after approval")
}

func TestAIChangeListAndApprovalUI(t *testing.T) {
	a := newApp(t.TempDir())
	a.ai.visible = true
	r, err := prepareAIReview(a.root, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(r.Work, "hello.go"), []byte("package main\n"), 0644)
	r.Changes, _ = r.scan()
	a.ai.review = r
	view := ui.NewTester(a.view, 1440, 900)
	if os.Getenv("OMNICODE_AI_REVIEW_SCREENSHOT") == "1" {
		os.MkdirAll("artifacts", 0755)
		f, err := os.Create("artifacts/ai-review.png")
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, view.Image())
		f.Close()
	}
	if !view.HasText("1 个文件变更 · 待批准") {
		t.Fatal("approval status missing")
	}
	if err := view.Click("全部批准"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.root, "hello.go")); err != nil {
		t.Fatal("UI approval did not apply")
	}
	if a.hasPendingAI() {
		t.Fatal("applied proposal retained")
	}
}

func TestAIObservesChangesWhileRunning(t *testing.T) {
	root := t.TempDir()
	r, err := prepareAIReview(root, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events := make(chan aiEvent, 20)
	done := make(chan error, 1)
	go func() {
		_, err := runAIObserved(ctx, []string{"/bin/sh", "-c", "printf changed > live.txt; sleep 2"}, nil, r, events)
		done <- err
	}()
	observed := false
	for !observed {
		select {
		case e := <-events:
			observed = e.scanned && len(e.changes) == 1 && e.changes[0].Path == "live.txt"
		case err := <-done:
			t.Fatal("no live change before completion", err)
		case <-ctx.Done():
			t.Fatal("watch timed out")
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
