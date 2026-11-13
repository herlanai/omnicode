package main

import (
	"fmt"
	"go/format"
	"path/filepath"
	"strings"
	"time"

	golet "github.com/HyperparameterSI/golet/backend"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

type app struct {
	database                   databasePanel
	recentFiles                []string
	recentFilesCollapsed       bool
	explorerRefresh            explorerRefresh
	trackWorkspace             bool
	lastWorkspace              workspaceSession
	backupWindow               *golet.Window
	backups                    backupBrowser
	fileFilter                 explorerFilter
	schedules                  schedulePanel
	vimRegister                vimRegister
	gitReview                  *gitReviewUI
	git                        gitPanel
	intelligence               intelligence
	contentSearch              contentSearch
	webPreviews                map[string]*webPreview
	ai                         aiPanel
	treeSelection              string
	prefs                      preferences
	settingsOpen               bool
	settingsTab, settingsError string
	modelEdit                  int
	systemDark                 bool
	tray                       *golet.Tray
	projects                   *projectStore
	recent                     []recentProject
	terminal                   terminalPanel
	filePrompt                 filePrompt
	fileClipboard              fileClipboard
	focusEditor                bool
	codeTheme                  *editor.Theme
	win                        *golet.Window
	root                       string
	folders                    []*workspaceFolder
	expanded                   map[string]bool
	docs                       []*document
	active                     int
	sidebar                    bool
	sidebarWidth               float32
	referenceEditorPercent     float32
	gitChangesWidth            float32
	split                      float32
	theme                      *ui.Theme
	quick                      bool
	query                      string
	index                      []string
	notice                     string
	serial                     int
	wrap                       bool
}

func newApp(root string) *app {
	a := &app{expanded: map[string]bool{}, active: -1, sidebar: true, sidebarWidth: 246, referenceEditorPercent: 76, gitChangesWidth: 270, split: 52, prefs: defaultPreferences()}
	a.newAITab()
	a.applyAppearance()
	if root != "" {
		a.setRoot(root)
	}
	return a
}
func (a *app) current() *document {
	if a.active < 0 || a.active >= len(a.docs) {
		return nil
	}
	return a.docs[a.active]
}
func (a *app) report(err error) {
	if err != nil {
		a.notice = err.Error()
	}
}
func (a *app) setRoot(path string) {
	folder, err := readWorkspaceFolder(path)
	if err != nil {
		a.report(err)
		return
	}
	a.replaceFolder(folder)
}
func (a *app) activateFolder(folder *workspaceFolder) bool {
	abs := folder.path
	if abs == a.root {
		return true
	}
	if a.git.mutating {
		a.notice = "请等待 Git 操作完成"
		return false
	}
	if a.busyAITab() != nil {
		a.notice = "请先结束 AI 任务并处理待批准修改"
		return false
	}

	for _, tab := range a.ai.tabs {
		if !a.saveAISession(tab, true) {
			a.ai.aiSession = tab
			return false
		}
	}
	a.fileFilter.close()
	a.schedules = schedulePanel{open: a.schedules.open}
	a.ai.tabs = nil
	a.ai.serial = 0
	a.newAITab()
	a.closeIntelligence()
	a.root = abs
	a.treeSelection = abs
	a.index = nil
	if a.root != "" {
		a.rememberProject()
	}
	if a.root != "" && len(a.ai.tabs) == 1 && a.ai.chatID == "" && a.ai.draft == "" {
		a.restoreLatestAI()
	}
	return true
}
func (a *app) refresh() {
	a.explorerRefresh.generation++
	a.fileFilter.resetSearch()
	for _, folder := range a.folders {
		list, err := children(folder.path)
		a.report(err)
		if err == nil {
			folder.entries = list
		}
	}
	a.index = nil
}
func (a *app) open(path string) {
	d, err := loadDocument(path)
	if err != nil {
		a.report(err)
		return
	}
	if !a.activateForPath(d.path) {
		return
	}
	a.git.visible = false
	a.focusEditor = true
	for i, existing := range a.docs {
		if existing.path == d.path {
			a.active = i
			a.quick = false
			return
		}
	}
	a.docs = append(a.docs, d)
	a.active = len(a.docs) - 1
	a.quick = false
	a.notice = ""
}
func (a *app) newFile() {
	a.focusEditor = true
	a.serial++
	a.docs = append(a.docs, newDocument(fmt.Sprintf("Untitled-%d", a.serial), ""))
	a.active = len(a.docs) - 1
}
func (a *app) pick(directory bool) {
	paths, err := golet.Dialog.Open(golet.OpenDialogOptions{Parent: a.win, Title: map[bool]string{true: "打开文件夹", false: "打开文件"}[directory], Directory: directory, Multiple: !directory})
	a.report(err)
	for _, p := range paths {
		if directory {
			a.chooseFolder(p)
		} else {
			a.open(p)
		}
	}
}
func (a *app) save(d *document, as bool) bool {
	if d == nil || !d.editable() {
		return true
	}
	path := d.path
	if path == "" || as {
		var err error
		path, err = golet.Dialog.Save(golet.SaveDialogOptions{Parent: a.win, Title: "保存文件", DefaultPath: filepath.Join(a.root, d.name), CreateDirectories: true})
		a.report(err)
		if err != nil || path == "" {
			return false
		}
	}
	for _, other := range a.docs {
		if other != d && other.path == path {
			a.notice = "目标文件已在另一个标签页打开"
			return false
		}
	}
	if err := d.save(path); err != nil {
		a.report(err)
		return false
	}
	a.rememberEditedFile(d.path)
	a.notice = "已保存 · " + d.name
	a.refresh()
	return true
}
func (a *app) canClose(d *document) bool {
	if !d.state.Buffer.Modified() {
		return true
	}
	if d.path != "" && a.writeAuto(d) {
		return true
	}
	r, err := golet.Dialog.Message(golet.MessageOptions{Parent: a.win, Type: golet.MessageWarning, Message: "保存对 " + d.name + " 的修改？", Detail: "关闭后，未保存的修改将丢失。", Buttons: []string{"保存", "取消", "不保存"}, DefaultButton: 0, CancelButton: 1, DestructiveButtons: []int{2}})
	if err != nil {
		a.report(err)
		return false
	}
	if r.Button == 0 {
		return a.save(d, false)
	}
	return r.Button == 2
}
func (a *app) closeDoc(i int) {
	if i < 0 || i >= len(a.docs) || !a.canClose(a.docs[i]) {
		return
	}
	a.removeDocument(i)
}
func (a *app) closeAllDocuments() {
	for _, d := range a.docs {
		if !a.canClose(d) {
			return
		}
	}
	for len(a.docs) > 0 {
		a.removeDocument(len(a.docs) - 1)
	}
	a.checkpointWorkspace()
}

func (a *app) removeDocument(i int) {
	a.releaseDocument(a.docs[i])
	a.docs = append(a.docs[:i], a.docs[i+1:]...)
	if i < a.active {
		a.active--
	}
	if a.active >= len(a.docs) {
		a.active = len(a.docs) - 1
	}
}
func (a *app) releaseDocument(d *document) {
	if h := d.http; h != nil && h.cancel != nil {
		h.cancel()
	}
	if v := d.slides; v != nil {
		if v.pane != nil {
			v.pane.close()
			v.pane = nil
		}
		if v.window != nil {
			v.window.Destroy()
		}
	}
}
func (a *app) canQuit() bool {
	if a.git.mutating {
		a.notice = "请先等待或停止 Git 操作"
		return false
	}
	if tab := a.busyAITab(); tab != nil {
		a.database.open = false
		a.settingsOpen = false
		a.schedules.open = false
		a.ai.aiSession = tab
		a.ai.visible = true
		if tab.running {
			tab.err = "请先停止 AI 任务，再关闭窗口"
		} else {
			tab.err = "请先批准或拒绝 AI 修改，再关闭窗口"
		}
		return false
	}
	for _, d := range a.docs {
		if !a.canClose(d) {
			return false
		}
	}
	return true
}
func (a *app) toggleQuick() {
	a.quick = a.settingsOpen || a.schedules.open || a.database.open || !a.quick
	a.leavePage()
	a.query = ""
	if a.contentSearch.cancel != nil {
		a.contentSearch.cancel()
	}
	a.contentSearch = contentSearch{}
	if a.quick && a.index == nil && a.root != "" {
		a.index = fileIndex(a.root)
	}
}
func (a *app) format() { a.formatDocument(a.current()) }

func (a *app) formatDocument(d *document) {
	if a.git.mutating {
		a.notice = "请等待 Git 操作完成"
		return
	}
	if d != nil && !d.state.ReadOnly {
		if filepath.Ext(d.name) != ".go" {
			a.notice = "格式化当前支持 Go 文件"
			return
		}
		text, err := format.Source([]byte(d.state.Buffer.String()))
		if err != nil {
			a.report(err)
			return
		}
		if string(text) != d.state.Buffer.String() {
			selection := d.state.Selection()
			d.state.Select(0, d.state.Buffer.Len())
			d.state.Insert(string(text))
			d.state.Select(min(selection.Anchor, len(text)), min(selection.Head, len(text)))
		}
		a.notice = "已格式化 · " + d.name
	}
}

func action(c *ui.Context, key, label string, fn func()) ui.Element {
	return forma.Button(c.Key(key), label, forma.ButtonOptions{Variant: forma.Ghost, Size: forma.Small}).Radius(6).OnClick(fn)
}
func muted(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(12).TextColor(c.Theme().TextMuted)
}
func (a *app) view(c *ui.Context) {
	defer a.checkpointWorkspace()
	a.tickExplorer(c)
	a.tickGit(c)
	a.updateIntelligence(c)
	a.drainAI()
	a.tickDatabase(c)
	a.tickSchedules(c)
	for _, tab := range a.ai.tabs {
		a.saveAISession(tab, false)
		if tab.dirty {
			c.After(500 * time.Millisecond)
		}
		if tab.running {
			c.After(50 * time.Millisecond)
		}
	}
	for _, d := range a.docs {
		if d.slides != nil && d.slides.pane != nil && (d != a.current() || d.slides.textMode || a.database.open || a.schedules.open || a.settingsOpen || a.quick || a.filePrompt.open) {
			d.slides.pane.hide()
		}
	}
	c.SetTheme(a.theme)
	a.autosave(c)
	if c.Shortcut(ui.Cmd, ui.KeyB) {
		a.sidebar = !a.sidebar
	}
	if a.settingsOpen || a.schedules.open || a.database.open {
		if c.Shortcut(ui.Cmd, ui.KeyW) {
			a.leavePage()
		} else if c.Shortcut(0, ui.KeyEscape) {
			if a.schedules.open && a.schedules.edit != nil {
				a.schedules.edit = nil
			} else {
				a.leavePage()
			}
		}
	} else {
		a.workspaceShortcuts(c)
	}
	ui.Column(c).Fill().Background(a.theme.Background).Children(func() {
		a.titlebar(c)
		ui.Row(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			a.rail(c)
			side, body := a.files, a.aiWorkspace
			width, _ := c.Size()
			showSidebar := a.sidebar
			switch {
			case a.database.open:
				side, body = a.databaseSidebar, a.databaseView
			case a.settingsOpen:
				side, body = a.settingsSidebar, a.settingsView
			case a.schedules.open:
				side, body = a.scheduleList, a.scheduleView
			default:
				showSidebar = a.sidebar && (!a.ai.visible || width >= 1100)
			}
			if showSidebar {
				a.workspaceSplit(c, &a.sidebarWidth, func() { side(c) }, func() { body(c) }).Grow(1).Shrink(1).MinWidth(0).FillHeight()
			} else {
				body(c)
			}
		})
		if !a.settingsOpen && !a.schedules.open && !a.database.open {
			a.status(c)
		}
	})
	if a.filePrompt.open {
		a.fileDialog(c)
	}
	a.gitDialog(c)
	a.terminalNameDialog(c)
}
func (a *app) leavePage() { a.settingsOpen = false; a.schedules.open = false; a.database.open = false }
func (a *app) workspaceShortcuts(c *ui.Context) {
	if c.Shortcut(ui.Ctrl, ui.KeyBackquote) {
		a.toggleTerminal()
	}
	if c.Shortcut(ui.Cmd, ui.KeyP) {
		a.toggleQuick()
	}
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		a.save(a.current(), false)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS) {
		a.save(a.current(), true)
	}
	if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.newFile()
	}
	if c.Shortcut(ui.Cmd, ui.KeyO) {
		a.pick(false)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyO) {
		a.pick(true)
	}
	if c.Shortcut(ui.Cmd, ui.KeyW) {
		a.closeDoc(a.active)
	}
	if c.Shortcut(ui.Alt|ui.Shift, ui.KeyF) {
		a.format()
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyV) {
		if d := a.current(); d != nil && d.markdown() {
			d.preview = !d.preview
			d.md.reading = false
		}
	}
	if a.quick && c.Shortcut(0, ui.KeyEscape) {
		a.quick = false
	}
}
func (a *app) workspaceSplit(c *ui.Context, width *float32, left, right func()) ui.Element {
	theme := *a.theme
	theme.Border = ui.Transparent
	theme.Accent = ui.Transparent
	if a.git.visible && !a.settingsOpen && !a.schedules.open && !a.database.open {
		theme.Border = a.theme.Sidebar
		theme.Accent = a.theme.TextMuted
	}
	c.SetTheme(&theme)
	divider := ui.Split(c, width, func() { c.SetTheme(a.theme); left() }, func() { c.SetTheme(a.theme); right() })
	c.SetTheme(a.theme)
	return divider
}

