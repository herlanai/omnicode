package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestHTTPProjectFileAndNativeSend(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- r.Method + " " + r.URL.RequestURI() + " " + r.Header.Get("X-Test") + " " + strings.TrimSpace(string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	a := newApp(t.TempDir())
	tt := ui.NewTester(a.view, 1200, 740)
	if err := tt.Click("HTTP 请求"); err != nil {
		t.Fatal(err)
	}
	d := a.current()
	if d == nil || d.path != filepath.Join(a.root, "requests.http") {
		t.Fatal("missing project collection")
	}
	src := strings.Replace(httpExample, "http://localhost:8080", server.URL, 1)
	src = strings.Replace(src, "/items", "/items?x=1", 1)
	src = strings.Replace(src, "Content-Type:", "X-Test: local\nContent-Type:", 1)
	d.state.Buffer.Set(src)
	d.state.Select(len(src), len(src))
	if !a.writeAuto(d) {
		t.Fatal("save failed")
	}
	a.openHTTP()
	data, err := os.ReadFile(d.path)
	if err != nil || string(data) != src || a.current() != d {
		t.Fatal("collection overwritten or duplicated")
	}
	tt.Frame()
	if err := tt.Click("发送光标所在的请求"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for d.http.reply.status == "" && time.Now().Before(deadline) {
		tt.Advance(50 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	if d.http.reply.status != "201 Created" || !strings.Contains(d.http.reply.body, "\n") {
		t.Fatalf("%+v %s", d.http.reply, d.http.err)
	}
	select {
	case got := <-requests:
		if got != `POST /items?x=1 local {"name": "example"}` {
			t.Fatal(got)
		}
	default:
		t.Fatal("request missing")
	}
	// Select the first block up to (but not into) the next heading.
	start := strings.Index(src, "GET {{baseUrl}}")
	end := strings.Index(src, "### 创建示例")
	d.state.Label = "HTTP 测试编辑区"
	d.state.Select(start, end)
	tt.Frame()
	r, ok := tt.Find("HTTP 测试编辑区")
	if !ok {
		t.Fatal("request editor missing")
	}
	tt.RightClickAt(r.X+100, r.Y+3.5*float32(a.prefs.EditorSize)*float32(d.state.LineHeight))
	if err := tt.ChooseMenuItem("run"); err != nil {
		t.Fatal(err, tt.Menu(), d.state.Selection())
	}
	deadline = time.Now().Add(3 * time.Second)
	for d.http.reply.label != "健康检查" && time.Now().Before(deadline) {
		tt.Advance(50 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	if d.http.reply.label != "健康检查" {
		t.Fatal(d.http.reply.label, d.http.err)
	}
	select {
	case got := <-requests:
		if got != "GET /health  " {
			t.Fatal(got)
		}
	default:
		t.Fatal("selected request missing")
	}
	if _, _, err := requestInRange(context.Background(), src, start, len(src)); err == nil {
		t.Fatal("accepted selection across two requests")
	}
	first, _, err := requestAt(context.Background(), src, 0)
	if err != nil || first.URL.Path != "/health" {
		t.Fatal(first, err)
	}
}

func TestHTTPBoundsAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, strings.Repeat("a", httpResponseLimit+10))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _, err := requestAt(ctx, "GET "+srv.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := executeHTTP(req, "size")
	if !got.truncated || got.size != httpResponseLimit {
		t.Fatal(got.size, got.err)
	}
	cancel()
	got = executeHTTP(req, "cancel")
	if !errors.Is(got.err, context.Canceled) {
		t.Fatal(got.err)
	}
	if _, _, err = requestAt(ctx, "GET {{missing}}", 0); err == nil {
		t.Fatal("undefined variable")
	}
	req, _, err = requestAt(context.Background(), "POST http://localhost/test\n\nIt's ok", 0)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := curlCommand(req)
	if err != nil || !strings.Contains(cmd, `--data-raw 'It'"'"'s ok'`) {
		t.Fatal(cmd, err)
	}
}
