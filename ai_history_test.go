package main

import (
	"path/filepath"
	"testing"
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
