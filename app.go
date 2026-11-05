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
	gitReview                  *gitReviewUI
	git                        gitPanel
	intelligence               intelligence
	contentSearch              contentSearch
	webPreviews                map[string]*webPreview
	ai                         aiPanel
	treeSelection              string
	prefs                      preferences
	settingsWindow             *golet.Window
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
	entries                    []*entry
	expanded                   map[string]bool
	docs                       []*document
	active                     int
	sidebar                    bool
	sidebarWidth               float32
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
	a := &app{expanded: map[string]bool{}, active: -1, sidebar: true, sidebarWidth: 246, split: 52, prefs: defaultPreferences()}
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
	if a.git.mutating {
		a.notice = "请等待 Git 操作完成"
		return
	}
	if a.ai.running || a.hasPendingAI() {
		a.report(fmt.Errorf("请先结束 AI 任务并处理待批准修改"))
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		a.report(err)
		return
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		a.report(err)
		return
	}
	list, err := children(abs)
	if err != nil {
		a.report(err)
		return
	}
	a.checkpointAI(true)
	a.closeIntelligence()
	a.root = abs
	a.treeSelection = abs
	a.entries = list
	a.expanded = map[string]bool{}
	a.index = nil
	a.rememberProject()
	a.restoreLatestAI()
}
func (a *app) refresh() {
	if a.root != "" {
		list, e := children(a.root)
		a.report(e)
		if e == nil {
			a.entries = list
			a.index = nil
		}
	}
}
func (a *app) open(path string) {
	a.git.visible = false
	a.focusEditor = true
	d, err := loadDocument(path)
	if err != nil {
		a.report(err)
		return
	}
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
			a.setRoot(p)
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
	if v := a.docs[i].slides; v != nil {
		if v.pane != nil {
			v.pane.close()
			v.pane = nil
		}
		if v.window != nil {
			v.window.Destroy()
		}
	}
	a.docs = append(a.docs[:i], a.docs[i+1:]...)
	if i < a.active {
		a.active--
	}
	if a.active >= len(a.docs) {
		a.active = len(a.docs) - 1
	}
}
func (a *app) canQuit() bool {
	if a.git.mutating {
		a.notice = "请先等待或停止 Git 操作"
		return false
	}
	if a.ai.running {
		a.ai.visible = true
		a.ai.err = "请先停止 AI 任务，再关闭窗口"
		return false
	}
	if a.hasPendingAI() {
		a.ai.visible = true
		a.ai.err = "请先批准或拒绝 AI 修改，再关闭窗口"
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
	a.quick = !a.quick
	a.query = ""
	if a.contentSearch.cancel != nil {
		a.contentSearch.cancel()
	}
	a.contentSearch = contentSearch{}
	if a.quick && a.index == nil && a.root != "" {
		a.index = fileIndex(a.root)
	}
}
func (a *app) format() {
	if a.git.mutating {
		a.notice = "请等待 Git 操作完成"
		return
	}
	if d := a.current(); d != nil {
		if filepath.Ext(d.name) != ".go" {
			a.notice = "格式化当前支持 Go 文件"
			return
		}
		text, err := format.Source([]byte(d.state.Buffer.String()))
		if err != nil {
			a.report(err)
			return
		}
		d.state.Buffer.Set(string(text))
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
	a.tickGit(c)
	a.updateIntelligence(c)
	a.drainAI()
	a.checkpointAI(false)
	if a.ai.dirty {
		c.After(500 * time.Millisecond)
	}
	if a.ai.running {
		c.After(50 * time.Millisecond)
	}
	for _, d := range a.docs {
		if d.slides != nil && d.slides.pane != nil && (d != a.current() || d.slides.textMode || a.quick || a.filePrompt.open) {
			d.slides.pane.hide()
		}
	}
	c.SetTheme(a.theme)
	if c.Shortcut(ui.Ctrl, ui.KeyBackquote) {
		a.toggleTerminal()
	}
	a.autosave(c)
	if c.Shortcut(ui.Cmd, ui.KeyP) {
		a.toggleQuick()
	}
	if c.Shortcut(ui.Cmd, ui.KeyB) {
		a.sidebar = !a.sidebar
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
		}
	}
	if a.quick && c.Shortcut(0, ui.KeyEscape) {
		a.quick = false
	}
	ui.Column(c).Fill().Background(a.theme.Background).Children(func() {
		a.titlebar(c)
		ui.Row(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			a.rail(c)
			windowWidth, _ := c.Size()
			if a.sidebar && (!a.ai.visible || windowWidth >= 1100) {
				// Keep the resize handle, but visually join the file pane to the editor.
				splitTheme := *a.theme
				splitTheme.Border = ui.Transparent
				splitTheme.Accent = ui.Transparent
				c.SetTheme(&splitTheme)
				ui.Split(c, &a.sidebarWidth, func() { c.SetTheme(a.theme); a.files(c) }, func() { c.SetTheme(a.theme); a.aiWorkspace(c) }).Grow(1).FillHeight()
				c.SetTheme(a.theme)
			} else {
				a.aiWorkspace(c)
			}
		})
		a.status(c)
	})
	if a.filePrompt.open {
		a.fileDialog(c)
	}
	a.gitDialog(c)
}
func (a *app) rail(c *ui.Context) {
	ui.Column(c).Width(42).FillHeight().Padding(8, 4).Gap(12).Background(a.theme.Surface).AlignItems(ui.Center).Children(func() {
		explorerBG, gitBG := a.theme.Background, ui.Transparent
		if a.git.visible {
			explorerBG, gitBG = ui.Transparent, a.theme.Background
		}
		iconButton(c, "explorer", "files", "文件浏览器", func() { a.sidebar = true; a.quick = false; a.git.visible = false }).Background(explorerBG)
		iconButton(c, "search", "search", "快速打开", a.toggleQuick)
		iconButton(c, "source-control", "git", "源代码管理", a.toggleGit).Background(gitBG)
		ui.Box(c).Grow(1)
		iconButton(c, "refresh", "refresh", "刷新文件列表", a.refresh)
		iconButton(c, "terminal", "code", "切换终端  Ctrl+`", a.toggleTerminal)
		iconButton(c, "settings", "settings", "设置  ⌘ ,", a.showSettings)
	})
}
func (a *app) files(c *ui.Context) {
	if a.git.visible {
		a.gitSidebar(c)
		return
	}
	ui.Box(c).Fill().Padding(0, 0, 0, 6).Background(a.theme.Surface).Children(func() {
		ui.Column(c).Fill().Radius(12, 0, 0, 12).Background(a.theme.Sidebar).Padding(12, 10).Gap(12).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "文件").Bold().Grow(1)
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
				muted(c, "打开文件夹，开始你的项目")
				action(c, "choose-folder", "打开文件夹", func() { a.pick(true) })
				return
			}
			ui.Row(c).Gap(7).AlignItems(ui.Center).Cursor(ui.CursorPointer).OnClick(func() { a.treeSelection = a.root }).ContextMenu(a.fileMenu(c, a.root, true, true)).Children(func() { icon(c, "folder", 14); ui.Text(c, filepath.Base(a.root)).Bold().FontSize(12) })
			ui.Scroll(c.Key("file-tree")).Grow(1).FillWidth().ContextMenu(a.fileMenu(c, a.root, true, true)).Children(func() { ui.Column(c).FillWidth().Gap(2).Children(func() { a.tree(c, a.entries, 0) }) })
		})
	})
}
func (a *app) tree(c *ui.Context, entries []*entry, depth int) {
	for _, e := range entries {
		node := e
		bg := ui.Transparent
		if a.treeSelection == node.path {
			bg = a.theme.Surface
		}
		row := ui.Row(c.Key(node.path)).FillWidth().Height(30).Padding(0, 8, 0, float32(8+depth*14)).Gap(7).AlignItems(ui.Center).Radius(6).Background(bg).Cursor(ui.CursorPointer).Label(node.name).OnClick(func() {
			a.treeSelection = node.path
			if node.dir {
				a.expanded[node.path] = !a.expanded[node.path]
			} else {
				a.open(node.path)
			}
		})
		row.ContextMenu(a.fileMenu(c, node.path, node.dir, false))
		row.Children(func() {
			if node.dir {
				chev := "›"
				if a.expanded[node.path] {
					chev = "⌄"
				}
				muted(c, chev)
				icon(c, "folder", 14)
			} else {
				icon(c, "file", 14)
			}
			ui.Text(c, node.name).FontSize(12)
		})
		if node.dir && a.expanded[node.path] {
			if !node.loaded {
				list, err := children(node.path)
				a.report(err)
				node.children = list
				node.loaded = true
			}
			a.tree(c, node.children, depth+1)
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
		ui.ScrollHorizontal(c.Key("tabs")).Height(43).Shrink(0).FillWidth().BorderWidth(0, 0, 1, 0).BorderColor(a.theme.Border).Children(func() {
			ui.Row(c).Height(42).Children(func() {
				for i, d := range a.docs {
					idx, doc := i, d
					bg := a.theme.Sidebar
					if i == a.active {
						bg = a.theme.Background
					}
					ui.Row(c.Key(fmt.Sprintf("tab-%p", doc))).Height(42).Padding(0, 10).Gap(6).Background(bg).BorderWidth(0, 1, 0, 0).BorderColor(a.theme.Border).Children(func() {
						title := doc.name
						if doc.state.Buffer.Modified() {
							title += " •"
						}
						action(c, "activate", title, func() { a.active = idx; a.focusEditor = true }).MinWidth(90)
						iconButton(c, "close", "close", "关闭 "+doc.name, func() { a.closeDoc(idx) }).Size(24, 24).Padding(5)
					})
				}
			})
		})
		d := a.current()
		if d == nil {
			return
		}
		ui.Row(c).Height(38).Shrink(0).Padding(0, 18).Gap(10).AlignItems(ui.Center).Children(func() {
			label := d.description()
			if a.root != "" && d.path != "" {
				if rel, e := filepath.Rel(a.root, d.path); e == nil {
					label = rel
				}
			}
			muted(c, label).Grow(1)
			if d.markdown() {
				label := "预览"
				if d.preview {
					label = "关闭预览"
				}
				iconButton(c, "preview", "panel-right", label, func() { d.preview = !d.preview })
			}
		})
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
			ui.Box(c).Grow(1).FillWidth().Children(func() { a.textContent(c, d) })
		}
	})
}
func (a *app) welcome(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(18).Children(func() {
		icon(c, "code", 42).TextColor(ui.Hex("#b5b7b3"))
		ui.Text(c, "让想法，落在代码里。").FontSize(28)
		muted(c, "一个安静、专注的原生编辑空间。")
		ui.Row(c).Gap(10).Margin(12, 0, 0, 0).Children(func() {
			forma.Button(c, "打开文件夹", forma.ButtonOptions{Variant: forma.Default}).OnClick(func() { a.pick(true) })
			forma.Button(c, "打开文件", forma.ButtonOptions{Variant: forma.Outline}).OnClick(func() { a.pick(false) })
		})
		if len(a.recent) > 0 {
			a.recentView(c)
			return
		}
		ui.Column(c).Width(275).Gap(13).Margin(30, 0, 0, 0).Children(func() {
			for _, item := range [][2]string{{"新建文件", "⌘ N"}, {"快速打开", "⌘ P"}, {"切换侧栏", "⌘ B"}} {
				ui.Row(c).FillWidth().Children(func() { muted(c, item[0]).Grow(1); muted(c, item[1]) })
			}
		})
	})
}
func (a *app) quickView(c *ui.Context) {
	ui.Column(c).Fill().Padding(28).Gap(16).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "快速打开").FontSize(20).Bold().Grow(1)
			action(c, "dismiss-quick", "关闭  Esc", func() { a.quick = false })
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
			muted(c, "请先打开一个文件夹。")
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
		muted(c, "Enter 打开首个结果 · 点击内容定位 · Esc 返回编辑器")
	})
}
func (a *app) status(c *ui.Context) {
	ui.Row(c).Height(28).Shrink(0).Padding(0, 14).Gap(16).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(a.theme.Border).Children(func() {
		text := "自动保存已开启"
		if d := a.current(); d != nil {
			if !d.editable() {
				text = "只读预览"
				if d.sheet != nil && !d.sheet.readOnly {
					text = "表格 · Enter 应用单元格修改"
				}
			} else if d.path == "" {
				text = "新文件 · ⌘ S 选择保存位置"
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
	return true
}
