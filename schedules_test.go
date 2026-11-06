package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestScheduleNext(t *testing.T) {
	now := time.Date(2026, 11, 6, 9, 30, 0, 0, time.Local)
	for _, tc := range []struct{ cadence, clock, want string }{
		{"每小时", "09:30", "06 10:30"}, {"每天", "09:00", "07 09:00"}, {"工作日", "09:30", "09 09:30"}, {"每周一", "23:10", "09 23:10"},
	} {
		next, err := (scheduledTask{cadence: tc.cadence, clock: tc.clock}).next(now)
		if err != nil || next.Format("02 15:04") != tc.want {
			t.Fatalf("%s: %v %v", tc.cadence, next, err)
		}
	}
}
func TestScheduledTaskDispatch(t *testing.T) {
	a := newApp(t.TempDir())
	store, err := openProjectStore(filepath.Join(t.TempDir(), "s.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	a.projects = store
	a.prefs.BuddyPath = filepath.Join(t.TempDir(), "buddy")
	if err = os.WriteFile(a.prefs.BuddyPath, []byte("#!/bin/sh\nprintf scheduled-reply\npwd > ran-in.txt\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a.prefs.AIAccess = "完全控制"
	tester := ui.NewTester(a.view, 1100, 720)
	if err = tester.Click("定时任务"); err != nil {
		t.Fatal(err)
	}
	if err = tester.Click("检查测试并修复"); err != nil {
		t.Fatal(err)
	}
	a.schedules.edit.engine = "CodeBuddy CLI"
	if err = tester.Click("保存任务"); err != nil {
		t.Fatal(err)
	}
	target := a.root
	a.setRoot(t.TempDir())
	prev := a.ai.aiSession
	if !a.scheduleSQL("UPDATE scheduled_tasks SET due=1 WHERE workspace=?", target) {
		t.Fatal(a.schedules.err)
	}
	tick := func() { ui.NewTester(a.tickSchedules, 100, 100) }
	a.git.mutating = true
	tick()
	if len(a.ai.tabs) != 1 {
		t.Fatal("dispatched during git operation")
	}
	a.git.mutating = false
	a.schedules.poll = time.Time{}
	tick()
	if a.ai.aiSession != prev || len(a.ai.tabs) != 2 {
		t.Fatal("expected separate background session")
	}
	tab := a.ai.tabs[1]
	deadline := time.Now().Add(5 * time.Second)
	for tab.running && time.Now().Before(deadline) {
		a.drainAI()
		time.Sleep(time.Millisecond)
	}
	if tab.err != "" || tab.running || len(a.ai.tabs) != 1 {
		t.Fatalf("scheduled run: %v", tab.err)
	}
	if err = a.loadSchedules(); err != nil {
		t.Fatal(err)
	}
	task := a.schedules.tasks[0]
	if task.due <= time.Now().Unix() {
		t.Fatal("occurrence was not advanced")
	}
	chat, err := store.loadAIChat(target, task.lastRun)
	if err != nil || len(chat.Messages) != 2 || chat.Messages[1].Text != "scheduled-reply" {
		t.Fatalf("history: %+v %v", chat, err)
	}
	data, err := os.ReadFile(filepath.Join(target, "ran-in.txt"))
	if err != nil || string(data) != target+"\n" {
		t.Fatalf("wrong working directory: %q %v", data, err)
	}
	a.schedules.selected = task.id
	a.dispatchSchedule(task, time.Now(), task.due)
	run := a.busyAITab()
	if run == nil {
		t.Fatal("manual dispatch did not start")
	}
	deadline = time.Now().Add(5 * time.Second)
	for run.running && time.Now().Before(deadline) {
		a.drainAI()
		time.Sleep(time.Millisecond)
	}
	if err = a.loadSchedules(); err != nil {
		t.Fatal(err)
	}
	if a.schedules.tasks[0].due != task.due || len(a.schedules.runs) != 2 {
		t.Fatal("manual run changed schedule or lost history")
	}
	a.openScheduledRun(target, task.lastRun)
	if a.ai.chatID != task.lastRun || a.ai.root != target {
		t.Fatal("older run did not open")
	}

}

func TestScheduleRailNavigation(t *testing.T) {
	a := newApp("")
	tester := ui.NewTester(a.view, 800, 520)
	click := func(label string) {
		t.Helper()
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	for _, label := range []string{"快速打开", "快速打开", "源代码管理", "源代码管理", "文件浏览器", "HTTP 请求"} {
		click("定时任务")
		if !a.schedules.open {
			t.Fatal("schedule page did not open")
		}
		click(label)
		if a.schedules.open || (label == "快速打开" && !a.quick) || (label == "源代码管理" && !a.git.visible) {
			t.Fatalf("failed to navigate to %s", label)
		}
	}
}
