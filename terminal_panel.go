package main

import (
	"fmt"

	"github.com/HyperparameterSI/golet/backend/plugins/terminal"
	"github.com/HyperparameterSI/golet/backend/ui"
)

type terminalPanel struct {
	session        *terminal.Terminal
	visible, right bool
	size           float32
	dir, err       string
	focus          bool
}

func (a *app) toggleTerminal() {
	p := &a.terminal
	p.visible = !p.visible
	if p.visible && p.session == nil {
		a.startTerminal()
	}
	p.focus = p.visible
}
func (a *app) startTerminal() {
	p := &a.terminal
	if p.session != nil {
		_ = p.session.Close()
		p.session = nil
	}
	p.dir = a.root
	p.err = ""
	p.focus = true
	term, err := terminal.New(terminal.Options{Dir: p.dir, Font: terminal.Font{Family: a.prefs.EditorFont, Size: float32(a.prefs.TerminalSize)}})
	if err != nil {
		p.err = err.Error()
		return
	}
	p.session = term
}
func (a *app) workspaceContent(c *ui.Context) {
	p := &a.terminal
	if !p.visible {
		a.content(c)
		return
	}
	if p.size == 0 {
		p.size = 68
	}
	if p.right {
		ui.SplitPercent(c, &p.size, func() { a.content(c) }, func() { a.terminalContent(c) }).Fill()
	} else {
		ui.SplitVerticalPercent(c, &p.size, func() { a.content(c) }, func() { a.terminalContent(c) }).Fill()
	}
}
func (a *app) terminalContent(c *ui.Context) {
	p := &a.terminal
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(34).Shrink(0).FillWidth().Padding(0, 10).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "终端").Bold()
			ui.Box(c).Grow(1)
			position := "移到右侧"
			positionIcon := "panel-right"
			if p.right {
				position = "移到底部"
				positionIcon = "panel-bottom"
			}
			ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
				iconButton(c, "terminal-position", positionIcon, position, func() { p.right = !p.right }).Size(28, 28).Padding(5)
				iconButton(c, "terminal-restart", "refresh", "重启终端会话", a.startTerminal).Size(28, 28).Padding(5)
				iconButton(c, "terminal-hide", "close", "隐藏终端", func() { p.visible = false }).Size(28, 28).Padding(5)
			})
		})
		if p.err != "" {
			ui.Text(c, p.err).TextColor(c.Theme().Danger)
			return
		}
		if p.session != nil {
			select {
			case <-p.session.Done():
				muted(c, fmt.Sprintf("已退出 · %d", p.session.ExitCode()))
			default:
			}
			e := terminal.View(c.Key("integrated-terminal"), p.session).Grow(1).FillWidth()
			if p.focus {
				e.Focus()
				p.focus = false
			}
		}
	})
}
