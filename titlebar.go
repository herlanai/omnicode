package main

import (
	"path/filepath"

	"github.com/HyperparameterSI/golet/backend/ui"
)

// The system supplies control insets, including changes in full-screen mode.
// Only blank titlebar space drags; buttons retain their normal input handling.
func (a *app) titlebar(c *ui.Context) {
	bar := c.TitleBar()
	ui.Row(c.Key("window-titlebar")).FillWidth().Height(max(float32(40), bar.Height)).Shrink(0).
		Padding(0, max(float32(10), bar.Right+8), 0, max(float32(10), bar.Left+8)).Gap(4).
		AlignItems(ui.Center).Background(a.theme.Sidebar).DragWindow().Children(func() {
		iconButton(c, "sidebar", "panel", "切换侧栏", func() { a.sidebar = !a.sidebar }).Size(30, 28).Padding(6)
		iconButton(c, "open-folder", "folder", "打开文件夹", func() { a.pick(true) }).Size(30, 28).Padding(6)
		ui.Box(c).Grow(1).MinWidth(12)
		name := "打开文件"
		if a.root != "" {
			name = filepath.Base(a.root)
		}
		ui.Button(c.Key("quick"), "").MaxWidth(420).Grow(1).MinWidth(0).Height(28).Padding(0, 10).Gap(8).
			Border(0, ui.Transparent).Background(ui.Transparent).Radius(7).Label("快速打开").Tooltip("快速打开  ⌘ P").
			OnClick(a.toggleQuick).Children(func() {
			ui.Row(c).Fill().Gap(8).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				icon(c, "search", 14)
				ui.Text(c, name).FontSize(12).MaxLines(1).MinWidth(0)
			})
		})
		ui.Box(c).Grow(1).MinWidth(12)
		iconButton(c, "title-terminal", "code", "切换终端  Ctrl+`", a.toggleTerminal).Size(30, 28).Padding(6)
		iconButton(c, "open-ai", "sparkles", "打开 AI 对话", a.toggleAI).Size(30, 28).Padding(6)
	})
}

// Child windows share the main window's compact, draggable chrome.
func childTitlebar(c *ui.Context, title string, actions func()) {
	bar := c.TitleBar()
	ui.Row(c).FillWidth().Height(max(float32(40), bar.Height)).Shrink(0).
		Padding(0, max(float32(12), bar.Right+8), 0, max(float32(12), bar.Left+8)).Gap(12).
		AlignItems(ui.Center).Background(c.Theme().Sidebar).DragWindow().Children(func() {
		ui.Text(c, title).FontSize(12).MaxLines(1).MinWidth(0).Grow(1)
		if actions != nil {
			actions()
		}
	})
}
