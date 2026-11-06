package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func TestAIHistoryReopenAndIsolation(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "cache.sqlite")
	s, err := openProjectStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(root)
	a.projects = s
	a.ai.messages = []aiMessage{{"user", "修复中文问题"}, {"assistant", "<analysis>检查</analysis>\n回复"}}
	a.ai.draft = "接着处理"
	a.ai.engine = "CodeBuddy CLI"
	a.ai.running = true
	a.ai.dirty = true
	if !a.checkpointAI(true) {
		t.Fatal(a.ai.storageErr)
	}
	id := a.ai.chatID
	if err = s.db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openProjectStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	b := newApp(root)
	b.projects = s
	b.restoreLatestAI()
	if b.ai.chatID != id || len(b.ai.messages) != 2 || b.ai.messages[1].text != a.ai.messages[1].text || b.ai.draft != "接着处理" {
		t.Fatalf("conversation lost: %+v", b.ai)
	}
	if b.ai.running || b.ai.err == "" || b.ai.engine != "CodeBuddy CLI" {
		t.Fatal("interrupted task incorrectly restored")
	}
	b.resetAI()
	if b.ai.chatID != "" || b.ai.draft != "" || len(b.ai.messages) != 0 {
		t.Fatal("new conversation not cleared")
	}
	entries, err := s.aiHistory(b.root)
	if err != nil || len(entries) != 1 {
		t.Fatal("new conversation deleted history", err)
	}
	b.ai.draft = "新草稿"
	b.ai.dirty = true
	if !b.checkpointAI(true) {
		t.Fatal(b.ai.storageErr)
	}
	entries, err = s.aiHistory(b.root)
	if err != nil || len(entries) != 2 {
		t.Fatal("draft not stored", err)
	}
	if _, err = s.loadAIChat(t.TempDir(), id); err == nil {
		t.Fatal("cross-project conversation loaded")
	}
	b.restoreAIChat(b.root, id)
	if b.ai.messages[0].text != "修复中文问题" {
		t.Fatal("history selection failed")
	}
}
func TestAISavesStreamingAndCompletion(t *testing.T) {
	s, err := openProjectStore(filepath.Join(t.TempDir(), "cache.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	a := newApp(t.TempDir())
	a.projects = s
	a.ai.root = a.root
	a.ai.messages = []aiMessage{{"user", "task"}, {"assistant", ""}}
	a.ai.running = true
	a.ai.events = make(chan aiEvent, 4)
	a.ai.events <- aiEvent{text: "partial"}
	a.drainAI()
	if !a.checkpointAI(true) {
		t.Fatal(a.ai.storageErr)
	}
	chat, err := s.loadAIChat(a.root, a.ai.chatID)
	if err != nil || !chat.Running || chat.Messages[1].Text != "partial" {
		t.Fatal("stream checkpoint failed", err)
	}
	a.ai.events <- aiEvent{text: " complete"}
	a.ai.events <- aiEvent{done: true}
	a.drainAI()
	chat, err = s.loadAIChat(a.root, a.ai.chatID)
	if err != nil || chat.Running || chat.Messages[1].Text != "partial complete" {
		t.Fatal("completion checkpoint failed", err)
	}
}

func TestSidebarAIHistorySwitch(t *testing.T) {
	store, err := openProjectStore(filepath.Join(t.TempDir(), "projects.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	a := newApp(t.TempDir())
	a.projects = store
	for _, title := range []string{"代码审查", "修复测试"} {
		a.ai.messages = []aiMessage{{"user", title}, {"assistant", title + "回复"}}
		a.ai.dirty = true
		if !a.checkpointAI(true) {
			t.Fatal(a.ai.storageErr)
		}
		if title == "代码审查" {
			a.closeAITab(a.ai.aiSession)
			a.newAITab()
		}
	}
	v := ui.NewTester(a.view, 1440, 900)
	header, ok := v.Find("AI 对话")
	if !ok || header.Y > 200 {
		t.Fatalf("history should follow the empty project tree: %v", header)
	}
	if err := v.Click("AI 对话"); err != nil {
		t.Fatal(err)
	}
	v.Frame()
	if v.HasText("代码审查") {
		t.Fatal("collapsed history is still visible")
	}
	if err := v.Click("AI 对话"); err != nil {
		t.Fatal(err)
	}
	v.Frame()
	for _, title := range []string{"代码审查", "修复测试", "代码审查"} {
		if err := v.Click(title); err != nil {
			t.Fatal(err)
		}
		v.Frame()
		if !a.ai.visible || a.ai.messages[1].text != title+"回复" || len(a.ai.tabs) != 2 {
			t.Fatalf("history selection failed for %q", title)
		}
	}
	deleted := a.ai.chatID
	a.ai.draft = "未保存的草稿"
	a.ai.dirty = true
	a.ai.lastSaved = time.Now()
	if err := v.RightClick("代码审查"); err != nil {
		t.Fatal(err)
	}
	if err := v.ChooseMenuItem("删除对话"); err != nil {
		t.Fatal(err)
	}
	v.Frame()
	entries, err := store.aiHistory(a.root)
	if err != nil || len(entries) != 1 || entries[0].ID == deleted || len(a.ai.tabs) != 1 || a.ai.chatID != entries[0].ID || v.HasText("代码审查") {
		t.Fatalf("deleted history was retained: %v %v", entries, err)
	}

	a.setRoot(t.TempDir())
	v.Frame()
	if v.HasText("修复测试") {
		t.Fatal("sidebar retained another workspace's conversation")
	}
}
