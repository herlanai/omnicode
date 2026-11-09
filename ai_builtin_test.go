package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuiltinConfiguredModelAndTools(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("configuration not used")
		}
		var body struct {
			Model    string
			Messages []struct{ Role string }
			Stream   bool
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "configured-model" || !body.Stream {
			t.Error("model or streaming incorrect")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"tool-1\",\"function\":{\"name\":\"write_file\",\"arguments\":\"{\\\"path\\\":\\\"result.go\\\",\\\"content\\\":\\\"package main\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
		} else {
			if body.Messages[len(body.Messages)-1].Role != "tool" {
				t.Error("tool result missing")
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"完成\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer server.Close()
	p := defaultPreferences()
	p.Models[0].APIKey = "test-secret"
	p.Models[0].Model = "configured-model"
	p.Models[0].BaseURL = server.URL
	root := t.TempDir()
	a := newApp(root)
	a.prefs = p
	a.ai.draft = "创建文件"
	a.sendAI()
	if !a.ai.running {
		t.Fatal(a.ai.err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.ai.running && time.Now().Before(deadline) {
		a.drainAI()
		time.Sleep(10 * time.Millisecond)
	}
	if a.ai.running || a.ai.err != "" || !a.ai.pending() {
		t.Fatalf("running=%v error=%s", a.ai.running, a.ai.err)
	}
	if _, err := os.Stat(filepath.Join(root, "result.go")); !os.IsNotExist(err) {
		t.Fatal("file applied before approval")
	}
	a.approveAIChanges(map[string]bool{"result.go": true})
	data, err := os.ReadFile(filepath.Join(root, "result.go"))
	if err != nil || string(data) != "package main" || calls != 2 {
		t.Fatalf("%q %v calls=%d", data, err, calls)
	}
}
func TestBuiltinReadOnlyAndConfinement(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, tool := range builtinTools(root, true) {
		if tool.Name == "write_file" {
			t.Fatal("read-only exposes write")
		}
	}
	for _, tool := range builtinTools(root, false) {
		_, err := tool.Execute(context.Background(), map[string]any{"path": "../escape", "content": "bad"})
		if err == nil || !strings.Contains(err.Error(), "相对路径") {
			t.Fatal(tool.Name, err)
		}
	}
}
