//go:build darwin

package main

import (
	"context"
	"fmt"
	golet "github.com/HyperparameterSI/golet/backend"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebPreviewNative(t *testing.T) {
	if os.Getenv("OMNICODE_WEB_NATIVE_TEST") != "1" {
		t.Skip("native web preview smoke test")
	}
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	os.WriteFile(filepath.Join(root, "index.html"), []byte(`<link rel="stylesheet" href="/style.css"><p id="result">loading</p><script src="/app.js"></script>`), 0644)
	os.WriteFile(filepath.Join(root, "style.css"), []byte(`body{color:rgb(12, 34, 56)}`), 0644)
	os.WriteFile(filepath.Join(root, "app.js"), []byte(`document.getElementById('result').textContent='ready'`), 0644)
	a := newApp(root)
	ready := make(chan *webPreview, 1)
	golet.App.WhenReady(func() {
		a.previewWeb(filepath.Join(root, "index.html"))
		ready <- a.webPreviews[filepath.Join(root, "index.html")]
	})
	p := <-ready
	if p == nil {
		t.Fatal("preview failed", a.notice)
	}
	defer golet.RunOnMain(func() { p.window.Destroy() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result any
	var err error
	for ctx.Err() == nil {
		result, err = p.web.Page().EvalContext(ctx, `document.getElementById('result')?.textContent + '|' + getComputedStyle(document.body).color`)
		if err == nil && result == "ready|rgb(12, 34, 56)" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("native HTML/JS/CSS preview failed: %v %v", result, err))
}
