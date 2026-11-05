package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebPreviewAssetsAndBoundaries(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "pages"), 0755)
	for path, body := range map[string]string{"index.html": "<h1>Home</h1>", "pages/a b.html": "<script src='/app.js'></script>", "app.js": "document.title='ready'", "style.css": "body{color:red}", ".env": "secret"} {
		os.WriteFile(filepath.Join(root, path), []byte(body), 0644)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("private"), 0644)
	os.Symlink(outside, filepath.Join(root, "escape.txt"))
	handler := previewHandler(root)
	for _, tc := range []struct {
		path string
		code int
		body string
	}{{"/", 200, "Home"}, {"/pages/a%20b.html", 200, "app.js"}, {"/app.js", 200, "ready"}, {"/style.css", 200, "color:red"}, {"/.env", 404, ""}, {"/pages/", 404, ""}, {"/../secret.txt", 404, ""}, {"/escape.txt", 404, ""}} {
		request := httptest.NewRequest("GET", "http://127.0.0.1:1234"+tc.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.code || !strings.Contains(response.Body.String(), tc.body) {
			t.Fatalf("%s: status %d", tc.path, response.Code)
		}
	}
	request := httptest.NewRequest("GET", "http://attacker.example:1234/index.html", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("unexpected host accepted")
	}
	if !isWebFile("INDEX.HTML") || !isWebFile("x.htm") || isWebFile("x.js") {
		t.Fatal("preview file detection")
	}
}
