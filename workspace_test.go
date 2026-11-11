package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func put(t *testing.T, path, text string) {
	t.Helper()
	if e := os.WriteFile(path, []byte(text), 0640); e != nil {
		t.Fatal(e)
	}
}
func TestSaveConflictAndLineEndings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.go")
	put(t, path, "package main\r\n")
	d, e := loadDocument(path)
	if e != nil {
		t.Fatal(e)
	}
	d.state.Buffer.Set("package main\n// 中文\n")
	if e = d.save(path); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "package main\r\n// 中文\r\n" {
		t.Fatalf("line endings lost: %q", data)
	}
	if d.state.Buffer.Modified() {
		t.Fatal("saved buffer is dirty")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatal("permissions changed")
	}
	put(t, path, "external change\n")
	d.state.Buffer.Set("my change\n")
	if e = d.save(path); !errors.Is(e, errConflict) {
		t.Fatalf("expected conflict, got %v", e)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "external change\n" {
		t.Fatal("external change overwritten")
	}
	if !d.state.Buffer.Modified() {
		t.Fatal("failed save cleared dirty state")
	}
}
func TestRejectNonTextAndLargeFiles(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{"binary": "a\x00b", "invalid": "\xff", "large": strings.Repeat("x", maxFileSize+1)} {
		p := filepath.Join(root, name)
		put(t, p, text)
		if _, e := loadDocument(p); e == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	if _, e := loadDocument(root); e == nil {
		t.Fatal("accepted directory")
	}
}
func TestSymlinkSaveAndIndex(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.go")
	put(t, p, "package main\n")
	link := filepath.Join(root, "link.go")
	if e := os.Symlink(p, link); e != nil {
		t.Skip(e)
	}
	d, e := loadDocument(link)
	if e != nil {
		t.Fatal(e)
	}
	d.state.Buffer.Set("package example\n")
	if e = d.save(link); e != nil {
		t.Fatal(e)
	}
	info, _ := os.Lstat(link)
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("replaced symlink")
	}
	_ = os.Mkdir(filepath.Join(root, "node_modules"), 0755)
	put(t, filepath.Join(root, "node_modules", "hidden.go"), "skip")
	for _, p := range fileIndex(root) {
		if strings.Contains(p, "hidden") {
			t.Fatal("indexed dependency tree")
		}
	}
}
func TestEditorWorkflow(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "hello.go")
	put(t, path, "package main\n")
	md := filepath.Join(root, "README.md")
	put(t, md, "# Hello\n\nNative **Markdown**.\n")
	a := newApp(root)
	a.open(path)
	tester := ui.NewTester(a.view, 1200, 780)
	if !tester.HasText("hello.go") {
		t.Fatal("missing shell")
	}
	tester.ClickAt(500, 155)
	tester.Key(ui.Cmd, ui.KeyEnd)
	tester.Type("// edited\n")
	if !a.current().state.Buffer.Modified() {
		t.Fatal("typing did not edit buffer")
	}
	tester.Key(ui.Cmd, ui.KeyZ)
	if a.current().state.Buffer.Modified() {
		t.Fatal("undo did not restore saved state")
	}
	tester.Key(ui.Cmd, ui.KeyP)
	tester.Type("README")
	if !a.quick {
		t.Fatal("quick open shortcut failed")
	}
	tester.Key(0, ui.KeyEnter)
	if a.current().name != "README.md" {
		t.Fatalf("quick open got %s query=%q quick=%v texts=%v", a.current().name, a.query, a.quick, tester.Texts())
	}
	if e := tester.Click("分栏预览"); e != nil {
		t.Fatal(e)
	}
	if !a.current().preview || !tester.HasText("Hello") {
		t.Fatal("markdown preview missing")
	}
	tester.Key(ui.Cmd, ui.KeyB)
	if a.sidebar {
		t.Fatal("sidebar toggle failed")
	}
	tester.SetSize(800, 520)
	tester.Key(ui.Cmd, ui.KeyW)
	if a.current().name != "hello.go" {
		t.Fatal("tab close failed")
	}
}

func TestAutosaveDebounceAndBackgroundTab(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	put(t, path, "")
	a := newApp(root)
	a.open(path)
	doc := a.current()
	tester := ui.NewTester(a.view, 1200, 780)
	for _, text := range tester.Texts() {
		if text == "保存" {
			t.Fatal("save button still visible")
		}
	}
	tester.Type("first")
	tester.Advance(500 * time.Millisecond)
	data, _ := os.ReadFile(path)
	if len(data) != 0 {
		t.Fatal("saved before debounce")
	}
	tester.Type(" second")
	tester.Advance(500 * time.Millisecond)
	data, _ = os.ReadFile(path)
	if len(data) != 0 {
		t.Fatal("typing did not reset debounce")
	}
	tester.Key(ui.Cmd, ui.KeyN)
	tester.Advance(300 * time.Millisecond)
	data, _ = os.ReadFile(path)
	if string(data) != "first second" || doc.state.Buffer.Modified() {
		t.Fatalf("background save failed: %q", data)
	}
	tester.Type("untitled draft")
	tester.Advance(2 * time.Second)
	if a.current().path != "" || !a.current().state.Buffer.Modified() {
		t.Fatal("untitled document should remain unsaved without a chosen path")
	}
}

func TestAutosaveConflictAndCloseFlush(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	put(t, path, "")
	a := newApp(root)
	a.open(path)
	d := a.current()
	tester := ui.NewTester(a.view, 1200, 780)
	tester.Type("local")
	put(t, path, "external")
	tester.Advance(time.Second)
	data, _ := os.ReadFile(path)
	if string(data) != "external" || !d.state.Buffer.Modified() || d.saveError == "" {
		t.Fatal("conflict did not protect both versions")
	}
	// A failed version must not retry automatically, even if disk returns to baseline.
	put(t, path, "")
	tester.Advance(2 * time.Second)
	data, _ = os.ReadFile(path)
	if len(data) != 0 {
		t.Fatal("failed version was retried")
	}
	tester.Type(" update")
	tester.Advance(time.Second)
	if d.state.Buffer.Modified() || d.saveError != "" {
		t.Fatal("new version did not retry successfully")
	}
	tester.Type(" close")
	tester.Key(ui.Cmd, ui.KeyW)
	data, _ = os.ReadFile(path)
	if len(a.docs) != 0 || string(data) != "local update close" {
		t.Fatalf("close did not flush: %q", data)
	}
}
