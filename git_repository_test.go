package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
)

func fixtureGit(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitOK(t, root, "init")
	gitOK(t, root, "config", "user.name", "Test")
	gitOK(t, root, "config", "user.email", "test@example.test")
	return root
}
func gitOK(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, e := repositoryGit(context.Background(), root, args...)
	if e != nil {
		t.Fatalf("git %v: %v", args, e)
	}
	return out
}
func writeGit(t *testing.T, root, path, text string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(root, path), []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func actionGit(t *testing.T, root, kind, value, extra string) {
	t.Helper()
	if e := repositoryAction(context.Background(), root, kind, value, extra); e != nil {
		t.Fatalf("%s: %v", kind, e)
	}
}
func TestRepositoryWorkflow(t *testing.T) {
	root := fixtureGit(t)
	ctx := context.Background()
	name := "中文 file\n[x].go"
	writeGit(t, root, name, "package main\n")
	s, e := readRepository(ctx, root)
	if e != nil || len(s.Files) != 1 || s.Files[0].Path != name {
		t.Fatalf("%+v %v", s, e)
	}
	actionGit(t, root, "stage", name, "")
	actionGit(t, root, "unstage", name, "")
	if _, e = os.Stat(filepath.Join(root, name)); e != nil {
		t.Fatal("unborn unstage removed file")
	}
	actionGit(t, root, "stage", name, "")
	actionGit(t, root, "commit", "Initial", "")
	writeGit(t, root, name, "package second\n")
	actionGit(t, root, "stage", name, "")
	writeGit(t, root, name, "package third\n")
	files, e := repositoryFiles(ctx, root)
	if e != nil || len(files) != 1 {
		t.Fatal(files, e)
	}
	before, after, e := repositoryDiff(ctx, root, files[0], true)
	if e != nil || before != "package main\n" || after != "package second\n" {
		t.Fatalf("staged %q %q %v", before, after, e)
	}
	before, after, e = repositoryDiff(ctx, root, files[0], false)
	if e != nil || before != "package second\n" || after != "package third\n" {
		t.Fatalf("working %q %q %v", before, after, e)
	}
	actionGit(t, root, "discard", name, "")
	b, _ := os.ReadFile(filepath.Join(root, name))
	if string(b) != "package second\n" {
		t.Fatal("discard must preserve staged content")
	}
	actionGit(t, root, "commit", "Second", "")
	gitOK(t, root, "mv", "--", name, "renamed.go")
	files, _ = repositoryFiles(ctx, root)
	if files[0].OldPath != name {
		t.Fatal(files)
	}
	before, after, e = repositoryDiff(ctx, root, files[0], true)
	if e != nil || before != after {
		t.Fatalf("rename diff %q %q %v", before, after, e)
	}
	actionGit(t, root, "unstage", "renamed.go", "")
	if _, e = os.Stat(filepath.Join(root, "renamed.go")); e != nil {
		t.Fatal(e)
	}
}
func TestRepositoryBranchesStashAndScope(t *testing.T) {
	root := fixtureGit(t)
	ctx := context.Background()
	writeGit(t, root, "a.txt", "one")
	actionGit(t, root, "stage", "", "")
	actionGit(t, root, "commit", "Initial", "")
	s, _ := readRepository(ctx, root)
	original := s.Branch
	actionGit(t, root, "branch", "feature", "")
	writeGit(t, root, "a.txt", "two")
	if e := repositoryAction(ctx, root, "switch", original, ""); e == nil {
		t.Fatal("dirty switch allowed")
	}
	actionGit(t, root, "stash", "draft", "")
	s, e := readRepository(ctx, root)
	if e != nil || len(s.Stashes) != 1 || len(s.Files) != 0 {
		t.Fatal(s, e)
	}
	actionGit(t, root, "switch", original, "")
	actionGit(t, root, "stash-apply", s.Stashes[0].Hash, "")
	b, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(b) != "two" {
		t.Fatal("stash lost")
	}
	sub := filepath.Join(root, "sub")
	_ = os.Mkdir(sub, 0700)
	if _, e = repositoryRoot(ctx, sub); e == nil {
		t.Fatal("parent repository accepted")
	}
	if e = repositoryAction(ctx, sub, "init", "", ""); e == nil {
		t.Fatal("nested repo init accepted")
	}
	if e = repositoryAction(ctx, root, "stage", "../outside", ""); e == nil {
		t.Fatal("outside path accepted")
	}
	if e = repositoryAction(ctx, root, "branch", "--orphan", ""); e == nil {
		t.Fatal("option injection accepted")
	}
}
func TestRepositoryNetworkLocal(t *testing.T) {
	root := fixtureGit(t)
	remote := t.TempDir()
	gitOK(t, remote, "init", "--bare")
	writeGit(t, root, "a", "one")
	actionGit(t, root, "stage", "", "")
	actionGit(t, root, "commit", "Initial", "")
	actionGit(t, root, "remote", "origin", remote)
	s, _ := readRepository(context.Background(), root)
	actionGit(t, root, "push", "origin", s.Branch)
	actionGit(t, root, "fetch", "origin", "")
	actionGit(t, root, "pull", "origin", s.Branch)
	s, e := readRepository(context.Background(), root)
	if e != nil || s.Upstream == "" || s.Ahead != 0 || s.Behind != 0 {
		t.Fatal(s, e)
	}
}
func TestRepositoryBinaryAndConflict(t *testing.T) {
	root := fixtureGit(t)
	ctx := context.Background()
	writeGit(t, root, "a", "one\n")
	actionGit(t, root, "stage", "", "")
	actionGit(t, root, "commit", "Initial", "")
	s, _ := readRepository(ctx, root)
	original := s.Branch
	actionGit(t, root, "branch", "other", "")
	writeGit(t, root, "a", "other\n")
	actionGit(t, root, "stage", "", "")
	actionGit(t, root, "commit", "Other", "")
	actionGit(t, root, "switch", original, "")
	writeGit(t, root, "a", "main\n")
	actionGit(t, root, "stage", "", "")
	actionGit(t, root, "commit", "Main", "")
	if e := repositoryAction(ctx, root, "merge", "other", ""); e == nil {
		t.Fatal("expected conflict")
	}
	s, e := readRepository(ctx, root)
	if e != nil || s.Operation != "merge" || !s.Files[0].Conflict {
		t.Fatal(s, e)
	}
	if e = repositoryAction(ctx, root, "commit", "bad", ""); e == nil {
		t.Fatal("conflict commit")
	}
	gitOK(t, root, "merge", "--abort")
	writeGit(t, root, "a", "\x00binary")
	files, _ := repositoryFiles(ctx, root)
	if _, _, e = repositoryDiff(ctx, root, files[0], false); e == nil {
		t.Fatal("binary preview allowed")
	}
}
func TestGitNativePanel(t *testing.T) {
	a := newApp("")
	a.git.visible = true
	a.git.state = repositoryState{Root: "/tmp/example", Branch: "main", Files: []repositoryFile{{Path: "main.go", Index: " ", Worktree: "M"}}}
	a.git.message = "改善欢迎信息"
	a.git.selection = "工作区 · main.go"
	a.git.diff = &editor.DiffState{Old: editor.NewBuffer("package main\n\nfunc greeting() string {\n    return \"Hello\"\n}\n"), New: editor.NewBuffer("package main\n\nfunc greeting() string {\n    return \"Hello, Omnicode\"\n}\n"), ExpandUnchanged: true}
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(a.theme)
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Width(246).FillHeight().Children(func() { a.gitSidebar(c) })
			ui.Box(c).Grow(1).FillHeight().Children(func() { a.gitContent(c) })
		})
	}, 1100, 700)
	tt.Frame()
	if output := os.Getenv("OMNICODE_GIT_SCREENSHOT"); output != "" {
		f, e := os.Create(output)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if e = png.Encode(f, tt.Image()); e != nil {
			t.Fatal(e)
		}
	}
	for _, text := range []string{"源代码管理", "main.go", "更改  1"} {
		if !tt.HasText(text) {
			t.Fatalf("missing %s: %v", text, tt.Texts())
		}
	}
}
func TestGitCancelCompletes(t *testing.T) {
	a := newApp(t.TempDir())
	a.gitTask(false, "", func(ctx context.Context, root string) gitPanelEvent {
		<-ctx.Done()
		return gitPanelEvent{err: ctx.Err()}
	})
	a.git.cancel()
	select {
	case ev := <-a.git.events:
		if ev.err == nil {
			t.Fatal("missing cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not deliver completion")
	}
}
func TestParseRepositoryRename(t *testing.T) {
	files := parseRepositoryFiles("R  new\x00old\x00?? a\nb\x00UU conflict\x00")
	if len(files) != 3 || files[0].OldPath != "old" || !files[2].Conflict || !strings.Contains(files[1].Path, "\n") {
		t.Fatal(files)
	}
}

func TestGitWorkspaceDividerDrag(t *testing.T) {
	a := newApp("")
	a.git.visible = true
	a.git.state = repositoryState{Root: "/tmp/example", Branch: "main"}
	tt := ui.NewTester(a.view, 1200, 760)
	tt.Frame()
	nav, changes := a.sidebarWidth, a.gitChangesWidth
	drag := func(x, delta float32) { tt.Press(x, 360); tt.Move(x+delta, 360); tt.Release(x+delta, 360); tt.Frame() }
	drag(42+nav, 45)
	if a.sidebarWidth != nav+45 || a.gitChangesWidth != changes {
		t.Fatalf("navigation drag: %v, %v", a.sidebarWidth, a.gitChangesWidth)
	}
	drag(42+a.sidebarWidth+1+changes, -55)
	if a.sidebarWidth != nav+45 || a.gitChangesWidth != changes-55 {
		t.Fatalf("changes drag: %v, %v", a.sidebarWidth, a.gitChangesWidth)
	}
	if !tt.HasText("工作区干净") {
		t.Fatal("preview missing after drag")
	}
}

func TestGitBackgroundRefreshKeepsWorkspaceStable(t *testing.T) {
	a := newApp(t.TempDir())
	p := &a.git
	p.root, p.visible = a.root, true
	p.state = repositoryState{Root: a.root, Branch: "main"}
	p.next = time.Now().Add(time.Minute)
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	before, ok := tt.Find("已暂存  0")
	if !ok {
		t.Fatal("missing file list")
	}
	a.gitTask(false, "auto-refresh", func(ctx context.Context, _ string) gitPanelEvent {
		<-ctx.Done()
		return gitPanelEvent{state: &repositoryState{Branch: "obsolete"}}
	})
	old := p.events
	tt.Frame()
	after, ok := tt.Find("已暂存  0")
	if !ok || after != before || p.busy || tt.HasText("正在处理…") {
		t.Fatal("background poll changed file-list layout or entered foreground busy state")
	}
	if err := tt.Click("提交历史"); err != nil || p.tab != "历史" {
		t.Fatal("navigation was blocked during refresh", err)
	}
	a.gitTask(false, "preview", func(context.Context, string) gitPanelEvent {
		return gitPanelEvent{preview: true, title: "commit", patch: "new preview"}
	})
	ev := waitGitEvent(t, a)
	p.events <- ev
	select {
	case <-old:
	case <-time.After(time.Second):
		t.Fatal("obsolete refresh did not stop")
	}
	tt.Frame()
	if p.state.Branch != "main" || p.selection != "commit" || p.busy || p.refreshing {
		t.Fatal("obsolete refresh replaced foreground result")
	}
}
