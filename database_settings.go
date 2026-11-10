package main

import (
	"fmt"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
)

func (a *app) deleteDatabaseConnection(conn dbConnection) {
	p := &a.database
	if p.busy {
		return
	}
	answer, err := golet.Dialog.Message(golet.MessageOptions{Parent: a.win, Message: "移除连接「" + conn.Name + "」？", Detail: "数据库和数据文件会保留。", Buttons: []string{"取消", "移除"}, DefaultButton: 0, CancelButton: 0})
	if err != nil {
		p.err = err.Error()
		return
	}
	if answer.Button != 1 {
		return
	}
	old := p.connections
	next := make([]dbConnection, 0, len(old))
	for _, item := range old {
		if item.ID != conn.ID {
			next = append(next, item)
		}
	}
	p.connections = next
	if !a.saveDatabasePanel() {
		p.connections = old
		return
	}
	delete(p.trees, conn.ID)
	if p.active == conn.ID {
		p.active = ""
		p.tables = nil
		p.table = ""
		p.result = dbResult{}
		p.structureTable = ""
		p.ddl = nil
	}
	if p.draft != nil && p.draft.ID == conn.ID {
		p.draft = nil
	}
	p.err = ""
	p.status = "已移除 · " + conn.Name
}
func (a *app) databaseSettings(c *ui.Context) {
	p := &a.database
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Padding(32).Gap(16).Children(func() {
		if p.draft != nil {
			a.databaseForm(c)
			return
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			settingsHeading(c, "database", "数据库", 22).Grow(1)
			iconButton(c, "settings-db-add", "plus", "新增数据库连接", func() {
				p.draft = &dbConnection{Engine: "MySQL", Host: "127.0.0.1", Port: "3306", TLS: true}
				p.err = ""
				p.status = ""
			}).Disabled(p.busy)
		})
		muted(c, fmt.Sprintf("共 %d 个连接 · 与数据库工具共用配置", len(p.connections)))
		if p.err != "" {
			ui.Text(c, p.err).TextColor(c.Theme().Danger).Wrap()
		}
		if p.status != "" {
			muted(c, p.status).Wrap()
		}
		ui.Scroll(c.Key("database-settings-list")).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(720).Gap(10).Children(func() {
				if len(p.connections) == 0 {
					muted(c, "暂无连接，点击右上角 + 添加 MySQL、SQLite 或 Cloudflare D1。").Wrap()
				}
				for _, conn := range p.connections {
					ui.Row(c.Key(conn.ID)).FillWidth().Padding(12).Gap(10).AlignItems(ui.Center).Radius(8).Border(1, c.Theme().Border).Children(func() {
						icon(c, "database", 18)
						ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
							ui.Text(c, conn.Name).Bold().MaxLines(1)
							detail := conn.Engine
							switch conn.Engine {
							case "MySQL":
								detail += " · " + conn.Host + ":" + conn.Port + " / " + conn.Database
							case "SQLite":
								detail += " · " + compactPath(conn.Path)
							case "Cloudflare D1":
								detail += " · " + conn.DatabaseID
							}
							muted(c, detail).MaxLines(1).Tooltip(detail)
						})
						iconButton(c, "test", "play", "测试连接 "+conn.Name, func() { a.databaseTask(conn, "test", "SELECT 1", nil, false, "", 0) }).Disabled(p.busy)
						iconButton(c, "edit", "settings", "编辑连接 "+conn.Name, func() { copy := conn; p.draft = &copy; p.err = ""; p.status = "" }).Disabled(p.busy)
						iconButton(c, "delete", "trash", "删除连接 "+conn.Name, func() { a.deleteDatabaseConnection(conn) }).Disabled(p.busy)
					})
				}
			})
		})
		if p.busy {
			iconButton(c, "cancel", "stop", "取消数据库操作", func() {
				if p.cancel != nil {
					p.cancel()
				}
			})
		}
	})
}
