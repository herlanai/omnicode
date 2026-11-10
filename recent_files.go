package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/HyperparameterSI/golet/backend/ui"
)

func (a *app) loadRecentFiles() {
	if a.projects == nil {
		return
	}
	rows, err := a.projects.db.Query(context.Background(), "SELECT value FROM settings WHERE key='recent-edited-files'")
	if err != nil {
		a.report(err)
		return
	}
	if len(rows.Values) > 0 {
		a.report(json.Unmarshal([]byte(rows.Values[0][0].(string)), &a.recentFiles))
	}
}
func (a *app) persistRecentFiles() {
	if a.projects == nil {
		return
	}
	data, err := json.Marshal(a.recentFiles)
	if err == nil {
		_, err = a.projects.db.Execute(context.Background(), "INSERT INTO settings(key,value) VALUES('recent-edited-files',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(data))
	}
	a.report(err)
}
func (a *app) rememberEditedFile(path string) {
	if path == "" {
		return
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return
	}
	path, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return
	}
	if len(a.recentFiles) > 0 && a.recentFiles[0] == path {
		return
	}
	a.recentFiles = slices.DeleteFunc(a.recentFiles, func(p string) bool { return p == path })
	a.recentFiles = append([]string{path}, a.recentFiles...)
	a.recentFiles = a.recentFiles[:min(100, len(a.recentFiles))]
	a.persistRecentFiles()
}
func (a *app) visibleRecentFiles() []string {
	var paths []string
	for _, path := range a.recentFiles {
		if a.folderForPath(path) == nil {
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		paths = append(paths, path)
		if len(paths) == 10 {
			break
		}
	}
	return paths
}
func (a *app) sidebarRecentFiles(c *ui.Context) {
	paths := a.visibleRecentFiles()
	ui.Column(c.Key("recent-edited-files")).FillWidth().Gap(2).Padding(8, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
		expanded := !a.recentFilesCollapsed
		header := ui.ButtonBase(c.Key("toggle")).FillWidth().Height(28).Padding(0, 8).Gap(7).AlignItems(ui.Center).Justify(ui.Start).Radius(6).Label("最近编辑").Expanded(expanded).OnClick(func() { a.recentFilesCollapsed = !a.recentFilesCollapsed })
		if header.Hovered() {
			header.Background(c.Theme().SurfaceHover)
		}
		header.Children(func() {
			symbol := "chevron-right"
			if expanded {
				symbol = "chevron-down"
			}
			icon(c, symbol, 12)
			muted(c, "最近编辑").Bold()
		})
		if !expanded {
			return
		}
		if len(paths) == 0 {
			muted(c, "暂无编辑记录").FontSize(12).Padding(4, 8)
			return
		}
		for _, path := range paths {
			label := filepath.Base(path)
			relative := path
			if folder := a.folderForPath(path); folder != nil {
				relative, _ = filepath.Rel(folder.path, path)
				if len(a.folders) > 1 {
					relative = filepath.Base(folder.path) + "/" + relative
				}
			}
			row := ui.ButtonBase(c.Key(path)).FillWidth().Height(28).Padding(0, 8).Gap(7).AlignItems(ui.Center).Justify(ui.Start).Radius(6).Label("打开最近编辑文件 " + path).Tooltip(path).OnClick(func() { a.open(path) })
			if d := a.current(); d != nil && d.path == path {
				row.Background(c.Theme().SurfaceHover)
			} else if row.Hovered() {
				row.Background(c.Theme().SurfaceHover)
			}
			row.Children(func() {
				icon(c, "file", 14)
				ui.Text(c, label).FontSize(12).MaxLines(1).MinWidth(0)
				if dir := filepath.Dir(relative); dir != "." {
					muted(c, dir).FontSize(10).MaxLines(1).MinWidth(0)
				}
			})
		}
	})
}
