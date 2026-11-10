package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/HyperparameterSI/golet/backend/plugins/terminal"
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/forma"
)

const maxTerminalSessions = 16

type shellPane struct {
	name                   string
	id, directory          string
	session                *terminal.Terminal
	searching, focusSearch bool
	query                  string
}

func (s *shellPane) cwd() string {
	if dir := s.session.Dir(); dir != "" {
		return dir
	}
	return s.directory
}

type shellTab struct {
	selected *shellPane
	panes    []*shellPane
	vertical bool
	ratio    float32
}

type terminalPanel struct {
	renameOpen                bool
	renaming                  *shellPane
	newName                   string
	tabs                      []*shellTab
	active                    *shellTab
	focus                     *shellPane
	serial                    int
	visible, right, maximized bool
	showReferences            bool
	bottomSize, rightSize     float32
	err                       string
}

func (p *terminalPanel) count() int {
	n := 0
	for _, tab := range p.tabs {
		n += len(tab.panes)
	}
	return n
}

func (a *app) launchShell(dir string) (*terminal.Terminal, error) {
	return terminal.New(terminal.Options{Dir: dir, Font: terminal.Font{Family: a.prefs.EditorFont, Size: float32(a.prefs.TerminalSize)}})
}

// An empty direction opens a new tab. A split starts beside its source in the same tab.
func (a *app) openTerminal(dir, direction string) {
	p := &a.terminal
	p.visible = true
	p.showReferences = false
	if p.count() >= maxTerminalSessions {
		p.err = "最多可打开 16 个终端"
		return
	}
	if direction != "" && (p.active == nil || len(p.active.panes) != 1) {
		return
	}
	if dir == "" {
		var err error
		dir, err = os.UserHomeDir()
		if err != nil {
			p.err = err.Error()
			return
		}
	}
	session, err := a.launchShell(dir)
	if err != nil {
		p.err = err.Error()
		return
	}
	p.serial++
	s := &shellPane{id: fmt.Sprint(p.serial), directory: dir, session: session}
	if direction == "" {
		p.active = &shellTab{panes: []*shellPane{s}, ratio: 50}
		p.tabs = append(p.tabs, p.active)
	} else {
		p.active.panes = append(p.active.panes, s)
		p.active.vertical = direction == "vertical"
		p.active.ratio = 50
	}
	p.focus = s
	p.err = ""
}

func (a *app) toggleTerminal() {
	p := &a.terminal
	p.visible = a.settingsOpen || a.schedules.open || a.database.open || !p.visible || (a.sharedBottomPanel() && p.showReferences)
	p.showReferences = false
	a.leavePage()
	if p.visible {
		if p.active == nil {
			a.openTerminal(a.root, "")
		} else {
			p.focus = p.active.panes[0]
		}
	}
}

func (p *terminalPanel) closePane(tab *shellTab, s *shellPane) {
	if len(tab.panes) == 1 && tab.panes[0] == s {
		p.closeTab(tab)
		return
	}
	for i, pane := range tab.panes {
		if pane == s {
			_ = s.session.Close()
			tab.panes = append(tab.panes[:i], tab.panes[i+1:]...)
			if p.active == tab {
				tab.selected = tab.panes[0]
				p.focus = tab.selected
			}
			return
		}
	}
}

func (p *terminalPanel) closeTab(tab *shellTab) {
	for i, t := range p.tabs {
		if t != tab {
			continue
		}
		for _, pane := range tab.panes {
			_ = pane.session.Close()
		}
		p.tabs = append(p.tabs[:i:i], p.tabs[i+1:]...)
		if p.active == tab {
			if len(p.tabs) == 0 {
				p.active = nil
				p.focus = nil
				p.visible = false
				p.maximized = false
				p.serial = 0
			} else {
				p.active = p.tabs[min(i, len(p.tabs)-1)]
				p.focus = p.active.panes[0]
			}
		}
		return
	}
}

func (a *app) workspaceContent(c *ui.Context) {
	p := &a.terminal
	if !p.visible {
		a.content(c)
		return
	}
	panel := func() {
		if a.sharedBottomPanel() && p.showReferences {
			a.referencePanel(c, a.intelligence.references)
		} else {
			a.terminalContent(c)
		}
	}
	if p.maximized {
		panel()
		return
	}
	editor := func() { a.content(c) }
	if p.right {
		if p.rightSize == 0 {
			p.rightSize = 65
		}
		ui.SplitPercent(c, &p.rightSize, editor, panel).Fill()
	} else {
		if p.bottomSize == 0 {
			p.bottomSize = 68
		}
		ui.SplitVerticalPercent(c, &p.bottomSize, editor, panel).Fill()
	}
}

