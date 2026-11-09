package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
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
	if !a.ai.visible {
		t.Fatal("AI panel not shown")
	}
	if err := tester.Click("Prompt text"); err != nil {
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
	p.Engine = "pi-coding-agent"
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

func TestAIConcurrentTabs(t *testing.T) {
	a := newApp(t.TempDir())
	a.prefs.AIAccess = "完全控制"
	a.prefs.Engine = "DSH"
	engine := filepath.Join(t.TempDir(), "buddy")
	if err := os.WriteFile(engine, []byte("#!/bin/sh\nsleep 0.1\nprintf '%s' \"$*\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a.prefs.BuddyPath = engine
	first := a.ai.aiSession
	first.engine, first.draft = "CodeBuddy CLI", "first-task"
	a.sendAI()
	a.newAITab()
	second := a.ai.aiSession
	second.engine, second.draft = "CodeBuddy CLI", "second-task"
	a.sendAI()
	if !first.running || !second.running {
		t.Fatalf("parallel launch failed: %s / %s", first.err, second.err)
	}
	a.closeAITab(first)
	if len(a.ai.tabs) != 2 {
		t.Fatal("closed running task")
	}
	until := time.Now().Add(5 * time.Second)
	for (first.running || second.running) && time.Now().Before(until) {
		a.drainAI()
		time.Sleep(10 * time.Millisecond)
	}
	for _, tab := range []*aiSession{first, second} {
		if tab.running || tab.err != "" || !strings.Contains(tab.messages[1].text, tab.messages[0].text) {
			t.Fatal("wrong stream", tab.messages, tab.err)
		}
	}
	if strings.Contains(second.messages[1].text, "first-task") || strings.Contains(first.messages[1].text, "second-task") {
		t.Fatal("crossed streams")
	}
	a.closeAITab(first)
	if a.ai.aiSession != second || a.prefs.Engine != "DSH" {
		t.Fatal("changed active session or global engine")
	}
}

func TestAITabComposerSwitch(t *testing.T) {
	a := newApp(t.TempDir())
	a.ai.visible = true
	view := ui.NewTester(a.view, 1440, 900)
	click := func(label string) {
		t.Helper()
		if err := view.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("Prompt text")
	view.Type("first")
	click("新建 AI 会话")
	click("Prompt text")
	view.Type("second")
	click("agent 1")
	if a.ai.draft != "first" || a.ai.tabs[1].draft != "second" {
		t.Fatalf("drafts crossed: %q / %q", a.ai.draft, a.ai.tabs[1].draft)
	}
}

func TestAITabHeaderLayout(t *testing.T) {
	a := newApp(t.TempDir())
	a.ai.visible = true
	a.newAITab()
	view := ui.NewTester(a.view, 1100, 700)
	find := func(label string) ui.Rect {
		t.Helper()
		r, ok := view.Find(label)
		if !ok {
			t.Fatalf("missing %s", label)
		}
		return r
	}
	tab, close, add := find("agent 2"), find("关闭 agent 2"), find("新建 AI 会话")
	if add.Y >= tab.Y+tab.H || add.Y+add.H <= tab.Y || close.X < tab.X+tab.W/2 || close.Y+close.H/2 >= tab.Y+tab.H/2 {
		t.Fatalf("unexpected header layout: tab %+v, close %+v, add %+v", tab, close, add)
	}
	if err := view.Click("关闭 agent 1"); err != nil {
		t.Fatal(err)
	}
	if len(a.ai.tabs) != 1 || a.ai.number != 2 {
		t.Fatal("closing background tab changed active session")
	}
	if err := view.Click("关闭 agent 2"); err != nil {
		t.Fatal(err)
	}
	if a.ai.visible || len(a.ai.tabs) != 0 || a.ai.aiSession != nil || !a.canQuit() {
		t.Fatal("last tab did not close the AI panel")
	}
	if err := view.Click("打开 AI 对话"); err != nil {
		t.Fatal(err)
	}
	if !a.ai.visible || len(a.ai.tabs) != 1 || a.ai.number != 1 {
		t.Fatal("reopening must create a single new session")
	}
}

func TestAIActiveFileContext(t *testing.T) {
	root := t.TempDir()
	doc := newDocument("item.go", "first\nsecond\nthird")
	doc.path = filepath.Join(root, "models", "item.go")
	doc.state.Select(13, 6)
	task := "请修改 handlers/item.go"
	prompt := aiPrompt(nil, task, root, doc, nil)
	if !strings.Contains(prompt, `Active editor file (project-relative): "models/item.go"`) || !strings.Contains(prompt, "Selected lines: 2-2") || !strings.HasSuffix(prompt, "Current user request:\n"+task) {
		t.Fatal(prompt)
	}
	doc.state.Select(13, 13)
	if prompt := aiPrompt(nil, task, root, doc, nil); !strings.Contains(prompt, "Cursor line: 3") {
		t.Fatal(prompt)
	}
	for _, active := range []*document{nil, {path: filepath.Join(t.TempDir(), "outside.go")}, {}} {
		if prompt := aiPrompt(nil, task, root, active, nil); strings.Contains(prompt, "Active editor file") {
			t.Fatal(prompt)
		}
	}
}

func TestAttachSelectionShortcut(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.go")
	src := "one\n二\nthree\nfour"
	put(t, path, src)
	a := newApp(root)
	a.open(path)
	tester := ui.NewTester(a.view, 1200, 780)
	tester.ClickAt(510, 155)
	a.current().state.Select(len("one\n二\nthree\n"), len("one\n"))
	tester.Key(ui.Cmd, ui.KeyI)
	if (!tester.HasText("sample.go") || !tester.HasText("第 2–3 行")) || a.ai.selection == nil {
		t.Fatal("missing selection attachment")
	}
	tester.Type("解释")
	if a.ai.draft != "解释" {
		t.Fatal("composer did not receive input", a.ai.draft)
	}
	a.current().state.Select(0, 0)
	prompt := aiPrompt(nil, a.ai.draft, root, a.current(), a.ai.selection)
	if a.ai.selection.Code != "二\nthree\n" || !strings.Contains(prompt, `"二\nthree\n"`) {
		t.Fatal(prompt)
	}
	if err := tester.Click("Remove attachment sample.go"); err != nil || a.ai.selection != nil {
		t.Fatal("remove attachment", err)
	}
}

func TestSelectionQuestionInLargeProject(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("native sandbox")
	}
	a := newApp(t.TempDir())
	large := filepath.Join(a.root, "large.dat")
	put(t, large, "")
	if err := os.Truncate(large, 129<<20); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(a.root, "schema.sql"), "related")
	engine := filepath.Join(t.TempDir(), "fake-codebuddy")
	put(t, engine, `#!/bin/sh
case "$*" in *"SELECT count("* ) ;; *) exit 1 ;; esac
if (echo leaked > unauthorized.txt) 2>/dev/null; then exit 1; fi
cat schema.sql
`)
	if err := os.Chmod(engine, 0700); err != nil {
		t.Fatal(err)
	}
	a.prefs.Engine = "CodeBuddy CLI"
	a.prefs.BuddyPath = engine
	a.ai.selection = &aiSelection{File: "query.go", First: 2, Last: 2, Code: "SELECT count(*)"}
	a.ai.draft = "这段代码完整 SQL 是什么？"
	a.sendAI()
	if !a.ai.running {
		t.Fatal(a.ai.err)
	}
	until := time.Now().Add(8 * time.Second)
	for a.ai.running && time.Now().Before(until) {
		a.drainAI()
		time.Sleep(10 * time.Millisecond)
	}
	if a.ai.running || a.ai.err != "" || a.ai.messages[1].text != "related" || a.ai.pending() {
		t.Fatalf("review state %v, error %s, replies %v", a.ai.running, a.ai.err, a.ai.messages)
	}
}
