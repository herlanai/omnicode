package main

import (
	"github.com/HyperparameterSI/golet/backend/ui"
	"github.com/HyperparameterSI/golet/backend/ui/editor"
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
