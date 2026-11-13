package main

import (
	"path/filepath"
	"runtime"

	golet "github.com/HyperparameterSI/golet/backend"
)

// The database supplies recents in descending last-opened order.
func recentDockItems(projects []recentProject, open func(string)) []*golet.MenuItem {
	projects = projects[:min(len(projects), 10)]
	names := make(map[string]int)
	for _, project := range projects {
		names[filepath.Base(project.Path)]++
	}
	items := make([]*golet.MenuItem, 0, len(projects))
	for _, project := range projects {
		path := project.Path
		name := filepath.Base(path)
		item := &golet.MenuItem{Label: "📁 " + name, ToolTip: path, Click: func(*golet.MenuItem, *golet.Window) { open(path) }}
		if names[name] > 1 {
			item.Subtitle = compactPath(filepath.Dir(path))
		}
		items = append(items, item)
	}
	return items
}

func (a *app) updateRecentDock() {
	if runtime.GOOS != "darwin" || a.win == nil {
		return
	}
	items := recentDockItems(a.recent, func(path string) {
		a.win.Show()
		a.win.Focus()
		a.chooseFolder(path)
		a.win.Invalidate()
	})
	if len(items) > 0 {
		items = append(items, golet.Separator())
	}
	for _, folder := range a.folders {
		folder := folder
		label := filepath.Base(folder.path)
		active := folder.path == a.root
		if d := a.current(); active && d != nil {
			label += " — " + filepath.Base(d.path)
		}
		item := &golet.MenuItem{Label: label, ToolTip: folder.path, Type: golet.MenuItemCheckbox, Checked: active}
		item.Click = func(*golet.MenuItem, *golet.Window) {
			a.win.Show()
			a.win.Focus()
			a.activateFolder(folder)
			a.updateRecentDock()
			a.win.Invalidate()
		}
		items = append(items, item)
	}
	if len(a.folders) > 0 {
		items = append(items, golet.Separator())
	}
	items = append(items, &golet.MenuItem{Label: "新建窗口", Click: func(*golet.MenuItem, *golet.Window) { a.report(launchProjectWindow("")) }})
	golet.App.Dock.SetMenu(golet.NewMenu(items))
}
