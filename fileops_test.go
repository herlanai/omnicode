package main

import (
	"github.com/HyperparameterSI/golet/backend/ui"
	"os"
	"path/filepath"
	"testing"
)

func TestFileOperations(t *testing.T) {
	a := newApp(t.TempDir())
	root := a.root
	a.filePrompt = filePrompt{kind: "新建文件夹", path: root, name: "src"}
	if e := a.applyFilePrompt(); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "src")
	a.filePrompt = filePrompt{kind: "新建文件", path: dir, name: "hello.go"}
	if e := a.applyFilePrompt(); e != nil {
		t.Fatal(e)
	}
	d := a.current()
	d.state.Buffer.Set("package main\n")
	moved := filepath.Join(root, "renamed")
	if e := a.movePath(dir, moved); e != nil {
		t.Fatal(e)
	}
	if d.path != filepath.Join(moved, "hello.go") || d.state.Buffer.Modified() {
		t.Fatal("open document not migrated/flushed")
	}
	a.fileClipboard = fileClipboard{path: moved}
	if e := a.pasteFile(moved); e == nil {
		t.Fatal("accepted recursive copy")
	}
	target := filepath.Join(root, "target")
	_ = os.Mkdir(target, 0755)
	if e := a.pasteFile(target); e != nil {
		t.Fatal(e)
	}
	copied := filepath.Join(target, "renamed", "hello.go")
	b, e := os.ReadFile(copied)
	if e != nil || string(b) != "package main\n" {
		t.Fatalf("copy: %q %v", b, e)
	}
	if e := a.pasteFile(target); e == nil {
		t.Fatal("overwrote existing destination")
	}
	a.fileClipboard = fileClipboard{path: d.path, cut: true}
	if e := a.pasteFile(root); e != nil {
		t.Fatal(e)
	}
	if d.path != filepath.Join(root, "hello.go") || a.fileClipboard.path != "" {
		t.Fatal("cut did not update document and clipboard")
	}
	if _, e := namedPath(root, "../escape"); e == nil {
		t.Fatal("accepted traversal")
	}
	trash := filepath.Join(root, "trash")
	_ = os.Mkdir(trash, 0755)
	d.state.Buffer.Set("package updated\n")
	if e := a.trashPath(d.path, func(p string) error { return os.Rename(p, filepath.Join(trash, filepath.Base(p))) }); e != nil {
		t.Fatal(e)
	}
	if len(a.docs) != 0 {
		t.Fatal("trashed document remains open")
	}
	b, _ = os.ReadFile(filepath.Join(trash, "hello.go"))
	if string(b) != "package updated\n" {
		t.Fatal("trash lost pending edits")
	}
}
func TestCopySymlinkDoesNotTraverse(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	_ = os.Mkdir(src, 0755)
	if e := os.Symlink(src, filepath.Join(src, "cycle")); e != nil {
		t.Skip(e)
	}
	dst := filepath.Join(root, "copy")
	if e := copyEntry(src, dst); e != nil {
		t.Fatal(e)
	}
	info, e := os.Lstat(filepath.Join(dst, "cycle"))
	if e != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("link not preserved")
	}
}
func TestFileContextMenu(t *testing.T) {
	a := newApp(t.TempDir())
	path := filepath.Join(a.root, "sample.txt")
	put(t, path, "sample")
	a.refresh()
	tester := ui.NewTester(a.view, 1200, 780)
	if e := tester.RightClick("sample.txt"); e != nil {
		t.Fatal(e)
	}
	if e := tester.ChooseMenuItem("重命名…"); e != nil {
		t.Fatal(e)
	}
	if !a.filePrompt.open {
		t.Fatal("rename dialog missing")
	}
	tester.Key(ui.Cmd, ui.KeyA)
	tester.Type("renamed.txt")
	tester.Key(0, ui.KeyEnter)
	if _, e := os.Stat(filepath.Join(a.root, "renamed.txt")); e != nil {
		t.Fatal(e)
	}
	if a.filePrompt.open {
		t.Fatal("dialog not closed")
	}
	if e := tester.RightClick("renamed.txt"); e != nil {
		t.Fatal(e)
	}
	if e := tester.ChooseMenuItem("新建文件…"); e != nil {
		t.Fatal(e)
	}
	tester.Type("new.txt")
	tester.Key(0, ui.KeyEnter)
	if a.current() == nil || a.current().name != "new.txt" {
		t.Fatal("created file did not open")
	}
}

func TestSidebarCreationParent(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "src")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.go")
	put(t, file, "package main\n")
	a := newApp(root)
	tester := ui.NewTester(a.view, 1000, 720)
	if err := tester.Click("src"); err != nil {
		t.Fatal(err)
	}
	if got := a.creationDirectory(); got != dir {
		t.Fatalf("selected folder: %s", got)
	}
	if err := tester.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	if got := a.creationDirectory(); got != dir {
		t.Fatalf("selected file must create sibling: %s", got)
	}
	a.askFile("新建文件夹", a.creationDirectory())
	a.filePrompt.name = "nested"
	if err := a.applyFilePrompt(); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "nested")
	if a.treeSelection != nested || !a.expanded[dir] {
		t.Fatal("new entry not selected or parent not expanded")
	}
	a.askFile("新建文件", a.creationDirectory())
	a.filePrompt.name = "hello.go"
	if err := a.applyFilePrompt(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(nested, "hello.go")); err != nil {
		t.Fatal(err)
	}
	a.treeSelection = ""
	if a.creationDirectory() != root {
		t.Fatal("missing root fallback")
	}
	a.treeSelection = filepath.Join(root, "removed")
	if a.creationDirectory() != root {
		t.Fatal("stale selection fallback")
	}
}
