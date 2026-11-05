package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchContentsUnicodeAndUnsaved(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.html")
	os.WriteFile(path, []byte("header\n中文 Hello.World\nhello.world again\n"), 0644)
	os.WriteFile(filepath.Join(root, "binary.bin"), []byte{0, 'h', 'e', 'l', 'l', 'o'}, 0644)
	os.Mkdir(filepath.Join(root, "node_modules"), 0755)
	os.WriteFile(filepath.Join(root, "node_modules/ignored.js"), []byte("hello.world"), 0644)
	result := searchContents(context.Background(), root, "HELLO.WORLD", nil)
	if len(result.Hits) != 2 || result.Hits[0].Line != 2 || result.Hits[1].Line != 3 {
		t.Fatalf("wrong matches: %+v", result)
	}
	original, _ := os.ReadFile(path)
	for _, h := range result.Hits {
		if string(original[h.Start:h.End]) != h.Match {
			t.Fatal("UTF-8 offsets incorrect")
		}
	}
	result = searchContents(context.Background(), root, "未保存", map[string]string{path: "第一行\n未保存内容\n"})
	if len(result.Hits) != 1 || result.Hits[0].Line != 2 {
		t.Fatal("unsaved buffer not searched")
	}
	if len(searchContents(context.Background(), root, "HelloXWorld", nil).Hits) != 0 {
		t.Fatal("query treated as regex")
	}
}
func TestContentSearchLimitsAndCancel(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "many.txt"), []byte(strings.Repeat("match\n", 120)), 0644)
	result := searchContents(context.Background(), root, "match", nil)
	if len(result.Hits) != 100 || !result.Limited {
		t.Fatal("result limit missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := searchContents(ctx, root, "match", nil); len(result.Hits) != 0 || !result.Limited {
		t.Fatal("cancel ignored")
	}
}
func TestContentHitOpensAndSelects(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	os.WriteFile(path, []byte("package main\n// 中文标记\n"), 0644)
	a := newApp(root)
	a.quick = true
	hits := searchContents(context.Background(), a.root, "中文标记", nil).Hits
	if len(hits) != 1 {
		t.Fatal("missing match")
	}
	a.openContentHit(hits[0])
	if a.quick || a.current() == nil || a.current().state.SelectedText() != "中文标记" {
		t.Fatal("navigation did not select match")
	}
	// An external edit shifts the match: reopening uses the current buffer safely.
	a.current().state.Buffer.Set("// added\npackage main\n// 中文标记\n")
	a.openContentHit(hits[0])
	if a.current().state.SelectedText() != "中文标记" {
		t.Fatal("stale result selected wrong range")
	}
}
