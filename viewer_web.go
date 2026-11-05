package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
)

type webPreview struct {
	window        *golet.Window
	web           *golet.WebView
	server        *http.Server
	root, address string
}

func isWebFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".html" || ext == ".htm"
}

// Serve project assets over loopback so modules, fetch and root-relative URLs
// behave like a website. No directory listing or paths outside the project.
func previewHandler(root string) http.Handler {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || host != "127.0.0.1" {
			http.Error(w, "Invalid host", http.StatusForbidden)
			return
		}
		for _, part := range strings.Split(strings.ReplaceAll(r.URL.Path, "\\", "/"), "/") {
			if part == ".." || strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		path := filepath.Join(root, filepath.FromSlash(r.URL.Path))
		path, err = filepath.EvalSymlinks(path)
		if err != nil || !inside(root, path) {
			http.NotFound(w, r)
			return
		}
		rel, _ := filepath.Rel(root, path)
		for _, part := range strings.Split(rel, string(os.PathSeparator)) {
			if part != "." && strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		info, err := os.Stat(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if info.IsDir() {
			if !strings.HasSuffix(r.URL.Path, "/") {
				http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
				return
			}
			path = filepath.Join(path, "index.html")
			path, err = filepath.EvalSymlinks(path)
			if err != nil || !inside(root, path) {
				http.NotFound(w, r)
				return
			}
			info, err = os.Stat(path)
		}
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	})
}
func (a *app) flushWebPreview(root string) bool {
	for _, d := range a.docs {
		if d.editable() && d.path != "" && inside(root, d.path) && d.state.Buffer.Modified() {
			if !a.writeAuto(d) {
				return false
			}
		}
	}
	return true
}
func (a *app) previewWeb(path string) {
	path, err := filepath.Abs(path)
	if err == nil {
		path, err = filepath.EvalSymlinks(path)
	}
	if err != nil {
		a.report(err)
		return
	}
	if !isWebFile(path) {
		a.report(fmt.Errorf("请选择 HTML 文件进行预览"))
		return
	}
	if p := a.webPreviews[path]; p != nil {
		if a.flushWebPreview(p.root) {
			p.web.Page().ReloadIgnoringCache()
		}
		p.window.Show()
		p.window.Focus()
		return
	}
	root := a.root
	if root == "" || !inside(root, path) {
		root = filepath.Dir(path)
	}
	if !a.flushWebPreview(root) {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		a.report(err)
		return
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		listener.Close()
		a.report(err)
		return
	}
	address := (&url.URL{Scheme: "http", Host: listener.Addr().String(), Path: "/" + filepath.ToSlash(rel)}).String()
	p := &webPreview{root: root, address: address, server: &http.Server{Handler: previewHandler(root), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}}
	go func() { _ = p.server.Serve(listener) }()
	p.window = golet.NewWindow(golet.WindowOptions{Title: filepath.Base(path) + " · 预览", TitleBarStyle: golet.TitleBarHiddenInset, TrafficLightPosition: &golet.Point{X: 12, Y: 11}, TitleBarHeight: 40, Width: 1100, Height: 780, MinWidth: 640, MinHeight: 420, Hidden: true, Content: ui.View(func(c *ui.Context) {
		c.SetTheme(a.theme)
		ui.Column(c).Fill().Children(func() {
			childTitlebar(c, filepath.Base(path)+" · 预览", func() {
				iconButton(c, "web-refresh", "refresh", "刷新预览", func() {
					if p.web != nil && a.flushWebPreview(root) {
						p.web.Page().ReloadIgnoringCache()
					}
				}).Size(28, 28).Padding(5)
			})
			if p.web != nil {
				ui.WebView(c, p.web).Fill()
			}
		})
	})})
	p.web, err = p.window.NewWebView(golet.WebViewOptions{URL: address, Page: golet.PageOptions{Untrusted: true}})
	if err != nil {
		p.server.Close()
		p.window.Destroy()
		a.report(err)
		return
	}
	if a.webPreviews == nil {
		a.webPreviews = map[string]*webPreview{}
	}
	a.webPreviews[path] = p
	p.window.OnClosed(func() { p.server.Close(); delete(a.webPreviews, path) })
	p.window.Show()
}
