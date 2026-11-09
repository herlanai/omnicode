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
	if len(items) == 0 {
		golet.App.Dock.SetMenu(nil)
		return
	}
	golet.App.Dock.SetMenu(golet.NewMenu(items))
}
