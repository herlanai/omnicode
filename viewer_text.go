package main

import (
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
	"github.com/HyperparameterSI/golet/backend/ui/markdown"
)

func (a *app) textContent(c *ui.Context, d *document) {
	d.state.ReadOnly = a.git.mutating
	d.state.Theme = a.codeTheme
	d.state.SoftWrap = a.wrap
	d.state.Font = ui.Font{Family: a.prefs.EditorFont, Size: float32(a.prefs.EditorSize)}
	d.vim.configure(d.state, a.prefs.VimMode, &a.vimRegister)
	a.bindReferenceMenu(d)
	e := editor.View(c.Key(d.description()+"-editor"), d.state).Fill().AutoFocus()
	if e.Focused() && c.Shortcut(ui.Cmd, ui.KeyI) {
		a.attachAISelection(d)
	}
	if a.focusEditor {
		e.Focus()
		a.focusEditor = false
	}
}
func (a *app) markdownContent(c *ui.Context, d *document) {
	edit := func() { a.textContent(c, d) }
	if d.preview {
		ui.SplitPercent(c, &a.split, edit, func() {
			ui.Scroll(c.Key(d.path + "-markdown-preview")).Fill().Padding(24).Children(func() { markdown.View(c, d.state.Buffer.String(), nil).FillWidth() })
		}).Grow(1).FillWidth()
	} else {
		ui.Box(c).Grow(1).FillWidth().Children(edit)
	}
}
