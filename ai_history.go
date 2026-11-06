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
	Selection *aiSelection      `json:"selection,omitempty"`
	Messages  []storedAIMessage `json:"messages"`
	Draft     string            `json:"draft"`
	Engine    string            `json:"engine"`
	Error     string            `json:"error,omitempty"`
	Running   bool              `json:"running,omitempty"`
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
	return a.saveAISession(a.ai.aiSession, force)
}
func (a *app) saveAISession(p *aiSession, force bool) bool {
	if a.projects == nil || !p.dirty {
		return true
	}
	if !force && time.Since(p.lastSaved) < 500*time.Millisecond {
		return true
	}
	if len(p.messages) == 0 && strings.TrimSpace(p.draft) == "" && p.chatID == "" && p.selection == nil {
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
	chat := storedAIChat{Selection: p.selection, Draft: p.draft, Engine: p.engine, Error: p.err, Running: p.running}
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
	a.ai.historyRoot = ""
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
	a.ai.aiSession.applyStoredChat(root, id, chat)
}

func (p *aiSession) applyStoredChat(root, id string, chat storedAIChat) {
	p.selection = chat.Selection
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
			a.openHistoryEntry(entry.ID)
		}
	}
}

func (a *app) openHistoryEntry(id string) {
	for _, tab := range a.ai.tabs {
		if tab.chatID == id && tab.root == a.root {
			a.ai.aiSession = tab
			a.showAI()
			return
		}
	}
	chat, err := a.projects.loadAIChat(a.root, id)
	if err != nil {
		a.report(err)
		return
	}
	a.newAITab()
	a.ai.aiSession.applyStoredChat(a.root, id, chat)
	a.showAI()
}

func (a *app) deleteHistoryEntry(id string) {
	var open *aiSession
	for _, tab := range a.ai.tabs {
		if tab.root == a.root && tab.chatID == id {
			if tab.running || tab.pending() {
				a.notice = "请先结束 AI 任务并处理待批准修改"
				return
			}
			open = tab
			break
		}
	}
	_, err := a.projects.db.Execute(context.Background(), "DELETE FROM ai_conversations WHERE workspace=? AND id=?", a.root, id)
	if err != nil {
		a.report(err)
		return
	}
	if open != nil {
		open.discardChanges()
		a.removeAITab(open)
	}
	a.ai.historyRoot = ""
}

func (a *app) sidebarAIHistory(c *ui.Context) {
	if a.projects == nil || a.root == "" {
		return
	}
	if a.ai.historyRoot != a.root {
		entries, err := a.projects.aiHistory(a.root)
		a.report(err)
		a.ai.history = entries
		a.ai.historyRoot = a.root
	}
	if len(a.ai.history) == 0 {
		return
	}
	ui.Column(c).FillWidth().Gap(4).Padding(8, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
		expanded := !a.ai.historyCollapsed
		header := ui.ButtonBase(c.Key("history-toggle")).FillWidth().Height(28).Padding(0, 8).Gap(7).AlignItems(ui.Center).Justify(ui.Start).Radius(6).Label("AI 对话").Expanded(expanded).OnClick(func() { a.ai.historyCollapsed = !a.ai.historyCollapsed })
		if header.Hovered() {
			header.Background(c.Theme().SurfaceHover)
		}
		header.Children(func() {
			symbol := "chevron-right"
			if expanded {
				symbol = "chevron-down"
			}
			icon(c, symbol, 12)
			muted(c, "AI 对话").Bold()
		})
		if !expanded {
			return
		}
		for _, entry := range a.ai.history {
			row := ui.ButtonBase(c.Key(entry.ID)).FillWidth().Height(28).Padding(0, 8).Gap(7).AlignItems(ui.Center).Justify(ui.Start).Radius(6).Label(entry.Title).Tooltip(entry.Title).OnClick(func() { a.openHistoryEntry(entry.ID) })
			row.ContextMenu(func(m *ui.Menu) {
				if m.Item("删除对话").Chosen() {
					a.deleteHistoryEntry(entry.ID)
				}
			})
			if a.ai.visible && a.ai.aiSession != nil && a.ai.chatID == entry.ID {
				row.Background(c.Theme().Sidebar.Mix(c.Theme().Accent, 0.14))
			} else if row.Hovered() {
				row.Background(c.Theme().SurfaceHover)
			}
			row.Transition(chromeTransition).Children(func() {
				icon(c, "history", 14)
				ui.Text(c, entry.Title).FontSize(12).MinWidth(0).MaxLines(1)
			})
		}
	})
}
