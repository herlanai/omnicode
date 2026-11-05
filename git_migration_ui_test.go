package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
)

func waitGitEvent(t *testing.T, a *app) gitPanelEvent {
	t.Helper()
	for {
		select {
		case ev := <-a.git.events:
			if !ev.progressOnly {
				return ev
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Git task did not complete")
			return gitPanelEvent{}
		}
	}
}
func TestGitAdvancedNativeDialogDispatch(t *testing.T) {
	g, p := gitTestRepo(t)
	gitWrite(t, p.Path, "a", "base")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	gitMust(t, p.Path, "branch", "feature")
	a := newApp(p.Path)
	a.git.root = p.Path
	a.git.state, _ = readRepository(context.Background(), p.Path)
	a.gitAsk("branch-action", "重命名分支", "renamed", "", "")
	a.git.dialog.ref = "refs/heads/feature"
	a.git.dialog.mode = "rename"
	a.git.dialog.branch = "main"
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(a.theme); a.gitDialog(c) }, 700, 500)
	tt.Frame()
	if e := tt.Click("确定"); e != nil {
		t.Fatal(e)
	}
	tt.Frame()
	ev := waitGitEvent(t, a)
	if ev.err != nil {
		t.Fatal(ev.err)
	}
	if _, e := g.runGit(p.Path, "show-ref", "--verify", "refs/heads/renamed"); e != nil {
		t.Fatal("native dialog did not call migrated operation", e)
	}
}
func TestGitGraphTopology(t *testing.T) {
	h := []GitHistory{{Hash: "merge", Parents: "left right"}, {Hash: "left", Parents: "base"}, {Hash: "right", Parents: "base"}, {Hash: "base"}}
	rows, width := buildGitGraph(h)
	if len(rows) != 4 || width < 2*16+24 {
		t.Fatal(rows, width)
	}
	parents := 0
	for _, e := range rows[0].Edges {
		if e.Start == 24 {
			parents++
		}
	}
	if parents != 2 {
		t.Fatal("merge edges missing")
	}
	for _, r := range rows {
		for _, e := range r.Edges {
			if e.To < 0 {
				t.Fatal("dangling lane")
			}
		}
		_ = gitGraphSVG(r, width)
	}
}
func TestGitMigrationNativeViews(t *testing.T) {
	g, p := gitTestRepo(t)
	gitWrite(t, p.Path, "a.go", "package main\nvar A = 1\n")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "Initial")
	gitMust(t, p.Path, "branch", "feature")
	gitMust(t, p.Path, "tag", "v1")
	gitWrite(t, p.Path, "a.go", "package main\nvar A = 2\n")
	a := newApp(p.Path)
	a.git.root = p.Path
	a.git.visible = true
	a.git.state, _ = readRepository(context.Background(), p.Path)
	a.git.advanced.rawPatch, _ = g.GitDiff(p.Path, "a.go", false, "")
	a.git.filePath = "a.go"
	a.git.advanced.lineMode = true
	for _, mode := range []string{"partial", "graph", "references", "sidebar", "workspace", "pull", "ignore"} {
		t.Run(mode, func(t *testing.T) {
			width := 1100
			if mode == "sidebar" {
				width = 246
			}
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(a.theme)
				switch mode {
				case "partial":
					a.gitPatchView(c)
				case "graph":
					a.gitHistoryView(c)
				case "references":
					a.gitRepositoryNavigation(c)
				case "sidebar":
					a.git.tab = "变更"
					a.gitSidebar(c)
				case "workspace":
					a.git.tab = "变更"
					ui.Row(c).Fill().Children(func() {
						ui.Box(c).Width(226).FillHeight().Children(func() { a.gitSidebar(c) })
						ui.Box(c).Grow(1).MinWidth(0).FillHeight().Children(func() { a.gitContent(c) })
					})
				case "pull":
					a.git.dialog = gitPanelDialog{open: true, kind: "pull", title: "拉取", mode: "merge", commit: true}
					a.gitDialog(c)
				case "ignore":
					a.git.dialog = gitPanelDialog{open: true, kind: "ignore", title: "忽略", mode: "exact", scope: "global", ref: "build.log"}
					a.gitDialog(c)
				}
			}, width, 700)
			tt.Frame()
			if out := os.Getenv("OMNICODE_GIT_SCREENSHOTS"); out != "" {
				_ = os.MkdirAll(out, 0700)
				f, e := os.Create(filepath.Join(out, mode+".png"))
				if e != nil {
					t.Fatal(e)
				}
				defer f.Close()
				if e = png.Encode(f, tt.Image()); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
func TestGitCloneWithoutProjectRegistry(t *testing.T) {
	g, p := gitTestRepo(t)
	gitWrite(t, p.Path, "README", "hello")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	parent := t.TempDir()
	cloned, e := g.GitCloneProject(p.Path, parent, "new-project")
	if e != nil {
		t.Fatal(e)
	}
	if cloned.Path == "" || cloned.ID != cloned.Path {
		t.Fatal(cloned)
	}
	if _, e = g.GitCloneProject(p.Path, parent, "new-project"); e == nil {
		t.Fatal("overwrote existing clone")
	}
	if _, e = g.gitRoot(cloned.ID); e == nil {
		t.Fatal("service accepted another project's identifier")
	}
	if _, e = os.Stat(filepath.Join(cloned.Path, "README")); e != nil {
		t.Fatal(e)
	}
}
func TestGitStashPopAndDropMigration(t *testing.T) {
	g, p := gitTestRepo(t)
	gitWrite(t, p.Path, "a", "base")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	gitWrite(t, p.Path, "a", "first")
	if e := g.GitStashSave(p.ID, "first"); e != nil {
		t.Fatal(e)
	}
	s, _ := g.GitSnapshot(p.ID)
	hash := s.Stashes[0].Hash
	patch, e := g.GitStashDiff(p.ID, hash)
	if e != nil || !strings.Contains(patch, "first") {
		t.Fatal(patch, e)
	}
	if e = g.GitStashApply(p.ID, hash, true); e != nil {
		t.Fatal(e)
	}
	if e = g.GitStashSave(p.ID, "second"); e != nil {
		t.Fatal(e)
	}
	s, _ = g.GitSnapshot(p.ID)
	if e = g.GitStashDrop(p.ID, s.Stashes[0].Hash); e != nil {
		t.Fatal(e)
	}
	s, _ = g.GitSnapshot(p.ID)
	if len(s.Stashes) != 0 {
		t.Fatal(s.Stashes)
	}
}
func TestGitBundledOCRRuntime(t *testing.T) {
	if os.Getenv("OMNICODE_GIT_OCR_TEST") != "1" {
		t.Skip("opt-in bundled runtime")
	}
	g, p := gitTestRepo(t)
	gitWrite(t, p.Path, "main.go", "package main\nfunc f() int {return 1}\n")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	gitWrite(t, p.Path, "main.go", "package main\nfunc f() int {return 2}\n")
	s, e := g.gitReviewSnapshot(p.ID, "all")
	if e != nil {
		t.Fatal(e)
	}
	if e = enrichOCRSnapshot(context.Background(), p.Path, &s); e != nil {
		t.Fatal(e)
	}
	if s.Rules == "" {
		t.Fatal("missing embedded rules")
	}
}

func TestGitPatchTextSelection(t *testing.T) {
	raw := "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+新内容\n"
	all := gitRowsFromSelection(raw, []editor.Selection{{Anchor: 0, Head: len(raw)}})
	if len(all) != 2 || !all[4] || !all[5] {
		t.Fatalf("selected metadata: %v", all)
	}
	start := strings.Index(raw, "+新内容")
	one := gitRowsFromSelection(raw, []editor.Selection{{Anchor: len(raw), Head: start}})
	if len(one) != 1 || !one[5] {
		t.Fatal(one)
	}
	none := gitRowsFromSelection(raw, []editor.Selection{{Anchor: start, Head: start}})
	if len(none) != 0 {
		t.Fatal(none)
	}
}

func TestGitReviewSnapshotSandbox(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("OMNICODE_GIT_SANDBOX_TEST") != "1" {
		t.Skip("opt-in native sandbox")
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	secret := filepath.Join(root, "secret")
	if e := os.WriteFile(secret, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("OMNICODE_TEST_REVIEW_ROOT", root)
	cli := filepath.Join(t.TempDir(), "fake-codebuddy")
	script := `#!/bin/sh
if /bin/cat "$OMNICODE_TEST_REVIEW_ROOT/secret" >/dev/null 2>&1; then echo READ_LEAK; exit 1; fi
if (echo changed > "$OMNICODE_TEST_REVIEW_ROOT/secret") 2>/dev/null; then echo WRITE_LEAK; exit 1; fi
if ! /bin/cat git-review.json >/dev/null; then exit 1; fi
echo SNAPSHOT_ONLY
`
	if e := os.WriteFile(cli, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	p := defaultPreferences()
	p.Engine = "CodeBuddy CLI"
	p.BuddyPath = cli
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, e := runGitSnapshotAI(ctx, p, root, GitReviewSnapshot{}, false, "", func(string) {})
	if e != nil || !strings.Contains(out, "SNAPSHOT_ONLY") {
		t.Fatalf("%s: %v", out, e)
	}
	b, _ := os.ReadFile(secret)
	if string(b) != "original" {
		t.Fatal("modified original")
	}
}

func TestGitNativeSelectedLineAction(t *testing.T) {
	g, p := gitTestRepo(t)
	gitWrite(t, p.Path, "a.txt", "one\ntwo\n")
	gitMust(t, p.Path, "add", ".")
	gitMust(t, p.Path, "commit", "-m", "base")
	gitWrite(t, p.Path, "a.txt", "ONE\ntwo\n")
	a := newApp(p.Path)
	a.git.root = p.Path
	a.git.filePath = "a.txt"
	a.git.state, _ = readRepository(context.Background(), p.Path)
	a.git.advanced.rawPatch, _ = g.GitDiff(p.ID, "a.txt", false, "")
	a.git.advanced.lines = gitChangedRows(a.git.advanced.rawPatch)
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(a.theme); a.gitPatchView(c) }, 900, 600)
	tt.Frame()
	if e := tt.Click("应用选中行"); e != nil {
		t.Fatal(e)
	}
	tt.Frame()
	ev := waitGitEvent(t, a)
	if ev.err != nil {
		t.Fatal(ev.err)
	}
	staged, e := g.runGit(p.Path, "show", ":a.txt")
	if e != nil || staged != "ONE\ntwo\n" {
		t.Fatalf("%q %v", staged, e)
	}
}
func TestGitCommitPreflightAndPushFailure(t *testing.T) {
	_, p := gitTestRepo(t)
	gitWrite(t, p.Path, "large.txt", strings.Repeat("x", 10*1024*1024+1))
	gitMust(t, p.Path, "add", ".")
	a := newApp(p.Path)
	a.git.root = p.Path
	a.git.advanced.pushAfter = true
	a.gitCommit(false, "feat: large fixture")
	ev := waitGitEvent(t, a)
	if ev.err != nil || !ev.needsCommit || ev.committed {
		t.Fatal(ev)
	}
	if _, e := repositoryGit(context.Background(), p.Path, "rev-parse", "--verify", "HEAD"); e == nil {
		t.Fatal("committed before confirmation")
	}
	a.git.busy = false
	a.git.mutating = false
	a.gitCommit(true, "feat: large fixture")
	ev = waitGitEvent(t, a)
	if !ev.committed || ev.err == nil || !strings.Contains(ev.err.Error(), "提交成功") {
		t.Fatal(ev)
	}
	msg, e := repositoryGit(context.Background(), p.Path, "log", "-1", "--format=%s")
	if e != nil || strings.TrimSpace(msg) != "feat: large fixture" {
		t.Fatal(msg, e)
	}
}
