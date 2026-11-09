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
	"sync/atomic"
	"testing"
	"time"
)

func TestPiBundledPaths(t *testing.T) {
	resources := filepath.Join(t.TempDir(), "App With Spaces.app", "Contents", "Resources")
	if _, _, err := piRuntimeAt(resources); err == nil {
		t.Fatal("incomplete runtime accepted")
	}
	node := filepath.Join(resources, "pi/bin/node")
	entry := filepath.Join(resources, "pi/node_modules/@earendil-works/pi-coding-agent/dist/cli.js")
	os.MkdirAll(filepath.Dir(node), 0755)
	os.MkdirAll(filepath.Dir(entry), 0755)
	os.WriteFile(node, []byte("test"), 0755)
	os.WriteFile(entry, []byte("test"), 0644)
	exe, args, err := piRuntimeAt(resources)
	if err != nil || exe != node || len(args) != 1 || args[0] != entry {
		t.Fatal("bundled paths not preserved")
	}
}

func TestBundledPiWritesWithModelKey(t *testing.T) {
	if os.Getenv("OMNICODE_PI_RUNTIME_TEST") != "1" {
		t.Skip("opt-in bundled runtime integration")
	}
	// Real Pi + bundled Node, with a local model server; no external model or key.
	var requests atomic.Int32
	liveText := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-test-key" {
			t.Error("configured API key not delivered")
			http.Error(w, "unauthorized", 401)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `data: {"id":"test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"write","arguments":"{\"path\":\"pi-result.txt\",\"content\":\"created by bundled Pi\\n\"}"}}]},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, `data: {"id":"test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"id":"test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"OK"},"finish_reason":null}]}`+"\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-liveText:
			case <-time.After(8 * time.Second):
				t.Error("text was not streamed before completion")
			}
			fmt.Fprint(w, `data: {"id":"test","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	p := defaultPreferences()
	p.Engine = "pi-coding-agent"
	p.Models = []modelProfile{{Name: "Local", Provider: "omnicode-test", Model: "local-test", API: "openai-completions", BaseURL: server.URL + "/v1", APIKey: "local-test-key"}}
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	root := t.TempDir()
	config := t.TempDir()
	work := root
	var review *aiReview
	if os.Getenv("OMNICODE_PI_REVIEW_TEST") == "1" {
		var err error
		review, err = prepareAIReview(root, config, true)
		if err != nil {
			t.Fatal(err)
		}
		work = review.Work
	}
	command, env, err := aiCommand(p, "Create pi-result.txt", config, work)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("OMNICODE_PI_BUNDLE_RESOURCES") != "" {
		node, prefix, err := piRuntimeAt(os.Getenv("OMNICODE_PI_BUNDLE_RESOURCES"))
		if err != nil {
			t.Fatal(err)
		}
		command = append(append([]string{node}, prefix...), command[2:]...)
		env = append(env, "PATH="+filepath.Dir(node)+":/usr/bin:/bin:/usr/sbin:/sbin")
	}
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	events := make(chan aiEvent, 1000)
	if review != nil {
		var sandboxEnv []string
		command, sandboxEnv, err = sandboxAICommand(command, review, p.Engine)
		if err != nil {
			t.Fatal(err)
		}
		env = append(env, sandboxEnv...)
	}
	finished := make(chan error, 1)
	go func() { finished <- runAI(ctx, command, env, work, events) }()
	var out strings.Builder
	streamed, toolSeen := false, false
	consume := func(e aiEvent) {
		out.WriteString(e.text)
		if e.toolID != "" {
			toolSeen = true
		}
		if strings.Contains(out.String(), "OK") && !streamed {
			streamed = true
			close(liveText)
		}
	}
waiting:
	for {
		select {
		case e := <-events:
			consume(e)
		case err = <-finished:
			break waiting
		}
	}
	for len(events) > 0 {
		consume(<-events)
	}
	if !streamed || !toolSeen {
		t.Fatal("missing live text or tool progress", out.String())
	}
	if err != nil {
		t.Fatalf("bundled Pi: %v; %s", err, out.String())
	}
	data, err := os.ReadFile(filepath.Join(work, "pi-result.txt"))
	if err != nil || string(data) != "created by bundled Pi\n" {
		t.Fatalf("tool write failed: %v; %s", err, out.String())
	}
	if !strings.Contains(out.String(), "OK") || requests.Load() < 2 {
		t.Fatal("model turn incomplete")
	}
	if review != nil {
		if _, err := os.Stat(filepath.Join(root, "pi-result.txt")); !os.IsNotExist(err) {
			t.Fatal("unapproved Pi write")
		}
		changes, err := review.scan()
		if err != nil || len(changes) != 1 {
			t.Fatal("missing Pi proposal", err)
		}
		if err := applyAIChange(root, changes[0]); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("Bundled Pi wrote a file using the configured model key with no user Node/Pi on PATH")
}
