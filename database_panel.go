package main

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/plugins/sqlite"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

type dbHistory struct{ Connection, SQL string }
type databaseTree struct {
	expanded, loaded bool
	tables           []string
	filter           string
}

type databasePanel struct {
	trees                      map[string]*databaseTree
	structureTable             string
	ddl                        *editor.State
	showDDL                    bool
	open, loaded, busy         bool
	connections                []dbConnection
	history                    []dbHistory
	active                     string
	draft                      *dbConnection
	sql                        *editor.State
	tables                     []string
	filter, table, err, status string
	offset                     int
	result                     dbResult
	list                       ui.ListState
	split                      float32
	cancel                     context.CancelFunc
	done                       chan dbEvent
}
type dbEvent struct {
	createSQL              string
	result                 dbResult
	kind, statement, table string
	offset                 int
	err                    error
}

func (p *databasePanel) tree(id string) *databaseTree {
	if p.trees == nil {
		p.trees = make(map[string]*databaseTree)
	}
	if p.trees[id] == nil {
		p.trees[id] = &databaseTree{}
	}
	return p.trees[id]
}
func (p *databasePanel) connection() (dbConnection, bool) {
	for _, c := range p.connections {
		if c.ID == p.active {
			return c, true
		}
	}
	return dbConnection{}, false
}
func (a *app) openDatabasePanel() {
	a.leavePage()
	p := &a.database
	p.open = true
	a.sidebar = true
	if p.sql == nil {
		p.sql = &editor.State{Buffer: editor.NewBuffer("SELECT 1;"), Label: "SQL 编辑器"}
		p.sql.Buffer.SetLanguage(editor.LanguageFor("query.sql"))
		p.split = 38
	}
	a.loadDatabaseConnections()
}
func (a *app) loadDatabaseConnections() {
	p := &a.database
	if p.loaded {
		return
	}
	p.loaded = true
	if a.projects != nil {
		rows, e := a.projects.db.Query(context.Background(), "SELECT value FROM settings WHERE key='database-tool'")
		if e != nil {
			p.err = e.Error()
			return
		}
		if len(rows.Values) > 0 {
			var data struct {
				Connections []dbConnection
				History     []dbHistory
			}
			if e = json.Unmarshal([]byte(rows.Values[0][0].(string)), &data); e != nil {
				p.err = e.Error()
				return
			}
			p.connections = data.Connections
			p.history = data.History
		}
	}
}
func (a *app) saveDatabasePanel() bool {
	p := &a.database
	if a.projects == nil {
		p.err = "本地配置存储不可用"
		return false
	}
	data, e := json.Marshal(struct {
		Connections []dbConnection
		History     []dbHistory
	}{p.connections, p.history})
	if e == nil {
		_, e = a.projects.db.Execute(context.Background(), "INSERT INTO settings(key,value) VALUES('database-tool',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(data))
	}
	if e != nil {
		p.err = e.Error()
		return false
	}
	return true
}
func (a *app) databaseTask(c dbConnection, kind, statement string, args []any, write bool, table string, offset int) {
	p := &a.database
	if p.busy {
		return
	}
	p.busy = true
	p.err = ""
	p.status = "正在执行…"
	p.done = make(chan dbEvent, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	p.cancel = cancel
	done := p.done
	go func() {
		defer cancel()
		r, e := dbRun(ctx, c, statement, args, write)
		ddl := ""
		if e == nil && kind == "structure" {
			ddl, e = dbCreateSQL(ctx, c, table)
		}
		done <- dbEvent{result: r, kind: kind, statement: statement, table: table, offset: offset, err: e, createSQL: ddl}
	}()
}
func (a *app) tickDatabase(c *ui.Context) {
	p := &a.database
	if !p.busy {
		return
	}
	select {
	case e := <-p.done:
		p.busy = false
		p.cancel = nil
		p.done = nil
		if e.err != nil {
			p.err = e.err.Error()
			p.status = "执行失败"
			return
		}
		p.status = fmt.Sprintf("完成 · %d 行 · 影响 %d 行 · %d ms", len(e.result.Rows), e.result.Affected, e.result.Elapsed.Milliseconds())
		if e.kind == "tables" {
			p.tables = nil
			for _, row := range e.result.Rows {
				if len(row) > 0 {
					p.tables = append(p.tables, dbCell(row[0]))
				}
			}
			tree := p.tree(p.active)
			tree.tables = append([]string(nil), p.tables...)
			tree.loaded = true
			if e.result.Truncated {
				p.status += " · 表列表仅显示前 1000 项"
			}
			return
		}
		if e.kind == "test" {
			p.status = "连接成功"
			return
		}
		p.structureTable = ""
		p.ddl = nil
		p.showDDL = false
		if e.kind == "structure" {
			p.structureTable = e.table
			p.ddl = &editor.State{Buffer: editor.NewBuffer(e.createSQL), ReadOnly: true, Label: "创建 SQL"}
			p.ddl.Buffer.SetLanguage(editor.LanguageFor("schema.sql"))
			e.table = ""
		}
		p.result = e.result
		p.list = ui.ListState{}
		p.table = e.table
		p.offset = e.offset
		if e.kind == "query" {
			p.history = append([]dbHistory{{p.active, e.statement}}, p.history...)
			p.history = p.history[:min(20, len(p.history))]
			a.saveDatabasePanel()
		}
	default:
		c.After(50 * time.Millisecond)
	}
}
func (a *app) selectDatabase(id string) {
	p := &a.database
	if p.busy {
		return
	}
	p.active = id
	p.draft = nil
	tree := p.tree(id)
	tree.expanded = true
	p.tables = tree.tables
	p.table = ""
	p.result = dbResult{}
	p.structureTable = ""
	p.ddl = nil
	p.err = ""
	p.filter = ""
	if conn, ok := p.connection(); ok && !tree.loaded {
		a.databaseTask(conn, "tables", dbTablesSQL(conn), nil, false, "", 0)
	}
}
func (a *app) browseDatabase(table string, offset int, structure bool) {
	p := &a.database
	conn, ok := p.connection()
	if !ok {
		return
	}
	if structure {
		q, args := dbStructureSQL(conn, table)
		a.databaseTask(conn, "structure", q, args, false, table, 0)
		return
	}
	q := fmt.Sprintf("SELECT * FROM %s LIMIT 101 OFFSET %d", dbQuote(table), max(0, offset))
	a.databaseTask(conn, "data", q, nil, false, table, max(0, offset))
}
func (a *app) runDatabaseSQL() {
	a.executeDatabaseSQL(func(conn dbConnection, statement string) (bool, error) {
		answer, err := golet.Dialog.Message(golet.MessageOptions{Parent: a.win, Message: "执行数据库修改？", Detail: conn.Name + " · " + conn.Engine + "\n" + statement, Buttons: []string{"取消", "执行修改"}, DefaultButton: 0, CancelButton: 0})
		return answer.Button == 1, err
	})
}

func (a *app) executeDatabaseSQL(confirm func(dbConnection, string) (bool, error)) {
	p := &a.database
	conn, ok := p.connection()
	if !ok || p.busy {
		return
	}
	statement := p.sql.Buffer.String()
	sel := p.sql.Selection()
	if sel.Anchor != sel.Head {
		statement = p.sql.Buffer.Slice(min(sel.Anchor, sel.Head), max(sel.Anchor, sel.Head))
	}
	_, words, e := dbSQL(statement, conn.Engine)
	if e != nil {
		p.err = e.Error()
		return
	}
	write := !dbReadSQL(words)
	if write {
		approved, err := confirm(conn, statement)
		if err != nil {
			p.err = err.Error()
			return
		}
		if !approved {
			return
		}
	}

	a.databaseTask(conn, "query", statement, nil, write, "", 0)
}
func (a *app) databaseSidebar(c *ui.Context) {
	p := &a.database
	a.sidebarPanel(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "数据库").Bold().Grow(1)
			iconButton(c, "db-add", "plus", "新建数据库连接", func() {
				p.draft = &dbConnection{Engine: "MySQL", Port: "3306", Host: "127.0.0.1", TLS: true}
				p.err = ""
				p.status = ""
			}).Disabled(p.busy)
		})
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for _, conn := range p.connections {
					tree := p.tree(conn.ID)
					ui.Column(c.Key("connection-" + conn.ID)).FillWidth().Gap(4).Children(func() {
						header := ui.ButtonBase(c.Key("toggle")).FillWidth().MinHeight(32).Padding(4, 8).Gap(7).AlignItems(ui.Center).Justify(ui.Start).Radius(6).Label(conn.Name).Expanded(tree.expanded).Disabled(p.busy).Tooltip(conn.Engine + " · " + conn.Name).OnClick(func() {
							if tree.expanded {
								tree.expanded = false
							} else {
								a.selectDatabase(conn.ID)
							}
						})
						if p.active == conn.ID || header.Hovered() {
							header.Background(c.Theme().SurfaceHover)
						}
						header.Children(func() {
							symbol := "chevron-right"
							if tree.expanded {
								symbol = "chevron-down"
							}
							icon(c, symbol, 12)
							icon(c, "database", 16)
							ui.Text(c, conn.Name).Bold()
						})
						if !tree.expanded {
							return
						}
						ui.Column(c.Key("tables")).FillWidth().Padding(0, 0, 8, 20).Gap(4).Children(func() {
							ui.Row(c).FillWidth().Children(func() {
								muted(c, "表与视图").Grow(1)
								iconButton(c, "refresh", "refresh", "刷新数据库表 "+conn.Name, func() {
									a.selectDatabase(conn.ID)
									if !p.busy {
										a.databaseTask(conn, "tables", dbTablesSQL(conn), nil, false, "", 0)
									}
								}).Disabled(p.busy)
							})
							forma.Input(c.Key("filter"), &tree.filter, forma.InputOptions{Placeholder: "搜索表名"}).FillWidth().Label("搜索数据库表 " + conn.Name)
							if !tree.loaded {
								muted(c, "尚未加载表列表")
							} else if len(tree.tables) == 0 {
								muted(c, "暂无表与视图")
							}
							for _, table := range tree.tables {
								if !strings.Contains(strings.ToLower(table), strings.ToLower(tree.filter)) {
									continue
								}
								action(c, "table-"+table, table, func() {
									if p.active != conn.ID {
										a.selectDatabase(conn.ID)
									}
									a.browseDatabase(table, 0, false)
								}).FillWidth().Justify(ui.Start).Disabled(p.busy).Tooltip(table).ContextMenu(func(m *ui.Menu) {
									if m.Item("查看表结构").Chosen() && !p.busy {
										if p.active != conn.ID {
											a.selectDatabase(conn.ID)
										}
										a.browseDatabase(table, 0, true)
									}
									if m.Item("生成 SELECT").Chosen() && !p.busy {
										if p.active != conn.ID {
											a.selectDatabase(conn.ID)
										}
										p.sql.Buffer.Set("SELECT * FROM " + dbQuote(table) + " LIMIT 100;")
									}
								})
							}
						})
					})
				}
				if len(p.connections) == 0 {
					muted(c, "添加 MySQL、SQLite 或 D1 连接").Wrap()
				}
			})
		})
	})
}
func (a *app) databaseView(c *ui.Context) {
	p := &a.database
	ui.Column(c).Fill().MinWidth(0).Padding(16).Gap(10).Children(func() {
		if p.draft != nil {
			a.databaseForm(c)
			return
		}
		conn, ok := p.connection()
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			title := "数据库管理"
			if ok {
				title = conn.Name + " · " + conn.Engine
			}
			ui.Text(c, title).Bold().Grow(1)
			iconButton(c, "db-edit", "settings", "编辑数据库连接", func() { copy := conn; p.draft = &copy; p.status = ""; p.err = "" }).Disabled(!ok || p.busy)
			iconButton(c, "db-remove", "trash", "删除数据库连接", func() { a.deleteDatabaseConnection(conn) }).Disabled(!ok || p.busy)
		})
		if p.err != "" {
			ui.Text(c, p.err).TextColor(c.Theme().Danger).Wrap().MaxLines(4)
		}
		if !ok {
			ui.Text(c, "选择或新建一个数据库连接").FontSize(20)
			muted(c, "浏览表与视图、查看结构、编写 SQL 并导出查询结果。").Wrap()
			return
		}
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			iconButton(c, "db-query", "play", "运行 SQL（选中 SQL 优先） ⌘ Enter", a.runDatabaseSQL).Disabled(p.busy)
			iconButton(c, "db-stop", "stop", "取消数据库查询", func() {
				if p.cancel != nil {
					p.cancel()
				}
			}).Disabled(!p.busy)
			iconButton(c, "db-history", "history", "SQL 历史", nil).Menu(func(m *ui.Menu) {
				n := 0
				for _, h := range p.history {
					if h.Connection == p.active {
						n++
						label := strings.ReplaceAll(h.SQL, "\n", " ")
						if len([]rune(label)) > 65 {
							label = string([]rune(label)[:65]) + "…"
						}
						if m.Item(fmt.Sprintf("%d. %s", n, label)).Chosen() {
							p.sql.Buffer.Set(h.SQL)
						}
					}
				}
				if n == 0 {
					m.Item("暂无查询记录").Disabled(true)
				}
			})
			ui.Spacer(c)
			muted(c, "查询最多展示 1000 行").FontSize(11)
		})
		ui.SplitVerticalPercent(c, &p.split, func() {
			p.sql.ReadOnly = p.busy
			p.sql.Theme = a.codeTheme
			editor.View(c.Key("database-sql"), p.sql).Fill()
		}, func() { a.databaseResults(c) }).Grow(1).FillWidth().MinWidth(0).MinHeight(0)
		if p.status != "" {
			muted(c, p.status).MaxLines(2)
		}
	})
	if c.Shortcut(ui.Cmd, ui.KeyEnter) && !p.busy && p.draft == nil {
		a.runDatabaseSQL()
	}
}
func (a *app) databaseForm(c *ui.Context) {
	p := &a.database
	d := p.draft
	ui.Row(c).FillWidth().Children(func() {
		ui.Text(c, "数据库连接").Bold().Grow(1)
		iconButton(c, "db-form-close", "close", "关闭连接表单", func() { p.draft = nil }).Disabled(p.busy)
	})
	ui.Scroll(c).FillWidth().Grow(1).Children(func() {
		ui.Column(c).Width(520).MaxWidthPercent(100).Gap(12).Children(func() {
			field := func(label string, value *string, secret bool) {
				forma.Field(c, label, func() {
					e := forma.Input(c.Key(label), value, forma.InputOptions{Disabled: p.busy}).FillWidth().Label(label)
					if secret {
						e.Password()
					}
				}).FillWidth()
			}
			field("连接名称", &d.Name, false)
			forma.Field(c, "类型", func() {
				forma.Select(c, &d.Engine, []string{"MySQL", "SQLite", "Cloudflare D1"}).FillWidth().Disabled(p.busy)
			})
			switch d.Engine {
			case "MySQL":
				field("主机", &d.Host, false)
				field("端口", &d.Port, false)
				field("用户名", &d.User, false)
				field("密码", &d.Password, true)
				field("数据库名称", &d.Database, false)
				forma.Checkbox(c, &d.TLS, "使用 TLS（校验证书）").Disabled(p.busy)
			case "SQLite":
				field("数据库文件", &d.Path, false)
				ui.Row(c).Gap(8).Children(func() {
					action(c, "db-pick", "选择文件", func() {
						result, e := golet.Dialog.Open(golet.OpenDialogOptions{Parent: a.win, Title: "选择 SQLite 数据库"})
						if e != nil {
							p.err = e.Error()
						} else if len(result) > 0 {
							d.Path = result[0]
						}
					}).Disabled(p.busy)
					action(c, "db-create", "新建 SQLite 文件", func() {
						path, e := golet.Dialog.Save(golet.SaveDialogOptions{Parent: a.win, Title: "新建 SQLite 数据库", DefaultPath: "database.sqlite"})
						if e != nil {
							p.err = e.Error()
							return
						}
						if path == "" {
							return
						}
						db, e := sqlite.Open(context.Background(), path, sqlite.OpenOptions{})
						if e != nil {
							p.err = e.Error()
							return
						}
						db.Close()
						d.Path = path
					}).Disabled(p.busy)
				})
			case "Cloudflare D1":
				field("Account ID", &d.AccountID, false)
				field("Database ID", &d.DatabaseID, false)
				field("API Token", &d.Token, true)
				muted(c, "使用具有目标账户 D1 Read / Edit 权限的 API Token。").Wrap()
			}
			muted(c, "连接信息和凭据保存在本机 Omnicode 配置数据库中。").Wrap()
			if p.err != "" {
				ui.Text(c, p.err).TextColor(c.Theme().Danger).Wrap()
			}
			if p.status != "" {
				muted(c, p.status).Wrap()
			}
			ui.Row(c).Gap(8).Children(func() {
				if p.busy {
					iconButton(c, "db-test-cancel", "stop", "取消连接测试", func() {
						if p.cancel != nil {
							p.cancel()
						}
					})
				}
				forma.Button(c, "测试连接").Disabled(p.busy).OnClick(func() { a.databaseTask(*d, "test", "SELECT 1", nil, false, "", 0) })
				forma.Button(c, "保存连接").Disabled(p.busy).OnClick(func() {
					if e := validateDB(*d); e != nil {
						p.err = e.Error()
						return
					}
					item := *d
					if item.ID == "" {
						var id [16]byte
						if _, e := rand.Read(id[:]); e != nil {
							p.err = e.Error()
							return
						}
						item.ID = hex.EncodeToString(id[:])
					}
					old := p.connections
					p.connections = append([]dbConnection(nil), old...)
					found := false
					for i, c := range p.connections {
						if c.ID == item.ID {
							p.connections[i] = item
							found = true
						}
					}
					if !found {
						p.connections = append(p.connections, item)
					}
					if !a.saveDatabasePanel() {
						p.connections = old
						return
					}
					p.draft = nil
					delete(p.trees, item.ID)
					if a.settingsOpen {
						if p.active == item.ID {
							p.tables = nil
							p.table = ""
							p.result = dbResult{}
							p.structureTable = ""
							p.ddl = nil
						}
						p.status = "已保存 · " + item.Name
						p.err = ""
					} else {
						a.selectDatabase(item.ID)
					}
				})
			})
		})
	})
}
func (a *app) databaseResults(c *ui.Context) {
	p := &a.database
	r := p.result
	count := len(r.Rows)
	more := false
	if p.table != "" && count > 100 {
		count = 100
		more = true
	}
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			title := "结果"
			if p.table != "" {
				title = p.table
			}
			if p.structureTable != "" {
				title = p.structureTable + " · 表结构"
			}
			ui.Text(c, title).Bold().Grow(1)
			if p.structureTable != "" {
				for _, tab := range []struct {
					name string
					ddl  bool
				}{{"字段", false}, {"创建 SQL", true}} {
					button := action(c, "structure-"+tab.name, tab.name, func() { p.showDDL = tab.ddl })
					if p.showDDL == tab.ddl {
						button.Background(c.Theme().SurfaceHover)
					}
				}
				iconButton(c, "copy-ddl", "copy", "复制创建 SQL", func() { c.WriteClipboard(p.ddl.Buffer.String()) })
			}
			if p.table != "" {
				action(c, "db-structure", "表结构", func() { a.browseDatabase(p.table, 0, true) }).Disabled(p.busy)
				iconButton(c, "db-prev", "chevron-left", "上一页数据", func() { a.browseDatabase(p.table, p.offset-100, false) }).Disabled(p.busy || p.offset == 0)
				muted(c, fmt.Sprintf("第 %d 页", p.offset/100+1))
				iconButton(c, "db-next", "chevron-right", "下一页数据", func() { a.browseDatabase(p.table, p.offset+100, false) }).Disabled(p.busy || !more)
			}
			iconButton(c, "db-export", "arrow-down", "导出当前结果 CSV", func() { a.exportDatabaseResult(count) }).Disabled(len(r.Columns) == 0 || p.busy)
		})
		if p.structureTable != "" && p.showDDL && p.ddl != nil {
			p.ddl.Theme = a.codeTheme
			editor.View(c.Key("database-ddl"), p.ddl).Grow(1).FillWidth().MinWidth(0).MinHeight(0)
			return
		}
		if r.Truncated {
			muted(c, "结果已截断为前 1000 行，请增加 LIMIT 或筛选条件。").Wrap()
		}
		if len(r.Columns) == 0 {
			muted(c, "运行 SQL 或选择左侧表查看数据。")
			return
		}
		columns := make([]ui.TableColumn, len(r.Columns))
		for i, name := range r.Columns {
			columns[i] = ui.TableColumn{ID: fmt.Sprint(i), Title: name, Width: 160, MinWidth: 80}
		}
		ui.Table(c.Key("db-result"), &p.list, columns, count, func(row, col int) {
			value := ""
			if col < len(r.Rows[row]) {
				value = dbCell(r.Rows[row][col])
			}
			display := value
			if len([]rune(display)) > 500 {
				display = string([]rune(display)[:500]) + "…"
			}
			ui.Text(c, display).FontSize(12).MaxLines(1).Tooltip(display).ContextMenu(func(m *ui.Menu) {
				if m.Item("复制单元格").Chosen() {
					c.WriteClipboard(value)
				}
			})
		}).Grow(1).FillWidth().MinWidth(0).MinHeight(0).Scrollbars(true).KeepScrollbars(true).Label("数据库查询结果")
	})
}
func (a *app) exportDatabaseResult(count int) {
	path, e := golet.Dialog.Save(golet.SaveDialogOptions{Parent: a.win, Title: "导出当前结果", DefaultPath: "query-result.csv"})
	if e != nil {
		a.database.err = e.Error()
		return
	}
	if path == "" {
		return
	}
	f, e := os.Create(path)
	if e != nil {
		a.database.err = e.Error()
		return
	}
	writer := csv.NewWriter(f)
	r := a.database.result
	e = writer.Write(r.Columns)
	for _, row := range r.Rows[:count] {
		if e != nil {
			break
		}
		values := make([]string, len(row))
		for i, v := range row {
			if v != nil {
				values[i] = dbCell(v)
			}
		}
		e = writer.Write(values)
	}
	writer.Flush()
	if e == nil {
		e = writer.Error()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		a.database.err = e.Error()
	} else {
		a.database.status = "已导出 · " + path
	}
}
