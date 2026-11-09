package main

import (
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
	"testing"
)

func TestAIFormaProgressAndStop(t *testing.T) {
	a := newApp(t.TempDir())
	a.ai.visible = true
	a.ai.running = true
	a.ai.messages = []aiMessage{{"assistant", "<think>检查文件</think>正在检查"}}
	a.ai.queue = forma.QueueState{PendingOpen: true, CompletedOpen: true}
	a.ai.activities = []aiEvent{
		{toolID: "1", progress: "读取文件"},
		{toolID: "2", progress: "检查完成", toolDone: true},
		{toolID: "3", progress: "执行失败", toolDone: true, toolFailed: true},
	}
	stopped := false
	a.ai.cancel = func() { stopped = true }
	view := ui.NewTester(a.view, 1440, 1000)
	for _, label := range []string{"读取文件", "检查完成", "执行失败"} {
		if !view.HasText(label) {
			t.Fatalf("missing tool status: %s", label)
		}
	}
	if err := view.Click("Stop response"); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("stop did not cancel active task")
	}
}