func (a *app) sharedBottomPanel() bool {
	return a.terminal.visible && !a.terminal.right && a.intelligence.references != nil
}

func (a *app) bottomPanelTabs(c *ui.Context) {
	p := &a.terminal
	for _, label := range []string{"引用", "终端"} {
		refs := label == "引用"
		tab := action(c, "bottom-tab-"+label, label, func() {
			p.showReferences = refs
			if !refs && p.active != nil {
				p.focus = p.active.selected
				if p.focus == nil {
					p.focus = p.active.panes[0]
				}
			}
		}).Label("切换到" + label).Height(28)
		if refs == p.showReferences {
			tab.Background(a.theme.Surface).BorderWidth(0, 0, 2, 0).BorderColor(a.theme.Accent)
		}
	}
}

func (a *app) terminalContent(c *ui.Context) {
	p := &a.terminal
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(34).Shrink(0).FillWidth().Padding(0, 8).Gap(2).AlignItems(ui.Center).Children(func() {
			if a.sharedBottomPanel() {
				a.bottomPanelTabs(c)
			} else {
				ui.Text(c, "终端").Bold().Padding(0, 4)
			}
			ui.ScrollHorizontal(c.Key("shell-tabs")).Grow(1).MinWidth(0).FillHeight().Children(func() {
				ui.Row(c).FillHeight().Gap(2).AlignItems(ui.Center).Children(func() {
					for _, tab := range p.tabs {
						t := tab
						first := t.panes[0]
						label := first.label()
						if len(t.panes) > 1 {
							label += " · " + t.panes[1].label()
						}
						bg := ui.Transparent
						if t == p.active {
							bg = a.theme.Sidebar
						}
						ui.Row(c.Key("shell-tab-" + first.id)).Height(30).Radius(6).Background(bg).Children(func() {
							action(c, "select", label, func() { p.active = t; p.focus = first }).Height(30).Padding(0, 20, 0, 9).Tooltip(first.cwd())
							ui.Button(c.Key("close"), "").Absolute().Top(0).Right(0).Size(18, 18).Padding(3).Background(ui.Transparent).Border(0, ui.Transparent).Radius(4).Label("关闭标签 " + label).Tooltip("关闭标签 " + label).OnClick(func() { p.closeTab(t) }).Children(func() { icon(c, "close", 12) })
						})
					}
				})
			})
			if tab := p.active; tab != nil {
				target := tab.selected
				if p.focus != nil {
					target = p.focus
				}
				if target == nil {
					target = tab.panes[0]
				}
				a.shellActions(c.Key("tools-"+target.id), tab, target)
			}
			iconButton(c, "terminal-new", "plus", "新建终端", func() { a.openTerminal(a.root, "") }).Size(26, 26).Padding(4).Disabled(p.count() >= maxTerminalSessions)
			position, symbol := "移到右侧", "panel-right"
			if p.right {
				position, symbol = "移到底部", "panel-bottom"
			}
			iconButton(c, "terminal-position", symbol, position, func() { p.right = !p.right; p.maximized = false }).Size(26, 26).Padding(4)
			zoom := "最大化终端"
			if p.maximized {
				zoom = "还原终端面板"
			}
			iconButton(c, "terminal-zoom", "maximize", zoom, func() { p.maximized = !p.maximized }).Size(26, 26).Padding(4)
			iconButton(c, "terminal-hide", "close", "隐藏终端", func() { p.visible = false }).Size(26, 26).Padding(4)
		})
		if p.err != "" {
			ui.Text(c, p.err).TextColor(c.Theme().Danger).Wrap()
		}
		tab := p.active
		if tab == nil {
			return
		}
		left := tab.panes[0]
		if len(tab.panes) == 1 {
			a.shellContent(c.Key(left.id), tab, left)
			return
		}
		right := tab.panes[1]
		first := func() { a.shellContent(c.Key(left.id), tab, left) }
		second := func() { a.shellContent(c.Key(right.id), tab, right) }
		if tab.vertical {
			ui.SplitVerticalPercent(c, &tab.ratio, first, second).Grow(1).FillWidth()
		} else {
			ui.SplitPercent(c, &tab.ratio, first, second).Grow(1).FillWidth()
		}
	})
}

