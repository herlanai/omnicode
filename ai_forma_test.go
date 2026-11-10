package main

import (
	"github.com/HyperparameterSI/golet/backend/ui"
	"testing"
)

func TestAIFormaProgressAndStop(t *testing.T) {
	a := newApp(t.TempDir())
	a.ai.visible = true
	a.ai.running = true
	a.ai.messages = []aiMessage{{"assistant", "<think>检查文件</think>正在检查"}}
	a.ai.progress = "读取文件"
	a.ai.activities = []aiEvent{
		{toolID: "1", progress: "读取文件"},
		{toolID: "2", progress: "检查完成", toolDone: true},
		{toolID: "3", progress: "执行失败", toolDone: true, toolFailed: true},
	}
	stopped := false
	a.ai.cancel = func() { stopped = true }
	view := ui.NewTester(a.view, 1440, 1000)
	if !view.HasText("读取文件") {
		t.Fatal("current progress missing")
	}
	if view.HasText("To do · 1") || view.HasText("Completed · 2") {
		t.Fatal("removed queue still visible")
	}

	if err := view.Click("Stop response"); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("stop did not cancel active task")
	}
}
