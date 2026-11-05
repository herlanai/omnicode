package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestAIEntryAndLayout(t *testing.T) {
	a := newApp(t.TempDir())
	tester := ui.NewTester(a.view, 1440, 900)
	if err := tester.Click("打开 AI 对话"); err != nil {
		t.Fatal(err)
	}
	if !a.ai.visible || !tester.HasText("一起，把想法实现。") {
		t.Fatal("AI panel not shown")
	}
	if err := tester.Click("AI 消息"); err != nil {
		t.Fatal(err)
	}
	tester.Type("解释项目")
	if a.ai.draft != "解释项目" {
		t.Fatal("draft lost")
	}
	if os.Getenv("OMNICODE_AI_SCREENSHOT") != "" {
		os.MkdirAll("artifacts", 0755)
		f, err := os.Create("artifacts/ai-panel.png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, tester.Image())
	}
	if err := tester.Click("隐藏 AI"); err != nil {
		t.Fatal(err)
	}
	if a.ai.visible || a.ai.draft != "解释项目" {
		t.Fatal("hiding must preserve draft")
	}
}
func TestAIModelCommand(t *testing.T) {
	p := defaultPreferences()
	p.PiPath = "/bin/echo"
	t.Setenv("DEEPSEEK_API_KEY", "test-only")
	dir := t.TempDir()
	cmd, env, err := aiCommand(p, "literal $(touch /tmp/not-executed)", dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cmd[len(cmd)-1], "Omnicode task:\nliteral $") {
		t.Fatal("prompt not passed literally")
	}
	if len(env) != 2 || env[0] != "PI_CODING_AGENT_DIR="+dir || env[1] != "OMNICODE_PI_EVENT_STREAM=1" {
		t.Fatal("config not isolated")
	}
	b, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "test-only") || !strings.Contains(string(b), "$DEEPSEEK_API_KEY") {
		t.Fatal("secret embedded in config")
	}
	t.Setenv("DEEPSEEK_API_KEY", "")
	if _, _, err = aiCommand(p, "task", dir, dir); err == nil {
		t.Fatal("missing key accepted")
	}
}
func TestAIRunAndCancellation(t *testing.T) {
	events := make(chan aiEvent, 64)
	root := t.TempDir()
	if err := runAI(context.Background(), []string{"/bin/sh", "-c", "pwd; printf response"}, nil, root, events); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for len(events) > 0 {
		output.WriteString((<-events).text)
	}
	real, _ := filepath.EvalSymlinks(root)
	if !strings.Contains(output.String(), real) || !strings.Contains(output.String(), "response") {
		t.Fatalf("wrong output: %q", output.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := runAI(ctx, []string{"/bin/sh", "-c", "sleep 30"}, nil, root, events)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatal("child did not stop promptly")
	}
}
func TestAIProjectAndRefresh(t *testing.T) {
	a := newApp(t.TempDir())
	path := filepath.Join(a.root, "main.go")
	put(t, path, "package main\n")
	a.open(path)
	a.ai.root = a.root
	put(t, path, "package changed\n")
	a.reloadAIText()
	if !strings.Contains(a.current().state.Buffer.String(), "changed") {
		t.Fatal("disk change not loaded")
	}
	a.current().state.Buffer.Set("manual edit")
	put(t, path, "external edit")
	a.reloadAIText()
	if a.current().state.Buffer.String() != "manual edit" {
		t.Fatal("manual edit overwritten")
	}
	a.ai.draft = "task"
	a.ai.root = filepath.Join(a.root, "old")
	a.sendAI()
	if a.ai.running || a.ai.err == "" {
		t.Fatal("workspace mismatch accepted")
	}
}

func TestAISendEndToEndWithLocalEngine(t *testing.T) {
	dir := t.TempDir()
	engine := filepath.Join(dir, "fake-agent")
	if err := os.WriteFile(engine, []byte("#!/bin/sh\nprintf 'local test reply'\nprintf 'created by local test' > ai-result.txt\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a := newApp(t.TempDir())
	a.prefs.Engine = "CodeBuddy CLI"
	a.prefs.BuddyPath = engine
	a.prefs.AIAccess = "完全控制"
	a.ai.draft = "create a file"
	a.sendAI()
	if !a.ai.running {
		t.Fatal(a.ai.err)
	}
	until := time.Now().Add(3 * time.Second)
	for a.ai.running && time.Now().Before(until) {
		a.drainAI()
		time.Sleep(10 * time.Millisecond)
	}
	if a.ai.running || a.ai.err != "" {
		t.Fatalf("task failed: %s", a.ai.err)
	}
	if len(a.ai.messages) != 2 || a.ai.messages[1].text != "local test reply" {
		t.Fatalf("reply lost: %+v", a.ai.messages)
	}
	if _, err := os.Stat(filepath.Join(a.root, "ai-result.txt")); err != nil {
		t.Fatal("wrong workspace", err)
	}
}