func (a *app) shellActions(c *ui.Context, tab *shellTab, s *shellPane) {
	p := &a.terminal
	iconButton(c, "find", "search", "搜索终端输出", func() {
		s.searching = !s.searching
		s.focusSearch = s.searching
		if !s.searching {
			s.session.EndSearch()
			p.focus = s
		}
	}).Size(26, 26).Padding(4)
	iconButton(c, "actions", "more", "终端操作", nil).Size(26, 26).Padding(4).Menu(func(m *ui.Menu) {
		canSplit := len(tab.panes) == 1 && p.count() < maxTerminalSessions
		if m.Item("左右分屏").Disabled(!canSplit).Chosen() {
			a.openTerminal(s.cwd(), "horizontal")
		}
		if m.Item("上下分屏").Disabled(!canSplit).Chosen() {
			a.openTerminal(s.cwd(), "vertical")
		}
		m.Separator()
		if m.Item("重命名…").Chosen() {
			p.renameOpen = true
			p.renaming = s
			p.newName = s.label()
		}
		if m.Item("清屏").Chosen() {
			if !s.session.ClearScreen() {
				p.err = "请先退出终端内的全屏程序"
			} else {
				p.err = ""
			}
		}
		if m.Item("用 AI 解释当前屏幕…").Chosen() {
			context := []rune(strings.TrimSpace(s.session.Text()))
			if len(context) > 4000 {
				context = context[len(context)-4000:]
			}
			a.showAI()
			a.ai.draft = strings.TrimSpace(a.ai.draft + "\n\n请解释以下终端输出，给出下一步建议。\n\n" + string(context))
			a.ai.dirty = true
		}
		if m.Item("重启此会话").Chosen() {
			dir := s.cwd()
			session, err := a.launchShell(dir)
			if err != nil {
				p.err = err.Error()
			} else {
				_ = s.session.Close()
				s.session = session
				s.directory = dir
				s.searching = false
				s.query = ""
				p.focus = s
				p.err = ""
			}
		}
		if m.Item("关闭此会话").Chosen() {
			p.closePane(tab, s)
		}
	})
}

func (a *app) shellContent(c *ui.Context, tab *shellTab, s *shellPane) {
	p := &a.terminal
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		select {
		case <-s.session.Done():
			muted(c, fmt.Sprintf("已退出 %d", s.session.ExitCode())).FontSize(11).Padding(0, 8)
		default:
		}
		if s.searching {
			ui.Row(c).FillWidth().Gap(3).AlignItems(ui.Center).Children(func() {
				field := forma.Input(c, &s.query, forma.InputOptions{Placeholder: "搜索屏幕与滚动历史…"}).Grow(1).MinWidth(0)
				if field.Focused() {
					tab.selected = s
				}
				if s.focusSearch {
					tab.selected = s
					field.Focus()
					s.focusSearch = false
					p.focus = nil
				}
				status := s.session.SearchStatus()
				if status.Text != s.query {
					s.session.Search(s.query)
					status = s.session.SearchStatus()
				}
				if s.query != "" {
					muted(c, fmt.Sprintf("%d/%d", status.Selected, status.Total)).FontSize(11)
				}
				iconButton(c, "older", "arrow-up", "较早的匹配", s.session.SearchNext).Size(24, 24).Disabled(status.Total == 0)
				iconButton(c, "newer", "arrow-down", "较新的匹配", s.session.SearchPrevious).Size(24, 24).Disabled(status.Total == 0)
				iconButton(c, "end-search", "close", "关闭搜索", func() { s.searching = false; s.session.EndSearch(); p.focus = s }).Size(24, 24)
			})
		}
		e := terminal.View(c.Key("shell-grid-"+s.id), s.session).Grow(1).FillWidth()
		if e.Focused() {
			tab.selected = s
		}
		if p.focus == s {
			tab.selected = s
			e.Focus()
			p.focus = nil
		}
	})
}

func (p *terminalPanel) closeAll() {
	for _, tab := range p.tabs {
		for _, s := range tab.panes {
			_ = s.session.Close()
		}
	}
	*p = terminalPanel{}
}

func (s *shellPane) label() string {
	if s.name != "" {
		return s.name
	}
	return "cmd" + s.id
}

func (a *app) terminalNameDialog(c *ui.Context) {
	p := &a.terminal
	if !p.renameOpen || p.renaming == nil {
		return
	}
	forma.Dialog(c.Key("terminal-name"), &p.renameOpen, "重命名终端", func() {
		forma.Input(c, &p.newName).FillWidth().AutoFocus()
		name := strings.TrimSpace(p.newName)
		valid := name != "" && len([]rune(name)) <= 40
		forma.Button(c, "确定").Disabled(!valid).OnClick(func() {
			p.renaming.name = name
			p.renameOpen = false
		})
	})
	if !p.renameOpen {
		p.renaming = nil
	}
}
