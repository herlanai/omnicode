package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestMethodReferencesFromContextMenu(t *testing.T) {
	if os.Getenv("OMNICODE_LSP_TEST") != "1" {
		t.Skip("requires bundled gopls")
	}
	a := newApp(t.TempDir())
	put(t, filepath.Join(a.root, "go.mod"), "module refs.test\n\ngo 1.22\n")
	source := "package refs\ntype S struct{}\nfunc (S) Work() {}\nfunc first(s S) { s.Work() }\n"
	put(t, filepath.Join(a.root, "main.go"), source)
	other := filepath.Join(a.root, "other.go")
	put(t, other, "package refs\ntype Other struct{}\nfunc (Other) Work() {}\nfunc second(s S, o Other) { s.Work(); o.Work() }\n")
	a.open(filepath.Join(a.root, "main.go"))
	d := a.current()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	w := &languageWorker{kind: "go", jobs: make(chan languageJob, 1), sent: map[string]languageSnapshot{}, cancel: cancel}
	a.intelligence.servers = map[string]*languageWorker{"go": w}
	events := make(chan intelligenceEvent, 128)
	go w.run(ctx, a.root, events)
	view := ui.NewTester(func(c *ui.Context) { a.textContent(c, d) }, 800, 500)
	view.Frame()
	// Click the declaration itself, with the caret initially on a different line.
	y := float32((d.state.LineTop(2) + d.state.LineTop(3)) / 2)
	for x := float32(50); x < 300; x += 5 {
		view.RightClickAt(x, y)
		offset := d.state.Cursor()
		if offset >= strings.Index(source, "Work") && offset < strings.Index(source, "Work")+4 {
			break
		}
		view.CloseMenu()
	}
	if err := view.ChooseMenuItem("查看引用"); err != nil {
		t.Fatal(err, d.state.CursorPos(), y)
	}
	if w.navigation == nil {
		t.Fatal("right-click did not request references", a.notice)
	}
	for a.intelligence.references == nil {
		select {
		case e := <-events:
			a.applyIntelligence(e)
			if e.err != nil {
				t.Fatal(e.err)
			}
		case <-ctx.Done():
			t.Fatal("references timed out", a.notice)
		}
	}
	refs := a.intelligence.references
	if refs.name != "Work" || len(refs.locations) != 2 {
		t.Fatalf("unexpected references: %s %+v", refs.name, refs.locations)
	}
	results := ui.NewTester(func(c *ui.Context) { a.referencePanel(c, refs) }, 800, 300)
	if err := results.Click("other.go:4:31"); err != nil {
		t.Fatal(err, refs.locations)
	}
	if a.current().path != other || a.current().state.SelectedText() != "Work" {
		t.Fatal("reference jump failed")
	}
}

func TestResizeReferencesPane(t *testing.T) {
	a := newApp(t.TempDir())
	path := filepath.Join(a.root, "refs.go")
	put(t, path, "package refs\nvar name int\n")
	a.open(path)
	refs := &referenceResults{name: "name", locations: []referenceLocation{{URI: lspURI(path), Range: lspRange{Start: lspPosition{1, 4}, End: lspPosition{1, 8}}}}}
	a.intelligence.references = refs
	view := ui.NewTester(a.content, 800, 600)
	bounds, ok := view.Find("关闭引用列表")
	if !ok {
		t.Fatal("missing references header")
	}
	// The header starts at the panel's four-DIP top padding, just below the divider.
	y := bounds.Y - 5
	view.Move(400, y)
	if view.Cursor() != ui.CursorResizeRow {
		t.Fatalf("hover cursor = %v", view.Cursor())
	}
	view.Press(400, y)
	view.Move(400, y-100)
	if view.Cursor() != ui.CursorResizeRow {
		t.Fatalf("drag cursor = %v", view.Cursor())
	}
	view.Release(400, y-100)
	after, ok := view.Find("关闭引用列表")
	if !ok || after.Y > bounds.Y-80 {
		t.Fatalf("divider did not resize panel: %v -> %v", bounds, after)
	}
	if err := view.Click("refs.go:2:5"); err != nil {
		t.Fatal(err)
	}
	if a.current().state.SelectedText() != "name" {
		t.Fatal("reference selection lost after resizing")
	}
	if err := view.Click("关闭引用列表"); err != nil {
		t.Fatal(err)
	}
	a.intelligence.references = refs
	view.Frame()
	reopened, ok := view.Find("关闭引用列表")
	if !ok || reopened.Y != after.Y {
		t.Fatal("panel did not retain its height")
	}
}

func TestReferencesAndTerminalShareBottomPanel(t *testing.T) {
	a := newApp(t.TempDir())
	a.terminal.visible = true
	refs := &referenceResults{name: "example"}
	a.intelligence.references = refs
	a.terminal.showReferences = true
	view := ui.NewTester(a.workspaceContent, 800, 600)
	if _, ok := view.Find("关闭引用列表"); !ok {
		t.Fatal("reference tab missing")
	}
	if _, ok := view.Find("新建终端"); ok {
		t.Fatal("panels are stacked")
	}
	if err := view.Click("切换到终端"); err != nil {
		t.Fatal(err)
	}
	if _, ok := view.Find("新建终端"); !ok || a.intelligence.references != refs {
		t.Fatal("terminal switch lost results")
	}
	if err := view.Click("切换到引用"); err != nil {
		t.Fatal(err)
	}
	if err := view.Click("关闭引用列表"); err != nil {
		t.Fatal(err)
	}
	if _, ok := view.Find("新建终端"); !ok || !a.terminal.visible {
		t.Fatal("terminal did not remain open")
	}
	if _, ok := view.Find("切换到引用"); ok {
		t.Fatal("closed reference tab still visible")
	}
}