func (a *app) rail(c *ui.Context) {
	ui.Column(c).Width(42).FillHeight().Padding(8, 4).Gap(7).Background(a.theme.Surface).AlignItems(ui.Center).Children(func() {
		w, _ := c.Size()
		page := a.settingsOpen || a.schedules.open || a.database.open
		explorer := !page && a.sidebar && (!a.ai.visible || w >= 1100) && !a.quick && !a.git.visible
		toolButton(c, "explorer", "files", "文件浏览器", explorer, func() {
			a.leavePage()
			a.sidebar = true
			a.quick = false
			a.git.visible = false
			if w < 1100 {
				a.ai.visible = false
			}
		})
		toolButton(c, "search", "search", "快速打开", !page && a.quick, a.toggleQuick)
		toolButton(c, "source-control", "git", "源代码管理", !page && a.git.visible && !a.quick, func() {
			if page {
				a.leavePage()
				a.git.visible = false
				if w < 1100 {
					a.ai.visible = false
				}
			}
			a.toggleGit()
		})
		toolButton(c, "schedules", "clock", "定时任务", a.schedules.open, func() {
			a.settingsOpen = false
			a.database.open = false
			a.schedules.open = !a.schedules.open
			if a.schedules.open {
				a.sidebar = true
			}
			a.schedules.poll = time.Time{}
		})
		ui.Box(c).Grow(1)
		toolButton(c, "database-tool", "database", "数据库管理", a.database.open, a.openDatabasePanel)
		iconButton(c, "http-client", "remote", "HTTP 请求", func() { a.leavePage(); a.openHTTP() })
		toolButton(c, "settings", "settings", "设置  ⌘ ,", a.settingsOpen, func() {
			if a.settingsOpen {
				a.leavePage()
			} else {
				a.showSettings()
			}
		})
	})
}
func (a *app) files(c *ui.Context) {
	if a.git.visible {
		a.gitSidebar(c)
		return
	}
	a.sidebarPanel(c, func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "文件").Bold().Grow(1)
			toolButton(c, "filter-files", "search", "按文件名筛选", a.fileFilter.open, func() {
				if a.fileFilter.open {
					a.fileFilter.close()
				} else {
					a.fileFilter.open = true
					a.fileFilter.focus = true
				}
			}).Disabled(a.root == "")
			iconButton(c, "refresh", "refresh", "刷新文件列表", a.refresh).Disabled(a.root == "")
			iconButton(c, "sidebar-create", "plus", "新建文件或文件夹", nil).Disabled(a.root == "").Menu(func(m *ui.Menu) {
				if m.Item("新建文件…").Chosen() {
					a.askFile("新建文件", a.creationDirectory())
				}
				if m.Item("新建文件夹…").Chosen() {
					a.askFile("新建文件夹", a.creationDirectory())
				}
			})
			iconButton(c, "sidebar-open", "folder", "选择工作区", func() { a.pick(true) })
		})
		if a.root == "" {
			action(c, "choose-folder", "打开文件夹", func() { a.pick(true) })
			return
		}
		if a.fileFilter.open && a.explorerFilterView(c) {
			return
		}
		ui.Scroll(c.Key("file-tree")).Grow(1).FillWidth().ContextMenu(a.fileMenu(c, a.root, true, true)).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for _, folder := range a.folders {
					a.folderTree(c, folder)
				}
				a.sidebarRecentFiles(c)
				a.sidebarAIHistory(c)
			})
		})
	})
}
func (a *app) sidebarPanel(c *ui.Context, content func()) {
	ui.Box(c).Fill().Padding(0, 0, 0, 6).Background(a.theme.Surface).Children(func() {
		ui.Column(c).Fill().Radius(12, 0, 0, 12).Background(a.theme.Sidebar).Padding(12, 10).Gap(12).Children(content)
	})
}
func (a *app) tree(c *ui.Context, folder *workspaceFolder, entries []*entry, depth int) {
	for _, e := range entries {
		node := e
		row := ui.Row(c.Key(node.path)).FillWidth().Height(30).Padding(0, 8, 0, float32(8+depth*14)).Gap(7).AlignItems(ui.Center).Radius(6).Background(ui.Transparent).Cursor(ui.CursorPointer).Label(node.name).OnClick(func() {
			if !a.activateFolder(folder) {
				return
			}
			a.treeSelection = node.path
			if node.dir {
				a.expanded[node.path] = !a.expanded[node.path]
			} else {
				a.open(node.path)
			}
		})
		if a.treeSelection == node.path {
			row.Background(a.theme.Sidebar.Mix(a.theme.Accent, 0.14))
		} else if row.Hovered() {
			row.Background(a.theme.SurfaceHover)
		}
		row.Transition(chromeTransition)
		row.ContextMenu(a.fileMenu(c, node.path, node.dir, false))
		row.Children(func() {
			if node.dir {
				chev := "chevron-right"
				if a.expanded[node.path] {
					chev = "chevron-down"
				}
				icon(c, chev, 10)
				icon(c, "folder", 14)
			} else {
				ui.Box(c).Width(10)
				icon(c, "file", 14)
			}
			ui.Text(c, node.name).FontSize(a.theme.FontSize).MaxLines(1).MinWidth(0).Tooltip(node.name)
		})
		if node.dir && a.expanded[node.path] {
			if !node.loaded {
				list, err := children(node.path)
				a.report(err)
				node.children = list
				node.loaded = true
			}
			a.tree(c, folder, node.children, depth+1)
		}
	}
}
func (a *app) content(c *ui.Context) {
	if a.git.visible && !a.quick {
		a.gitContent(c)
		return
	}
	ui.Column(c).FillHeight().Grow(1).MinWidth(0).Children(func() {
		if a.quick {
			a.quickView(c)
			return
		}
		if len(a.docs) == 0 {
			a.welcome(c)
			return
		}
		ui.ScrollHorizontal(c.Key("tabs")).Height(42).Shrink(0).FillWidth().Background(a.theme.Sidebar).Children(func() {
			ui.Row(c).Height(42).Padding(0, 8).Gap(4).AlignItems(ui.Center).Children(func() {
				for i, d := range a.docs {
					idx, doc := i, d
					bg := ui.Transparent
					if i == a.active {
						bg = a.theme.Background
						if a.theme.Dark {
							bg = a.theme.Surface
						}
					}
					chip := ui.Row(c.Key(fmt.Sprintf("tab-%p", doc))).Height(30).Padding(0, 4).Radius(8).AlignItems(ui.Center).Background(bg)
					if i == a.active {
						chip.BorderWidth(0, 0, 2, 0).BorderColor(a.theme.Accent)
					}
					chip.ContextMenu(func(m *ui.Menu) {
						if m.Item("关闭全部").Chosen() {
							a.closeAllDocuments()
						}
					}).Transition(chromeTransition).Children(func() {
						title := doc.name
						if doc.state.Buffer.Modified() {
							title += " •"
						}
						action(c, "activate", title, func() {
							if a.activateForPath(doc.path) {
								a.active = idx
								a.focusEditor = true
							}
						}).MinWidth(68).Height(28).MinHeight(0)
						iconButton(c, "close", "close", "关闭 "+doc.name, func() { a.closeDoc(idx) }).Size(24, 24).Padding(5)
					})
				}
			})
		})
		d := a.current()
		if d == nil {
			return
		}
		if d.path != "" || d.markdown() {
			ui.Row(c).Height(30).Shrink(0).Padding(0, 16).Gap(10).AlignItems(ui.Center).Children(func() {
				label := d.description()
				if a.root != "" && d.path != "" {
					if rel, e := filepath.Rel(a.root, d.path); e == nil {
						label = rel
					}
				}
				muted(c, label).Grow(1)
				if d.path != "" {
					iconButton(c, "ai-file-backups", "history", "AI 修改前的版本", func() { a.showAIBackups(d.path, false) })
				}

			})
		}

		if d.editable() && isHTTPDocument(d.name) {
			a.httpContent(c, d)
			return
		}
		documentPane := func() {
			switch d.kind {
			case spreadsheetFile:
				a.spreadsheetContent(c, d)
			case presentationFile:
				a.slidesContent(c, d)
			case imageFile, pdfFile:
				a.mediaContent(c, d)
			case markdownFile:
				a.markdownContent(c, d)
			default:
				ui.Box(c).Grow(1).FillWidth().MinHeight(0).Children(func() { a.textContent(c, d) })
			}
		}
		if refs := a.intelligence.references; refs != nil && !a.sharedBottomPanel() {
			ui.SplitVerticalPercent(c.Key("references-split"), &a.referenceEditorPercent, documentPane, func() { a.referencePanel(c, refs) }).Grow(1).FillWidth().MinHeight(0)
		} else {
			documentPane()
		}
	})
}
func (a *app) welcome(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(16).Children(func() {
		title := "Omnicode"
		if a.root != "" {
			title = filepath.Base(a.root)
		}
		ui.Row(c).Width(320).MaxWidth(320).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(38, 38).Radius(9).Background(a.theme.Sidebar).Center().Children(func() { icon(c, "code", 21) })
			ui.Text(c, title).FontSize(21).Bold().MinWidth(0).MaxLines(1)
		})
		ui.Column(c).Width(320).Gap(2).Children(func() {
			for _, item := range []struct {
				name, label, shortcut string
				run                   func()
			}{
				{"folder", "打开文件夹", "⇧ ⌘ O", func() { a.pick(true) }},
				{"file", "打开文件", "⌘ O", func() { a.pick(false) }},
				{"plus", "新建文件", "⌘ N", a.newFile},
			} {
				button := ui.Button(c.Key(item.name), "").FillWidth().Height(34).MinHeight(0).Padding(0, 10).Radius(6).Border(0, ui.Transparent).Background(ui.Transparent).Label(item.label).OnClick(item.run)
				if button.Hovered() {
					button.Background(a.theme.SurfaceHover)
				}
				button.Transition(chromeTransition).Children(func() {
					ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
						icon(c, item.name, 15)
						ui.Text(c, item.label).FontSize(13).Grow(1)
						muted(c, item.shortcut)
					})
				})
			}
		})
		if len(a.recent) > 0 {
			a.recentView(c)
		}
	})
}
func (a *app) quickView(c *ui.Context) {
	ui.Column(c).Fill().Padding(28).Gap(16).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "快速打开").FontSize(20).Bold().Grow(1)
			iconButton(c, "dismiss-quick", "close", "关闭  Esc", func() { a.quick = false })
		})
		input := forma.Input(c.Key("query"), &a.query, forma.InputOptions{Placeholder: "搜索文件名或文件内容…"}).FillWidth().AutoFocus()
		a.updateContentSearch(c)
		var matches []string
		for _, p := range a.index {
			if strings.Contains(strings.ToLower(p), strings.ToLower(a.query)) {
				matches = append(matches, p)
				if len(matches) == 100 {
					break
				}
			}
		}
		if input.Submitted() {
			if len(matches) > 0 {
				a.open(filepath.Join(a.root, matches[0]))
			} else if len(a.contentSearch.hits) > 0 {
				a.openContentHit(a.contentSearch.hits[0])
			}
		}
		if a.root == "" {
			action(c, "quick-folder", "打开文件夹", func() { a.pick(true); a.index = fileIndex(a.root) })
		} else if len(matches) == 0 && len(a.contentSearch.hits) == 0 && !a.contentSearch.busy {
			muted(c, "没有匹配的文件或内容")
		}
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				if len(matches) > 0 && a.query != "" {
					muted(c, "文件").Margin(0, 0, 6, 0)
				}
				for _, p := range matches {
					path := p
					action(c, path, path, func() { a.open(filepath.Join(a.root, path)) }).FillWidth().Justify(ui.Start)
				}

				if a.query != "" {
					label := "文件内容"
					if a.contentSearch.busy {
						label += " · 搜索中…"
					}
					muted(c, label).Margin(12, 0, 6, 0)
					for i, hit := range a.contentSearch.hits {
						hit := hit
						ui.Button(c.Key(fmt.Sprintf("content-hit-%d", i)), "").FillWidth().Padding(10).Radius(6).Border(0, ui.Transparent).Background(ui.Transparent).Label(contentHitLabel(hit)).OnClick(func() { a.openContentHit(hit) }).Children(func() {
							ui.Column(c).FillWidth().Gap(4).Children(func() { ui.Text(c, contentHitLabel(hit)).FontSize(12); muted(c, hit.Snippet).MaxLines(2).Wrap() })
						})
					}
					if a.contentSearch.limited {
						muted(c, "已显示部分结果，请缩小搜索范围")
					}
				}
			})
		})

	})
}
func (a *app) status(c *ui.Context) {
	if len(a.docs) == 0 && a.notice == "" {
		return
	}
	ui.Row(c).Height(28).Shrink(0).Padding(0, 14).Gap(14).AlignItems(ui.Center).Background(a.theme.Sidebar).Children(func() {
		text := ""
		if d := a.current(); d != nil {
			if !d.editable() {
				text = "只读预览"
				if d.sheet != nil && !d.sheet.readOnly {
					text = "表格 · Enter 应用单元格修改"
				}
			} else if d.path == "" {
				text = "尚未保存"
			} else if d.state.Buffer.Modified() {
				text = "等待自动保存…"
			} else {
				text = "已自动保存"
			}
		}
		if a.notice != "" {
			text = a.notice
		}
		for _, d := range a.docs {
			if d.saveError != "" {
				text = d.saveError
				break
			}
		}
		muted(c, text).Grow(1)
		if a.notice != "" {
			action(c, "dismiss-notice", "×", func() { a.notice = "" }).Size(20, 20)
		}
		if d := a.current(); d != nil && d.editable() {
			if !a.quick && !a.git.visible {
				label, tip := "Vim 关", "开启 Vim 模式"
				if a.prefs.VimMode {
					label, tip = d.vim.label(), "关闭 Vim 模式"
				}
				action(c, "vim-toggle", label, func() {
					a.prefs.VimMode = !a.prefs.VimMode
					a.savePreferences()
					if a.settingsError != "" {
						a.notice = a.settingsError
					}
					a.focusEditor = true
				}).Height(24).MinHeight(0).Padding(0, 6).Tooltip(tip)
			}
			pos := d.state.CursorPos()
			muted(c, fmt.Sprintf("行 %d，列 %d", pos.Line+1, pos.Col+1))
			muted(c, "UTF-8")
			ending := "LF"
			if d.state.Buffer.LineEnding() == "\r\n" {
				ending = "CRLF"
			}
			muted(c, ending)
			language := "Plain Text"
			if l := d.state.Buffer.Language(); l != nil {
				language = l.Name
			}
			muted(c, language)
		}
	})
}

// Track all tabs so switching documents never cancels a pending save.
const autosaveDelay = 750 * time.Millisecond

func (a *app) autosave(c *ui.Context) {
	if a.git.mutating {
		return
	}
	for _, d := range a.docs {
		if !d.editable() || d.path == "" {
			continue
		}
		if !d.state.Buffer.Modified() {
			d.autoDue = time.Time{}
			d.autoFailed = false
			d.saveError = ""
			continue
		}
		version := d.state.Buffer.Version()
		if version != d.autoVersion || (d.autoDue.IsZero() && !d.autoFailed) {
			d.autoVersion = version
			d.autoDue = c.Now().Add(autosaveDelay)
			d.autoFailed = false
		}
		if d.autoFailed {
			continue
		}
		if wait := d.autoDue.Sub(c.Now()); wait > 0 {
			c.After(wait)
			continue
		}
		a.writeAuto(d)
	}
}

func (a *app) writeAuto(d *document) bool {
	if err := d.save(d.path); err != nil {
		d.autoFailed = true
		d.saveError = "自动保存失败 · " + d.name + "：" + err.Error()
		return false
	}
	a.rememberEditedFile(d.path)
	return true
}
