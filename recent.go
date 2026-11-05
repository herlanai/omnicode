package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HyperparameterSI/golet/backend/plugins/sqlite"
	"github.com/HyperparameterSI/golet/backend/ui"
)

type recentProject struct {
	Path     string
	OpenedAt int64
}
type projectStore struct{ db *sqlite.DB }

func openProjectStore(path string) (*projectStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sqlite.Open(context.Background(), path, sqlite.OpenOptions{BusyTimeout: 500 * time.Millisecond})
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	_, err = db.Execute(context.Background(), `CREATE TABLE IF NOT EXISTS recent_projects (path TEXT PRIMARY KEY NOT NULL, opened_at INTEGER NOT NULL)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	store := &projectStore{db: db}
	if err = store.initAIHistory(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}
func (s *projectStore) remember(path string) error {
	_, err := s.db.Transaction(context.Background(), []sqlite.Statement{
		{SQL: `INSERT INTO recent_projects(path, opened_at) VALUES (?, ?) ON CONFLICT(path) DO UPDATE SET opened_at = excluded.opened_at`, Args: []any{path, time.Now().UnixNano()}},
		{SQL: `DELETE FROM recent_projects WHERE path NOT IN (SELECT path FROM recent_projects ORDER BY opened_at DESC, path LIMIT 20)`},
	})
	return err
}
func (s *projectStore) list() ([]recentProject, error) {
	rows, err := s.db.Query(context.Background(), `SELECT path, opened_at FROM recent_projects ORDER BY opened_at DESC, path`)
	if err != nil {
		return nil, err
	}
	out := make([]recentProject, 0, len(rows.Values))
	for _, row := range rows.Values {
		out = append(out, recentProject{Path: row[0].(string), OpenedAt: row[1].(int64)})
	}
	return out, nil
}
func (s *projectStore) remove(path string) error {
	_, err := s.db.Execute(context.Background(), `DELETE FROM recent_projects WHERE path = ?`, path)
	return err
}
func (a *app) initProjectStore() {
	dir, err := os.UserConfigDir()
	if err != nil {
		a.report(err)
		return
	}
	store, err := openProjectStore(filepath.Join(dir, "Omnicode", "cache.sqlite"))
	if err != nil {
		a.report(fmt.Errorf("项目记录不可用：%w", err))
		return
	}
	a.projects = store
	a.restoreLatestAI()
	if a.root != "" {
		a.rememberProject()
	} else {
		a.loadRecent()
	}
}
func (a *app) loadRecent() {
	if a.projects == nil {
		return
	}
	items, err := a.projects.list()
	if err != nil {
		a.report(err)
		return
	}
	a.recent = items
}
func (a *app) rememberProject() {
	if a.projects == nil {
		return
	}
	if err := a.projects.remember(a.root); err != nil {
		a.report(err)
		return
	}
	a.loadRecent()
}
func compactPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
func (a *app) recentView(c *ui.Context) {
	ui.Column(c).Width(420).MaxWidth(420).Gap(8).Margin(16, 0, 0, 0).Children(func() {
		muted(c, "最近项目")
		ui.Scroll(c.Key("recent-projects")).FillWidth().Height(float32(min(len(a.recent), 4) * 48)).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for _, item := range a.recent {
					path := item.Path
					ui.Row(c.Key(path)).FillWidth().Height(46).Gap(4).AlignItems(ui.Center).Children(func() {
						ui.Button(c.Key("open-recent"), "").Grow(1).MinWidth(0).Height(44).Padding(4, 8).Border(0, ui.Transparent).Background(ui.Transparent).Radius(6).Label("打开项目 " + path).Tooltip(path).OnClick(func() { a.setRoot(path) }).Children(func() {
							ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
								icon(c, "folder", 16)
								ui.Column(c).Grow(1).MinWidth(0).Clip().Gap(2).Children(func() {
									ui.Text(c, filepath.Base(path)).FontSize(13).MaxLines(1)
									muted(c, compactPath(path)).FontSize(11).MaxLines(1)
								})
							})
						})
						iconButton(c, "remove-recent", "close", "移除最近项目记录", func() {
							if err := a.projects.remove(path); err != nil {
								a.report(err)
							} else {
								a.loadRecent()
							}
						}).Size(26, 26).Padding(4)
					})
				}
			})
		})
	})
}
