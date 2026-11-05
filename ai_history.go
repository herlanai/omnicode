package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/HyperparameterSI/golet/backend/ui"
)

type storedAIMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}
type storedAIChat struct {
	Messages []storedAIMessage `json:"messages"`
	Draft    string            `json:"draft"`
	Engine   string            `json:"engine"`
	Error    string            `json:"error,omitempty"`
	Running  bool              `json:"running,omitempty"`
}
type aiHistoryEntry struct {
	ID, Title string
	Updated   int64
}

func (s *projectStore) initAIHistory() error {
	_, err := s.db.Execute(context.Background(), `CREATE TABLE IF NOT EXISTS ai_conversations (id TEXT PRIMARY KEY, workspace TEXT NOT NULL, title TEXT NOT NULL, updated_at INTEGER NOT NULL, payload TEXT NOT NULL)`)
	if err != nil {
		return err
	}
	_, err = s.db.Execute(context.Background(), `CREATE INDEX IF NOT EXISTS ai_conversations_workspace ON ai_conversations(workspace,updated_at DESC)`)
	return err
}
func (s *projectStore) aiHistory(root string) ([]aiHistoryEntry, error) {
	rows, err := s.db.Query(context.Background(), `SELECT id,title,updated_at FROM ai_conversations WHERE workspace=? ORDER BY updated_at DESC,id LIMIT 100`, root)
	if err != nil {
		return nil, err
	}
	var out []aiHistoryEntry
	for _, r := range rows.Values {
		out = append(out, aiHistoryEntry{r[0].(string), r[1].(string), r[2].(int64)})
	}
	return out, nil
}
func (s *projectStore) saveAIChat(id, root, title string, chat storedAIChat) error {
	b, err := json.Marshal(chat)
	if err != nil {
		return err
	}
	_, err = s.db.Execute(context.Background(), `INSERT INTO ai_conversations(id,workspace,title,updated_at,payload) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,updated_at=excluded.updated_at,payload=excluded.payload`, id, root, title, time.Now().UnixNano(), string(b))
	return err
}
func (s *projectStore) loadAIChat(root, id string) (storedAIChat, error) {
	var chat storedAIChat
	rows, err := s.db.Query(context.Background(), `SELECT payload FROM ai_conversations WHERE workspace=? AND id=?`, root, id)
	if err != nil {
		return chat, err
	}
	if len(rows.Values) == 0 {
		return chat, fmt.Errorf("会话不存在")
	}
	err = json.Unmarshal([]byte(rows.Values[0][0].(string)), &chat)
	return chat, err
}
func (a *app) checkpointAI(force bool) bool {
	p := &a.ai
	if a.projects == nil || !p.dirty {
		return true
	}
	if !force && time.Since(p.lastSaved) < 500*time.Millisecond {
		return true
	}
	if len(p.messages) == 0 && strings.TrimSpace(p.draft) == "" && p.chatID == "" {
		p.dirty = false
		return true
	}
	root := p.root
	if root == "" {
		root = a.root
	}
	if root == "" {
		return true
	}
	if p.chatID == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			p.storageErr = err.Error()
			return false
		}
		p.chatID = hex.EncodeToString(id[:])
	}
	chat := storedAIChat{Draft: p.draft, Engine: p.engine, Error: p.err, Running: p.running}
	title := p.draft
	for _, m := range p.messages {
		chat.Messages = append(chat.Messages, storedAIMessage{m.role, m.text})
		if m.role == "user" && len(chat.Messages) == 1 {
			title = m.text
		}
	}
	title = strings.Join(strings.Fields(title), " ")
	runes := []rune(title)
	if len(runes) > 48 {
		title = string(runes[:48]) + "…"
	}
	if title == "" {
		title = "新会话"
	}
	p.lastSaved = time.Now()
	if err := a.projects.saveAIChat(p.chatID, root, title, chat); err != nil {
		p.storageErr = "对话保存失败：" + err.Error()
		return false
	}
	p.root = root
	p.dirty = false
	p.storageErr = ""
	return true
}
func (a *app) restoreAIChat(root, id string) {
	if a.projects == nil || a.ai.running || a.hasPendingAI() {
		return
	}
	if !a.checkpointAI(true) {
		return
	}
	chat, err := a.projects.loadAIChat(root, id)
	if err != nil {
		a.ai.storageErr = err.Error()
		return
	}
	a.discardAIChanges()
	p := &a.ai
	p.progress = ""
	p.activities = nil
	p.messages = nil
	for _, m := range chat.Messages {
		p.messages = append(p.messages, aiMessage{m.Role, m.Text})
	}
	p.chatID = id
	p.root = root
	p.draft = chat.Draft
	p.engine = chat.Engine
	p.err = chat.Error
	p.storageErr = ""
	p.dirty = false
	p.thoughtOpen = nil
	if chat.Running {
		p.err = "上次任务已中断，已恢复收到的内容；可继续发送消息。"
	}
}
func (a *app) restoreLatestAI() {
	if a.projects == nil || a.ai.running || a.hasPendingAI() {
		return
	}
	if !a.checkpointAI(true) {
		return
	}
	entries, err := a.projects.aiHistory(a.root)
	if err != nil {
		a.ai.storageErr = err.Error()
		return
	}
	a.resetAI()
	if len(entries) > 0 {
		a.restoreAIChat(a.root, entries[0].ID)
	}
}
func (a *app) aiHistoryMenu(m *ui.Menu) {
	if a.projects == nil {
		m.Item("历史记录不可用").Disabled(true)
		return
	}
	entries, err := a.projects.aiHistory(a.root)
	if err != nil {
		a.ai.storageErr = err.Error()
		return
	}
	if len(entries) == 0 {
		m.Item("暂无历史会话").Disabled(true)
	}
	for _, entry := range entries {
		label := entry.Title + " · " + time.Unix(0, entry.Updated).Local().Format("01-02 15:04")
		if m.Item(label).Chosen() {
			a.restoreAIChat(a.root, entry.ID)
		}
	}
}
